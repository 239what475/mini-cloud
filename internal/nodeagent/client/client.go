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
	"mini-cloud/internal/transport"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"
)

type Client struct {
	target string
	token  string
	dialer func(context.Context, string) (net.Conn, error)

	mu   sync.Mutex
	conn *grpc.ClientConn
}

type Config struct {
	ServerURL string
	Token     string
	Dialer    func(context.Context, string) (net.Conn, error)
}

const (
	operationRegisterNode      = "register node"
	operationSendHeartbeat     = "send heartbeat"
	operationPollExecutionWork = "poll execution work"
	operationReportExecution   = "report execution"
)

type CloudPlaneError struct {
	Operation string
	Code      codes.Code
	Message   string
}

func (e *CloudPlaneError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return fmt.Sprintf("%s failed: %s", e.Operation, e.Message)
	}
	return fmt.Sprintf("%s failed with grpc code %s", e.Operation, e.Code)
}

func New(cfg Config) *Client {
	return &Client{
		target: normalizeTarget(cfg.ServerURL),
		token:  strings.TrimSpace(cfg.Token),
		dialer: cfg.Dialer,
	}
}

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

func IsUnknownNode(err error) bool {
	var controlErr *CloudPlaneError
	if !errors.As(err, &controlErr) {
		return false
	}
	return controlErr.Code == codes.NotFound &&
		(controlErr.Operation == operationSendHeartbeat ||
			controlErr.Operation == operationPollExecutionWork)
}

func (c *Client) RegisterNode(ctx context.Context, input *nodeagentv1.RegisterNodeRequest) (*nodeagentv1.RegisterNodeResponse, error) {
	client, err := c.grpcClient()
	if err != nil {
		return nil, err
	}
	resp, err := client.RegisterNode(withOutgoingMetadata(ctx, c.token), input)
	if err != nil {
		return nil, grpcCloudPlaneError(err, operationRegisterNode)
	}
	if resp == nil {
		return nil, errors.New("invalid register response: empty response")
	}
	if strings.TrimSpace(resp.GetNodeId()) == "" {
		return nil, errors.New("invalid register response")
	}
	return resp, nil
}

func (c *Client) SendHeartbeat(ctx context.Context, input *nodeagentv1.RecordHeartbeatRequest) (*nodeagentv1.RecordHeartbeatResponse, error) {
	client, err := c.grpcClient()
	if err != nil {
		return nil, err
	}
	resp, err := client.RecordHeartbeat(withOutgoingMetadata(ctx, c.token), input)
	if err != nil {
		return nil, grpcCloudPlaneError(err, operationSendHeartbeat)
	}
	if resp == nil {
		return nil, errors.New("invalid heartbeat response: empty response")
	}
	if strings.TrimSpace(resp.GetNodeId()) == "" || strings.TrimSpace(resp.GetObservedStatus()) == "" {
		return nil, errors.New("invalid heartbeat response")
	}
	return resp, nil
}

func (c *Client) PollExecutionWork(ctx context.Context, nodeID string) (*nodeagentv1.WorkItem, error) {
	client, err := c.grpcClient()
	if err != nil {
		return nil, err
	}
	resp, err := client.PollWork(withOutgoingMetadata(ctx, c.token), &nodeagentv1.PollWorkRequest{
		NodeId: nodeID,
	})
	if err != nil {
		return nil, grpcCloudPlaneError(err, operationPollExecutionWork)
	}
	if resp == nil {
		return nil, errors.New("invalid work response: empty response")
	}
	return resp.GetItem(), nil
}

func (c *Client) ReportExecution(ctx context.Context, input *nodeagentv1.ReportExecutionRequest) (*nodeagentv1.ReportExecutionResponse, error) {
	client, err := c.grpcClient()
	if err != nil {
		return nil, err
	}
	resp, err := client.ReportExecution(withOutgoingMetadata(ctx, c.token), input)
	if err != nil {
		return nil, grpcCloudPlaneError(err, operationReportExecution)
	}
	if resp == nil {
		return nil, errors.New("invalid report execution response: empty response")
	}
	if resp.GetAck() == nil || resp.GetAck().GetExecution() == nil || strings.TrimSpace(resp.GetAck().GetExecution().GetId()) == "" {
		return nil, errors.New("invalid report execution response")
	}
	return resp, nil
}

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

func withOutgoingMetadata(ctx context.Context, bearerToken string) context.Context {
	requestID := transport.EnsureRequestID(transport.RequestIDFromContext(ctx))
	ctx = transport.ContextWithRequestID(ctx, requestID)

	pairs := []string{
		strings.ToLower(transport.RequestIDHeader), requestID,
	}
	if token := strings.TrimSpace(bearerToken); token != "" {
		pairs = append(pairs, transport.BearerMetadataKey, transport.BearerHeader(token))
	}
	return metadata.AppendToOutgoingContext(ctx, pairs...)
}

func grpcCloudPlaneError(err error, operation string) error {
	st, ok := grpcstatus.FromError(err)
	if !ok {
		return err
	}
	return &CloudPlaneError{
		Operation: operation,
		Code:      st.Code(),
		Message:   st.Message(),
	}
}

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
