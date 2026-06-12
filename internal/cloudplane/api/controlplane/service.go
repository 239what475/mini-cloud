package controlplane

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"mini-cloud/internal/cloudplane/infra/store"
	cloudmodel "mini-cloud/internal/cloudplane/model"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type serviceServer struct {
	cloudplanev1.UnimplementedControlPlaneServiceServer

	logger *slog.Logger
	store  *store.Store
	auth   authenticator
}

func newServiceServer(logger *slog.Logger, stores *store.Store, auth authenticator) cloudplanev1.ControlPlaneServiceServer {
	return &serviceServer{logger: logger, store: stores, auth: auth}
}

func (s *serviceServer) UpsertService(ctx context.Context, req *cloudplanev1.UpsertServiceRequest) (*cloudplanev1.UpsertServiceResponse, error) {
	if err := s.auth.authorize(ctx); err != nil {
		return nil, err
	}
	exposure, err := cloudmodel.ParseExposure(req.GetExposure())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	env := make(map[string]string, len(req.GetEnv()))
	for key, value := range req.GetEnv() {
		env[key] = value
	}
	input := cloudmodel.UpsertServiceInput{
		ID:          strings.TrimSpace(req.GetServiceId()),
		Name:        strings.TrimSpace(req.GetServiceName()),
		DisplayName: strings.TrimSpace(req.GetDisplayName()),
		Host:        strings.TrimSpace(req.GetHost()),
		Generation:  req.GetServiceGeneration(),
		Spec: cloudmodel.ServiceSpec{
			InstanceClass: strings.TrimSpace(req.GetInstanceClass()),
			Exposure:      exposure,
			Image:         strings.TrimSpace(req.GetImage()),
			Command:       append([]string(nil), req.GetCommand()...),
			Args:          append([]string(nil), req.GetArgs()...),
			Env:           env,
			ContainerPort: int(req.GetContainerPort()),
			ReadinessPath: strings.TrimSpace(req.GetReadinessPath()),
		},
	}
	if _, err := s.store.UpsertService(ctx, input); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return &cloudplanev1.UpsertServiceResponse{}, nil
}

func (s *serviceServer) DeleteService(ctx context.Context, req *cloudplanev1.DeleteServiceRequest) (*cloudplanev1.DeleteServiceResponse, error) {
	if err := s.auth.authorize(ctx); err != nil {
		return nil, err
	}
	serviceID := strings.TrimSpace(req.GetServiceId())
	if serviceID == "" {
		return nil, status.Error(codes.InvalidArgument, "serviceID is required")
	}
	if err := s.store.DeleteService(ctx, cloudmodel.DeleteServiceInput{
		ID:         serviceID,
		Generation: req.GetServiceGeneration(),
	}); err != nil {
		if errors.Is(err, store.ErrServiceNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return &cloudplanev1.DeleteServiceResponse{}, nil
}
