package daemon

import (
	"context"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"testing"
	"time"

	"mini-cloud/internal/contract/nodeagentapi"
	nodeagentv1 "mini-cloud/internal/gen/proto/minicloud/nodeagent/v1"
	agentclient "mini-cloud/internal/nodeagent/client"
	agentconfig "mini-cloud/internal/nodeagent/config"
	"mini-cloud/internal/nodeagent/runtime"
	"mini-cloud/internal/nodeagent/state"
	"mini-cloud/internal/nodeagent/workloadlogs"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestHandleNodeErrorClearsStateOnInvalidNodeSession 验证无效节点会话错误会清除本地状态和 session token。
func TestHandleNodeErrorClearsStateOnInvalidNodeSession(t *testing.T) {
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

			path := filepath.Join(t.TempDir(), "agent-state.json")
			if err := state.Save(path, state.State{
				NodeID:       "node_stale",
				SessionToken: "mcws_stale_secret",
			}); err != nil {
				t.Fatalf("state.Save returned error: %v", err)
			}

			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			client := agentclient.New(agentclient.Config{})
			client.SetSessionToken("mcws_stale_secret")
			runner := NewRunner(
				logger,
				agentconfig.Config{StateFile: path},
				client,
				stubRuntime{},
				nil,
			)
			runner.handleNodeError("node_stale", &agentclient.ControlError{
				Operation: "send heartbeat",
				Code:      tc.code,
				Message:   tc.message,
			})

			loaded, err := state.Load(path)
			if err != nil {
				t.Fatalf("state.Load returned error: %v", err)
			}
			if loaded.NodeID != "" || loaded.SessionToken != "" {
				t.Fatalf("expected cleared state file, got %+v", loaded)
			}
		})
	}
}

// TestTryHeartbeatCycleReRegistersAfterInvalidSession 验证心跳发现会话失效后下一轮会重新注册。
func TestTryHeartbeatCycleReRegistersAfterInvalidSession(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "agent-state.json")
	if err := state.Save(path, state.State{
		NodeID:       "node-stale",
		SessionToken: "stale-session",
	}); err != nil {
		t.Fatalf("state.Save returned error: %v", err)
	}

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
				ObservedStatus: string(nodeagentapi.NodeStatusRegistering),
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
					ObservedStatus: string(nodeagentapi.NodeStatusReady),
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
		RegisterInput: nodeagentapi.RegisterNodeRequest{
			Provider:      "aliyun",
			Region:        "cn-beijing",
			Name:          "node-a",
			PrivateIP:     "10.0.0.10",
			InstanceID:    "i-node-a",
			InstanceType:  "ecs.u1-c1m1.large",
			CPUMilliTotal: 2000,
			MemoryMiTotal: 4096,
		},
		AgentVersion:        "test-agent",
		CPUMilliAllocatable: 2000,
		MemoryMiAllocatable: 4096,
		StateFile:           path,
		Timeouts: agentconfig.TimeoutsConfig{
			RuntimeStart: time.Second,
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	containerRuntime := stubRuntime{runningContainers: 0}
	runner := NewRunner(logger, cfg, client, containerRuntime, nil)
	runner.state = state.State{
		NodeID:       "node-stale",
		SessionToken: "stale-session",
	}

	runner.tryHeartbeatCycle(context.Background())
	if runner.state.NodeID != "" {
		t.Fatalf("first heartbeat cycle nodeID = %q, want empty string after invalid session", runner.state.NodeID)
	}
	clearedState, err := state.Load(path)
	if err != nil {
		t.Fatalf("state.Load after invalid session returned error: %v", err)
	}
	if clearedState.NodeID != "" || clearedState.SessionToken != "" {
		t.Fatalf("expected cleared state after invalid session, got %+v", clearedState)
	}

	runner.tryHeartbeatCycle(context.Background())
	if runner.state.NodeID != "node-renewed" {
		t.Fatalf("second heartbeat cycle nodeID = %q, want node-renewed", runner.state.NodeID)
	}
	renewedState, err := state.Load(path)
	if err != nil {
		t.Fatalf("state.Load after re-register returned error: %v", err)
	}
	if renewedState.NodeID != "node-renewed" || renewedState.SessionToken != "session-new" {
		t.Fatalf("unexpected renewed state: %+v", renewedState)
	}
	if registerCalls != 1 {
		t.Fatalf("registerCalls = %d, want 1", registerCalls)
	}
	if heartbeatCalls != 2 {
		t.Fatalf("heartbeatCalls = %d, want 2", heartbeatCalls)
	}
}

