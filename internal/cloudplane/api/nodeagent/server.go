package nodeagent

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"strings"

	"mini-cloud/internal/cloudplane/infra/store"
	cloudmodel "mini-cloud/internal/cloudplane/model"
	nodeagentv1 "mini-cloud/internal/gen/proto/minicloud/nodeagent/v1"
	"mini-cloud/internal/transport"
	"mini-cloud/internal/workload"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func RegisterGRPC(grpcServer *grpc.Server, logger *slog.Logger, stores *store.Store, token string) {
	if logger == nil {
		logger = slog.Default()
	}
	nodeagentv1.RegisterNodeAgentServiceServer(grpcServer, &service{
		logger: logger,
		store:  stores,
		token:  strings.TrimSpace(token),
	})
}

type service struct {
	nodeagentv1.UnimplementedNodeAgentServiceServer

	logger *slog.Logger
	store  *store.Store
	token  string
}

func (s *service) requireNodeAgentToken(ctx context.Context) error {
	if s.token == "" {
		return status.Error(codes.Unavailable, "node agent token is not configured")
	}
	secret, ok := transport.BearerFromIncomingContext(ctx)
	if !ok || !transport.BearerMatches(secret, s.token) {
		return status.Error(codes.Unauthenticated, "invalid node agent token")
	}
	return nil
}

func (s *service) RegisterNode(ctx context.Context, req *nodeagentv1.RegisterNodeRequest) (*nodeagentv1.RegisterNodeResponse, error) {
	if err := s.requireNodeAgentToken(ctx); err != nil {
		return nil, err
	}

	registered, err := s.store.RegisterNode(ctx, cloudmodel.RegisterInput{
		Provider:      req.GetProvider(),
		Region:        req.GetRegion(),
		Name:          req.GetName(),
		PrivateIP:     req.GetPrivateIp(),
		InstanceID:    req.GetInstanceId(),
		InstanceType:  req.GetInstanceType(),
		CPUMilliTotal: int(req.GetCpuMilliTotal()),
		MemoryMiTotal: int(req.GetMemoryMiTotal()),
	})
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	return &nodeagentv1.RegisterNodeResponse{
		NodeId: registered.ID,
	}, nil
}

