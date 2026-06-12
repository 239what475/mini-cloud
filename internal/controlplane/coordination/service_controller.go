package coordination

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"mini-cloud/internal/controlplane/model"
	"mini-cloud/internal/controlplane/store"
)

var errPlaneNotReady = errors.New("plane is not ready")

type ServiceController struct {
	logger            *slog.Logger
	store             *store.Store
	serviceBaseDomain string
	dispatcher        *serviceDispatcher
	planeSyncer       *PlaneSyncer
}

func NewServiceController(logger *slog.Logger, stores *store.Store, southboundToken string, serviceBaseDomain string) *ServiceController {
	if logger == nil {
		logger = slog.Default()
	}
	return &ServiceController{
		logger:            logger,
		store:             stores,
		serviceBaseDomain: strings.Trim(strings.ToLower(strings.TrimSpace(serviceBaseDomain)), "."),
		dispatcher:        newServiceDispatcher(logger, stores, southboundToken),
	}
}

func (c *ServiceController) SetPlaneSyncer(syncer *PlaneSyncer) {
	c.planeSyncer = syncer
}

func (c *ServiceController) Create(ctx context.Context, input store.CreateServiceInput) (model.Service, error) {
	input.Host = serviceHost(input.Name, c.serviceBaseDomain)
	created, err := c.store.CreateService(ctx, input)
	if err != nil {
		return model.Service{}, err
	}
	return c.applyRemoteService(ctx, created)
}

func serviceHost(name string, baseDomain string) string {
	name = strings.Trim(strings.ToLower(strings.TrimSpace(name)), ".")
	baseDomain = strings.Trim(strings.ToLower(strings.TrimSpace(baseDomain)), ".")
	if name == "" || baseDomain == "" {
		return ""
	}
	return name + "." + baseDomain
}

func (c *ServiceController) List(ctx context.Context) ([]model.Service, error) {
	if c.planeSyncer != nil {
		if err := c.planeSyncer.SyncRegisteredPlanes(ctx, 10*time.Second); err != nil {
			c.logger.Warn("sync planes before listing services failed", "error", err)
		}
	}
	return c.store.ListServices(ctx)
}

func (c *ServiceController) Get(ctx context.Context, serviceID string) (model.Service, error) {
	if err := c.syncServicePlane(ctx, serviceID); err != nil {
		return model.Service{}, err
	}
	return c.store.GetService(ctx, serviceID)
}

func (c *ServiceController) Update(ctx context.Context, serviceID string, input store.UpdateServiceInput) (model.Service, error) {
	if err := c.syncServicePlane(ctx, serviceID); err != nil {
		return model.Service{}, err
	}
	updated, err := c.store.UpdateService(ctx, serviceID, input)
	if err != nil {
		return model.Service{}, err
	}
	return c.applyRemoteService(ctx, updated)
}

func (c *ServiceController) Delete(ctx context.Context, serviceID string) (model.Service, error) {
	if err := c.syncServicePlane(ctx, serviceID); err != nil {
		return model.Service{}, err
	}
	deleting, err := c.store.MarkServiceDeletionRequested(ctx, serviceID)
	if err != nil {
		return model.Service{}, err
	}
	planeID, err := c.targetPlaneID(ctx, deleting)
	if err != nil {
		statusErr := c.updateServiceStatus(ctx, deleting.Metadata.ID, deleting.Metadata.Generation, deletingFailureServiceStatus(deleting.Metadata.Generation, err), nil)
		return model.Service{}, errors.Join(err, statusErr)
	}
	if err := c.dispatcher.DeleteService(ctx, planeID, deleting.Metadata.ID, deleting.Metadata.Generation); err != nil && !errors.Is(err, errPlaneObjectNotFound) {
		statusErr := c.updateServiceStatus(ctx, deleting.Metadata.ID, deleting.Metadata.Generation, deletingFailureServiceStatus(deleting.Metadata.Generation, err), nil)
		return model.Service{}, errors.Join(err, statusErr)
	}
	run := deletingRunStatus(deleting, "")
	if err := c.updateServiceStatus(ctx, deleting.Metadata.ID, deleting.Metadata.Generation, serviceDeletingStatus(deleting.Metadata.Generation), &run); err != nil {
		return model.Service{}, err
	}
	current, err := c.store.GetService(ctx, deleting.Metadata.ID)
	if err != nil {
		return model.Service{}, err
	}
	return current, nil
}

func (c *ServiceController) syncServicePlane(ctx context.Context, serviceID string) error {
	if c.planeSyncer == nil {
		return nil
	}
	serviceItem, err := c.store.GetService(ctx, serviceID)
	if err != nil {
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

func (c *ServiceController) applyRemoteService(ctx context.Context, service model.Service) (model.Service, error) {
	planeID, err := c.targetPlaneID(ctx, service)
	if err != nil {
		statusErr := c.updateServiceStatus(ctx, service.Metadata.ID, service.Metadata.Generation, failedServiceStatus(service.Metadata.Generation, err), nil)
		return model.Service{}, errors.Join(err, statusErr)
	}
	applied, err := c.dispatcher.ApplyService(ctx, planeID, service)
	if err != nil {
		statusErr := c.updateServiceStatus(ctx, service.Metadata.ID, service.Metadata.Generation, failedServiceStatus(service.Metadata.Generation, err), nil)
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
