package control

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

const (
	provisioningNodeTimeout = 10 * time.Minute
	providerRequestTimeout  = 30 * time.Second
)

type nodeReconciler struct {
	logger *slog.Logger
	store  *store.Store
	driver infranodeprovider.Driver
	config cloudplaneconfig.Config
}

func newNodeReconciler(logger *slog.Logger, stores *store.Store, driver infranodeprovider.Driver, cfg cloudplaneconfig.Config) *nodeReconciler {
	if logger == nil {
		logger = slog.Default()
	}
	return &nodeReconciler{logger: logger, store: stores, driver: driver, config: cfg}
}

func (s *nodeReconciler) reconcileOnce(ctx context.Context) error {
	if err := s.reconcileTerminatingNodes(ctx); err != nil {
		return err
	}
	if err := s.reconcileStaleProvisioningNodes(ctx); err != nil {
		return err
	}
	createdNode, err := s.reconcilePendingExecutionCapacity(ctx)
	if err != nil {
		return err
	}
	if createdNode {
		return nil
	}
	return s.reconcileIdleNodes(ctx)
}

func (s *nodeReconciler) reconcilePendingExecutionCapacity(ctx context.Context) (bool, error) {
	candidate, err := s.store.GetNodeProvisioningCandidate(ctx, s.config.Plane.Name+cloudplaneconfig.NodeNameSuffix, s.config.NodeProvisioning.InstanceType)
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
		StatusReason: "pending execution requires more node capacity",
	})
	if err != nil {
		return false, err
	}

	providerCtx, cancel := context.WithTimeout(ctx, providerRequestTimeout)
	result, err := s.driver.Create(providerCtx, infranodeprovider.CreateRequest{
		Name:        candidate.NodeName,
		ClientToken: candidate.ClientToken,
		CPUMilli:    candidate.CPUMilli,
		MemoryMi:    candidate.MemoryMi,
	})
	cancel()
	if err != nil {
		reason := "provider node creation failed: " + err.Error()
		_, cleanupErr := s.store.MarkNodeDeleted(
			ctx,
			node.ID,
			reason,
			time.Now().UTC(),
		)
		failErr := s.store.MarkExecutionPlanFailed(ctx, candidate.PlanID, reason)
		return false, errors.Join(err, cleanupErr, failErr)
	}
	if _, err := s.store.BindProvisionedNode(
		ctx,
		node.ID,
		result.InstanceID,
		result.InstanceName,
		result.InstanceType,
		"provider accepted node creation",
		time.Now().UTC(),
	); err != nil {
		reason := "provider node was created but cloud-plane failed to bind it: " + err.Error()
		deleteErr := s.deleteProviderNode(ctx, result.InstanceID)
		_, cleanupErr := s.store.MarkNodeDeleted(ctx, node.ID, reason, time.Now().UTC())
		failErr := s.store.MarkExecutionPlanFailed(ctx, candidate.PlanID, reason)
		return false, errors.Join(err, deleteErr, cleanupErr, failErr)
	}
	return true, nil
}

func (s *nodeReconciler) reconcileStaleProvisioningNodes(ctx context.Context) error {
	items, err := s.store.ListElasticNodesByStatuses(ctx, cloudmodel.StatusProvisioning)
	if err != nil {
		return err
	}
	cutoff := time.Now().UTC().Add(-provisioningNodeTimeout)
	nodeNamePrefix := s.config.Plane.Name + cloudplaneconfig.NodeNameSuffix
	var joinedErr error
	for _, item := range items {
		if item.UpdatedAt.After(cutoff) {
			continue
		}
		if err := s.reconcileStaleProvisioningNode(ctx, nodeNamePrefix, item); err != nil {
			joinedErr = errors.Join(joinedErr, err)
			s.logger.Warn("stale provisioning node cleanup failed",
				"node_id", item.ID,
				"node_name", item.Name,
				"instance_id", item.InstanceID,
				"error", err,
			)
		}
	}
	return joinedErr
}

func (s *nodeReconciler) reconcileStaleProvisioningNode(ctx context.Context, nodeNamePrefix string, item cloudmodel.Node) error {
	if item.Status != cloudmodel.StatusProvisioning || !item.Elastic {
		return nil
	}
	reason := "node did not register before provisioning timeout"
	if strings.TrimSpace(item.InstanceID) != "" {
		if err := s.deleteProviderNode(ctx, item.InstanceID); err != nil {
			return err
		}
	}
	_, nodeErr := s.store.MarkNodeDeleted(ctx, item.ID, reason, time.Now().UTC())
	planErr := s.store.MarkPendingExecutionFailedForProvisioningNode(ctx, nodeNamePrefix, item.Name, reason)
	return errors.Join(nodeErr, planErr)
}

func (s *nodeReconciler) reconcileIdleNodes(ctx context.Context) error {
	unsettled, err := s.store.HasUnsettledExecutionIntents(ctx)
	if err != nil {
		return err
	}
	if unsettled {
		return nil
	}

	items, err := s.store.ListElasticNodesByStatuses(ctx, cloudmodel.StatusReady)
	if err != nil {
		return err
	}
	return s.reconcileDeletableNodes(ctx, items)
}

func (s *nodeReconciler) reconcileTerminatingNodes(ctx context.Context) error {
	items, err := s.store.ListElasticNodesByStatuses(ctx, cloudmodel.StatusDraining)
	if err != nil {
		return err
	}
	return s.reconcileDeletableNodes(ctx, items)
}

func (s *nodeReconciler) reconcileDeletableNodes(ctx context.Context, items []cloudmodel.Node) error {
	var joinedErr error
	for _, item := range items {
		if strings.TrimSpace(item.InstanceID) == "" {
			continue
		}
		if !item.Elastic {
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

func (s *nodeReconciler) reconcileNodeDeletion(ctx context.Context, item cloudmodel.Node) error {
	if item.Status != cloudmodel.StatusReady && item.Status != cloudmodel.StatusDraining {
		return nil
	}
	if !item.Elastic || strings.TrimSpace(item.InstanceID) == "" {
		return nil
	}

	draining, deletable, err := s.store.PrepareNodeDeletion(
		ctx,
		item.ID,
		"node has no active execution and cloud-plane is deleting the provider instance",
		time.Now().UTC(),
	)
	if err != nil {
		return err
	}
	if !deletable {
		return nil
	}

	if err := s.deleteProviderNode(ctx, draining.InstanceID); err != nil {
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

func (s *nodeReconciler) deleteProviderNode(ctx context.Context, instanceID string) error {
	providerCtx, cancel := context.WithTimeout(ctx, providerRequestTimeout)
	defer cancel()
	return s.driver.Delete(providerCtx, infranodeprovider.DeleteRequest{InstanceID: instanceID})
}
