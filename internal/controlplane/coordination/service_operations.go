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
	created.Spec = input.Spec
	created.Status.Observed = acceptedServiceStatus(created.Metadata.Generation, "service accepted by cloud-plane")
	created.Status.Run = model.RunStatus{Phase: model.RunPhaseDispatching, Message: "service accepted by cloud-plane; waiting for node-agent execution result"}

	if err := c.dispatchServiceSpec(ctx, created); err != nil {
		if deleteErr := c.store.DeleteServiceForGeneration(ctx, created.Metadata.ID, created.Metadata.Generation); deleteErr != nil && !errors.Is(deleteErr, store.ErrServiceNotFound) {
			return model.Service{}, errors.Join(err, deleteErr)
		}
		return model.Service{}, err
	}
	c.syncServicePlane(ctx, created.Metadata.ID, created.Spec.PlaneID)
	return created, nil
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
	bindings, err := c.store.ListServices(ctx)
	if err != nil {
		return nil, err
	}
	if len(bindings) == 0 {
		return bindings, nil
	}

	snapshots := make(map[string]*cloudplanev1.PlaneSnapshot)
	for _, binding := range bindings {
		planeID := strings.TrimSpace(binding.Spec.PlaneID)
		if planeID == "" || snapshots[planeID] != nil {
			continue
		}
		snapshot, err := c.loadPlaneSnapshot(ctx, planeID)
		if err != nil {
			c.logger.Warn("load plane snapshot for service list failed", "plane_id", planeID, "error", err)
			continue
		}
		snapshots[planeID] = snapshot
	}

	out := make([]model.Service, 0, len(bindings))
	for _, binding := range bindings {
		out = append(out, c.serviceFromSnapshot(binding, snapshots[binding.Spec.PlaneID]))
	}
	return out, nil
}

func (c *ServiceOperations) Get(ctx context.Context, serviceID string) (model.Service, error) {
	binding, err := c.store.GetService(ctx, serviceID)
	if err != nil {
		return model.Service{}, err
	}
	snapshot, err := c.loadPlaneSnapshot(ctx, binding.Spec.PlaneID)
	if err != nil {
		c.logger.Warn("load plane snapshot for service get failed", "service_id", serviceID, "plane_id", binding.Spec.PlaneID, "error", err)
		return binding, nil
	}
	return c.serviceFromSnapshot(binding, snapshot), nil
}

func (c *ServiceOperations) Update(ctx context.Context, serviceID string, input store.UpdateServiceInput) (model.Service, error) {
	current, err := c.store.GetService(ctx, serviceID)
	if err != nil {
		return model.Service{}, err
	}
	if current.Status.DesiredState == model.DesiredStateDeleted {
		return model.Service{}, store.ErrServiceDeleting
	}

	next := current
	next.Metadata.DisplayName = strings.TrimSpace(input.DisplayName)
	next.Metadata.Generation++
	next.Spec = model.ServiceSpec{
		PlaneID:       current.Spec.PlaneID,
		InstanceClass: input.Spec.InstanceClass,
		Exposure:      input.Spec.Exposure,
		Image:         input.Spec.Image,
		Command:       append([]string(nil), input.Spec.Command...),
		Args:          append([]string(nil), input.Spec.Args...),
		DefaultPort:   input.Spec.DefaultPort,
		ReadinessPath: input.Spec.ReadinessPath,
		Env:           cloneEnv(input.Spec.Env),
	}
	next.Status.Observed = acceptedServiceStatus(next.Metadata.Generation, "service update accepted by cloud-plane")
	next.Status.Run = model.RunStatus{Phase: model.RunPhaseDispatching, Message: "service update accepted by cloud-plane; waiting for node-agent execution result"}

	if err := c.dispatchServiceSpec(ctx, next); err != nil {
		return model.Service{}, err
	}
	updated, err := c.store.UpdateServiceBinding(ctx, serviceID, input)
	if err != nil {
		return model.Service{}, err
	}
	updated.Spec = next.Spec
	updated.Status = next.Status
	c.syncServicePlane(ctx, updated.Metadata.ID, updated.Spec.PlaneID)
	return updated, nil
}

