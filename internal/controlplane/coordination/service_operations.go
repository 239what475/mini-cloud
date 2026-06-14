package coordination

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"mini-cloud/internal/controlplane/model"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"
)

var (
	errPlaneNotReady    = errors.New("plane is not ready")
	errPlaneIDRequired  = errors.New("planeID is required")
	errServiceIDMissing = errors.New("serviceID is required")
	errServiceSpec      = errors.New("invalid service spec")
)

const (
	dispatchServiceTimeout = 15 * time.Second
	dispatchDeleteTimeout  = 15 * time.Second
)

var serviceNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

type CreateServiceInput struct {
	Name        string
	DisplayName string
	Spec        model.ServiceSpec
}

type UpdateServiceInput struct {
	PlaneID     string
	ServiceID   string
	DisplayName string
	Spec        model.WorkloadSpec
}

type DeleteServiceInput struct {
	PlaneID   string
	ServiceID string
}

type ServiceOperations struct {
	logger            *slog.Logger
	planes            *PlaneCatalog
	serviceBaseDomain string
	southboundToken   string
	planeSyncer       *PlaneSyncer
}

func NewServiceOperations(logger *slog.Logger, planes *PlaneCatalog, southboundToken string, serviceBaseDomain string, planeSyncer *PlaneSyncer) *ServiceOperations {
	if logger == nil {
		logger = slog.Default()
	}
	return &ServiceOperations{
		logger:            logger,
		planes:            planes,
		serviceBaseDomain: strings.Trim(strings.ToLower(strings.TrimSpace(serviceBaseDomain)), "."),
		southboundToken:   strings.TrimSpace(southboundToken),
		planeSyncer:       planeSyncer,
	}
}

func (c *ServiceOperations) Create(ctx context.Context, input CreateServiceInput) (model.Service, error) {
	if err := validateCreateServiceInput(input); err != nil {
		return model.Service{}, err
	}
	planeID := strings.TrimSpace(input.Spec.PlaneID)
	if _, err := c.readyPlane(ctx, planeID); err != nil {
		return model.Service{}, err
	}
	serviceID, err := newPublicID("svc")
	if err != nil {
		return model.Service{}, err
	}
	service := model.Service{
		Metadata: model.ServiceMetadata{
			ID:          serviceID,
			Name:        strings.TrimSpace(input.Name),
			DisplayName: strings.TrimSpace(input.DisplayName),
			Host:        serviceHost(input.Name, c.serviceBaseDomain),
			Generation:  1,
		},
		Spec: input.Spec,
		Status: model.ServiceStatus{
			Observed: acceptedServiceStatus(1, "service accepted by cloud-plane"),
			Run:      model.RunStatus{Phase: model.RunPhaseDispatching, Message: "service accepted by cloud-plane; waiting for node-agent execution result"},
		},
	}
	if err := c.sendServiceSpecToPlane(ctx, planeID, service); err != nil {
		return model.Service{}, err
	}
	c.bestEffortRefreshServiceDNS(ctx, service.Metadata.ID, planeID)
	return service, nil
}

func (c *ServiceOperations) List(ctx context.Context) ([]model.Service, error) {
	views, err := c.loadPlaneSnapshotViews(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]model.Service, 0)
	for _, view := range views {
		out = append(out, servicesFromSnapshot(view.Plane.ID, view.Snapshot)...)
	}
	return out, nil
}

func (c *ServiceOperations) Get(ctx context.Context, planeID string, serviceID string) (model.Service, error) {
	serviceID = strings.TrimSpace(serviceID)
	if serviceID == "" {
		return model.Service{}, errServiceIDMissing
	}
	view, err := c.loadPlaneSnapshotView(ctx, planeID)
	if err != nil {
		return model.Service{}, err
	}
	for _, service := range servicesFromSnapshot(view.Plane.ID, view.Snapshot) {
		if service.Metadata.ID == serviceID {
			return service, nil
		}
	}
	return model.Service{}, ErrServiceNotFound
}

