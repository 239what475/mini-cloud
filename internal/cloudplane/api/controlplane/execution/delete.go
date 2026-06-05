package execution

import (
	"context"
	"strings"

	cloudexecution "mini-cloud/internal/cloudplane/domain/execution"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) DeleteExecutionPlan(ctx context.Context, req *cloudplanev1.DeleteExecutionPlanRequest) (*cloudplanev1.DeleteExecutionPlanResponse, error) {
	if err := s.auth.Authorize(ctx); err != nil {
		return nil, err
	}
	serviceID := strings.TrimSpace(req.GetServiceId())
	if serviceID == "" {
		return nil, status.Error(codes.InvalidArgument, "serviceID is required")
	}
	deleted, err := s.store.DeleteExecutionPlansForService(ctx, cloudexecution.DeletePlanInput{
		ServiceID:         serviceID,
		ServiceGeneration: req.GetServiceGeneration(),
		PlanID:            strings.TrimSpace(req.GetPlanId()),
	})
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return &cloudplanev1.DeleteExecutionPlanResponse{ServiceId: serviceID, Deleted: deleted}, nil
}
