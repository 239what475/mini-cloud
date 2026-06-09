package client

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"

	"mini-cloud/internal/common/logctx"
	nodeagentv1 "mini-cloud/internal/gen/proto/minicloud/nodeagent/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestClientAddsRequestIDMetadata 验证客户端会在 gRPC metadata 中携带 request id。
func TestClientAddsRequestIDMetadata(t *testing.T) {
	t.Run("generates request id when missing", func(t *testing.T) {
		var seenRequestID string
		client := newBufconnClient(t, &testNodeControlService{
			pollWork: func(ctx context.Context, req *nodeagentv1.PollWorkRequest) (*nodeagentv1.PollWorkResponse, error) {
				seenRequestID = firstMetadataValue(ctx, strings.ToLower(logctx.HeaderRequestID))
				return &nodeagentv1.PollWorkResponse{}, nil
			},
		})
		client.SetSessionToken("node-session")

		if _, err := client.PollExecutionWork(context.Background(), "node-a"); err != nil {
			t.Fatalf("PollExecutionWork returned error: %v", err)
		}
		if seenRequestID == "" {
			t.Fatalf("expected generated request id metadata")
		}
	})

	t.Run("keeps supplied request id", func(t *testing.T) {
		var seenRequestID string
		client := newBufconnClient(t, &testNodeControlService{
			pollWork: func(ctx context.Context, req *nodeagentv1.PollWorkRequest) (*nodeagentv1.PollWorkResponse, error) {
				seenRequestID = firstMetadataValue(ctx, strings.ToLower(logctx.HeaderRequestID))
				return &nodeagentv1.PollWorkResponse{}, nil
			},
		})
		client.SetSessionToken("node-session")

		ctx := logctx.WithFields(context.Background(), logctx.Fields{RequestID: "req-from-test"})
		if _, err := client.PollExecutionWork(ctx, "node-b"); err != nil {
			t.Fatalf("PollExecutionWork returned error: %v", err)
		}
		if seenRequestID != "req-from-test" {
			t.Fatalf("request id metadata = %q, want req-from-test", seenRequestID)
		}
	})
}

