// Package nodepool 承载执行 node 池的后台收敛控制用例。
package nodepool

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	infranodeprovider "mini-cloud/internal/cloudplane/infra/nodeprovider"
	"mini-cloud/internal/cloudplane/infra/store"
	cloudmodel "mini-cloud/internal/cloudplane/model"
)

type Service struct {
	// logger 记录 node 池收敛过程中的单节点失败。
	logger *slog.Logger
	// store 提供 node 和 execution intent 的持久化访问。
	store *store.Store
	// driver 调用云厂商 API 创建和删除 node 对应的云实例。
	driver infranodeprovider.Driver
	config cloudplaneconfig.Config
}

func NewService(logger *slog.Logger, stores *store.Store, driver infranodeprovider.Driver, cfg cloudplaneconfig.Config) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{logger: logger, store: stores, driver: driver, config: cfg}
}

// ReconcileOnce 将 node 池收敛到当前 execution 需求。
func (s *Service) ReconcileOnce(ctx context.Context) error {
	if s == nil || s.store == nil || s.driver == nil {
		return nil
	}
	if err := s.reconcileTerminatingNodes(ctx); err != nil {
		return err
	}
	scaledOut, err := s.reconcileCapacityShortage(ctx)
	if err != nil {
		return err
	}
	if scaledOut {
		return nil
	}
	return s.reconcileIdleNodes(ctx)
}

func (s *Service) reconcileCapacityShortage(ctx context.Context) (bool, error) {
	candidate, err := s.store.GetNodeScaleOutCandidate(ctx, s.config.Plane.Name+cloudplaneconfig.NodeNameSuffix, s.config.RuntimeProvisioning.InstanceType)
	if err != nil {
		return false, err
	}
	if candidate == nil || candidate.HasCapacity || candidate.HasProvisioning {
		return false, nil
	}

	node, err := s.store.CreateProvisioningNode(ctx, cloudmodel.ProvisioningInput{
		Provider:     s.config.Infrastructure.Provider,
		Region:       s.config.Infrastructure.RegionID,
		Name:         candidate.NodeName,
		InstanceType: candidate.InstanceType,
		StatusReason: "pending execution requires more runtime capacity",
	})
	if err != nil {
		return false, err
	}

	result, err := s.driver.Create(ctx, infranodeprovider.CreateRequest{
		Name:        candidate.NodeName,
		ClientToken: candidate.ClientToken,
		CPUMilli:    candidate.CPUMilli,
		MemoryMi:    candidate.MemoryMi,
	})
	if err != nil {
		_, cleanupErr := s.store.MarkNodeDeleted(
			ctx,
			node.ID,
			"provider node creation failed: "+err.Error(),
			time.Now().UTC(),
		)
		return false, errors.Join(err, cleanupErr)
	}
	_, err = s.store.BindProvisionedNode(
		ctx,
		node.ID,
		result.InstanceID,
		result.InstanceName,
		result.InstanceType,
		"provider accepted node creation",
		time.Now().UTC(),
	)
	return err == nil, err
}

func (s *Service) reconcileTerminatingNodes(ctx context.Context) error {
	items, err := s.store.ListNodesByStatuses(ctx, cloudmodel.StatusDraining)
	if err != nil {
		return err
	}
	return s.reconcileDeletableNodes(ctx, items)
}

func (s *Service) reconcileIdleNodes(ctx context.Context) error {
	unsettled, err := s.store.HasUnsettledExecutionIntents(ctx)
	if err != nil {
		return err
	}
	if unsettled {
		return nil
	}

	items, err := s.store.ListNodesByStatuses(ctx, cloudmodel.StatusReady)
	if err != nil {
		return err
	}
	return s.reconcileDeletableNodes(ctx, items)
}

func (s *Service) reconcileDeletableNodes(ctx context.Context, items []cloudmodel.Node) error {
	var joinedErr error
	for _, item := range items {
		if strings.TrimSpace(item.InstanceID) == "" {
			continue
		}
		if err := s.reconcileNodeDeletion(ctx, item); err != nil {
			joinedErr = errors.Join(joinedErr, err)
			s.logger.Warn("node deletion failed",
				"node_id", item.ID,
				"instance_id", item.InstanceID,
				"error", err,
			)
		}
	}
	return joinedErr
}

// reconcileNodeDeletion 推进单个 idle node 的删除流程。
func (s *Service) reconcileNodeDeletion(ctx context.Context, item cloudmodel.Node) error {
	// 已经 deleted 的记录不会被扫描到；如果并发状态变化导致输入不再可删除，直接跳过。
	if item.Status != cloudmodel.StatusReady && item.Status != cloudmodel.StatusDraining {
		return nil
	}
	if strings.TrimSpace(item.InstanceID) == "" {
		return nil
	}

	draining, changed, err := s.store.MarkNodeDraining(
		ctx,
		item.ID,
		"node has no active execution and cloud-plane is deleting the provider instance",
		time.Now().UTC(),
	)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}

	if err := s.driver.Delete(ctx, infranodeprovider.DeleteRequest{InstanceID: draining.InstanceID}); err != nil {
		return err
	}

	_, err = s.store.MarkNodeDeleted(
		ctx,
		draining.ID,
		"provider instance was deleted or was already absent",
		time.Now().UTC(),
	)
	return err
}