func (c *ServiceOperations) Delete(ctx context.Context, serviceID string) (model.Service, error) {
	deleting, err := c.store.MarkServiceDeletionRequested(ctx, serviceID)
	if err != nil {
		return model.Service{}, err
	}
	if err := c.dispatchDeletingService(ctx, deleting); err != nil {
		c.logger.Warn("service delete dispatch failed; service remains deleting", "service_id", deleting.Metadata.ID, "error", err)
	} else {
		c.syncServicePlane(ctx, deleting.Metadata.ID, deleting.Spec.PlaneID)
	}
	deleting.Status.Observed = model.DeletingServiceStatus(deleting.Metadata.Generation, "service deletion requested")
	deleting.Status.Run = model.PendingRunStatus("waiting for cloud-plane cleanup")
	current, err := c.store.GetService(ctx, deleting.Metadata.ID)
	if err != nil {
		if errors.Is(err, store.ErrServiceNotFound) {
			return deleting, nil
		}
		return model.Service{}, err
	}
	current.Status = deleting.Status
	return current, nil
}

func (c *ServiceOperations) dispatchDeletingService(ctx context.Context, serviceItem model.Service) error {
	planeID, err := c.boundPlaneID(ctx, serviceItem)
	if err != nil {
		return err
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
		return err
	}
	return nil
}

func (c *ServiceOperations) syncServicePlane(ctx context.Context, serviceID string, planeID string) {
	if c.planeSyncer == nil {
		return
	}
	planeID = strings.TrimSpace(planeID)
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
		return err
	}
	return c.sendServiceSpecToPlane(ctx, planeID, service)
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

func (c *ServiceOperations) loadPlaneSnapshot(ctx context.Context, planeID string) (*cloudplanev1.PlaneSnapshot, error) {
	client, err := c.planeClient(ctx, planeID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			c.logger.Warn("close plane client failed", "plane_id", planeID, "error", closeErr)
		}
	}()
	requestCtx, cancel := context.WithTimeout(ctx, RequestPlaneSyncTimeout)
	defer cancel()
	return client.Snapshot(requestCtx)
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

func (c *ServiceOperations) serviceFromSnapshot(binding model.Service, snapshot *cloudplanev1.PlaneSnapshot) model.Service {
	if binding.Status.DesiredState == model.DesiredStateDeleted {
		binding.Status.Observed = model.DeletingServiceStatus(binding.Metadata.Generation, "service deletion requested")
		binding.Status.Run = model.PendingRunStatus("waiting for cloud-plane cleanup")
	}
	if snapshot == nil {
		return binding
	}
	for _, item := range snapshot.GetServices() {
		if item == nil || item.GetServiceId() != binding.Metadata.ID {
			continue
		}
		binding.Metadata.Name = item.GetName()
		binding.Metadata.DisplayName = item.GetDisplayName()
		binding.Metadata.Host = item.GetHost()
		binding.Metadata.Generation = item.GetGeneration()
		binding.Status.DesiredState = item.GetDesiredState()
		binding.Spec.InstanceClass = item.GetInstanceClass()
		binding.Spec.Exposure = item.GetExposure()
		binding.Spec.Image = item.GetImage()
		binding.Spec.Command = append([]string(nil), item.GetCommand()...)
		binding.Spec.Args = append([]string(nil), item.GetArgs()...)
		binding.Spec.DefaultPort = int(item.GetContainerPort())
		binding.Spec.ReadinessPath = item.GetReadinessPath()
		binding.Spec.Env = cloneEnv(item.GetEnv())
		break
	}
	for _, execution := range snapshot.GetExecutions() {
		if execution == nil || execution.GetServiceId() != binding.Metadata.ID || execution.GetServiceGeneration() != binding.Metadata.Generation {
			continue
		}
		status := serviceStatusFromExecutionSnapshot(execution)
		binding.Status.Observed = status.Observed
		binding.Status.Run = status.Run
		break
	}
	return binding
}

func acceptedServiceStatus(generation int64, message string) model.ServiceObservedStatus {
	now := time.Now().UTC()
	return model.ServiceObservedStatus{
		ObservedGeneration: generation,
		Phase:              model.PhaseProgressing,
		Message:            message,
		LastObservedAt:     &now,
	}
}

func cloneEnv(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
