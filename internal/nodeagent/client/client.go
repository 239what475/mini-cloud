package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"mini-cloud/internal/common/logctx"
	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/projectedfile"
	"mini-cloud/internal/contract/nodeagentapi"
	nodeagentv1 "mini-cloud/internal/gen/proto/minicloud/nodeagent/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Client 是 node-agent 访问控制面 NodeAgentService 的 gRPC 客户端。
type Client struct {
	// target 是 gRPC dial 使用的 host 或 passthrough 目标。
	target string
	// bootstrapToken 是节点注册前使用的启动令牌。
	bootstrapToken string
	// sessionToken 是注册成功后用于心跳、拉取任务和上报结果的节点会话令牌。
	sessionToken string
	// dialer 是测试可注入的 gRPC 底层连接建立函数。
	dialer func(context.Context, string) (net.Conn, error)

	// mu 保护 sessionToken 和 conn 的并发访问。
	mu sync.Mutex
	// conn 是复用的 gRPC 客户端连接。
	conn *grpc.ClientConn
}

// Config 配置 node-agent 控制面客户端。
type Config struct {
	// ServerURL 是控制面的内网明文 gRPC 地址，可使用 host:port 或 http://host:port。
	ServerURL string
	// BootstrapToken 是首次注册节点时发送的启动令牌。
	BootstrapToken string
	// Dialer 覆盖 gRPC 底层连接建立函数，主要用于测试内存连接。
	Dialer func(context.Context, string) (net.Conn, error)
}

const (
	// operationRegisterNode 标识节点注册请求。
	operationRegisterNode = "register node"
	// operationSendHeartbeat 标识节点心跳请求。
	operationSendHeartbeat = "send heartbeat"
	// operationPollExecutionWork 标识执行任务拉取请求。
	operationPollExecutionWork = "poll execution work"
	// operationReportExecution 标识执行结果上报请求。
	operationReportExecution = "report execution"
)

// ControlError 表示控制面 gRPC status 错误。
type ControlError struct {
	// Operation 是失败的控制面操作名称。
	Operation string
	// Code 是控制面返回的 gRPC status code。
	Code codes.Code
	// Message 是 gRPC status message。
	Message string
}

// Error 返回包含控制面操作和 gRPC 错误信息的字符串。
func (e *ControlError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return fmt.Sprintf("%s failed: %s", e.Operation, e.Message)
	}
	return fmt.Sprintf("%s failed with grpc code %s", e.Operation, e.Code)
}

// New 根据控制面地址和启动令牌创建客户端。
func New(cfg Config) *Client {
	return &Client{
		target:         normalizeTarget(cfg.ServerURL),
		bootstrapToken: strings.TrimSpace(cfg.BootstrapToken),
		dialer:         cfg.Dialer,
	}
}

// SetSessionToken 设置后续节点级请求使用的会话令牌。
func (c *Client) SetSessionToken(secret string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessionToken = strings.TrimSpace(secret)
}

// getSessionToken 返回当前会话令牌。
func (c *Client) getSessionToken() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionToken
}

// Close 关闭当前 gRPC 连接。
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	conn := c.conn
	c.conn = nil
	return conn.Close()
}

// IsInvalidSession 判断错误是否表示本地节点会话不再被控制面接受。
func IsInvalidSession(err error) bool {
	var controlErr *ControlError
	if !errors.As(err, &controlErr) {
		return false
	}
	switch controlErr.Code {
	case codes.Unauthenticated, codes.PermissionDenied:
		return true
	case codes.NotFound:
		// NotFound 只有在节点级请求上才表示本地节点身份已失效。
		// 例如 report execution 的 NotFound 可能只是执行不存在，不能清空节点会话。
		return controlErr.Operation == operationSendHeartbeat ||
			controlErr.Operation == operationPollExecutionWork
	default:
		return false
	}
}

