package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"

	nodeagentv1 "mini-cloud/internal/gen/proto/minicloud/nodeagent/v1"
	"mini-cloud/internal/logctx"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"
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
func (c *Client) RegisterNode(ctx context.Context, input *nodeagentv1.RegisterNodeRequest) (*nodeagentv1.RegisterNodeResponse, error) {
	client, err := c.grpcClient()
	if err != nil {
		return nil, err
	}
	resp, err := client.RegisterNode(withOutgoingMetadata(ctx, c.bootstrapToken), input)
	if err != nil {
		return nil, grpcControlError(err, operationRegisterNode)
	}
	if resp == nil {
		return nil, errors.New("invalid register response: empty response")
	}
	if strings.TrimSpace(resp.GetNodeId()) == "" || strings.TrimSpace(resp.GetSessionToken()) == "" || resp.GetAcceptedAt() == nil {
		return nil, errors.New("invalid register response")
	}
	c.SetSessionToken(resp.GetSessionToken())
	return resp, nil
}

// SendHeartbeat 使用节点会话令牌向控制面上报一次节点心跳。
func (c *Client) SendHeartbeat(ctx context.Context, input *nodeagentv1.HeartbeatRequest) (*nodeagentv1.HeartbeatResponse, error) {
	client, err := c.grpcClient()
	if err != nil {
		return nil, err
	}
	resp, err := client.RecordHeartbeat(withOutgoingMetadata(ctx, c.getSessionToken()), input)
	if err != nil {
		return nil, grpcControlError(err, operationSendHeartbeat)
	}
	if resp == nil {
		return nil, errors.New("invalid heartbeat response: empty response")
	}
	if strings.TrimSpace(resp.GetNodeId()) == "" || strings.TrimSpace(resp.GetObservedStatus()) == "" || resp.GetReceivedAt() == nil {
		return nil, errors.New("invalid heartbeat response")
	}
	return resp, nil
}

// PollExecutionWork 从控制面拉取当前节点的下一项执行任务。
func (c *Client) PollExecutionWork(ctx context.Context, nodeID string) (*nodeagentv1.WorkItem, error) {
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
	return resp.GetItem(), nil
}

// ReportExecution 向控制面上报指定执行的运行中或终态结果。
func (c *Client) ReportExecution(ctx context.Context, input *nodeagentv1.ReportExecutionRequest) (*nodeagentv1.ReportExecutionResponse, error) {
	client, err := c.grpcClient()
	if err != nil {
		return nil, err
	}
	resp, err := client.ReportExecution(withOutgoingMetadata(ctx, c.getSessionToken()), input)
	if err != nil {
		return nil, grpcControlError(err, operationReportExecution)
	}
	if resp == nil {
		return nil, errors.New("invalid report execution response: empty response")
	}
	if resp.GetAck() == nil || resp.GetAck().GetExecution() == nil || strings.TrimSpace(resp.GetAck().GetExecution().GetId()) == "" {
		return nil, errors.New("invalid report execution response")
	}
	return resp, nil
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
