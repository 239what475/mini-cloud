package coordination

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"mini-cloud/internal/controlplane/model"
	"mini-cloud/internal/controlplane/store"
)

var errPlaneNotReady = errors.New("plane is not ready")

type ServiceOperations struct {
	logger            *slog.Logger
	store             *store.Store
	serviceBaseDomain string
	southboundToken   string
	planeSyncer       *PlaneSyncer
}

func NewServiceOperations(logger *slog.Logger, stores *store.Store, southboundToken string, serviceBaseDomain string, planeSyncer *PlaneSyncer) *ServiceOperations {
	if logger == nil {
		logger = slog.Default()
	}
	return &ServiceOperations{
		logger:            logger,
		store:             stores,
		serviceBaseDomain: strings.Trim(strings.ToLower(strings.TrimSpace(serviceBaseDomain)), "."),
		southboundToken:   strings.TrimSpace(southboundToken),
		planeSyncer:       planeSyncer,
	}
}

func (c *ServiceOperations) Create(ctx context.Context, input store.CreateServiceInput) (model.Service, error) {
	input.Host = serviceHost(input.Name, c.serviceBaseDomain)
	created, err := c.store.CreateService(ctx, input)
	if err != nil {
		return model.Service{}, err
	}
	if err := c.applyRemoteService(ctx, created); err != nil {
		c.logger.Warn("initial service apply failed; service remains pending", "service_id", created.Metadata.ID, "error", err)
	}
	return c.store.GetService(ctx, created.Metadata.ID)
}

func serviceHost(name string, baseDomain string) string {
	name = strings.Trim(strings.ToLower(strings.TrimSpace(name)), ".")
	baseDomain = strings.Trim(strings.ToLower(strings.TrimSpace(baseDomain)), ".")
	if name == "" || baseDomain == "" {
		return ""
	}
	return name + "." + baseDomain
}

func (c *ServiceOperations) List(ctx context.Context) ([]model.Service, error) {
	c.syncRegisteredPlanes(ctx, "listing services")
	c.advanceAllPendingServices(ctx)
	c.syncRegisteredPlanes(ctx, "listing services after pending work")
	return c.store.ListServices(ctx)
}

func (c *ServiceOperations) Get(ctx context.Context, serviceID string) (model.Service, error) {
	c.advanceServicePendingWork(ctx, "getting service", serviceID)
	return c.store.GetService(ctx, serviceID)
}

func (c *ServiceOperations) Update(ctx context.Context, serviceID string, input store.UpdateServiceInput) (model.Service, error) {
	c.syncServicePlaneForRequest(ctx, serviceID)
	updated, err := c.store.UpdateService(ctx, serviceID, input)
	if err != nil {
		return model.Service{}, err
	}
	if err := c.applyRemoteService(ctx, updated); err != nil {
		c.logger.Warn("service update apply failed; service remains pending", "service_id", updated.Metadata.ID, "error", err)
	}
	return c.store.GetService(ctx, updated.Metadata.ID)
}

func (c *ServiceOperations) Delete(ctx context.Context, serviceID string) (model.Service, error) {
	c.syncServicePlaneForRequest(ctx, serviceID)
	deleting, err := c.store.MarkServiceDeletionRequested(ctx, serviceID)
	if err != nil {
		return model.Service{}, err
	}
	if err := c.dispatchDeletingService(ctx, deleting); err != nil {
		c.logger.Warn("service delete dispatch failed; service remains deleting", "service_id", deleting.Metadata.ID, "error", err)
	}
	current, err := c.store.GetService(ctx, deleting.Metadata.ID)
	if err != nil {
		return model.Service{}, err
	}
	return current, nil
}

func (c *ServiceOperations) advanceServicePendingWork(ctx context.Context, action string, serviceID string) {
	c.syncServicePlaneForRequest(ctx, serviceID)
	if err := c.advancePendingServiceApply(ctx, serviceID); err != nil {
		c.logger.Warn("advance pending service apply failed", "service_id", serviceID, "action", action, "error", err)
	}
	if err := c.advancePendingServiceDelete(ctx, serviceID); err != nil {
		c.logger.Warn("advance pending service delete failed", "service_id", serviceID, "action", action, "error", err)
	}
	c.syncServicePlaneForRequest(ctx, serviceID)
}

func (c *ServiceOperations) syncRegisteredPlanes(ctx context.Context, action string) {
	if c.planeSyncer == nil {
		return
	}
	if err := c.planeSyncer.SyncRegisteredPlanes(ctx, RequestPlaneSyncTimeout); err != nil {
		c.logger.Warn("sync registered planes failed", "action", action, "error", err)
	}
}

