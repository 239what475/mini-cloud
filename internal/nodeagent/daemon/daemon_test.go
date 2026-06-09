package daemon

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	nodeagentv1 "mini-cloud/internal/gen/proto/minicloud/nodeagent/v1"
	agentclient "mini-cloud/internal/nodeagent/client"
	agentconfig "mini-cloud/internal/nodeagent/config"
	"mini-cloud/internal/nodeagent/runtime"
	"mini-cloud/internal/nodeagent/workloadlogs"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestHandleNodeErrorClearsInMemorySessionOnInvalidNodeSession 验证无效节点会话错误会清除内存 nodeID 和 session token。
func TestHandleNodeErrorClearsInMemorySessionOnInvalidNodeSession(t *testing.T) {
	t.Parallel()

	cases := []struct {
		// name 是当前表驱动用例名称。
		name string
		// code 是模拟控制面返回的 gRPC status code。
		code codes.Code
		// message 是模拟控制面返回的错误消息。
		message string
	}{
		{
			name:    "node not found",
			code:    codes.NotFound,
			message: "node not found",
		},
		{
			name:    "invalid session",
			code:    codes.Unauthenticated,
			message: "invalid node session token",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			client := agentclient.New(agentclient.Config{})
			client.SetSessionToken("mcws_stale_secret")
			runner := NewRunner(
				logger,
				agentconfig.Config{},
				client,
				&stubRuntime{},
				nil,
			)
			runner.nodeID = "node_stale"
			runner.runtimeResetDone = true
			runner.handleNodeError("node_stale", &agentclient.ControlError{
				Operation: "send heartbeat",
				Code:      tc.code,
				Message:   tc.message,
			})

			if runner.nodeID != "" {
				t.Fatalf("nodeID = %q, want empty after invalid session", runner.nodeID)
			}
			if runner.runtimeResetDone {
				t.Fatal("runtimeResetDone = true, want false after invalid session")
			}
		})
	}
}