func (c *ServiceOperations) Update(ctx context.Context, input UpdateServiceInput) (model.Service, error) {
	if err := validateUpdateServiceInput(input); err != nil {
		return model.Service{}, err
	}
	current, err := c.Get(ctx, input.PlaneID, input.ServiceID)
	if err != nil {
		return model.Service{}, err
	}
	next := current
	next.Metadata.DisplayName = strings.TrimSpace(input.DisplayName)
	next.Metadata.Generation++
	next.Spec = model.ServiceSpec{
		PlaneID:       strings.TrimSpace(input.PlaneID),
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
	if err := c.sendServiceSpecToPlane(ctx, input.PlaneID, next); err != nil {
		return model.Service{}, err
	}
	c.bestEffortRefreshServiceDNS(ctx, next.Metadata.ID, input.PlaneID)
	return next, nil
}

func (c *ServiceOperations) Delete(ctx context.Context, input DeleteServiceInput) (model.Service, error) {
	planeID := strings.TrimSpace(input.PlaneID)
	serviceID := strings.TrimSpace(input.ServiceID)
	if planeID == "" {
		return model.Service{}, errPlaneIDRequired
	}
	if serviceID == "" {
		return model.Service{}, errServiceIDMissing
	}
	current, err := c.Get(ctx, planeID, serviceID)
	if err != nil {
		return model.Service{}, err
	}
	deleteGeneration := current.Metadata.Generation + 1
	if err := c.deleteRemoteService(ctx, planeID, serviceID, deleteGeneration); err != nil {
		if errors.Is(err, errPlaneObjectNotFound) {
			c.cleanupServiceDNS(ctx, planeID, serviceID)
			current.Status.Observed = model.DeletingServiceStatus(deleteGeneration, "service already absent from cloud-plane")
			current.Status.Run = model.PendingRunStatus("service already absent from cloud-plane")
			return current, nil
		}
		return model.Service{}, err
	}
	current.Metadata.Generation = deleteGeneration
	current.Status.Observed = model.DeletingServiceStatus(deleteGeneration, "service deletion requested")
	current.Status.Run = model.PendingRunStatus("waiting for cloud-plane cleanup")
	c.cleanupServiceDNS(ctx, planeID, serviceID)
	c.bestEffortRefreshServiceDNS(ctx, serviceID, planeID)
	return current, nil
}

func (c *ServiceOperations) cleanupServiceDNS(ctx context.Context, planeID string, serviceID string) {
	if c.planeSyncer == nil {
		return
	}
	if err := c.planeSyncer.deleteServiceDNS(ctx, planeID, serviceID); err != nil {
		c.logger.Warn("delete service DNS records failed", "service_id", serviceID, "plane_id", planeID, "error", err)
	}
}

func serviceHost(name string, baseDomain string) string {
	name = strings.Trim(strings.ToLower(strings.TrimSpace(name)), ".")
	baseDomain = strings.Trim(strings.ToLower(strings.TrimSpace(baseDomain)), ".")
	if name == "" || baseDomain == "" {
		return ""
	}
	return name + "." + baseDomain
}

func (c *ServiceOperations) bestEffortRefreshServiceDNS(ctx context.Context, serviceID string, planeID string) {
	if c.planeSyncer == nil {
		return
	}
	planeID = strings.TrimSpace(planeID)
	if planeID == "" {
		return
	}
	view, err := c.planeSyncer.GetPlaneSnapshotView(ctx, planeID)
	if err != nil {
		c.logger.Warn("refresh service DNS failed", "service_id", serviceID, "plane_id", planeID, "error", err)
		return
	}
	if view.Snapshot == nil {
		c.logger.Warn("refresh service DNS failed", "service_id", serviceID, "plane_id", planeID, "error", view.Plane.Status.Message)
	}
}

func (c *ServiceOperations) sendServiceSpecToPlane(ctx context.Context, planeID string, service model.Service) error {
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
	if err := client.DeleteService(requestCtx, &cloudplanev1.DeleteServiceRequest{ServiceId: serviceID, ServiceGeneration: serviceGeneration}); err != nil {
		return fmt.Errorf("delete service from cloud-plane: %w", err)
	}
	return nil
}

func (c *ServiceOperations) planeClient(ctx context.Context, planeID string) (*planeClient, error) {
	plane, err := c.readyPlane(ctx, planeID)
	if err != nil {
		return nil, err
	}
	return newPlaneClient(plane.GRPCEndpoint, c.southboundToken)
}

func (c *ServiceOperations) readyPlane(ctx context.Context, planeID string) (model.PlaneDetail, error) {
	planeID = strings.TrimSpace(planeID)
	if planeID == "" {
		return model.PlaneDetail{}, errPlaneIDRequired
	}
	plane, err := c.planes.GetPlane(ctx, planeID)
	if err != nil {
		return model.PlaneDetail{}, err
	}
	if plane.Status.Status == model.StatusOffline {
		return model.PlaneDetail{}, fmt.Errorf("%w: current status is %s", errPlaneNotReady, plane.Status.Status)
	}
	return plane, nil
}

func (c *ServiceOperations) loadPlaneSnapshotViews(ctx context.Context) ([]PlaneSnapshotView, error) {
	if c.planeSyncer != nil {
		return c.planeSyncer.ListPlaneSnapshotViews(ctx, RequestPlaneSyncTimeout)
	}
	planes, err := c.planes.ListPlanes(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]PlaneSnapshotView, 0, len(planes))
	for _, plane := range planes {
		view, err := c.loadPlaneSnapshotView(ctx, plane.ID)
		if err != nil {
			c.logger.Warn("load plane snapshot view failed", "plane_id", plane.ID, "error", err)
			views = append(views, PlaneSnapshotView{Plane: plane})
			continue
		}
		views = append(views, view)
	}
	return views, nil
}