// RegisterNode 使用启动令牌向控制面注册节点，并保存返回的会话令牌。
func (c *Client) RegisterNode(ctx context.Context, input nodeagentapi.RegisterNodeRequest) (nodeagentapi.RegisterNodeResponse, error) {
	client, err := c.grpcClient()
	if err != nil {
		return nodeagentapi.RegisterNodeResponse{}, err
	}
	resp, err := client.RegisterNode(withOutgoingMetadata(ctx, c.bootstrapToken), &nodeagentv1.RegisterNodeRequest{
		Provider:      input.Provider,
		Region:        input.Region,
		Name:          input.Name,
		PrivateIp:     input.PrivateIP,
		PublicIp:      input.PublicIP,
		InstanceId:    input.InstanceID,
		InstanceType:  input.InstanceType,
		CpuMilliTotal: int32(input.CPUMilliTotal),
		MemoryMiTotal: int32(input.MemoryMiTotal),
	})
	if err != nil {
		return nodeagentapi.RegisterNodeResponse{}, grpcControlError(err, operationRegisterNode)
	}
	if resp == nil {
		return nodeagentapi.RegisterNodeResponse{}, errors.New("invalid register response: empty response")
	}
	out := nodeagentapi.RegisterNodeResponse{
		NodeID:         resp.GetNodeId(),
		SessionToken:   resp.GetSessionToken(),
		ObservedStatus: resp.GetObservedStatus(),
		AcceptedAt:     timestampAsTime(resp.GetAcceptedAt()),
	}
	if err := out.Validate(); err != nil {
		return nodeagentapi.RegisterNodeResponse{}, fmt.Errorf("invalid register response: %w", err)
	}
	c.SetSessionToken(out.SessionToken)
	return out, nil
}

// SendHeartbeat 使用节点会话令牌向控制面上报一次节点心跳。
func (c *Client) SendHeartbeat(ctx context.Context, nodeID string, input nodeagentapi.HeartbeatRequest) (nodeagentapi.HeartbeatResponse, error) {
	client, err := c.grpcClient()
	if err != nil {
		return nodeagentapi.HeartbeatResponse{}, err
	}
	resp, err := client.RecordHeartbeat(withOutgoingMetadata(ctx, c.getSessionToken()), &nodeagentv1.HeartbeatRequest{
		NodeId:              nodeID,
		ReportedAt:          timestampOrNil(input.ReportedAt),
		AgentVersion:        input.AgentVersion,
		CpuMilliAllocatable: int32(input.CPUMilliAllocatable),
		MemoryMiAllocatable: int32(input.MemoryMiAllocatable),
		RunningContainers:   int32(input.RunningContainers),
		Status:              input.Status,
	})
	if err != nil {
		return nodeagentapi.HeartbeatResponse{}, grpcControlError(err, operationSendHeartbeat)
	}
	if resp == nil {
		return nodeagentapi.HeartbeatResponse{}, errors.New("invalid heartbeat response: empty response")
	}
	out := nodeagentapi.HeartbeatResponse{
		NodeID:         resp.GetNodeId(),
		Accepted:       resp.GetAccepted(),
		ObservedStatus: resp.GetObservedStatus(),
		ReceivedAt:     timestampAsTime(resp.GetReceivedAt()),
	}
	if err := out.Validate(); err != nil {
		return nodeagentapi.HeartbeatResponse{}, fmt.Errorf("invalid heartbeat response: %w", err)
	}
	return out, nil
}

// PollExecutionWork 从控制面拉取当前节点的下一项执行任务。
func (c *Client) PollExecutionWork(ctx context.Context, nodeID string) (*nodeagentapi.WorkItem, error) {
	client, err := c.grpcClient()
	if err != nil {
		return nil, err
	}
	resp, err := client.PollWork(withOutgoingMetadata(ctx, c.getSessionToken()), &nodeagentv1.PollWorkRequest{
		NodeId: nodeID,
	})
	if err != nil {
		return nil, grpcControlError(err, operationPollExecutionWork)
	}
	if resp == nil {
		return nil, errors.New("invalid work response: empty response")
	}
	item := contractWorkItem(resp.GetItem())
	if item != nil {
		if err := item.Validate(); err != nil {
			return nil, fmt.Errorf("invalid work response: %w", err)
		}
	}
	return item, nil
}

