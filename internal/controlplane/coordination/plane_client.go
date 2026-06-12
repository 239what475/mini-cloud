package coordination

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"

	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"
	"mini-cloud/internal/transport"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var errPlaneObjectNotFound = errors.New("plane api object not found")

type planeRPCError struct {
	Code    string
	Message string
}

func (e *planeRPCError) Error() string {
	if strings.TrimSpace(e.Code) != "" {
		return fmt.Sprintf("plane gRPC returned code %s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("plane gRPC returned error: %s", e.Message)
}

type planeClient struct {
	bearerToken  string
	conn         *grpc.ClientConn
	snapshotRPC  cloudplanev1.ControlPlaneSnapshotServiceClient
	executionRPC cloudplanev1.ControlPlaneExecutionServiceClient
}

func newPlaneClient(grpcEndpoint string, bearerToken string) (*planeClient, error) {
	target, transportCredentials, err := resolveTarget(grpcEndpoint)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(bearerToken) == "" {
		return nil, fmt.Errorf("bearerToken is required")
	}

	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(transportCredentials))
	if err != nil {
		return nil, fmt.Errorf("dial plane service: %w", err)
	}
	return &planeClient{
		bearerToken:  strings.TrimSpace(bearerToken),
		conn:         conn,
		snapshotRPC:  cloudplanev1.NewControlPlaneSnapshotServiceClient(conn),
		executionRPC: cloudplanev1.NewControlPlaneExecutionServiceClient(conn),
	}, nil
}

func (c *planeClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *planeClient) Snapshot(ctx context.Context) (*cloudplanev1.PlaneSnapshot, error) {
	resp, err := c.snapshotRPC.GetSnapshot(withAuth(ctx, c.bearerToken), &cloudplanev1.GetSnapshotRequest{})
	if err != nil {
		return nil, classifyRPCError(err)
	}
	if resp.GetSnapshot() == nil {
		return nil, fmt.Errorf("plane snapshot response is empty")
	}
	return resp.GetSnapshot(), nil
}

func (c *planeClient) ApplyService(ctx context.Context, input *cloudplanev1.ApplyServiceRequest) error {
	if _, err := c.executionRPC.ApplyService(withAuth(ctx, c.bearerToken), input); err != nil {
		return classifyRPCError(err)
	}
	return nil
}

func (c *planeClient) DeleteService(ctx context.Context, input *cloudplanev1.DeleteServiceRequest) error {
	_, err := c.executionRPC.DeleteService(withAuth(ctx, c.bearerToken), input)
	if err != nil {
		return classifyRPCError(err)
	}
	return nil
}

func resolveTarget(grpcEndpoint string) (string, credentials.TransportCredentials, error) {
	trimmedGRPCEndpoint := strings.TrimSpace(grpcEndpoint)
	if trimmedGRPCEndpoint == "" {
		return "", nil, fmt.Errorf("grpcEndpoint is required")
	}
	if strings.ContainsAny(trimmedGRPCEndpoint, " \t\r\n") {
		return "", nil, fmt.Errorf("grpcEndpoint must not contain whitespace")
	}
	lower := strings.ToLower(trimmedGRPCEndpoint)
	switch {
	case strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://"):
		return "", nil, fmt.Errorf("grpcEndpoint must be a gRPC target, not an HTTP URL")
	case strings.HasPrefix(lower, "grpc://"):
		target := strings.TrimSpace(trimmedGRPCEndpoint[len("grpc://"):])
		if target == "" || strings.Contains(target, "/") {
			return "", nil, fmt.Errorf("grpcEndpoint must include host:port")
		}
		return target, insecure.NewCredentials(), nil
	case strings.HasPrefix(lower, "grpcs://"):
		target := strings.TrimSpace(trimmedGRPCEndpoint[len("grpcs://"):])
		if target == "" || strings.Contains(target, "/") {
			return "", nil, fmt.Errorf("grpcEndpoint must include host:port")
		}
		return target, credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12}), nil
	default:
		return trimmedGRPCEndpoint, insecure.NewCredentials(), nil
	}
}

func withAuth(ctx context.Context, bearerToken string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, transport.BearerMetadataKey, transport.BearerHeader(bearerToken))
}

func classifyRPCError(err error) error {
	st := status.Convert(err)
	if st.Code() == codes.NotFound {
		return fmt.Errorf("%w: %s", errPlaneObjectNotFound, st.Message())
	}
	apiErr := &planeRPCError{
		Code:    st.Code().String(),
		Message: st.Message(),
	}
	return apiErr
}
