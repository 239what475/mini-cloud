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
	dispatcher        *serviceDispatcher
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
		dispatcher:        newServiceDispatcher(logger, stores, southboundToken),
		planeSyncer:       planeSyncer,
	}
}

func (c *ServiceOperations) Create(ctx context.Context, input store.CreateServiceInput) (model.Service, error) {
	input.Host = serviceHost(input.Name, c.serviceBaseDomain)
	created, err := c.store.CreateService(ctx, input)
	if err != nil {
		return model.Service{}, err
	}
	if _, err := c.applyRemoteService(ctx, created); err != nil {
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
	return c.store.ListServices(ctx)
}

func (c *ServiceOperations) Get(ctx context.Context, serviceID string) (model.Service, error) {
	c.advanceServicePendingWork(ctx, "getting service", serviceID)
	return c.store.GetService(ctx, serviceID)
}

func (c *ServiceOperations) Update(ctx context.Context, serviceID string, input store.UpdateServiceInput) (model.Service, error) {
	if err := c.syncServicePlane(ctx, serviceID); err != nil {
		return model.Service{}, err
	}
	updated, err := c.store.UpdateService(ctx, serviceID, input)
	if err != nil {
		return model.Service{}, err
	}
	if _, err := c.applyRemoteService(ctx, updated); err != nil {
		c.logger.Warn("service update apply failed; service remains pending", "service_id", updated.Metadata.ID, "error", err)
	}
	return c.store.GetService(ctx, updated.Metadata.ID)
}

func (c *ServiceOperations) Delete(ctx context.Context, serviceID string) (model.Service, error) {
	if err := c.syncServicePlane(ctx, serviceID); err != nil {
		return model.Service{}, err
	}
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
	if err := c.syncServicePlane(ctx, serviceID); err != nil {
		c.logger.Warn("sync service plane before advancing pending work failed", "service_id", serviceID, "action", action, "error", err)
	}
	if err := c.advancePendingServiceApply(ctx, serviceID); err != nil {
		c.logger.Warn("advance pending service apply failed", "service_id", serviceID, "action", action, "error", err)
	}
	if err := c.advancePendingServiceDelete(ctx, serviceID); err != nil {
		c.logger.Warn("advance pending service delete failed", "service_id", serviceID, "action", action, "error", err)
	}
	if err := c.syncServicePlane(ctx, serviceID); err != nil {
		c.logger.Warn("sync service plane after advancing pending work failed", "service_id", serviceID, "action", action, "error", err)
	}
}

func (c *ServiceOperations) advancePendingServiceApply(ctx context.Context, serviceID string) error {
	item, ok, err := c.store.GetPendingApplyService(ctx, serviceID)
	if err != nil || !ok {
		return err
	}
	_, err = c.applyRemoteService(ctx, item)
	return err
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
	if err := c.dispatcher.DeleteService(ctx, planeID, serviceItem.Metadata.ID, serviceItem.Metadata.Generation); err != nil && !errors.Is(err, errPlaneObjectNotFound) {
		statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, deletingFailureServiceStatus(serviceItem, err), nil)
		return errors.Join(err, statusErr)
	}
	run := deletingRunStatus(serviceItem, "")
	if err := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, serviceDeletingStatus(serviceItem.Metadata.Generation), &run); err != nil &&
		!errors.Is(err, store.ErrServiceNotFound) &&
		!errors.Is(err, store.ErrServiceGenerationConflict) {
		return err
	}
	return nil
}

func (c *ServiceOperations) syncServicePlane(ctx context.Context, serviceID string) error {
	if c.planeSyncer == nil {
		return nil
	}
	serviceItem, err := c.store.GetService(ctx, serviceID)
	if err != nil {
		if errors.Is(err, store.ErrServiceNotFound) {
			return nil
		}
		return err
	}
	planeID := strings.TrimSpace(serviceItem.Spec.PlaneID)
	if planeID == "" {
		return nil
	}
	if err := c.planeSyncer.SyncPlane(ctx, planeID); err != nil {
		c.logger.Warn("sync service plane failed", "service_id", serviceID, "plane_id", planeID, "error", err)
	}
	return nil
}

func (c *ServiceOperations) applyRemoteService(ctx context.Context, service model.Service) (model.Service, error) {
	planeID, err := c.targetPlaneID(ctx, service)
	if err != nil {
		statusErr := c.updateServiceStatus(ctx, service.Metadata.ID, service.Metadata.Generation, failedServiceStatus(service, err), nil)
		return model.Service{}, errors.Join(err, statusErr)
	}
	applied, err := c.dispatcher.ApplyService(ctx, planeID, service)
	if err != nil {
		statusErr := c.updateServiceStatus(ctx, service.Metadata.ID, service.Metadata.Generation, failedServiceStatus(service, err), nil)
		return model.Service{}, errors.Join(err, statusErr)
	}
	run := dispatchedRunStatus(service, "")
	if err := c.updateServiceStatus(ctx, service.Metadata.ID, service.Metadata.Generation, serviceAcceptedStatus(service.Metadata.Generation), &run); err != nil {
		return model.Service{}, err
	}
	current, err := c.store.GetService(ctx, service.Metadata.ID)
	if err != nil {
		return model.Service{}, err
	}
	if strings.TrimSpace(applied.Metadata.ID) != "" {
		current.Metadata = applied.Metadata
		current.Spec = applied.Spec
	}
	return current, nil
}