// ReportExecution 向控制面上报指定执行的运行中或终态结果。
func (c *Client) ReportExecution(ctx context.Context, nodeID string, executionID string, input nodeagentapi.ReportExecutionRequest) (nodeagentapi.ReportExecutionResponse, error) {
	client, err := c.grpcClient()
	if err != nil {
		return nodeagentapi.ReportExecutionResponse{}, err
	}
	resp, err := client.ReportExecution(withOutgoingMetadata(ctx, c.getSessionToken()), &nodeagentv1.ReportExecutionRequest{
		NodeId:                nodeID,
		ExecutionId:           executionID,
		Status:                input.Status,
		Reason:                input.Reason,
		ContainerId:           input.ContainerID,
		ContainerName:         input.ContainerName,
		HostPort:              int32(input.HostPort),
		SupersededExecutionId: input.SupersededExecutionID,
	})
	if err != nil {
		return nodeagentapi.ReportExecutionResponse{}, grpcControlError(err, operationReportExecution)
	}
	if resp == nil {
		return nodeagentapi.ReportExecutionResponse{}, errors.New("invalid report execution response: empty response")
	}
	out := nodeagentapi.ReportExecutionResponse{
		Ack: nodeagentapi.ReportExecutionAck{
			Execution:  contractExecutionRecord(resp.GetAck().GetExecution()),
			ObservedAt: timestampAsTime(resp.GetAck().GetObservedAt()),
		},
	}
	if err := out.Validate(); err != nil {
		return nodeagentapi.ReportExecutionResponse{}, fmt.Errorf("invalid report execution response: %w", err)
	}
	return out, nil
}

// grpcClient 返回基于复用 ClientConn 的 NodeAgentService gRPC stub。
func (c *Client) grpcClient() (nodeagentv1.NodeAgentServiceClient, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return nodeagentv1.NewNodeAgentServiceClient(c.conn), nil
	}

	dialOptions := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if c.dialer != nil {
		dialOptions = append(dialOptions, grpc.WithContextDialer(c.dialer))
	}

	target := c.target
	if c.dialer != nil && !strings.Contains(target, "://") {
		target = "passthrough:///" + target
	}
	conn, err := grpc.NewClient(target, dialOptions...)
	if err != nil {
		return nil, fmt.Errorf("dial grpc server: %w", err)
	}
	c.conn = conn
	return nodeagentv1.NewNodeAgentServiceClient(conn), nil
}

// withOutgoingMetadata 在 gRPC metadata 中追加 request id 和可选 bearer token。
func withOutgoingMetadata(ctx context.Context, bearerToken string) context.Context {
	requestID := logctx.EnsureRequestID(logctx.RequestID(ctx))
	ctx = logctx.WithFields(ctx, logctx.Fields{RequestID: requestID})

	pairs := []string{
		strings.ToLower(logctx.HeaderRequestID), requestID,
	}
	if token := strings.TrimSpace(bearerToken); token != "" {
		pairs = append(pairs, "authorization", "Bearer "+token)
	}
	return metadata.AppendToOutgoingContext(ctx, pairs...)
}

// grpcControlError 将 gRPC status error 转换为控制面语义错误。
func grpcControlError(err error, operation string) error {
	st, ok := grpcstatus.FromError(err)
	if !ok {
		return err
	}
	return &ControlError{
		Operation: operation,
		Code:      st.Code(),
		Message:   st.Message(),
	}
}

// normalizeTarget 从控制面服务地址中提取明文 gRPC dial target。
func normalizeTarget(serverURL string) string {
	trimmed := strings.TrimSpace(serverURL)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return strings.TrimPrefix(trimmed, "http://")
	}
	return parsed.Host
}

// timestampOrNil 将非零 time.Time 转为 protobuf Timestamp，零值返回 nil。
func timestampOrNil(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value.UTC())
}

// timestampAsTime 将 protobuf Timestamp 转为 time.Time，nil 返回零值时间。
func timestampAsTime(value *timestamppb.Timestamp) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.AsTime()
}