func (c *ServiceOperations) loadPlaneSnapshotView(ctx context.Context, planeID string) (PlaneSnapshotView, error) {
	if c.planeSyncer != nil {
		return c.planeSyncer.GetPlaneSnapshotView(ctx, planeID)
	}
	plane, err := c.readyPlane(ctx, planeID)
	if err != nil {
		return PlaneSnapshotView{}, err
	}
	client, err := newPlaneClient(plane.GRPCEndpoint, c.southboundToken)
	if err != nil {
		return PlaneSnapshotView{}, err
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			c.logger.Warn("close plane client failed", "plane_id", plane.ID, "error", closeErr)
		}
	}()
	requestCtx, cancel := context.WithTimeout(ctx, RequestPlaneSyncTimeout)
	defer cancel()
	snapshot, err := client.Snapshot(requestCtx)
	if err != nil {
		return PlaneSnapshotView{}, err
	}
	return PlaneSnapshotView{Plane: plane, Snapshot: snapshot}, nil
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

func servicesFromSnapshot(planeID string, snapshot *cloudplanev1.PlaneSnapshot) []model.Service {
	if snapshot == nil {
		return nil
	}
	executions := make(map[string]*cloudplanev1.PlaneExecutionSnapshot)
	for _, execution := range snapshot.GetExecutions() {
		if execution == nil {
			continue
		}
		executions[execution.GetServiceId()] = execution
	}
	out := make([]model.Service, 0, len(snapshot.GetServices()))
	for _, item := range snapshot.GetServices() {
		if item == nil || strings.TrimSpace(item.GetServiceId()) == "" {
			continue
		}
		service := serviceFromSnapshotItem(planeID, item)
		if execution := executions[service.Metadata.ID]; execution != nil && execution.GetServiceGeneration() == service.Metadata.Generation {
			status := serviceStatusFromExecutionSnapshot(execution)
			service.Status.Observed = status.Observed
			service.Status.Run = status.Run
		}
		out = append(out, service)
	}
	return out
}

func serviceFromSnapshotItem(planeID string, item *cloudplanev1.PlaneService) model.Service {
	return model.Service{
		Metadata: model.ServiceMetadata{
			ID:          item.GetServiceId(),
			Name:        item.GetName(),
			DisplayName: item.GetDisplayName(),
			Host:        item.GetHost(),
			Generation:  item.GetGeneration(),
		},
		Spec: model.ServiceSpec{
			PlaneID:       planeID,
			InstanceClass: item.GetInstanceClass(),
			Exposure:      item.GetExposure(),
			Image:         item.GetImage(),
			Command:       append([]string(nil), item.GetCommand()...),
			Args:          append([]string(nil), item.GetArgs()...),
			DefaultPort:   int(item.GetContainerPort()),
			ReadinessPath: item.GetReadinessPath(),
			Env:           cloneEnv(item.GetEnv()),
		},
		Status: model.ServiceStatus{
			Observed:  model.PendingServiceStatus(item.GetGeneration(), "waiting for service run status"),
			Run:       model.PendingRunStatus("waiting for service run status"),
			FrontDoor: serviceFrontDoorFromSnapshotItem(item),
		},
	}
}

