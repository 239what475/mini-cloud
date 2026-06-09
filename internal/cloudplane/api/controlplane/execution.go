package controlplane

import (
	"context"
	"log/slog"
	"strings"

	"mini-cloud/internal/cloudplane/infra/store"
	cloudmodel "mini-cloud/internal/cloudplane/model"
	"mini-cloud/internal/common/projectedfile"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ExecutionServer struct {
	cloudplanev1.UnimplementedControlPlaneExecutionServiceServer

	logger *slog.Logger
	store  *store.Store
	auth   Authenticator
}

func NewExecutionServer(logger *slog.Logger, stores *store.Store, auth Authenticator) cloudplanev1.ControlPlaneExecutionServiceServer {
	return &ExecutionServer{logger: logger, store: stores, auth: auth}
}

func (s *ExecutionServer) ApplyExecutionPlan(ctx context.Context, req *cloudplanev1.ApplyExecutionPlanRequest) (*cloudplanev1.ApplyExecutionPlanResponse, error) {
	if err := s.auth.Authorize(ctx); err != nil {
		return nil, err
	}
	cpuMilliRequest, memoryMiRequest, err := cloudmodel.ResourceRequestForInstanceClass(req.GetInstanceClass())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	exposure, err := cloudmodel.ParseExposure(req.GetExposure())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	input := cloudmodel.PlanInput{
		PlanID:            strings.TrimSpace(req.GetPlanId()),
		ServiceID:         strings.TrimSpace(req.GetServiceId()),
		ServiceName:       strings.TrimSpace(req.GetServiceName()),
		ServiceGeneration: req.GetServiceGeneration(),
		Image:             strings.TrimSpace(req.GetImage()),
		Command:           append([]string(nil), req.GetCommand()...),
		Args:              append([]string(nil), req.GetArgs()...),
		Env:               cloneStringMap(req.GetEnv()),
		ProjectedFiles:    projectedFilesFromProto(req.GetProjectedFiles()),
		ContainerPort:     int(req.GetContainerPort()),
		ReadinessPath:     strings.TrimSpace(req.GetReadinessPath()),
		CPUMilliRequest:   cpuMilliRequest,
		MemoryMiRequest:   memoryMiRequest,
		Exposure:          exposure,
	}
	if cred := req.GetImageCredential(); cred != nil {
		input.ImageCredential = &cloudmodel.ImageCredential{
			Server:   strings.TrimSpace(cred.GetServer()),
			Username: strings.TrimSpace(cred.GetUsername()),
			Password: cred.GetPassword(),
		}
	}

	planID, err := s.store.ApplyExecutionPlan(ctx, input)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return &cloudplanev1.ApplyExecutionPlanResponse{Action: "accepted", PlanId: planID}, nil
}

func (s *ExecutionServer) DeleteExecutionPlan(ctx context.Context, req *cloudplanev1.DeleteExecutionPlanRequest) (*cloudplanev1.DeleteExecutionPlanResponse, error) {
	if err := s.auth.Authorize(ctx); err != nil {
		return nil, err
	}
	serviceID := strings.TrimSpace(req.GetServiceId())
	if serviceID == "" {
		return nil, status.Error(codes.InvalidArgument, "serviceID is required")
	}
	if err := s.store.DeleteExecutionPlansForService(ctx, cloudmodel.DeletePlanInput{
		ServiceID:         serviceID,
		ServiceGeneration: req.GetServiceGeneration(),
		PlanID:            strings.TrimSpace(req.GetPlanId()),
	}); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return &cloudplanev1.DeleteExecutionPlanResponse{ServiceId: serviceID, Deleted: true}, nil
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func projectedFilesFromProto(items []*cloudplanev1.ExecutionProjectedFile) []projectedfile.File {
	if len(items) == 0 {
		return nil
	}
	out := make([]projectedfile.File, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, projectedfile.File{
			MountPath: strings.TrimSpace(item.GetMountPath()),
			Content:   item.GetContent(),
			Mode:      item.GetMode(),
			Sensitive: item.GetSensitive(),
		})
	}
	return projectedfile.CloneFiles(out)
}
