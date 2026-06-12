package daemon

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	nodeagentv1 "mini-cloud/internal/gen/proto/minicloud/nodeagent/v1"
	"mini-cloud/internal/nodeagent/capacity"
	agentclient "mini-cloud/internal/nodeagent/client"
	agentconfig "mini-cloud/internal/nodeagent/config"
	"mini-cloud/internal/nodeagent/runtime"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestHandleNodeErrorClearsLocalNodeOnUnknownNode(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := NewRunner(
		logger,
		agentconfig.Config{},
		agentclient.New(agentclient.Config{}),
		&stubRuntime{},
	)
	runner.nodeID = "node_stale"
	runner.runtimeResetNode = "node_stale"
	runner.handleNodeError("node_stale", &agentclient.CloudPlaneError{
		Operation: "send heartbeat",
		Code:      codes.NotFound,
		Message:   "node not found",
	})

	if runner.nodeID != "" {
		t.Fatalf("nodeID = %q, want empty after unknown node", runner.nodeID)
	}
	if runner.runtimeResetNode != "" {
		t.Fatalf("runtimeResetNode = %q, want empty after unknown node", runner.runtimeResetNode)
	}
}

func TestHandleNodeErrorKeepsLocalNodeOnAuthFailure(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := NewRunner(
		logger,
		agentconfig.Config{},
		agentclient.New(agentclient.Config{}),
		&stubRuntime{},
	)
	runner.nodeID = "node-a"
	runner.runtimeResetNode = "node-a"
	runner.handleNodeError("node-a", &agentclient.CloudPlaneError{
		Operation: "send heartbeat",
		Code:      codes.Unauthenticated,
		Message:   "invalid node agent token",
	})

	if runner.nodeID != "node-a" {
		t.Fatalf("nodeID = %q, want node-a", runner.nodeID)
	}
	if runner.runtimeResetNode != "node-a" {
		t.Fatalf("runtimeResetNode = %q, want node-a", runner.runtimeResetNode)
	}
}

