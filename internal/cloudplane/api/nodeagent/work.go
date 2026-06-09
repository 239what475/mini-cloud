package nodeagent

import (
	"context"
	"maps"
	"strings"

	cloudmodel "mini-cloud/internal/cloudplane/model"
	"mini-cloud/internal/common/projectedfile"
	nodeagentv1 "mini-cloud/internal/gen/proto/minicloud/nodeagent/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// PollWork 让 node-agent 拉取分配给当前 node 的一个 execution work item。
// 参数说明：ctx 控制本次 gRPC 请求生命周期并携带 metadata；req 表示请求参数。
func (s *service) PollWork(ctx context.Context, req *nodeagentv1.PollWorkRequest) (*nodeagentv1.PollWorkResponse, error) {
	// 拉取任务必须带 nodeID，session token 也必须绑定到同一个 nodeID。
	nodeID := strings.TrimSpace(req.GetNodeId())
	if nodeID == "" {
		return nil, status.Error(codes.InvalidArgument, "nodeID is required")
	}
	// 校验 node-agent session token，防止一个节点拉取另一个节点的任务。
	if err := s.requireNodeAgentSession(ctx, nodeID); err != nil {
		return nil, err
	}

	// store 的事务边界方法以 nodeID 为并发边界领取并组装一个待执行任务；没有任务时 item 可以为空。
	item, err := s.store.CreateExecutionClaim(ctx, nodeID)
	if err != nil {
		s.logger.Error("claim execution work failed", "node_id", nodeID, "error", err)
		return nil, status.Error(codes.Internal, "internal server error")
	}
	// item 为 nil 时 protoWorkItem 返回 nil，表示当前没有可领取任务。
	return &nodeagentv1.PollWorkResponse{Item: protoWorkItem(item)}, nil
}

// protoWorkItem 将 cloud-plane execution work item 转换为 node-agent protobuf 响应。
// 参数说明：item 是分配给某个 node-agent 的待执行任务；nil 表示暂无任务。
func protoWorkItem(item *cloudmodel.WorkItem) *nodeagentv1.WorkItem {
	// nil work item 映射为 nil protobuf message，让 PollWork 表达“当前无任务”。
	if item == nil {
		return nil
	}
	// 基础字段描述 execution plan、service 和目标 node。
	out := &nodeagentv1.WorkItem{
		Action:      item.Action,
		ExecutionId: item.ExecutionID,
		PlanId:      item.PlanID,
		NodeId:      item.NodeID,
		ServiceId:   item.ServiceID,
		ServiceName: item.ServiceName,
		Image:       item.Image,
		// 可变容器复制后返回，避免 node-agent 响应共享领域对象底层数据。
		Command: append([]string(nil), item.Command...),
		Args:    append([]string(nil), item.Args...),
		// Env 可能包含由 secret set 渲染出的敏感值；这里随 gRPC 响应明文下发，依赖 node session 鉴权和传输层保护。
		Env:            cloneStringMap(item.Env),
		ProjectedFiles: protoProjectedFiles(item.ProjectedFiles),
		ContainerPort:  int32(item.ContainerPort),
		ReadinessPath:  item.ReadinessPath,
		ContainerName:  item.ContainerName,
		ContainerId:    item.ContainerID,
		HostPort:       int32(item.HostPort),
	}
	// 当 work item 携带镜像凭据时下发给执行节点；这里会包含密码明文，不做字段级加密或脱敏。
	if item.ImageCredential != nil {
		out.ImageCredential = &nodeagentv1.ImageCredential{
			Server:   item.ImageCredential.Server,
			Username: item.ImageCredential.Username,
			Password: item.ImageCredential.Password,
		}
	}
	// 如果新 execution 替换旧 execution，把旧容器信息一并下发给 node-agent 清理。
	if item.SupersededExecution != nil {
		out.SupersededExecution = &nodeagentv1.SupersededExecution{
			PlanId:        item.SupersededExecution.PlanID,
			ExecutionId:   item.SupersededExecution.ExecutionID,
			ContainerId:   item.SupersededExecution.ContainerID,
			ContainerName: item.SupersededExecution.ContainerName,
		}
	}
	return out
}

// cloneStringMap 复制 string map，避免共享可变引用。
// 参数说明：input 是待复制的字符串键值映射。
func cloneStringMap(input map[string]string) map[string]string {
	// 空 map 和 nil map 都返回新的空 map，便于调用方直接写入而不用判 nil。
	if len(input) == 0 {
		return map[string]string{}
	}
	// 按输入容量创建新 map，逐项复制 key/value。
	out := make(map[string]string, len(input))
	maps.Copy(out, input)
	return out
}

// protoProjectedFiles 将已渲染 projected file 列表转换为 node-agent protobuf。
// 参数说明：items 是需要写入容器文件系统的文件内容集合。
func protoProjectedFiles(items []projectedfile.File) []*nodeagentv1.ProjectedFile {
	// 空列表返回 nil，表示该 work item 没有 projected files。
	if len(items) == 0 {
		return nil
	}
	// CloneFiles 会复制、规范化并按 MountPath 排序；输出顺序不是原始输入顺序。
	out := make([]*nodeagentv1.ProjectedFile, 0, len(items))
	for _, item := range projectedfile.CloneFiles(items) {
		// Content 可能包含 secret 渲染结果；这里随 gRPC 响应明文下发给目标 node-agent，依赖 session 鉴权和传输层保护。
		out = append(out, &nodeagentv1.ProjectedFile{
			MountPath: item.MountPath,
			Content:   item.Content,
			Mode:      item.Mode,
			Sensitive: item.Sensitive,
		})
	}
	return out
}
