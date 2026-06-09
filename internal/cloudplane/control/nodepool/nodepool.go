// Package nodepool 承载 runtime node 池的后台收敛控制用例。
package nodepool

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	infraruntimepool "mini-cloud/internal/cloudplane/infra/runtimepool"
	"mini-cloud/internal/cloudplane/infra/store"
	cloudmodel "mini-cloud/internal/cloudplane/model"
)

type Service struct {
	// logger 记录 runtime node 池收敛过程中的单节点失败。
	logger *slog.Logger
	// store 提供 runtime node 和 execution intent 的持久化访问。
	store *store.Store
	// driver 调用云厂商 API 创建和删除 runtime node 对应的云实例。
	driver infraruntimepool.RuntimeDriver
	config cloudplaneconfig.Config
}

func NewService(logger *slog.Logger, stores *store.Store, driver infraruntimepool.RuntimeDriver, cfg cloudplaneconfig.Config) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{logger: logger, store: stores, driver: driver, config: cfg}
}

// ReconcileOnce 将 runtime node 池收敛到当前 execution 需求。
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
	candidate, err := s.store.GetRuntimeNodeScaleOutCandidate(ctx, s.config.Plane.Name+cloudplaneconfig.RuntimeNodeNameSuffix, s.config.RuntimeProvisioning.InstanceType)
	if err != nil {
		return false, err
	}
	if candidate == nil || candidate.HasCapacity || candidate.HasProvisioning {
		return false, nil
	}

	intent, err := s.store.CreateRuntimeNodeIntent(ctx, infraruntimepool.CreateIntentInput{
		Provider:      s.config.Infrastructure.Provider,
		Region:        s.config.Infrastructure.RegionID,
		InstanceName:  candidate.InstanceName,
		InstanceType:  candidate.InstanceType,
		StatusReason:  "pending execution requires more runtime capacity",
		ProvisionedAt: time.Now().UTC(),
	})
	if err != nil {
		return false, err
	}

	result, err := s.driver.Create(ctx, infraruntimepool.CreateRequest{
		Name:        candidate.InstanceName,
		ClientToken: candidate.ClientToken,
		CPUMilli:    candidate.CPUMilli,
		MemoryMi:    candidate.MemoryMi,
	})
	if err != nil {
		_, cleanupErr := s.store.MarkRuntimeNodeDeleted(
			ctx,
			intent.ID,
			"provider runtime node creation failed: "+err.Error(),
			time.Now().UTC(),
		)
		return false, errors.Join(err, cleanupErr)
	}
	_, err = s.store.BindRuntimeNodeProvisioned(
		ctx,
		intent.ID,
		result.InstanceID,
		result.InstanceName,
		result.InstanceType,
		"provider accepted runtime node creation",
		time.Now().UTC(),
	)
	return err == nil, err
}

func (s *Service) reconcileTerminatingNodes(ctx context.Context) error {
	items, err := s.store.ListRuntimeNodesByStatuses(ctx, infraruntimepool.StatusTerminating)
	if err != nil {
		return err
	}
	return s.reconcileDeletableNodes(ctx, items)
}

func (s *Service) reconcileIdleNodes(ctx context.Context) error {
	unsettled, err := s.store.HasExecutionIntentsWithStatuses(
		ctx,
		cloudmodel.StatusPending,
		cloudmodel.StatusDeploying,
	)
	if err != nil {
		return err
	}
	if unsettled {
		return nil
	}

	items, err := s.store.ListRuntimeNodesByStatuses(ctx, infraruntimepool.StatusReady)
	if err != nil {
		return err
	}
	return s.reconcileDeletableNodes(ctx, items)
}

func (s *Service) reconcileDeletableNodes(ctx context.Context, items []infraruntimepool.Record) error {
	var joinedErr error
	for _, item := range items {
		if strings.TrimSpace(item.InstanceID) == "" || strings.TrimSpace(item.NodeID) == "" {
			continue
		}
		activeCount, err := s.store.CountActiveExecutionsByNode(ctx, item.NodeID)
		if err != nil {
			return err
		}
		if activeCount > 0 {
			continue
		}
		if err := s.reconcileRuntimeNodeDeletion(ctx, item); err != nil {
			joinedErr = errors.Join(joinedErr, err)
			s.logger.Warn("runtime node deletion failed",
				"runtime_node_id", item.ID,
				"instance_id", item.InstanceID,
				"node_id", item.NodeID,
				"error", err,
			)
		}
	}
	return joinedErr
}

// reconcileRuntimeNodeDeletion 推进单个 runtime node 的删除流程。
// 参数说明：ctx 控制本次操作；item 是本轮扫描得到的 runtime node 快照。
func (s *Service) reconcileRuntimeNodeDeletion(ctx context.Context, item infraruntimepool.Record) error {
	// 已经 deleted 的记录不会被扫描到；如果并发状态变化导致输入不再可删除，直接跳过。
	if item.Status != infraruntimepool.StatusReady && item.Status != infraruntimepool.StatusTerminating {
		return nil
	}
	if strings.TrimSpace(item.InstanceID) == "" || strings.TrimSpace(item.NodeID) == "" {
		return nil
	}

	terminating, changed, err := s.store.MarkRuntimeNodeTerminating(
		ctx,
		item.ID,
		"runtime node has no active execution and cloud-plane is deleting the provider instance",
		time.Now().UTC(),
	)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}

	if err := s.driver.Delete(ctx, infraruntimepool.DeleteRequest{InstanceID: terminating.InstanceID}); err != nil {
		return err
	}

	// provider 删除请求成功返回或返回 not found 后，本地状态收敛为 deleted，backing node 保留为 offline inventory。
	_, err = s.store.MarkRuntimeNodeDeleted(
		ctx,
		terminating.ID,
		"provider instance was deleted or was already absent",
		time.Now().UTC(),
	)
	return err
}