// TestRegisterNodeUsesBootstrapToken 验证注册节点时使用 bootstrap token 并保存 session token。
func TestRegisterNodeUsesBootstrapToken(t *testing.T) {
	t.Parallel()

	var seenAuth string
	client := newBufconnClientWithConfig(t, Config{BootstrapToken: "bootstrap-secret"}, &testNodeControlService{
		registerNode: func(ctx context.Context, req *nodeagentv1.RegisterNodeRequest) (*nodeagentv1.RegisterNodeResponse, error) {
			seenAuth = firstMetadataValue(ctx, "authorization")
			return &nodeagentv1.RegisterNodeResponse{
				NodeId:         "node_x",
				SessionToken:   "session-secret",
				ObservedStatus: "registering",
				AcceptedAt:     timestamppb.Now(),
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
	if seenAuth != "Bearer bootstrap-secret" {
		t.Fatalf("authorization = %q, want bootstrap bearer", seenAuth)
	}
	if client.sessionToken != "session-secret" {
		t.Fatalf("session token = %q, want session-secret", client.sessionToken)
	}
}

// TestPollExecutionWorkDecodesScopedFields 验证拉取任务响应会解码服务和部署作用域字段。
func TestPollExecutionWorkDecodesScopedFields(t *testing.T) {
	t.Parallel()

	client := newBufconnClient(t, &testNodeControlService{
		pollWork: func(ctx context.Context, req *nodeagentv1.PollWorkRequest) (*nodeagentv1.PollWorkResponse, error) {
			return &nodeagentv1.PollWorkResponse{
				Item: &nodeagentv1.WorkItem{
					ExecutionId: "exec_demo",
					PlanId:      "plan_demo",
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
	client.SetSessionToken("node-session")

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
	if item.GetPlanId() != "plan_demo" {
		t.Fatalf("PlanID = %q, want plan_demo", item.GetPlanId())
	}
}

// TestClientRejectsInvalidRegisterResponse 验证客户端拒绝不完整的注册响应。
func TestClientRejectsInvalidRegisterResponse(t *testing.T) {
	t.Parallel()

	client := newBufconnClientWithConfig(t, Config{BootstrapToken: "bootstrap-secret"}, &testNodeControlService{
		registerNode: func(ctx context.Context, req *nodeagentv1.RegisterNodeRequest) (*nodeagentv1.RegisterNodeResponse, error) {
			return &nodeagentv1.RegisterNodeResponse{
				NodeId:         "node_x",
				ObservedStatus: "ready",
				AcceptedAt:     timestamppb.Now(),
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
	if err == nil {
		t.Fatalf("RegisterNode returned nil error, want invalid response error")
	}
	if !strings.Contains(err.Error(), "invalid register response") {
		t.Fatalf("error = %v, want invalid register response", err)
	}
}

// TestClientRejectsInvalidHeartbeatResponse 验证客户端拒绝不完整的心跳响应。
func TestClientRejectsInvalidHeartbeatResponse(t *testing.T) {
	t.Parallel()

	client := newBufconnClient(t, &testNodeControlService{
		recordHeartbeat: func(ctx context.Context, req *nodeagentv1.HeartbeatRequest) (*nodeagentv1.HeartbeatResponse, error) {
			return &nodeagentv1.HeartbeatResponse{
				NodeId:     req.GetNodeId(),
				Accepted:   true,
				ReceivedAt: timestamppb.Now(),
			}, nil
		},
	})
	client.SetSessionToken("node-session")

	_, err := client.SendHeartbeat(context.Background(), &nodeagentv1.HeartbeatRequest{
		NodeId:              "node-a",
		ReportedAt:          timestamppb.Now(),
		AgentVersion:        "test",
		CpuMilliAllocatable: 1000,
		MemoryMiAllocatable: 1024,
		RunningContainers:   1,
		Status:              "ready",
	})
	if err == nil {
		t.Fatalf("SendHeartbeat returned nil error, want invalid response error")
	}
	if !strings.Contains(err.Error(), "invalid heartbeat response") {
		t.Fatalf("error = %v, want invalid heartbeat response", err)
	}
}

// TestClientCloseAllowsRedial 验证关闭连接后下一次请求可以重新建连。
func TestClientCloseAllowsRedial(t *testing.T) {
	t.Parallel()

	client := newBufconnClient(t, &testNodeControlService{
		pollWork: func(ctx context.Context, req *nodeagentv1.PollWorkRequest) (*nodeagentv1.PollWorkResponse, error) {
			return &nodeagentv1.PollWorkResponse{}, nil
		},
	})
	client.SetSessionToken("node-session")

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

// TestSessionTokenMetadataConcurrentSafe 覆盖并发设置和读取 session token 的调用路径。
func TestSessionTokenMetadataConcurrentSafe(t *testing.T) {
	t.Parallel()

	client := newBufconnClient(t, &testNodeControlService{
		pollWork: func(ctx context.Context, req *nodeagentv1.PollWorkRequest) (*nodeagentv1.PollWorkResponse, error) {
			if auth := firstMetadataValue(ctx, "authorization"); !strings.HasPrefix(auth, "Bearer token-") {
				t.Fatalf("authorization = %q, want session bearer", auth)
			}
			return &nodeagentv1.PollWorkResponse{}, nil
		},
	})
	client.SetSessionToken("token-start")

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			client.SetSessionToken("token-" + string(rune('a'+i%26)))
			_, _ = client.PollExecutionWork(context.Background(), "node-a")
		}()
	}
	wg.Wait()
}

// TestIsInvalidSessionOnlyTreatsNodeScopedNotFoundAsInvalidSession 验证执行级 NotFound 不会被误判为节点会话失效。
func TestIsInvalidSessionOnlyTreatsNodeScopedNotFoundAsInvalidSession(t *testing.T) {
	t.Parallel()

	if !IsInvalidSession(&ControlError{Operation: operationSendHeartbeat, Code: codes.NotFound, Message: "node not found"}) {
		t.Fatalf("node-scoped NotFound should be treated as invalid session")
	}
	if IsInvalidSession(&ControlError{Operation: operationReportExecution, Code: codes.NotFound, Message: "execution not found"}) {
		t.Fatalf("execution-scoped NotFound should not be treated as invalid session")
	}
}

// testNodeControlService 是 client 测试用的可回调 NodeAgentService 实现。
type testNodeControlService struct {
	// UnimplementedNodeAgentServiceServer 保持测试服务满足生成代码要求的向前兼容嵌入。
	nodeagentv1.UnimplementedNodeAgentServiceServer

	// registerNode 覆盖 RegisterNode RPC 的测试行为。
	registerNode func(context.Context, *nodeagentv1.RegisterNodeRequest) (*nodeagentv1.RegisterNodeResponse, error)
	// pollWork 覆盖 PollWork RPC 的测试行为。
	pollWork func(context.Context, *nodeagentv1.PollWorkRequest) (*nodeagentv1.PollWorkResponse, error)
	// recordHeartbeat 覆盖 RecordHeartbeat RPC 的测试行为。
	recordHeartbeat func(context.Context, *nodeagentv1.HeartbeatRequest) (*nodeagentv1.HeartbeatResponse, error)
}

// RegisterNode 实现测试服务的注册节点 RPC。
func (s *testNodeControlService) RegisterNode(ctx context.Context, req *nodeagentv1.RegisterNodeRequest) (*nodeagentv1.RegisterNodeResponse, error) {
	if s.registerNode != nil {
		return s.registerNode(ctx, req)
	}
	return &nodeagentv1.RegisterNodeResponse{}, nil
}

// PollWork 实现测试服务的拉取任务 RPC。
func (s *testNodeControlService) PollWork(ctx context.Context, req *nodeagentv1.PollWorkRequest) (*nodeagentv1.PollWorkResponse, error) {
	if s.pollWork != nil {
		return s.pollWork(ctx, req)
	}
	return &nodeagentv1.PollWorkResponse{}, nil
}

// RecordHeartbeat 实现测试服务的心跳 RPC。
func (s *testNodeControlService) RecordHeartbeat(ctx context.Context, req *nodeagentv1.HeartbeatRequest) (*nodeagentv1.HeartbeatResponse, error) {
	if s.recordHeartbeat != nil {
		return s.recordHeartbeat(ctx, req)
	}
	return &nodeagentv1.HeartbeatResponse{}, nil
}

// newBufconnClient 创建使用默认配置的内存 gRPC 连接客户端。
func newBufconnClient(t *testing.T, server nodeagentv1.NodeAgentServiceServer) *Client {
	return newBufconnClientWithConfig(t, Config{}, server)
}

// newBufconnClientWithConfig 创建使用指定配置和 bufconn dialer 的测试客户端。
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

// firstMetadataValue 返回入站 gRPC metadata 中指定 key 的第一个值。
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
