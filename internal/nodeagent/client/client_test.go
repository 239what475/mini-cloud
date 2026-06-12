package client

import (
	"context"
	"net"
	"strings"
	"testing"

	nodeagentv1 "mini-cloud/internal/gen/proto/minicloud/nodeagent/v1"
	"mini-cloud/internal/transport"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

func TestClientAddsRequestIDMetadata(t *testing.T) {
	t.Run("generates request id when missing", func(t *testing.T) {
		var seenRequestID string
		client := newBufconnClient(t, &testNodeAgentService{
			pollWork: func(ctx context.Context, req *nodeagentv1.PollWorkRequest) (*nodeagentv1.PollWorkResponse, error) {
				seenRequestID = firstMetadataValue(ctx, strings.ToLower(transport.RequestIDHeader))
				return &nodeagentv1.PollWorkResponse{}, nil
			},
		})

		if _, err := client.PollExecutionWork(context.Background(), "node-a"); err != nil {
			t.Fatalf("PollExecutionWork returned error: %v", err)
		}
		if seenRequestID == "" {
			t.Fatalf("expected generated request id metadata")
		}
	})

	t.Run("keeps supplied request id", func(t *testing.T) {
		var seenRequestID string
		client := newBufconnClient(t, &testNodeAgentService{
			pollWork: func(ctx context.Context, req *nodeagentv1.PollWorkRequest) (*nodeagentv1.PollWorkResponse, error) {
				seenRequestID = firstMetadataValue(ctx, strings.ToLower(transport.RequestIDHeader))
				return &nodeagentv1.PollWorkResponse{}, nil
			},
		})

		ctx := transport.ContextWithRequestID(context.Background(), "req-from-test")
		if _, err := client.PollExecutionWork(ctx, "node-b"); err != nil {
			t.Fatalf("PollExecutionWork returned error: %v", err)
		}
		if seenRequestID != "req-from-test" {
			t.Fatalf("request id metadata = %q, want req-from-test", seenRequestID)
		}
	})
}

func TestRegisterNodeUsesToken(t *testing.T) {
	t.Parallel()

	var seenAuth string
	client := newBufconnClientWithConfig(t, Config{Token: "node-agent-secret"}, &testNodeAgentService{
		registerNode: func(ctx context.Context, req *nodeagentv1.RegisterNodeRequest) (*nodeagentv1.RegisterNodeResponse, error) {
			seenAuth = firstMetadataValue(ctx, "authorization")
			return &nodeagentv1.RegisterNodeResponse{
				NodeId: "node_x",
			}, nil
		},
	})

	_, err := client.RegisterNode(context.Background(), &nodeagentv1.RegisterNodeRequest{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		Name:          "node-a",
		PrivateIp:     "10.0.0.10",
		InstanceId:    "i-abc",
		InstanceType:  "ecs.u1-c1m1.large",
		CpuMilliTotal: 2000,
		MemoryMiTotal: 4096,
	})
	if err != nil {
		t.Fatalf("RegisterNode returned error: %v", err)
	}
	if seenAuth != "Bearer node-agent-secret" {
		t.Fatalf("authorization = %q, want node-agent bearer", seenAuth)
	}
}

func TestPollExecutionWorkDecodesScopedFields(t *testing.T) {
	t.Parallel()

	client := newBufconnClient(t, &testNodeAgentService{
		pollWork: func(ctx context.Context, req *nodeagentv1.PollWorkRequest) (*nodeagentv1.PollWorkResponse, error) {
			return &nodeagentv1.PollWorkResponse{
				Item: &nodeagentv1.WorkItem{
					ExecutionId: "exec_demo",
					NodeId:      "node_demo",
					ServiceId:   "svc_demo",
					ServiceName: "hello",
					Image:       "nginx:1.27-alpine",
					Command:     []string{"nginx"},
					Args:        []string{"-g", "daemon off;"},
					Env: map[string]string{
						"APP_ENV": "test",
					},
					ContainerPort: 8080,
					ReadinessPath: "/healthz",
					ContainerName: "mini-cloud-dep-demo",
				},
			}, nil
		},
	})

	item, err := client.PollExecutionWork(context.Background(), "node_demo")
	if err != nil {
		t.Fatalf("PollExecutionWork returned error: %v", err)
	}
	if item == nil {
		t.Fatalf("PollExecutionWork returned nil item")
	}
	if item.GetServiceId() != "svc_demo" {
		t.Fatalf("ServiceID = %q, want svc_demo", item.GetServiceId())
	}
	if item.GetServiceName() != "hello" {
		t.Fatalf("ServiceName = %q, want hello", item.GetServiceName())
	}
}

func TestClientRejectsInvalidRegisterResponse(t *testing.T) {
	t.Parallel()

	client := newBufconnClientWithConfig(t, Config{Token: "node-agent-secret"}, &testNodeAgentService{
		registerNode: func(ctx context.Context, req *nodeagentv1.RegisterNodeRequest) (*nodeagentv1.RegisterNodeResponse, error) {
			return &nodeagentv1.RegisterNodeResponse{}, nil
		},
	})

	_, err := client.RegisterNode(context.Background(), &nodeagentv1.RegisterNodeRequest{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		Name:          "node-a",
		PrivateIp:     "10.0.0.10",
		InstanceId:    "i-abc",
		InstanceType:  "ecs.u1-c1m1.large",
		CpuMilliTotal: 2000,
		MemoryMiTotal: 4096,
	})
	if err == nil {
		t.Fatalf("RegisterNode returned nil error, want invalid response error")
	}
	if !strings.Contains(err.Error(), "invalid register response") {
		t.Fatalf("error = %v, want invalid register response", err)
	}
}

func TestClientRejectsInvalidHeartbeatResponse(t *testing.T) {
	t.Parallel()

	client := newBufconnClient(t, &testNodeAgentService{
		recordHeartbeat: func(ctx context.Context, req *nodeagentv1.RecordHeartbeatRequest) (*nodeagentv1.RecordHeartbeatResponse, error) {
			return &nodeagentv1.RecordHeartbeatResponse{
				NodeId: req.GetNodeId(),
			}, nil
		},
	})

	_, err := client.SendHeartbeat(context.Background(), &nodeagentv1.RecordHeartbeatRequest{
		NodeId:              "node-a",
		CpuMilliAllocatable: 1000,
		MemoryMiAllocatable: 1024,
	})
	if err == nil {
		t.Fatalf("SendHeartbeat returned nil error, want invalid response error")
	}
	if !strings.Contains(err.Error(), "invalid heartbeat response") {
		t.Fatalf("error = %v, want invalid heartbeat response", err)
	}
}

func TestClientCloseAllowsRedial(t *testing.T) {
	t.Parallel()

	client := newBufconnClient(t, &testNodeAgentService{
		pollWork: func(ctx context.Context, req *nodeagentv1.PollWorkRequest) (*nodeagentv1.PollWorkResponse, error) {
			return &nodeagentv1.PollWorkResponse{}, nil
		},
	})

	if _, err := client.PollExecutionWork(context.Background(), "node-a"); err != nil {
		t.Fatalf("first PollExecutionWork returned error: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if _, err := client.PollExecutionWork(context.Background(), "node-a"); err != nil {
		t.Fatalf("second PollExecutionWork returned error: %v", err)
	}
}

func TestNodeCallsUseToken(t *testing.T) {
	t.Parallel()

	client := newBufconnClientWithConfig(t, Config{Token: "node-agent-secret"}, &testNodeAgentService{
		pollWork: func(ctx context.Context, req *nodeagentv1.PollWorkRequest) (*nodeagentv1.PollWorkResponse, error) {
			if auth := firstMetadataValue(ctx, "authorization"); auth != "Bearer node-agent-secret" {
				t.Fatalf("authorization = %q, want node-agent bearer", auth)
			}
			return &nodeagentv1.PollWorkResponse{}, nil
		},
	})

	if _, err := client.PollExecutionWork(context.Background(), "node-a"); err != nil {
		t.Fatalf("PollExecutionWork returned error: %v", err)
	}
}

func TestIsUnknownNodeOnlyTreatsNodeScopedNotFoundAsUnknownNode(t *testing.T) {
	t.Parallel()

	if !IsUnknownNode(&CloudPlaneError{Operation: operationSendHeartbeat, Code: codes.NotFound, Message: "node not found"}) {
		t.Fatalf("node-scoped NotFound should be treated as unknown node")
	}
	if IsUnknownNode(&CloudPlaneError{Operation: operationReportExecution, Code: codes.NotFound, Message: "execution not found"}) {
		t.Fatalf("execution-scoped NotFound should not be treated as unknown node")
	}
	if IsUnknownNode(&CloudPlaneError{Operation: operationSendHeartbeat, Code: codes.Unauthenticated, Message: "invalid token"}) {
		t.Fatalf("auth failure should not be treated as unknown node")
	}
}

type testNodeAgentService struct {
	nodeagentv1.UnimplementedNodeAgentServiceServer

	registerNode    func(context.Context, *nodeagentv1.RegisterNodeRequest) (*nodeagentv1.RegisterNodeResponse, error)
	pollWork        func(context.Context, *nodeagentv1.PollWorkRequest) (*nodeagentv1.PollWorkResponse, error)
	recordHeartbeat func(context.Context, *nodeagentv1.RecordHeartbeatRequest) (*nodeagentv1.RecordHeartbeatResponse, error)
}

func (s *testNodeAgentService) RegisterNode(ctx context.Context, req *nodeagentv1.RegisterNodeRequest) (*nodeagentv1.RegisterNodeResponse, error) {
	if s.registerNode != nil {
		return s.registerNode(ctx, req)
	}
	return &nodeagentv1.RegisterNodeResponse{}, nil
}

func (s *testNodeAgentService) PollWork(ctx context.Context, req *nodeagentv1.PollWorkRequest) (*nodeagentv1.PollWorkResponse, error) {
	if s.pollWork != nil {
		return s.pollWork(ctx, req)
	}
	return &nodeagentv1.PollWorkResponse{}, nil
}

func (s *testNodeAgentService) RecordHeartbeat(ctx context.Context, req *nodeagentv1.RecordHeartbeatRequest) (*nodeagentv1.RecordHeartbeatResponse, error) {
	if s.recordHeartbeat != nil {
		return s.recordHeartbeat(ctx, req)
	}
	return &nodeagentv1.RecordHeartbeatResponse{}, nil
}

func newBufconnClient(t *testing.T, server nodeagentv1.NodeAgentServiceServer) *Client {
	return newBufconnClientWithConfig(t, Config{}, server)
}

func newBufconnClientWithConfig(t *testing.T, cfg Config, server nodeagentv1.NodeAgentServiceServer) *Client {
	t.Helper()

	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	nodeagentv1.RegisterNodeAgentServiceServer(grpcServer, server)
	go func() {
		_ = grpcServer.Serve(listener)
	}()
	t.Cleanup(func() {
		grpcServer.Stop()
		if err := listener.Close(); err != nil {
			t.Logf("close bufconn listener: %v", err)
		}
	})

	cfg.ServerURL = "http://bufconn"
	cfg.Dialer = func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}
	return New(cfg)
}

func firstMetadataValue(ctx context.Context, key string) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	values := md.Get(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
