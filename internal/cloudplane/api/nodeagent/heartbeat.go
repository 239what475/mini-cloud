package nodeagent

import (
	"context"
	"errors"
	"strings"

	"mini-cloud/internal/cloudplane/infra/store"
	cloudmodel "mini-cloud/internal/cloudplane/model"
	nodeagentv1 "mini-cloud/internal/gen/proto/minicloud/nodeagent/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// RecordHeartbeat 记录 node-agent 周期性心跳和可调度容量。
// 参数说明：ctx 控制本次 gRPC 请求生命周期并携带 metadata；req 表示请求参数。
func (s *service) RecordHeartbeat(ctx context.Context, req *nodeagentv1.HeartbeatRequest) (*nodeagentv1.HeartbeatResponse, error) {
	// 心跳必须带 nodeID，session token 也必须绑定到同一个 nodeID。
	nodeID := strings.TrimSpace(req.GetNodeId())
	if nodeID == "" {
		return nil, status.Error(codes.InvalidArgument, "nodeID is required")
	}
	// 校验 node-agent session token，并确认 token 归属的 node 与请求 nodeID 一致。
	if err := s.requireNodeAgentSession(ctx, nodeID); err != nil {
		return nil, err
	}
	if req.GetReportedAt() == nil {
		return nil, status.Error(codes.InvalidArgument, "reportedAt is required")
	}

	// 写入心跳摘要，同时更新 node 的 allocatable 容量和最新状态。
	summary, receivedAt, err := s.store.RecordNodeHeartbeat(ctx, nodeID, cloudmodel.HeartbeatInput{
		ReportedAt:          req.GetReportedAt().AsTime(),
		AgentVersion:        req.GetAgentVersion(),
		CPUMilliAllocatable: int(req.GetCpuMilliAllocatable()),
		MemoryMiAllocatable: int(req.GetMemoryMiAllocatable()),
		RunningContainers:   int(req.GetRunningContainers()),
		Status:              req.GetStatus(),
	})
	if err != nil {
		// node 不存在返回 NotFound；除 NotFound 外当前统一映射为 InvalidArgument。
		switch {
		case errors.Is(err, store.ErrNodeNotFound):
			return nil, status.Error(codes.NotFound, err.Error())
		default:
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
	}

	// 返回 accepted 标记、node-agent 本次上报状态和 cloud-plane 接收时间。
	// ObservedStatus 当前反映心跳输入状态，不一定等于 nodes 表最终合成后的持久化状态。
	return &nodeagentv1.HeartbeatResponse{
		NodeId:         nodeID,
		Accepted:       true,
		ObservedStatus: summary.Status,
		ReceivedAt:     timestamppb.New(receivedAt.UTC()),
	}, nil
}