func (s *service) RecordHeartbeat(ctx context.Context, req *nodeagentv1.HeartbeatRequest) (*nodeagentv1.HeartbeatResponse, error) {
	nodeID := strings.TrimSpace(req.GetNodeId())
	if nodeID == "" {
		return nil, status.Error(codes.InvalidArgument, "nodeID is required")
	}
	if err := s.requireNodeAgentToken(ctx); err != nil {
		return nil, err
	}
	observedStatus, err := s.store.RecordNodeHeartbeat(ctx, nodeID, cloudmodel.HeartbeatInput{
		CPUMilliAllocatable: int(req.GetCpuMilliAllocatable()),
		MemoryMiAllocatable: int(req.GetMemoryMiAllocatable()),
	})
	if err != nil {
		if errors.Is(err, store.ErrNodeNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	return &nodeagentv1.HeartbeatResponse{
		NodeId:         nodeID,
		ObservedStatus: observedStatus,
	}, nil
}

func (s *service) PollWork(ctx context.Context, req *nodeagentv1.PollWorkRequest) (*nodeagentv1.PollWorkResponse, error) {
	nodeID := strings.TrimSpace(req.GetNodeId())
	if nodeID == "" {
		return nil, status.Error(codes.InvalidArgument, "nodeID is required")
	}
	if err := s.requireNodeAgentToken(ctx); err != nil {
		return nil, err
	}

	item, err := s.store.CreateExecutionClaim(ctx, nodeID)
	if err != nil {
		s.logger.Error("claim execution work failed", "node_id", nodeID, "error", err)
		return nil, status.Error(codes.Internal, "internal server error")
	}
	if item == nil {
		return &nodeagentv1.PollWorkResponse{}, nil
	}

	projectedFiles := make([]*nodeagentv1.ProjectedFile, 0, len(item.ProjectedFiles))
	for _, file := range workload.CloneProjectedFiles(item.ProjectedFiles) {
		projectedFiles = append(projectedFiles, &nodeagentv1.ProjectedFile{
			MountPath: file.MountPath,
			Content:   file.Content,
			Mode:      file.Mode,
			Sensitive: file.Sensitive,
		})
	}

	work := &nodeagentv1.WorkItem{
		Action:         item.Action,
		ExecutionId:    item.ExecutionID,
		PlanId:         item.PlanID,
		NodeId:         item.NodeID,
		ServiceId:      item.ServiceID,
		ServiceName:    item.ServiceName,
		Image:          item.Image,
		Command:        append([]string(nil), item.Command...),
		Args:           append([]string(nil), item.Args...),
		Env:            maps.Clone(item.Env),
		ProjectedFiles: projectedFiles,
		ContainerPort:  int32(item.ContainerPort),
		ReadinessPath:  item.ReadinessPath,
		ContainerName:  item.ContainerName,
		ContainerId:    item.ContainerID,
		HostPort:       int32(item.HostPort),
	}
	if item.ImageCredential != nil {
		work.ImageCredential = &nodeagentv1.ImageCredential{
			Server:   item.ImageCredential.Server,
			Username: item.ImageCredential.Username,
			Password: item.ImageCredential.Password,
		}
	}
	if item.SupersededExecution != nil {
		work.SupersededExecution = &nodeagentv1.SupersededExecution{
			PlanId:        item.SupersededExecution.PlanID,
			ExecutionId:   item.SupersededExecution.ExecutionID,
			ContainerId:   item.SupersededExecution.ContainerID,
			ContainerName: item.SupersededExecution.ContainerName,
		}
	}
	return &nodeagentv1.PollWorkResponse{Item: work}, nil
}

func (s *service) ReportExecution(ctx context.Context, req *nodeagentv1.ReportExecutionRequest) (*nodeagentv1.ReportExecutionResponse, error) {
	nodeID := strings.TrimSpace(req.GetNodeId())
	if nodeID == "" {
		return nil, status.Error(codes.InvalidArgument, "nodeID is required")
	}
	if err := s.requireNodeAgentToken(ctx); err != nil {
		return nil, err
	}

	executionID := strings.TrimSpace(req.GetExecutionId())
	if executionID == "" {
		return nil, status.Error(codes.InvalidArgument, "executionID is required")
	}

	ack, err := s.store.UpdateExecutionFromNodeReport(ctx, nodeID, executionID, cloudmodel.ReportInput{
		Status:                req.GetStatus(),
		Reason:                req.GetReason(),
		ContainerID:           req.GetContainerId(),
		ContainerName:         req.GetContainerName(),
		HostPort:              int(req.GetHostPort()),
		SupersededExecutionID: req.GetSupersededExecutionId(),
	})
	if err != nil {
		if errors.Is(err, store.ErrExecutionNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	execution := ack.Execution
	var finishedAt *timestamppb.Timestamp
	if execution.FinishedAt != nil && !execution.FinishedAt.IsZero() {
		finishedAt = timestamppb.New(execution.FinishedAt.UTC())
	}

	return &nodeagentv1.ReportExecutionResponse{
		Ack: &nodeagentv1.ReportExecutionAck{
			Execution: &nodeagentv1.ExecutionRecord{
				Id:            execution.ID,
				PlanId:        execution.PlanID,
				NodeId:        execution.NodeID,
				Image:         execution.Image,
				ContainerName: execution.ContainerName,
				ContainerId:   execution.ContainerID,
				ContainerPort: int32(execution.ContainerPort),
				HostPort:      int32(execution.HostPort),
				ReadinessPath: execution.ReadinessPath,
				Status:        execution.Status,
				StatusReason:  execution.StatusReason,
				StartedAt:     timestamppb.New(execution.StartedAt.UTC()),
				FinishedAt:    finishedAt,
				CreatedAt:     timestamppb.New(execution.CreatedAt.UTC()),
				UpdatedAt:     timestamppb.New(execution.UpdatedAt.UTC()),
			},
			ObservedAt: timestamppb.New(ack.ObservedAt.UTC()),
		},
	}, nil
}