// contractWorkItem 将 protobuf WorkItem 转换为 nodeagentapi 契约模型。
func contractWorkItem(item *nodeagentv1.WorkItem) *nodeagentapi.WorkItem {
	if item == nil {
		return nil
	}
	out := &nodeagentapi.WorkItem{
		ExecutionID:    item.GetExecutionId(),
		DeploymentID:   item.GetDeploymentId(),
		ReplicaIndex:   int(item.GetReplicaIndex()),
		NodeID:         item.GetNodeId(),
		ProjectID:      item.GetProjectId(),
		ServiceID:      item.GetServiceId(),
		ServiceName:    item.GetServiceName(),
		RevisionID:     item.GetRevisionId(),
		RevisionLabel:  item.GetRevisionLabel(),
		Image:          item.GetImage(),
		Command:        append([]string(nil), item.GetCommand()...),
		Args:           append([]string(nil), item.GetArgs()...),
		Env:            cloneStringMap(item.GetEnv()),
		ProjectedFiles: contractProjectedFiles(item.GetProjectedFiles()),
		PersistentDirs: contractPersistentDirs(item.GetPersistentDirs()),
		ContainerPort:  int(item.GetContainerPort()),
		ReadinessPath:  item.GetReadinessPath(),
		ContainerName:  item.GetContainerName(),
	}
	if item.GetImageCredential() != nil {
		out.ImageCredential = &nodeagentapi.ImageCredential{
			Server:   item.GetImageCredential().GetServer(),
			Username: item.GetImageCredential().GetUsername(),
			Password: item.GetImageCredential().GetPassword(),
		}
	}
	if item.GetSupersededExecution() != nil {
		out.SupersededExecution = &nodeagentapi.SupersededExecution{
			DeploymentID:  item.GetSupersededExecution().GetDeploymentId(),
			ExecutionID:   item.GetSupersededExecution().GetExecutionId(),
			ContainerID:   item.GetSupersededExecution().GetContainerId(),
			ContainerName: item.GetSupersededExecution().GetContainerName(),
		}
	}
	return out
}

// contractProjectedFiles 将 protobuf projected file 列表转换为内部契约模型。
func contractProjectedFiles(items []*nodeagentv1.ProjectedFile) []projectedfile.File {
	if len(items) == 0 {
		return nil
	}
	out := make([]projectedfile.File, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, projectedfile.File{
			MountPath: item.GetMountPath(),
			Content:   item.GetContent(),
			Mode:      item.GetMode(),
			Sensitive: item.GetSensitive(),
		})
	}
	return projectedfile.CloneFiles(out)
}

// contractPersistentDirs 将 protobuf persistent dir mount 列表转换为内部契约模型。
func contractPersistentDirs(items []*nodeagentv1.PersistentDirMount) []persistentdir.Mount {
	if len(items) == 0 {
		return nil
	}
	out := make([]persistentdir.Mount, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, persistentdir.Mount{
			Name:       item.GetName(),
			MountPath:  item.GetMountPath(),
			SourcePath: item.GetSourcePath(),
		})
	}
	return persistentdir.CloneMounts(out)
}

// contractExecutionRecord 将 protobuf ExecutionRecord 转换为 nodeagentapi 契约模型。
func contractExecutionRecord(item *nodeagentv1.ExecutionRecord) nodeagentapi.ExecutionRecord {
	if item == nil {
		return nodeagentapi.ExecutionRecord{}
	}
	var finishedAt *time.Time
	if item.GetFinishedAt() != nil {
		value := timestampAsTime(item.GetFinishedAt())
		finishedAt = &value
	}
	return nodeagentapi.ExecutionRecord{
		ID:            item.GetId(),
		DeploymentID:  item.GetDeploymentId(),
		NodeID:        item.GetNodeId(),
		Image:         item.GetImage(),
		ContainerName: item.GetContainerName(),
		ContainerID:   item.GetContainerId(),
		ContainerPort: int(item.GetContainerPort()),
		HostPort:      int(item.GetHostPort()),
		ReadinessPath: item.GetReadinessPath(),
		Status:        item.GetStatus(),
		StatusReason:  item.GetStatusReason(),
		StartedAt:     timestampAsTime(item.GetStartedAt()),
		FinishedAt:    finishedAt,
		CreatedAt:     timestampAsTime(item.GetCreatedAt()),
		UpdatedAt:     timestampAsTime(item.GetUpdatedAt()),
	}
}

// cloneStringMap 复制字符串 map，空输入返回空 map。
func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
