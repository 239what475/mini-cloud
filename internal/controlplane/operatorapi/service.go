package operatorapi

import (
	"context"
	"errors"
	"log/slog"

	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/project"
	"mini-cloud/internal/common/projectedfile"
	plane "mini-cloud/internal/controlplane/plane"
	"mini-cloud/internal/controlplane/planesync"
	controlservice "mini-cloud/internal/controlplane/service"
	"mini-cloud/internal/controlplane/servicecontroller"
	"mini-cloud/internal/controlplane/store"
	controlplanev1 "mini-cloud/internal/gen/proto/minicloud/controlplane/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type Service struct {
	controlplanev1.UnimplementedOperatorServiceServer

	logger         *slog.Logger
	adminToken     string
	store          *store.Store
	planeSync      *planesync.Service
	serviceControl *servicecontroller.Controller
}

type overviewCounts struct {
	PlanesTotal         int
	PlanesRegistering   int
	PlanesReady         int
	PlanesDegraded      int
	PlanesOffline       int
	ProjectsTotal       int
	ServicesTotal       int
	ServicesPending     int
	ServicesProgressing int
	ServicesReady       int
	ServicesDegraded    int
	ServicesDeleting    int
}

func NewService(logger *slog.Logger, stores *store.Store, adminToken string, planeSync *planesync.Service, serviceControl *servicecontroller.Controller) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		logger:         logger,
		adminToken:     adminToken,
		store:          stores,
		planeSync:      planeSync,
		serviceControl: serviceControl,
	}
}

func (s *Service) GetOverview(ctx context.Context, _ *emptypb.Empty) (*controlplanev1.Overview, error) {
	if err := authorizeAdmin(ctx, s.adminToken); err != nil {
		return nil, err
	}
	projects, err := s.store.ListProjects(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "list projects failed")
	}
	planes, err := s.store.ListPlanes(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "list planes failed")
	}
	services, err := s.store.ListServices(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "list services failed")
	}
	counts := buildOverview(projects, planes, services)
	return protoOverview(counts), nil
}

func (s *Service) ListPlanes(ctx context.Context, _ *emptypb.Empty) (*controlplanev1.ListPlanesResponse, error) {
	if err := authorizeAdmin(ctx, s.adminToken); err != nil {
		return nil, err
	}
	items, err := s.store.ListPlanes(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "list planes failed")
	}
	out := &controlplanev1.ListPlanesResponse{Items: make([]*controlplanev1.Plane, 0, len(items))}
	for _, item := range items {
		out.Items = append(out.Items, protoPlane(item))
	}
	return out, nil
}

func (s *Service) GetPlane(ctx context.Context, req *controlplanev1.GetPlaneRequest) (*controlplanev1.Plane, error) {
	if err := authorizeAdmin(ctx, s.adminToken); err != nil {
		return nil, err
	}
	planeID := req.GetPlaneId()
	if planeID == "" {
		return nil, status.Error(codes.InvalidArgument, "planeID is required")
	}
	item, err := s.store.GetPlane(ctx, planeID)
	if err != nil {
		if errors.Is(err, store.ErrPlaneNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, "get plane failed")
	}
	return protoPlane(item), nil
}

func (s *Service) SyncPlane(ctx context.Context, req *controlplanev1.SyncPlaneRequest) (*controlplanev1.SyncPlaneResponse, error) {
	if err := authorizeAdmin(ctx, s.adminToken); err != nil {
		return nil, err
	}
	if s.planeSync == nil {
		return nil, status.Error(codes.Unavailable, "plane sync service is not configured")
	}
	planeID := req.GetPlaneId()
	if planeID == "" {
		return nil, status.Error(codes.InvalidArgument, "planeID is required")
	}
	result, err := s.planeSync.SyncPlane(ctx, planeID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrPlaneNotFound):
			return nil, status.Error(codes.NotFound, err.Error())
		case planesync.IsSyncFailure(err):
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		default:
			return nil, status.Error(codes.Internal, "sync plane failed")
		}
	}
	return &controlplanev1.SyncPlaneResponse{
		Plane:            protoPlane(result.Plane),
		ObservedProvider: result.ObservedProvider,
		ObservedRegion:   result.ObservedRegion,
		HealthCheckedAt:  requiredTimestamp(result.HealthCheckedAt),
		SyncedAt:         requiredTimestamp(result.SyncedAt),
		AlertsFiring:     int32(result.AlertsFiring),
	}, nil
}

