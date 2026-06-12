package coordination

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"mini-cloud/internal/controlplane/model"
	"mini-cloud/internal/controlplane/store"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"
)

var (
	errPlaneNotReady    = errors.New("plane is not ready")
	errPlaneIDRequired  = errors.New("planeID is required")
	errServiceIDMissing = errors.New("serviceID is required")
)

const (
	dispatchServiceTimeout = 15 * time.Second
	dispatchDeleteTimeout  = 15 * time.Second
)

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
	if err := c.dispatchServiceSpec(ctx, created); err != nil {
		c.logger.Warn("initial service dispatch failed; service remains pending", "service_id", created.Metadata.ID, "error", err)
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
	if err := c.dispatchServiceSpec(ctx, updated); err != nil {
		c.logger.Warn("service update dispatch failed; service remains pending", "service_id", updated.Metadata.ID, "error", err)
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
		if errors.Is(err, store.ErrServiceNotFound) {
			return deleting, nil
		}
		return model.Service{}, err
	}
	return current, nil
}

func (c *ServiceOperations) advanceServicePendingWork(ctx context.Context, action string, serviceID string) {
	c.syncServicePlaneForRequest(ctx, serviceID)
	if err := c.advancePendingServiceDispatch(ctx, serviceID); err != nil {
		c.logger.Warn("advance pending service dispatch failed", "service_id", serviceID, "action", action, "error", err)
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
	if err := c.advanceAllPendingServiceDispatches(ctx); err != nil {
		c.logger.Warn("advance pending service dispatches failed", "error", err)
	}
	if err := c.advanceAllPendingServiceDeletes(ctx); err != nil {
		c.logger.Warn("advance pending service deletes failed", "error", err)
	}
}

func (c *ServiceOperations) advanceAllPendingServiceDispatches(ctx context.Context) error {
	items, err := c.store.ListPendingDispatchServices(ctx)
	if err != nil {
		return err
	}
	var joinedErr error
	for _, item := range items {
		if err := c.dispatchServiceSpec(ctx, item); err != nil {
			c.logger.Warn("advance pending service dispatch failed", "service_id", item.Metadata.ID, "error", err)
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

func (c *ServiceOperations) advancePendingServiceDispatch(ctx context.Context, serviceID string) error {
	item, ok, err := c.store.GetPendingDispatchService(ctx, serviceID)
	if err != nil || !ok {
		return err
	}
	return c.dispatchServiceSpec(ctx, item)
}

func (c *ServiceOperations) advancePendingServiceDelete(ctx context.Context, serviceID string) error {
	item, ok, err := c.store.GetDeletingService(ctx, serviceID)
	if err != nil || !ok {
		return err
	}
	return c.dispatchDeletingService(ctx, item)
}

func (c *ServiceOperations) dispatchDeletingService(ctx context.Context, serviceItem model.Service) error {
	planeID, err := c.boundPlaneID(ctx, serviceItem)
	if err != nil {
		statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, deletingFailureServiceStatus(serviceItem, err), nil)
		return errors.Join(err, statusErr)
	}
	if err := c.deleteRemoteService(ctx, planeID, serviceItem.Metadata.ID, serviceItem.Metadata.Generation); err != nil {
		if errors.Is(err, errPlaneObjectNotFound) {
			if c.planeSyncer != nil {
				if err := c.planeSyncer.deleteServiceDNS(ctx, serviceItem); err != nil {
					return err
				}
			}
			if err := c.store.DeleteServiceForGeneration(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation); err != nil &&
				!errors.Is(err, store.ErrServiceNotFound) &&
				!errors.Is(err, store.ErrServiceGenerationConflict) {
				return err
			}
			return nil
		}
		statusErr := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, deletingFailureServiceStatus(serviceItem, err), nil)
		return errors.Join(err, statusErr)
	}
	observedAt := time.Now().UTC()
	run := model.RunStatus{
		Phase:   model.RunPhaseDispatching,
		Message: "service delete accepted by cloud-plane; waiting for node-agent cleanup result",
	}
	status := model.ServiceObservedStatus{
		ObservedGeneration: serviceItem.Metadata.Generation,
		Phase:              model.PhaseDeleting,
		Message:            "service delete accepted by cloud-plane",
		LastObservedAt:     &observedAt,
	}
	if err := c.updateServiceStatus(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, status, &run); err != nil &&
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

func (c *ServiceOperations) dispatchServiceSpec(ctx context.Context, service model.Service) error {
	planeID, err := c.boundPlaneID(ctx, service)
	if err != nil {
		statusErr := c.updateServiceStatus(ctx, service.Metadata.ID, service.Metadata.Generation, failedServiceStatus(service, err), nil)
		return errors.Join(err, statusErr)
	}
	if err := c.sendServiceSpecToPlane(ctx, planeID, service); err != nil {
		statusErr := c.updateServiceStatus(ctx, service.Metadata.ID, service.Metadata.Generation, failedServiceStatus(service, err), nil)
		return errors.Join(err, statusErr)
	}
	observedAt := time.Now().UTC()
	run := model.RunStatus{
		Phase:   model.RunPhaseDispatching,
		Message: "service accepted by cloud-plane; waiting for node-agent execution result",
	}
	status := model.ServiceObservedStatus{
		ObservedGeneration: service.Metadata.Generation,
		Phase:              model.PhaseProgressing,
		Message:            "service accepted by cloud-plane",
		LastObservedAt:     &observedAt,
	}
	if err := c.updateServiceStatus(ctx, service.Metadata.ID, service.Metadata.Generation, status, &run); err != nil {
		return err
	}
	return nil
}

func (c *ServiceOperations) sendServiceSpecToPlane(ctx context.Context, planeID string, service model.Service) error {
	if planeID == "" {
		return errPlaneIDRequired
	}

	client, err := c.planeClient(ctx, planeID)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			c.logger.Warn("close plane client failed", "plane_id", planeID, "error", closeErr)
		}
	}()

	request, err := serviceDispatchRequest(service)
	if err != nil {
		return err
	}
	requestCtx, cancel := context.WithTimeout(ctx, dispatchServiceTimeout)
	defer cancel()

	if err := client.UpsertService(requestCtx, request); err != nil {
		return fmt.Errorf("dispatch service spec to cloud-plane: %w", err)
	}
	return nil
}

func (c *ServiceOperations) deleteRemoteService(ctx context.Context, planeID string, serviceID string, serviceGeneration int64) error {
	if planeID == "" {
		return errPlaneIDRequired
	}
	if serviceID == "" {
		return errServiceIDMissing
	}
	if serviceGeneration <= 0 {
		return fmt.Errorf("serviceGeneration must be greater than 0")
	}

	client, err := c.planeClient(ctx, planeID)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			c.logger.Warn("close plane client failed", "plane_id", planeID, "error", closeErr)
		}
	}()

	requestCtx, cancel := context.WithTimeout(ctx, dispatchDeleteTimeout)
	defer cancel()

	if err := client.DeleteService(requestCtx, &cloudplanev1.DeleteServiceRequest{
		ServiceId:         serviceID,
		ServiceGeneration: serviceGeneration,
	}); err != nil {
		return fmt.Errorf("delete service from cloud-plane: %w", err)
	}
	return nil
}