// TestRunnerRunUsesInjectedComponents 验证 Runner 使用注入组件完成启动周期。
func TestRunnerRunUsesInjectedComponents(t *testing.T) {
	t.Parallel()

	var registerCalls int
	var heartbeatCalls int
	var pollCalls int
	var lastHeartbeat nodeagentapi.HeartbeatRequest
	done := make(chan struct{})
	client := newDaemonTestClient(t, agentclient.Config{}, &nodeAgentTestService{
		registerNode: func(context.Context, *nodeagentv1.RegisterNodeRequest) (*nodeagentv1.RegisterNodeResponse, error) {
			registerCalls++
			return &nodeagentv1.RegisterNodeResponse{
				NodeId:         "node-injected",
				SessionToken:   "session-injected",
				ObservedStatus: nodeagentapi.NodeStatusRegistering,
				AcceptedAt:     timestamppb.Now(),
			}, nil
		},
		recordHeartbeat: func(_ context.Context, req *nodeagentv1.HeartbeatRequest) (*nodeagentv1.HeartbeatResponse, error) {
			heartbeatCalls++
			lastHeartbeat = nodeagentapi.HeartbeatRequest{
				AgentVersion:        req.GetAgentVersion(),
				CPUMilliAllocatable: int(req.GetCpuMilliAllocatable()),
				MemoryMiAllocatable: int(req.GetMemoryMiAllocatable()),
				RunningContainers:   int(req.GetRunningContainers()),
				Status:              req.GetStatus(),
			}
			return &nodeagentv1.HeartbeatResponse{
				NodeId:         "node-injected",
				Accepted:       true,
				ObservedStatus: nodeagentapi.NodeStatusReady,
				ReceivedAt:     timestamppb.Now(),
			}, nil
		},
		pollWork: func(context.Context, *nodeagentv1.PollWorkRequest) (*nodeagentv1.PollWorkResponse, error) {
			pollCalls++
			close(done)
			return &nodeagentv1.PollWorkResponse{}, nil
		},
	})
	workloadLogs, err := workloadlogs.NewManager(testDaemonLogger(), workloadlogs.Config{}, nil)
	if err != nil {
		t.Fatalf("workloadlogs.NewManager returned error: %v", err)
	}
	cfg := agentconfig.Config{
		RegisterInput: nodeagentapi.RegisterNodeRequest{
			Provider:      "aliyun",
			Region:        "cn-beijing",
			Name:          "node-injected",
			PrivateIP:     "127.0.0.1",
			InstanceID:    "i-node-injected",
			InstanceType:  "ecs.u1-c1m1.large",
			CPUMilliTotal: 1000,
			MemoryMiTotal: 1024,
		},
		AgentVersion:        "test-agent",
		CPUMilliAllocatable: 1000,
		MemoryMiAllocatable: 1024,
		StateFile:           filepath.Join(t.TempDir(), "state.json"),
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
			stubRuntime{runningContainers: 3},
			workloadLogs,
		).Run(ctx)
	}()
	select {
	case <-done:
		cancel()
	case <-time.After(time.Second):
		cancel()
		t.Fatal("timed out waiting for first work poll")
	}

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
	if lastHeartbeat.Status != nodeagentapi.NodeStatusReady {
		t.Fatalf("heartbeat status = %q, want %q", lastHeartbeat.Status, nodeagentapi.NodeStatusReady)
	}
	if pollCalls != 1 {
		t.Fatalf("pollCalls = %d, want 1", pollCalls)
	}
}

// stubRuntime 是 daemon 测试用的最小运行时实现。
type stubRuntime struct {
	// runningContainers 是 CountRunning 返回的预设值。
	runningContainers int
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

// Run 实现测试用运行时启动接口。
func (s stubRuntime) Run(context.Context, runtime.RunInput) (runtime.RunResult, error) {
	return runtime.RunResult{}, nil
}

// Stop 实现测试用运行时停止接口。
func (s stubRuntime) Stop(context.Context, string) error {
	return nil
}

// Logs 实现测试用运行时日志接口。
func (s stubRuntime) Logs(context.Context, string, int) (string, error) {
	return "", nil
}

// CountRunning 返回预设的运行中容器数量。
func (s stubRuntime) CountRunning(context.Context) (int, error) {
	return s.runningContainers, nil
}

// FollowLogs 实现测试用日志跟随接口。
func (s stubRuntime) FollowLogs(context.Context, string, runtime.LogEmitter) error {
	return nil
}

// GarbageCollect 实现测试用运行时垃圾回收接口。
func (s stubRuntime) GarbageCollect(context.Context) error {
	return nil
}

// Close 实现测试用运行时关闭接口。
func (s stubRuntime) Close() error {
	return nil
}