func (s *Service) ListProjects(ctx context.Context, _ *emptypb.Empty) (*controlplanev1.ListProjectsResponse, error) {
	if err := authorizeAdmin(ctx, s.adminToken); err != nil {
		return nil, err
	}
	items, err := s.store.ListProjects(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "list projects failed")
	}
	out := &controlplanev1.ListProjectsResponse{Items: make([]*controlplanev1.Project, 0, len(items))}
	for _, item := range items {
		out.Items = append(out.Items, protoProject(item))
	}
	return out, nil
}

func (s *Service) GetProject(ctx context.Context, req *controlplanev1.GetProjectRequest) (*controlplanev1.Project, error) {
	if err := authorizeAdmin(ctx, s.adminToken); err != nil {
		return nil, err
	}
	projectID := req.GetProjectId()
	if projectID == "" {
		return nil, status.Error(codes.InvalidArgument, "projectID is required")
	}
	item, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		if errors.Is(err, store.ErrProjectNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, "get project failed")
	}
	return protoProject(item), nil
}

func (s *Service) ListServices(ctx context.Context, req *controlplanev1.ListServicesRequest) (*controlplanev1.ListServicesResponse, error) {
	if err := authorizeAdmin(ctx, s.adminToken); err != nil {
		return nil, err
	}
	projectID := req.GetProjectId()
	if projectID == "" {
		return nil, status.Error(codes.InvalidArgument, "projectID is required")
	}
	if _, err := s.store.GetProject(ctx, projectID); err != nil {
		if errors.Is(err, store.ErrProjectNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, "get project failed")
	}
	items, err := s.serviceControl.ListByProject(ctx, projectID)
	if err != nil {
		return nil, status.Error(codes.Internal, "list services failed")
	}
	out := &controlplanev1.ListServicesResponse{Items: make([]*controlplanev1.Service, 0, len(items))}
	for _, item := range items {
		out.Items = append(out.Items, protoService(item))
	}
	return out, nil
}

func (s *Service) GetService(ctx context.Context, req *controlplanev1.GetServiceRequest) (*controlplanev1.Service, error) {
	if err := authorizeAdmin(ctx, s.adminToken); err != nil {
		return nil, err
	}
	projectID := req.GetProjectId()
	serviceID := req.GetServiceId()
	if projectID == "" || serviceID == "" {
		return nil, status.Error(codes.InvalidArgument, "projectID and serviceID are required")
	}
	view, err := s.serviceControl.Get(ctx, projectID, serviceID)
	if err != nil {
		if errors.Is(err, store.ErrServiceNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, "get service failed")
	}
	return protoService(view), nil
}