// TestTryHeartbeatCycleReRegistersAfterInvalidSession 验证心跳发现会话失效后下一轮会重新注册并重置本地 runtime。
func TestTryHeartbeatCycleReRegistersAfterInvalidSession(t *testing.T) {
	t.Parallel()

	var registerCalls int
	var heartbeatCalls int
	client := newDaemonTestClient(t, agentclient.Config{BootstrapToken: "bootstrap-secret"}, &nodeAgentTestService{
		registerNode: func(ctx context.Context, req *nodeagentv1.RegisterNodeRequest) (*nodeagentv1.RegisterNodeResponse, error) {
			registerCalls++
			if firstIncomingMetadata(ctx, "authorization") != "Bearer bootstrap-secret" {
				t.Fatalf("unexpected register auth header: %q", firstIncomingMetadata(ctx, "authorization"))
			}
			return &nodeagentv1.RegisterNodeResponse{
				NodeId:         "node-renewed",
				SessionToken:   "session-new",
				ObservedStatus: "registering",
				AcceptedAt:     timestamppb.Now(),
			}, nil
		},
		recordHeartbeat: func(ctx context.Context, req *nodeagentv1.HeartbeatRequest) (*nodeagentv1.HeartbeatResponse, error) {
			heartbeatCalls++
			switch req.GetNodeId() {
			case "node-stale":
				return nil, status.Error(codes.Unauthenticated, "invalid node session token")
			case "node-renewed":
				if firstIncomingMetadata(ctx, "authorization") != "Bearer session-new" {
					t.Fatalf("unexpected renewed heartbeat auth header: %q", firstIncomingMetadata(ctx, "authorization"))
				}
				return &nodeagentv1.HeartbeatResponse{
					NodeId:         "node-renewed",
					Accepted:       true,
					ObservedStatus: "ready",
					ReceivedAt:     timestamppb.Now(),
				}, nil
			default:
				t.Fatalf("unexpected node id: %s", req.GetNodeId())
				return nil, status.Error(codes.Internal, "unexpected node id")
			}
		},
	})
	client.SetSessionToken("stale-session")

	cfg := agentconfig.Config{
		ServerURL:      "http://bufconn",
		BootstrapToken: "bootstrap-secret",
		RegisterInput: &nodeagentv1.RegisterNodeRequest{
			Provider:      "aliyun",
			Region:        "cn-beijing",
			Name:          "node-a",
			PrivateIp:     "10.0.0.10",
			InstanceId:    "i-node-a",
			InstanceType:  "ecs.u1-c1m1.large",
			CpuMilliTotal: 2000,
			MemoryMiTotal: 4096,
		},
		AgentVersion:        "test-agent",
		CPUMilliAllocatable: 2000,
		MemoryMiAllocatable: 4096,
		Timeouts: agentconfig.TimeoutsConfig{
			RuntimeStart: time.Second,
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	containerRuntime := &stubRuntime{runningContainers: 0}
	runner := NewRunner(logger, cfg, client, containerRuntime, nil)
	runner.nodeID = "node-stale"
	runner.runtimeResetDone = true

	runner.tryHeartbeatCycle(context.Background())
	if runner.nodeID != "" {
		t.Fatalf("first heartbeat cycle nodeID = %q, want empty string after invalid session", runner.nodeID)
	}

	runner.tryHeartbeatCycle(context.Background())
	if runner.nodeID != "node-renewed" {
		t.Fatalf("second heartbeat cycle nodeID = %q, want node-renewed", runner.nodeID)
	}
	if registerCalls != 1 {
		t.Fatalf("registerCalls = %d, want 1", registerCalls)
	}
	if heartbeatCalls != 2 {
		t.Fatalf("heartbeatCalls = %d, want 2", heartbeatCalls)
	}
	if containerRuntime.resetNodeIDs[0] != "node-renewed" {
		t.Fatalf("runtime reset nodeIDs = %v, want [node-renewed]", containerRuntime.resetNodeIDs)
	}
}

// TestRunnerRunUsesInjectedComponents 验证 Runner 使用注入组件完成启动周期。
func TestRunnerRunUsesInjectedComponents(t *testing.T) {
	t.Parallel()

	var registerCalls int
	var heartbeatCalls int
	var pollCalls int
	var lastHeartbeat *nodeagentv1.HeartbeatRequest
	heartbeatDone := make(chan struct{})
	pollDone := make(chan struct{})
	client := newDaemonTestClient(t, agentclient.Config{}, &nodeAgentTestService{
		registerNode: func(context.Context, *nodeagentv1.RegisterNodeRequest) (*nodeagentv1.RegisterNodeResponse, error) {
			registerCalls++
			return &nodeagentv1.RegisterNodeResponse{
				NodeId:         "node-injected",
				SessionToken:   "session-injected",
				ObservedStatus: "registering",
				AcceptedAt:     timestamppb.Now(),
			}, nil
		},
		recordHeartbeat: func(_ context.Context, req *nodeagentv1.HeartbeatRequest) (*nodeagentv1.HeartbeatResponse, error) {
			heartbeatCalls++
			if heartbeatCalls == 1 {
				close(heartbeatDone)
			}
			lastHeartbeat = req
			return &nodeagentv1.HeartbeatResponse{
				NodeId:         "node-injected",
				Accepted:       true,
				ObservedStatus: "ready",
				ReceivedAt:     timestamppb.Now(),
			}, nil
		},
		pollWork: func(context.Context, *nodeagentv1.PollWorkRequest) (*nodeagentv1.PollWorkResponse, error) {
			pollCalls++
			if pollCalls == 1 {
				close(pollDone)
			}
			return &nodeagentv1.PollWorkResponse{}, nil
		},
	})
	workloadLogs, err := workloadlogs.NewManager(testDaemonLogger(), workloadlogs.Config{}, nil)
	if err != nil {
		t.Fatalf("workloadlogs.NewManager returned error: %v", err)
	}
	cfg := agentconfig.Config{
		RegisterInput: &nodeagentv1.RegisterNodeRequest{
			Provider:      "aliyun",
			Region:        "cn-beijing",
			Name:          "node-injected",
			PrivateIp:     "127.0.0.1",
			InstanceId:    "i-node-injected",
			InstanceType:  "ecs.u1-c1m1.large",
			CpuMilliTotal: 1000,
			MemoryMiTotal: 1024,
		},
		AgentVersion:        "test-agent",
		CPUMilliAllocatable: 1000,
		MemoryMiAllocatable: 1024,
		HeartbeatInterval:   time.Hour,
		WorkInterval:        time.Hour,
		Timeouts: agentconfig.TimeoutsConfig{
			RuntimeStart: time.Millisecond,
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- NewRunner(
			testDaemonLogger(),
			cfg,
			client,
			&stubRuntime{runningContainers: 3},
			workloadLogs,
		).Run(ctx)
	}()
	waitForTestSignal(t, heartbeatDone, cancel, "first heartbeat")
	waitForTestSignal(t, pollDone, cancel, "first work poll")
	cancel()

	err = <-errCh
	if err != nil {
		t.Fatalf("Runner.Run returned error: %v", err)
	}
	if registerCalls != 1 {
		t.Fatalf("registerCalls = %d, want 1", registerCalls)
	}
	if heartbeatCalls != 1 {
		t.Fatalf("heartbeatCalls = %d, want 1", heartbeatCalls)
	}
	if lastHeartbeat.GetStatus() != "ready" {
		t.Fatalf("heartbeat status = %q, want ready", lastHeartbeat.GetStatus())
	}
	if pollCalls != 1 {
		t.Fatalf("pollCalls = %d, want 1", pollCalls)
	}
}

// stubRuntime 是 daemon 测试用的最小运行时实现。
type stubRuntime struct {
	// runningContainers 是 CountRunning 返回的预设值。
	runningContainers int
	// resetNodeIDs 记录 ResetNode 调用收到的 nodeID。
	resetNodeIDs []string
}

// testDaemonLogger 返回丢弃输出的 daemon 测试 logger。
func testDaemonLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// nodeAgentTestService 是 daemon 测试用的 gRPC NodeAgentService。
type nodeAgentTestService struct {
	// UnimplementedNodeAgentServiceServer 保持测试服务满足生成代码要求的向前兼容嵌入。
	nodeagentv1.UnimplementedNodeAgentServiceServer

	// registerNode 覆盖注册节点 RPC 行为。
	registerNode func(context.Context, *nodeagentv1.RegisterNodeRequest) (*nodeagentv1.RegisterNodeResponse, error)
	// recordHeartbeat 覆盖心跳 RPC 行为。
	recordHeartbeat func(context.Context, *nodeagentv1.HeartbeatRequest) (*nodeagentv1.HeartbeatResponse, error)
	// pollWork 覆盖任务拉取 RPC 行为。
	pollWork func(context.Context, *nodeagentv1.PollWorkRequest) (*nodeagentv1.PollWorkResponse, error)
	// reportExecution 覆盖执行上报 RPC 行为。
	reportExecution func(context.Context, *nodeagentv1.ReportExecutionRequest) (*nodeagentv1.ReportExecutionResponse, error)
}

// RegisterNode 转发到测试注入的注册回调。
func (s *nodeAgentTestService) RegisterNode(ctx context.Context, req *nodeagentv1.RegisterNodeRequest) (*nodeagentv1.RegisterNodeResponse, error) {
	return s.registerNode(ctx, req)
}

// RecordHeartbeat 转发到测试注入的心跳回调。
func (s *nodeAgentTestService) RecordHeartbeat(ctx context.Context, req *nodeagentv1.HeartbeatRequest) (*nodeagentv1.HeartbeatResponse, error) {
	return s.recordHeartbeat(ctx, req)
}

// PollWork 转发到测试注入的任务拉取回调。
func (s *nodeAgentTestService) PollWork(ctx context.Context, req *nodeagentv1.PollWorkRequest) (*nodeagentv1.PollWorkResponse, error) {
	if s.pollWork != nil {
		return s.pollWork(ctx, req)
	}
	return &nodeagentv1.PollWorkResponse{}, nil
}

// ReportExecution 转发到测试注入的执行上报回调。
func (s *nodeAgentTestService) ReportExecution(ctx context.Context, req *nodeagentv1.ReportExecutionRequest) (*nodeagentv1.ReportExecutionResponse, error) {
	if s.reportExecution != nil {
		return s.reportExecution(ctx, req)
	}
	return &nodeagentv1.ReportExecutionResponse{}, nil
}

func newDaemonTestClient(t *testing.T, cfg agentclient.Config, server nodeagentv1.NodeAgentServiceServer) *agentclient.Client {
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
	return agentclient.New(cfg)
}

// firstIncomingMetadata 返回入站 gRPC metadata 中指定 key 的第一个值。
func firstIncomingMetadata(ctx context.Context, key string) string {
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

func waitForTestSignal(t *testing.T, signal <-chan struct{}, cancel context.CancelFunc, label string) {
	t.Helper()

	select {
	case <-signal:
	case <-time.After(time.Second):
		cancel()
		t.Fatalf("timed out waiting for %s", label)
	}
}

// Run 实现测试用运行时启动接口。
func (s *stubRuntime) Run(context.Context, runtime.RunInput) (runtime.RunResult, error) {
	return runtime.RunResult{}, nil
}

// Stop 实现测试用运行时停止接口。
func (s *stubRuntime) Stop(context.Context, string) error {
	return nil
}

// Logs 实现测试用运行时日志接口。
func (s *stubRuntime) Logs(context.Context, string, int) (string, error) {
	return "", nil
}

// CountRunning 返回预设的运行中容器数量。
func (s *stubRuntime) CountRunning(context.Context) (int, error) {
	return s.runningContainers, nil
}

// ResetNode 记录测试中的节点 runtime reset 调用。
func (s *stubRuntime) ResetNode(_ context.Context, nodeID string) error {
	s.resetNodeIDs = append(s.resetNodeIDs, nodeID)
	return nil
}

// StreamLogs 实现测试用日志跟随接口。
func (s *stubRuntime) StreamLogs(context.Context, string, runtime.LogEmitter) error {
	return nil
}

// GarbageCollect 实现测试用运行时垃圾回收接口。
func (s *stubRuntime) GarbageCollect(context.Context) error {
	return nil
}

// Close 实现测试用运行时关闭接口。
func (s *stubRuntime) Close() error {
	return nil
}