func (c *ServiceOperations) advanceAllPendingServices(ctx context.Context) {
	if err := c.advanceAllPendingServiceApplies(ctx); err != nil {
		c.logger.Warn("advance pending service applies failed", "error", err)
	}
	if err := c.advanceAllPendingServiceDeletes(ctx); err != nil {
		c.logger.Warn("advance pending service deletes failed", "error", err)
	}
}

func (c *ServiceOperations) advanceAllPendingServiceApplies(ctx context.Context) error {
	items, err := c.store.ListPendingApplyServices(ctx)
	if err != nil {
		return err
	}
	var joinedErr error
	for _, item := range items {
		if err := c.applyRemoteService(ctx, item); err != nil {
			c.logger.Warn("advance pending service apply failed", "service_id", item.Metadata.ID, "error", err)
			joinedErr = errors.Join(joinedErr, err)
		}
	}
	return joinedErr
}

func (c *ServiceOperations) advanceAllPendingServiceDeletes(ctx context.Context) error {
	items, err := c.store.ListDeletingServices(ctx)
	if err != nil {
		return err
	}
	var joinedErr error
	for _, item := range items {
		if err := c.dispatchDeletingService(ctx, item); err != nil {
			c.logger.Warn("advance pending service delete failed", "service_id", item.Metadata.ID, "error", err)
			joinedErr = errors.Join(joinedErr, err)
		}
	}
	return joinedErr
}

func (c *ServiceOperations) advancePendingServiceApply(ctx context.Context, serviceID string) error {
	item, ok, err := c.store.GetPendingApplyService(ctx, serviceID)
	if err != nil || !ok {
		return err
	}
	return c.applyRemoteService(ctx, item)
}

func (c *ServiceOperations) advancePendingServiceDelete(ctx context.Context, serviceID string) error {
	item, ok, err := c.store.GetDeletingService(ctx, serviceID)
	if err != nil || !ok {
		return err
	}
	return c.dispatchDeletingService(ctx, item)
}

func (c *ServiceOperations) dispatchDeletingService(ctx context.Context, serviceItem model.Service) error {
	planeID, err := c.targetPlaneID(ctx, serviceItem)
	if err != nil {
		statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, deletingFailureServiceStatus(serviceItem, err), nil)
		return errors.Join(err, statusErr)
	}
	if err := c.deleteRemoteService(ctx, planeID, serviceItem.Metadata.ID, serviceItem.Metadata.Generation); err != nil && !errors.Is(err, errPlaneObjectNotFound) {
		statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, deletingFailureServiceStatus(serviceItem, err), nil)
		return errors.Join(err, statusErr)
	}
	run := deletingRunStatus()
	if err := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, serviceDeletingStatus(serviceItem.Metadata.Generation), &run); err != nil &&
		!errors.Is(err, store.ErrServiceNotFound) &&
		!errors.Is(err, store.ErrServiceGenerationConflict) {
		return err
	}
	return nil
}

func (c *ServiceOperations) syncServicePlaneForRequest(ctx context.Context, serviceID string) {
	if c.planeSyncer == nil {
		return
	}
	serviceItem, err := c.store.GetService(ctx, serviceID)
	if err != nil {
		if errors.Is(err, store.ErrServiceNotFound) {
			return
		}
		c.logger.Warn("load service before plane sync failed", "service_id", serviceID, "error", err)
		return
	}
	planeID := strings.TrimSpace(serviceItem.Spec.PlaneID)
	if planeID == "" {
		return
	}
	if err := c.planeSyncer.SyncPlane(ctx, planeID); err != nil {
		c.logger.Warn("sync service plane failed", "service_id", serviceID, "plane_id", planeID, "error", err)
	}
}

func (c *ServiceOperations) applyRemoteService(ctx context.Context, service model.Service) error {
	planeID, err := c.targetPlaneID(ctx, service)
	if err != nil {
		statusErr := c.updateServiceStatus(ctx, service.Metadata.ID, service.Metadata.Generation, failedServiceStatus(service, err), nil)
		return errors.Join(err, statusErr)
	}
	if err := c.dispatchRemoteService(ctx, planeID, service); err != nil {
		statusErr := c.updateServiceStatus(ctx, service.Metadata.ID, service.Metadata.Generation, failedServiceStatus(service, err), nil)
		return errors.Join(err, statusErr)
	}
	run := dispatchedRunStatus()
	if err := c.updateServiceStatus(ctx, service.Metadata.ID, service.Metadata.Generation, serviceAcceptedStatus(service.Metadata.Generation), &run); err != nil {
		return err
	}
	return nil
}
