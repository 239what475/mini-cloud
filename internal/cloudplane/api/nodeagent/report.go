package nodeagent

import (
	"context"
	"errors"
	"strings"
	"time"

	"mini-cloud/internal/cloudplane/infra/store"
	cloudmodel "mini-cloud/internal/cloudplane/model"
	nodeagentv1 "mini-cloud/internal/gen/proto/minicloud/nodeagent/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ReportExecution 接受 node-agent 对单个 execution 的执行结果上报。
// 参数说明：ctx 控制本次 gRPC 请求生命周期并携带 metadata；req 表示请求参数。
func (s *service) ReportExecution(ctx context.Context, req *nodeagentv1.ReportExecutionRequest) (*nodeagentv1.ReportExecutionResponse, error) {
	// 上报结果必须带 nodeID，session token 也必须绑定到同一个 nodeID。
	nodeID := strings.TrimSpace(req.GetNodeId())
	if nodeID == "" {
		return nil, status.Error(codes.InvalidArgument, "nodeID is required")
	}
	// 校验 node-agent session token，防止一个节点上报另一个节点的 execution。
	if err := s.requireNodeAgentSession(ctx, nodeID); err != nil {
		return nil, err
	}

	// executionID 定位本次上报对应的执行记录。
	executionID := strings.TrimSpace(req.GetExecutionId())
	if executionID == "" {
		return nil, status.Error(codes.InvalidArgument, "executionID is required")
	}

	// 写入 execution 结果；该 store 事务边界方法会在同一事务内推进 execution intent 和 node allocation。
	ack, _, _, err := s.store.UpdateExecutionFromNodeReport(ctx, nodeID, executionID, cloudmodel.ReportInput{
		Status:                req.GetStatus(),
		Reason:                req.GetReason(),
		ContainerID:           req.GetContainerId(),
		ContainerName:         req.GetContainerName(),
		HostPort:              int(req.GetHostPort()),
		SupersededExecutionID: req.GetSupersededExecutionId(),
	})
	if err != nil {
		// execution 不存在返回 NotFound；除 NotFound 外当前统一映射为 InvalidArgument。
		switch {
		case errors.Is(err, store.ErrExecutionNotFound):
			return nil, status.Error(codes.NotFound, err.Error())
		default:
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
	}
	// 返回持久化后的 execution 记录和 cloud-plane 观测时间。
	return &nodeagentv1.ReportExecutionResponse{
		Ack: &nodeagentv1.ReportExecutionAck{
			Execution:  protoExecutionRecord(ack.Execution),
			ObservedAt: timestamppb.New(ack.ObservedAt.UTC()),
		},
	}, nil
}

// protoExecutionRecord 将 cloud-plane execution 记录转换为 node-agent protobuf ack。
// 参数说明：item 是本次上报后持久化的 execution 记录。
func protoExecutionRecord(item cloudmodel.Record) *nodeagentv1.ExecutionRecord {
	// 时间字段统一转成 protobuf Timestamp；FinishedAt 允许为空。
	return &nodeagentv1.ExecutionRecord{
		// 身份字段用于 node-agent 关联本次 ack 对应的 execution plan/node。
		Id:     item.ID,
		PlanId: item.PlanID,
		NodeId: item.NodeID,
		// 运行字段回显容器镜像、容器身份和端口信息，均来自持久化后的 execution 记录。
		Image:         item.Image,
		ContainerName: item.ContainerName,
		ContainerId:   item.ContainerID,
		ContainerPort: int32(item.ContainerPort),
		HostPort:      int32(item.HostPort),
		ReadinessPath: item.ReadinessPath,
		// 状态字段描述 cloud-plane 接受上报后的最终记录状态。
		Status:       item.Status,
		StatusReason: item.StatusReason,
		// 时间字段由 helper 统一处理 UTC 和 nil/zero 值。
		StartedAt:  timestamppb.New(item.StartedAt.UTC()),
		FinishedAt: optionalTimestamp(item.FinishedAt),
		CreatedAt:  timestamppb.New(item.CreatedAt.UTC()),
		UpdatedAt:  timestamppb.New(item.UpdatedAt.UTC()),
	}
}

// optionalTimestamp 将可空 time 指针转换为 protobuf Timestamp。
// 参数说明：value 是可能为空或零值的时间。
func optionalTimestamp(value *time.Time) *timestamppb.Timestamp {
	// nil 或零值都表示业务上未设置该时间。
	if value == nil || value.IsZero() {
		return nil
	}
	// 非空时间统一转为 UTC 后写入响应。
	return timestamppb.New(value.UTC())
}
