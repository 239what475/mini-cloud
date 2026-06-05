package execution

import (
	"context"
	"strings"

	cloudexecution "mini-cloud/internal/cloudplane/domain/execution"
	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/projectedfile"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) ApplyExecutionPlan(ctx context.Context, req *cloudplanev1.ApplyExecutionPlanRequest) (*cloudplanev1.ApplyExecutionPlanResponse, error) {
	if err := s.auth.Authorize(ctx); err != nil {
		return nil, err
	}
	input := cloudexecution.PlanInput{
		PlanID:            strings.TrimSpace(req.GetPlanId()),
		ServiceID:         strings.TrimSpace(req.GetServiceId()),
		ServiceName:       strings.TrimSpace(req.GetServiceName()),
		ServiceGeneration: req.GetServiceGeneration(),
		Image:             strings.TrimSpace(req.GetImage()),
		Command:           append([]string(nil), req.GetCommand()...),
		Args:              append([]string(nil), req.GetArgs()...),
		Env:               cloneStringMap(req.GetEnv()),
		ProjectedFiles:    projectedFilesFromProto(req.GetProjectedFiles()),
		PersistentDirs:    persistentDirsFromProto(req.GetPersistentDirs()),
		ContainerPort:     int(req.GetContainerPort()),
		ReadinessPath:     strings.TrimSpace(req.GetReadinessPath()),
		InstanceClass:     strings.TrimSpace(req.GetInstanceClass()),
		Exposure:          strings.TrimSpace(req.GetExposure()),
	}
	if cred := req.GetImageCredential(); cred != nil {
		input.ImageCredential = &cloudexecution.ImageCredential{
			Server:   strings.TrimSpace(cred.GetServer()),
			Username: strings.TrimSpace(cred.GetUsername()),
			Password: cred.GetPassword(),
		}
	}

	result, err := s.store.ApplyExecutionPlan(ctx, input)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return &cloudplanev1.ApplyExecutionPlanResponse{Action: result.Action, PlanId: result.PlanID}, nil
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

func persistentDirsFromProto(items []*cloudplanev1.ExecutionPersistentDir) []persistentdir.Mount {
	if len(items) == 0 {
		return nil
	}
	out := make([]persistentdir.Mount, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, persistentdir.Mount{
			Name:       strings.TrimSpace(item.GetName()),
			MountPath:  strings.TrimSpace(item.GetMountPath()),
			SourcePath: strings.TrimSpace(item.GetSourcePath()),
		})
	}
	return persistentdir.CloneMounts(out)
}
