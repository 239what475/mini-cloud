package operatorclient

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/url"
	"strings"

	controlplanev1 "mini-cloud/internal/gen/proto/minicloud/controlplane/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"
)

type Client struct {
	bearerToken string
	conn        *grpc.ClientConn
	operatorRPC controlplanev1.OperatorServiceClient
}

func New(grpcEndpoint string, bearerToken string) (*Client, error) {
	return NewWithDialOptions(grpcEndpoint, bearerToken)
}

func NewWithDialOptions(grpcEndpoint string, bearerToken string, dialOptions ...grpc.DialOption) (*Client, error) {
	target, transportCredentials, err := resolveTarget(grpcEndpoint)
	if err != nil {
		return nil, err
	}

	opts := append([]grpc.DialOption{grpc.WithTransportCredentials(transportCredentials)}, dialOptions...)
	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, fmt.Errorf("dial operator service: %w", err)
	}

	return &Client{
		bearerToken: strings.TrimSpace(bearerToken),
		conn:        conn,
		operatorRPC: controlplanev1.NewOperatorServiceClient(conn),
	}, nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) GetOverview(ctx context.Context) (*controlplanev1.Overview, error) {
	return c.operatorRPC.GetOverview(withAuth(ctx, c.bearerToken), &emptypb.Empty{})
}

func (c *Client) ListPlanes(ctx context.Context) ([]*controlplanev1.Plane, error) {
	resp, err := c.operatorRPC.ListPlanes(withAuth(ctx, c.bearerToken), &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	return resp.GetItems(), nil
}

func (c *Client) SyncPlane(ctx context.Context, planeID string) (*controlplanev1.SyncPlaneResponse, error) {
	return c.operatorRPC.SyncPlane(withAuth(ctx, c.bearerToken), &controlplanev1.SyncPlaneRequest{
		PlaneId: strings.TrimSpace(planeID),
	})
}

func (c *Client) ListServices(ctx context.Context) ([]*controlplanev1.Service, error) {
	resp, err := c.operatorRPC.ListServices(withAuth(ctx, c.bearerToken), &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	return resp.GetItems(), nil
}

func (c *Client) CreateService(ctx context.Context, req *controlplanev1.CreateServiceRequest) (*controlplanev1.Service, error) {
	if req == nil {
		req = &controlplanev1.CreateServiceRequest{}
	}
	return c.operatorRPC.CreateService(withAuth(ctx, c.bearerToken), req)
}

func resolveTarget(grpcEndpoint string) (string, credentials.TransportCredentials, error) {
	trimmedGRPCEndpoint := strings.TrimSpace(grpcEndpoint)
	if trimmedGRPCEndpoint == "" {
		return "", nil, fmt.Errorf("grpcEndpoint is required")
	}

	parsed, err := url.Parse(trimmedGRPCEndpoint)
	if err != nil {
		return "", nil, fmt.Errorf("parse grpcEndpoint: %w", err)
	}
	if parsed.Host == "" {
		return "", nil, fmt.Errorf("grpcEndpoint must include scheme and host")
	}

	switch strings.ToLower(parsed.Scheme) {
	case "http":
		return parsed.Host, insecure.NewCredentials(), nil
	case "https":
		return parsed.Host, credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12}), nil
	default:
		return "", nil, fmt.Errorf("unsupported grpcEndpoint scheme %q", parsed.Scheme)
	}
}

func withAuth(ctx context.Context, bearerToken string) context.Context {
	token := strings.TrimSpace(bearerToken)
	if token == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
}