func TestTryHeartbeatCycleReRegistersAfterUnknownNode(t *testing.T) {
	t.Parallel()

	var registerCalls int
	var heartbeatCalls int
	client := newDaemonTestClient(t, agentclient.Config{Token: "node-agent-secret"}, &nodeAgentTestService{
		registerNode: func(ctx context.Context, req *nodeagentv1.RegisterNodeRequest) (*nodeagentv1.RegisterNodeResponse, error) {
			registerCalls++
			if firstIncomingMetadata(ctx, "authorization") != "Bearer node-agent-secret" {
				t.Fatalf("unexpected register auth header: %q", firstIncomingMetadata(ctx, "authorization"))
			}
			return &nodeagentv1.RegisterNodeResponse{
				NodeId: "node-renewed",
			}, nil
		},
		recordHeartbeat: func(ctx context.Context, req *nodeagentv1.RecordHeartbeatRequest) (*nodeagentv1.RecordHeartbeatResponse, error) {
			heartbeatCalls++
			switch req.GetNodeId() {
			case "node-stale":
				return nil, status.Error(codes.NotFound, "node not found")
			case "node-renewed":
				if firstIncomingMetadata(ctx, "authorization") != "Bearer node-agent-secret" {
					t.Fatalf("unexpected renewed heartbeat auth header: %q", firstIncomingMetadata(ctx, "authorization"))
				}
				return &nodeagentv1.RecordHeartbeatResponse{
					NodeId:         "node-renewed",
					ObservedStatus: "ready",
				}, nil
			default:
				t.Fatalf("unexpected node id: %s", req.GetNodeId())
				return nil, status.Error(codes.Internal, "unexpected node id")
			}
		},
	})

	cfg := agentconfig.Config{
		Server: agentconfig.ServerConfig{URL: "http://bufconn"},
		Auth:   agentconfig.AuthConfig{Token: "node-agent-secret"},
		Node: agentconfig.NodeConfig{
			Provider:     "aliyun",
			Region:       "cn-beijing",
			Name:         "node-a",
			PrivateIP:    "10.0.0.10",
			InstanceID:   "i-node-a",
			InstanceType: "ecs.u1-c1m1.large",
		},
		ResolvedCapacity: capacity.Resolved{
			Total:       capacity.Resources{CPUMilli: 2000, MemoryMi: 4096},
			Allocatable: capacity.Resources{CPUMilli: 2000, MemoryMi: 4096},
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	containerRuntime := &stubRuntime{}
	runner := NewRunner(logger, cfg, client, containerRuntime)
	runner.nodeID = "node-stale"
	runner.runtimeResetNode = "node-stale"

	runner.tryHeartbeatCycle(context.Background())
	if runner.nodeID != "" {
		t.Fatalf("first heartbeat cycle nodeID = %q, want empty string after unknown node", runner.nodeID)
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

func TestResetRuntimeOnceDoesNotMarkDifferentNodeReset(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := NewRunner(
		logger,
		agentconfig.Config{},
		agentclient.New(agentclient.Config{}),
		nil,
	)
	runner.nodeID = "node-old"
	containerRuntime := &stubRuntime{
		resetNode: func(context.Context, string) error {
			runner.mu.Lock()
			runner.nodeID = "node-new"
			runner.mu.Unlock()
			return nil
		},
	}
	runner.containerRuntime = containerRuntime

	if err := runner.resetRuntimeOnce(context.Background(), "node-old"); err != nil {
		t.Fatalf("resetRuntimeOnce returned error: %v", err)
	}
	if runner.runtimeResetNode != "" {
		t.Fatalf("runtimeResetNode = %q, want empty after node changed during reset", runner.runtimeResetNode)
	}
	if len(containerRuntime.resetNodeIDs) != 1 || containerRuntime.resetNodeIDs[0] != "node-old" {
		t.Fatalf("resetNodeIDs = %v, want [node-old]", containerRuntime.resetNodeIDs)
	}
}

func TestRunnerRunUsesInjectedComponents(t *testing.T) {
	t.Parallel()

	var registerCalls int
	var heartbeatCalls int
	var pollCalls int
	heartbeatDone := make(chan struct{})
	pollDone := make(chan struct{})
	client := newDaemonTestClient(t, agentclient.Config{}, &nodeAgentTestService{
		registerNode: func(context.Context, *nodeagentv1.RegisterNodeRequest) (*nodeagentv1.RegisterNodeResponse, error) {
			registerCalls++
			return &nodeagentv1.RegisterNodeResponse{
				NodeId: "node-injected",
			}, nil
		},
		recordHeartbeat: func(context.Context, *nodeagentv1.RecordHeartbeatRequest) (*nodeagentv1.RecordHeartbeatResponse, error) {
			heartbeatCalls++
			if heartbeatCalls == 1 {
				close(heartbeatDone)
			}
			return &nodeagentv1.RecordHeartbeatResponse{
				NodeId:         "node-injected",
				ObservedStatus: "ready",
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
	cfg := agentconfig.Config{
		Node: agentconfig.NodeConfig{
			Provider:     "aliyun",
			Region:       "cn-beijing",
			Name:         "node-injected",
			PrivateIP:    "127.0.0.1",
			InstanceID:   "i-node-injected",
			InstanceType: "ecs.u1-c1m1.large",
		},
		ResolvedCapacity: capacity.Resolved{
			Total:       capacity.Resources{CPUMilli: 1000, MemoryMi: 1024},
			Allocatable: capacity.Resources{CPUMilli: 1000, MemoryMi: 1024},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- NewRunner(
			testDaemonLogger(),
			cfg,
			client,
			&stubRuntime{},
		).Run(ctx)
	}()
	waitForTestSignal(t, heartbeatDone, cancel, "first heartbeat")
	waitForTestSignal(t, pollDone, cancel, "first work poll")
	cancel()

	if err := <-errCh; err != nil {
		t.Fatalf("Runner.Run returned error: %v", err)
	}
	if registerCalls != 1 {
		t.Fatalf("registerCalls = %d, want 1", registerCalls)
	}
	if heartbeatCalls != 1 {
		t.Fatalf("heartbeatCalls = %d, want 1", heartbeatCalls)
	}
	if pollCalls != 1 {
		t.Fatalf("pollCalls = %d, want 1", pollCalls)
	}
}

type stubRuntime struct {
	resetNodeIDs []string
	resetNode    func(context.Context, string) error
}

func testDaemonLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type nodeAgentTestService struct {
	nodeagentv1.UnimplementedNodeAgentServiceServer

	registerNode    func(context.Context, *nodeagentv1.RegisterNodeRequest) (*nodeagentv1.RegisterNodeResponse, error)
	recordHeartbeat func(context.Context, *nodeagentv1.RecordHeartbeatRequest) (*nodeagentv1.RecordHeartbeatResponse, error)
	pollWork        func(context.Context, *nodeagentv1.PollWorkRequest) (*nodeagentv1.PollWorkResponse, error)
	reportExecution func(context.Context, *nodeagentv1.ReportExecutionRequest) (*nodeagentv1.ReportExecutionResponse, error)
}

func (s *nodeAgentTestService) RegisterNode(ctx context.Context, req *nodeagentv1.RegisterNodeRequest) (*nodeagentv1.RegisterNodeResponse, error) {
	return s.registerNode(ctx, req)
}

func (s *nodeAgentTestService) RecordHeartbeat(ctx context.Context, req *nodeagentv1.RecordHeartbeatRequest) (*nodeagentv1.RecordHeartbeatResponse, error) {
	return s.recordHeartbeat(ctx, req)
}

func (s *nodeAgentTestService) PollWork(ctx context.Context, req *nodeagentv1.PollWorkRequest) (*nodeagentv1.PollWorkResponse, error) {
	if s.pollWork != nil {
		return s.pollWork(ctx, req)
	}
	return &nodeagentv1.PollWorkResponse{}, nil
}

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

func (s *stubRuntime) Run(context.Context, runtime.RunInput) (runtime.RunResult, error) {
	return runtime.RunResult{}, nil
}

func (s *stubRuntime) Stop(context.Context, string) error {
	return nil
}

func (s *stubRuntime) Logs(context.Context, string, int) (string, error) {
	return "", nil
}

func (s *stubRuntime) ResetNode(ctx context.Context, nodeID string) error {
	s.resetNodeIDs = append(s.resetNodeIDs, nodeID)
	if s.resetNode != nil {
		return s.resetNode(ctx, nodeID)
	}
	return nil
}

func (s *stubRuntime) Close() error {
	return nil
}