func serviceFrontDoorFromSnapshotItem(item *cloudplanev1.PlaneService) model.FrontDoorStatus {
	frontDoor := model.FrontDoorStatus{CNAME: strings.TrimSpace(item.GetFrontdoorCname())}
	verifyHost := strings.TrimSpace(item.GetFrontdoorVerifySubdomain())
	verifyValue := strings.TrimSpace(item.GetFrontdoorVerifyValue())
	if verifyHost != "" && verifyValue != "" {
		recordType := strings.TrimSpace(item.GetFrontdoorVerifyType())
		if recordType == "" {
			recordType = "TXT"
		}
		frontDoor.Verification = &model.FrontDoorDNSRecord{
			Host:       verifyHost,
			RecordType: recordType,
			Value:      verifyValue,
		}
	}
	return frontDoor
}

func acceptedServiceStatus(generation int64, message string) model.ServiceObservedStatus {
	now := time.Now().UTC()
	return model.ServiceObservedStatus{ObservedGeneration: generation, Phase: model.PhaseProgressing, Message: message, LastObservedAt: &now}
}

func validateCreateServiceInput(input CreateServiceInput) error {
	if strings.TrimSpace(input.Name) == "" {
		return fmt.Errorf("%w: name is required", errServiceSpec)
	}
	if !serviceNamePattern.MatchString(strings.TrimSpace(input.Name)) {
		return fmt.Errorf("%w: name must use lowercase letters, digits, and hyphens", errServiceSpec)
	}
	if strings.TrimSpace(input.DisplayName) == "" {
		return fmt.Errorf("%w: displayName is required", errServiceSpec)
	}
	return validateServiceSpec(input.Spec)
}

func validateUpdateServiceInput(input UpdateServiceInput) error {
	if strings.TrimSpace(input.PlaneID) == "" {
		return errPlaneIDRequired
	}
	if strings.TrimSpace(input.ServiceID) == "" {
		return errServiceIDMissing
	}
	if strings.TrimSpace(input.DisplayName) == "" {
		return fmt.Errorf("%w: displayName is required", errServiceSpec)
	}
	return validateWorkloadSpec(input.Spec)
}

func validateServiceSpec(spec model.ServiceSpec) error {
	if strings.TrimSpace(spec.PlaneID) == "" {
		return errPlaneIDRequired
	}
	return validateWorkloadSpec(model.WorkloadSpec{
		InstanceClass: spec.InstanceClass,
		Exposure:      spec.Exposure,
		Image:         spec.Image,
		Command:       spec.Command,
		Args:          spec.Args,
		DefaultPort:   spec.DefaultPort,
		ReadinessPath: spec.ReadinessPath,
		Env:           spec.Env,
	})
}

func validateWorkloadSpec(spec model.WorkloadSpec) error {
	if !model.IsInstanceClass(spec.InstanceClass) {
		return fmt.Errorf("%w: instanceClass must be one of small, medium, large", errServiceSpec)
	}
	resolvedExposure := strings.ToLower(strings.TrimSpace(spec.Exposure))
	if resolvedExposure == "" {
		resolvedExposure = model.ExposurePublic
	}
	if !model.IsServiceExposure(resolvedExposure) {
		return fmt.Errorf("%w: exposure must be one of public, private", errServiceSpec)
	}
	if strings.TrimSpace(spec.Image) == "" {
		return fmt.Errorf("%w: image is required", errServiceSpec)
	}
	if spec.DefaultPort <= 0 || spec.DefaultPort > 65535 {
		return fmt.Errorf("%w: defaultPort must be between 1 and 65535", errServiceSpec)
	}
	if !strings.HasPrefix(strings.TrimSpace(spec.ReadinessPath), "/") {
		return fmt.Errorf("%w: readinessPath must start with /", errServiceSpec)
	}
	for key := range spec.Env {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("%w: env keys must not be empty", errServiceSpec)
		}
	}
	return nil
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