func (s *Service) CreateService(ctx context.Context, req *controlplanev1.CreateServiceRequest) (*controlplanev1.Service, error) {
	if err := authorizeAdmin(ctx, s.adminToken); err != nil {
		return nil, err
	}
	projectID := req.GetProjectId()
	if projectID == "" {
		return nil, status.Error(codes.InvalidArgument, "projectID is required")
	}
	input, err := createInputFromProto(req)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	view, err := s.serviceControl.Create(ctx, projectID, input)
	if err != nil {
		switch {
		case isServiceInputError(err):
			return nil, status.Error(codes.InvalidArgument, err.Error())
		case errors.Is(err, store.ErrProjectNotFound),
			errors.Is(err, store.ErrProjectConfigSetNotFound),
			errors.Is(err, store.ErrProjectSecretSetNotFound),
			errors.Is(err, store.ErrProjectRegistryCredentialNotFound):
			return nil, status.Error(codes.NotFound, err.Error())
		case errors.Is(err, store.ErrServiceNameAlreadyExists):
			return nil, status.Error(codes.AlreadyExists, err.Error())
		default:
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
	}
	return protoService(view), nil
}

func buildOverview(projects []project.Project, planes []plane.Detail, services []controlservice.Service) overviewCounts {
	out := overviewCounts{
		ProjectsTotal: len(projects),
		PlanesTotal:   len(planes),
		ServicesTotal: len(services),
	}
	for _, item := range planes {
		switch item.Status.Status {
		case plane.StatusRegistering:
			out.PlanesRegistering++
		case plane.StatusReady:
			out.PlanesReady++
		case plane.StatusDegraded:
			out.PlanesDegraded++
		case plane.StatusOffline:
			out.PlanesOffline++
		}
	}
	for _, item := range services {
		switch item.Status.Observed.Phase {
		case controlservice.PhasePending:
			out.ServicesPending++
		case controlservice.PhaseProgressing:
			out.ServicesProgressing++
		case controlservice.PhaseReady:
			out.ServicesReady++
		case controlservice.PhaseDegraded:
			out.ServicesDegraded++
		case controlservice.PhaseDeleting:
			out.ServicesDeleting++
		}
	}
	return out
}

func isServiceInputError(err error) bool {
	return errors.Is(err, errServiceSpecRequired) ||
		errors.Is(err, controlservice.ErrProjectIDRequired) ||
		errors.Is(err, controlservice.ErrServiceNameRequired) ||
		errors.Is(err, controlservice.ErrInvalidServiceName) ||
		errors.Is(err, controlservice.ErrDisplayNameRequired) ||
		errors.Is(err, controlservice.ErrProviderRequired) ||
		errors.Is(err, controlservice.ErrRegionRequired) ||
		errors.Is(err, controlservice.ErrPinnedPlaneIDInvalid) ||
		errors.Is(err, controlservice.ErrInvalidReplicas) ||
		errors.Is(err, controlservice.ErrInvalidInstanceClass) ||
		errors.Is(err, controlservice.ErrInvalidInstanceClass) ||
		errors.Is(err, controlservice.ErrInvalidExposure) ||
		errors.Is(err, controlservice.ErrImageRequired) ||
		errors.Is(err, controlservice.ErrInvalidDefaultPort) ||
		errors.Is(err, controlservice.ErrInvalidReadinessPath) ||
		errors.Is(err, controlservice.ErrInvalidEnvironmentKey) ||
		errors.Is(err, controlservice.ErrPersistentDirsReplicaLimit) ||
		errors.Is(err, controlservice.ErrPersistentDirsRolloutUnsupported) ||
		errors.Is(err, controlservice.ErrPersistentDirsPlacementChangeUnsupported) ||
		errors.Is(err, projectedfile.ErrMountPathRequired) ||
		errors.Is(err, projectedfile.ErrMountPathAbsolute) ||
		errors.Is(err, projectedfile.ErrMountPathInvalid) ||
		errors.Is(err, projectedfile.ErrSourceKindInvalid) ||
		errors.Is(err, projectedfile.ErrSourceIDRequired) ||
		errors.Is(err, projectedfile.ErrSourceKeyRequired) ||
		errors.Is(err, projectedfile.ErrDuplicateMountPath) ||
		errors.Is(err, persistentdir.ErrNameRequired) ||
		errors.Is(err, persistentdir.ErrInvalidName) ||
		errors.Is(err, persistentdir.ErrMountPathRequired) ||
		errors.Is(err, persistentdir.ErrMountPathAbsolute) ||
		errors.Is(err, persistentdir.ErrMountPathInvalid) ||
		errors.Is(err, persistentdir.ErrDuplicateName) ||
		errors.Is(err, persistentdir.ErrDuplicateMountPath) ||
		errors.Is(err, persistentdir.ErrNestedMountPath) ||
		errors.Is(err, persistentdir.ErrProjectedConflict)
}