func (c *ServiceOperations) planeClient(ctx context.Context, planeID string) (*planeClient, error) {
	plane, err := c.store.GetPlane(ctx, planeID)
	if err != nil {
		return nil, err
	}
	return newPlaneClient(plane.GRPCEndpoint, c.southboundToken)
}

func serviceDispatchRequest(service model.Service) (*cloudplanev1.UpsertServiceRequest, error) {
	if strings.TrimSpace(service.Metadata.ID) == "" {
		return nil, errServiceIDMissing
	}

	env := make(map[string]string, len(service.Spec.Env))
	for key, value := range service.Spec.Env {
		env[key] = value
	}
	return &cloudplanev1.UpsertServiceRequest{
		ServiceId:         service.Metadata.ID,
		ServiceName:       service.Metadata.Name,
		DisplayName:       service.Metadata.DisplayName,
		Host:              service.Metadata.Host,
		ServiceGeneration: service.Metadata.Generation,
		Image:             service.Spec.Image,
		Command:           append([]string(nil), service.Spec.Command...),
		Args:              append([]string(nil), service.Spec.Args...),
		Env:               env,
		ContainerPort:     int32(service.Spec.DefaultPort),
		ReadinessPath:     service.Spec.ReadinessPath,
		InstanceClass:     service.Spec.InstanceClass,
		Exposure:          service.Spec.Exposure,
	}, nil
}

func (c *ServiceOperations) boundPlaneID(ctx context.Context, serviceItem model.Service) (string, error) {
	planeID := strings.TrimSpace(serviceItem.Spec.PlaneID)
	if planeID == "" {
		return "", errPlaneIDRequired
	}
	planeDetail, err := c.store.GetPlane(ctx, planeID)
	if err != nil {
		return "", err
	}
	if planeDetail.Status.Status == model.StatusOffline {
		return "", fmt.Errorf("%w: current status is %s", errPlaneNotReady, planeDetail.Status.Status)
	}
	return planeID, nil
}

func (c *ServiceOperations) updateServiceStatus(ctx context.Context, serviceID string, expectedGeneration int64, status model.ServiceObservedStatus, run *model.RunStatus) error {
	input := store.UpdateServiceStatusInput{
		ObservedGeneration: status.ObservedGeneration,
		Phase:              status.Phase,
		Message:            status.Message,
		LastObservedAt:     status.LastObservedAt,
		Run:                run,
	}
	err := c.store.UpdateServiceStatusForGeneration(ctx, serviceID, expectedGeneration, input)
	if errors.Is(err, store.ErrServiceGenerationConflict) || errors.Is(err, store.ErrServiceNotFound) {
		return nil
	}
	return err
}

func failedServiceStatus(serviceItem model.Service, err error) model.ServiceObservedStatus {
	now := time.Now().UTC()
	message := fmt.Sprintf("service dispatch failed: %v", err)
	return model.ServiceObservedStatus{
		ObservedGeneration: serviceItem.Status.Observed.ObservedGeneration,
		Phase:              model.PhaseDegraded,
		Message:            message,
		LastObservedAt:     &now,
	}
}

func deletingFailureServiceStatus(serviceItem model.Service, err error) model.ServiceObservedStatus {
	now := time.Now().UTC()
	message := fmt.Sprintf("service teardown failed: %v", err)
	return model.ServiceObservedStatus{
		ObservedGeneration: serviceItem.Status.Observed.ObservedGeneration,
		Phase:              model.PhaseDeleting,
		Message:            message,
		LastObservedAt:     &now,
	}
}
