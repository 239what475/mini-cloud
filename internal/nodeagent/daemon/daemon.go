package daemon

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	nodeagentv1 "mini-cloud/internal/gen/proto/minicloud/nodeagent/v1"
	"mini-cloud/internal/logctx"
	agentclient "mini-cloud/internal/nodeagent/client"
	agentconfig "mini-cloud/internal/nodeagent/config"
	"mini-cloud/internal/nodeagent/runtime"
	"mini-cloud/internal/nodeagent/work"
	"mini-cloud/internal/nodeagent/workloadlogs"

	"google.golang.org/protobuf/types/known/timestamppb"
)

type Runner struct {
	logger           *slog.Logger
	cfg              agentconfig.Config
	controlClient    *agentclient.Client
	containerRuntime runtime.Runtime
	workloadLogs     *workloadlogs.Manager

	registerMu       sync.Mutex
	mu               sync.Mutex
	nodeID           string
	resetMu          sync.Mutex
	runtimeResetDone bool
	closeOnce        sync.Once
	closeErr         error
}

func NewRunner(
	logger *slog.Logger,
	cfg agentconfig.Config,
	controlClient *agentclient.Client,
	containerRuntime runtime.Runtime,
	workloadLogs *workloadlogs.Manager,
) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{
		logger:           logger,
		cfg:              cfg,
		controlClient:    controlClient,
		containerRuntime: containerRuntime,
		workloadLogs:     workloadLogs,
	}
}

func (r *Runner) Run(ctx context.Context) error {
	defer r.Close()

	if nodeID, err := r.registerNode(ctx); err != nil {
		r.logger.Warn("node agent initial registration failed", "instance_id", r.cfg.RegisterInput.GetInstanceId(), "error", err)
	} else if err := r.resetRuntimeOnce(ctx, nodeID); err != nil {
		r.logger.Warn("node agent initial runtime reset failed", "node_id", nodeID, "error", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		r.runHeartbeatLoop(ctx)
	}()
	go func() {
		defer wg.Done()
		r.runWorkerLoop(ctx)
	}()

	<-ctx.Done()
	r.logger.Info("node agent loop stopped", "reason", ctx.Err())

	shutdownTimer := time.NewTimer(r.timeout(r.cfg.Timeouts.ShutdownGrace))
	defer shutdownTimer.Stop()
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		wg.Wait()
	}()
	select {
	case <-stopped:
	case <-shutdownTimer.C:
		r.logger.Warn("node agent shutdown grace period elapsed")
		return nil
	}
	return nil
}

func (r *Runner) Close() error {
	r.closeOnce.Do(func() {
		var errs []error
		if r.workloadLogs != nil {
			closeCtx, cancel := context.WithTimeout(context.Background(), r.timeout(r.cfg.Timeouts.CleanupHard))
			if err := r.workloadLogs.Close(closeCtx); err != nil {
				r.logger.Warn("close workload log manager failed", "error", err)
				errs = append(errs, err)
			}
			cancel()
		}
		if r.controlClient != nil {
			if err := r.controlClient.Close(); err != nil {
				r.logger.Warn("close node control client failed", "error", err)
				errs = append(errs, err)
			}
		}
		if r.containerRuntime != nil {
			if err := r.containerRuntime.Close(); err != nil {
				r.logger.Warn("close node runtime failed", "error", err)
				errs = append(errs, err)
			}
		}
		r.closeErr = errors.Join(errs...)
	})
	return r.closeErr
}

func (r *Runner) runHeartbeatLoop(ctx context.Context) {
	r.tryHeartbeatCycle(ctx)
	ticker := time.NewTicker(r.cfg.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.tryHeartbeatCycle(ctx)
		}
	}
}

func (r *Runner) runWorkerLoop(ctx context.Context) {
	r.tryWorkCycle(ctx)
	ticker := time.NewTicker(r.cfg.WorkInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.tryWorkCycle(ctx)
		}
	}
}

func (r *Runner) tryHeartbeatCycle(ctx context.Context) {
	nodeID, err := r.ensureNodeRegistration(ctx)
	if err != nil {
		r.logger.Warn("node agent registration failed", "instance_id", r.cfg.RegisterInput.GetInstanceId(), "error", err)
		return
	}
	if err := r.resetRuntimeOnce(ctx, nodeID); err != nil {
		r.logger.Warn("node runtime reset failed before heartbeat", "node_id", nodeID, "error", err)
		return
	}

	if err := r.sendHeartbeat(ctx, nodeID); err != nil {
		r.handleNodeError(nodeID, err)
		return
	}
}

func (r *Runner) tryWorkCycle(ctx context.Context) {
	nodeID, err := r.ensureNodeRegistration(ctx)
	if err != nil {
		r.logger.Warn("node agent registration failed before work poll", "instance_id", r.cfg.RegisterInput.GetInstanceId(), "error", err)
		return
	}
	if err := r.resetRuntimeOnce(ctx, nodeID); err != nil {
		r.logger.Warn("node runtime reset failed before work poll", "node_id", nodeID, "error", err)
		return
	}

	result, err := work.ExecuteNext(ctx, r.logger, r.controlClient, r.containerRuntime, work.Options{
		Node: work.NodeOptions{
			ID:        nodeID,
			PrivateIP: r.cfg.RegisterInput.GetPrivateIp(),
		},
		Runtime: work.RuntimeOptions{
			HostPortMin: r.cfg.Runtime.HostPortMin,
			HostPortMax: r.cfg.Runtime.HostPortMax,
		},
		Readiness: work.ReadinessOptions{
			Attempts: r.cfg.Work.ReadinessAttempts,
			Interval: r.cfg.Work.ReadinessInterval,
			Timeout:  r.cfg.Work.ReadinessTimeout,
		},
		Timeouts: work.TimeoutOptions{
			RuntimeStart: r.cfg.Timeouts.RuntimeStart,
			RuntimeStop:  r.cfg.Timeouts.RuntimeStop,
			RuntimeLogs:  r.cfg.Timeouts.RuntimeLogs,
			PollWork:     r.cfg.Timeouts.PollWork,
			Report:       r.cfg.Timeouts.Report,
			CleanupHard:  r.cfg.Timeouts.CleanupHard,
		},
		Observability: work.ObservabilityOptions{
			PlatformName:         r.cfg.Work.PlatformName,
			WorkloadLogs:         r.startWorkloadLogs,
			WorkloadOTLPEndpoint: r.cfg.Work.WorkloadOTLPEndpoint,
			LogTail:              r.cfg.Work.LogTail,
		},
		Network: work.NetworkOptions{
			EgressProxy: work.EgressProxyOptions{
				Enabled:  r.cfg.Network.EgressProxy.Enabled,
				Endpoint: r.cfg.Network.EgressProxy.Endpoint,
				NoProxy:  r.cfg.Network.EgressProxy.NoProxy,
			},
		},
	})
	if err != nil {
		workLogger := logctx.WithLoggerFields(r.logger, logctx.Fields{NodeID: nodeID})
		if result.WorkItem != nil {
			workLogger = logctx.WithLoggerFields(workLogger, logctx.Fields{
				ServiceID:   result.WorkItem.GetServiceId(),
				PlanID:      result.WorkItem.GetPlanId(),
				ExecutionID: result.WorkItem.GetExecutionId(),
			})
		}
		workLogger.Warn("node work execution failed", "error", err)
		r.handleNodeError(nodeID, err)
		return
	}
	if result.WorkFound {
		workLogger := logctx.WithLoggerFields(r.logger, logctx.Fields{NodeID: nodeID})
		reportStatus := ""
		if result.WorkItem != nil {
			workLogger = logctx.WithLoggerFields(workLogger, logctx.Fields{
				ServiceID:   result.WorkItem.GetServiceId(),
				PlanID:      result.WorkItem.GetPlanId(),
				ExecutionID: result.WorkItem.GetExecutionId(),
			})
		}
		if result.Report != nil {
			reportStatus = result.Report.GetAck().GetExecution().GetStatus()
		}
		workLogger.Info("node work execution finished",
			"report_status", reportStatus,
		)
	}
}

func (r *Runner) ensureNodeRegistration(ctx context.Context) (string, error) {
	r.registerMu.Lock()
	defer r.registerMu.Unlock()

	r.mu.Lock()
	if r.nodeID != "" {
		nodeID := r.nodeID
		r.mu.Unlock()
		return nodeID, nil
	}
	r.mu.Unlock()

	return r.registerNode(ctx)
}

func (r *Runner) registerNode(ctx context.Context) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, r.timeout(r.cfg.Timeouts.Register))
	defer cancel()
	reqCtx = logctx.WithFields(reqCtx, logctx.Fields{
		RequestID: logctx.EnsureRequestID(""),
	})
	registered, err := r.controlClient.RegisterNode(reqCtx, r.cfg.RegisterInput)
	if err != nil {
		return "", err
	}

	r.mu.Lock()
	r.nodeID = registered.GetNodeId()
	r.runtimeResetDone = false
	r.mu.Unlock()

	r.controlClient.SetSessionToken(registered.GetSessionToken())

	logctx.WithLoggerFields(r.logger, logctx.Fields{NodeID: registered.GetNodeId()}).Info("node agent registered",
		"instance_id", r.cfg.RegisterInput.GetInstanceId(),
		"request_id", logctx.RequestID(reqCtx),
	)
	return registered.GetNodeId(), nil
}

func (r *Runner) sendHeartbeat(ctx context.Context, nodeID string) error {
	logger := logctx.WithLoggerFields(r.logger, logctx.Fields{NodeID: nodeID})
	runningContainers := 0
	countCtx, cancel := context.WithTimeout(ctx, r.timeout(r.cfg.Timeouts.RuntimeLogs))
	count, err := r.containerRuntime.CountRunning(countCtx)
	cancel()
	if err != nil {
		logger.Warn("count running containers failed", "node_id", nodeID, "error", err)
	} else {
		runningContainers = count
	}

	reqCtx, cancel := context.WithTimeout(ctx, r.timeout(r.cfg.Timeouts.Heartbeat))
	defer cancel()
	reqCtx = logctx.WithFields(reqCtx, logctx.Fields{
		RequestID: logctx.EnsureRequestID(""),
		NodeID:    nodeID,
	})
	ack, err := r.controlClient.SendHeartbeat(reqCtx, &nodeagentv1.HeartbeatRequest{
		NodeId:              nodeID,
		ReportedAt:          timestamppb.New(time.Now().UTC()),
		AgentVersion:        r.cfg.AgentVersion,
		CpuMilliAllocatable: int32(r.cfg.CPUMilliAllocatable),
		MemoryMiAllocatable: int32(r.cfg.MemoryMiAllocatable),
		RunningContainers:   int32(runningContainers),
		Status:              r.heartbeatStatus(),
	})
	if err != nil {
		return err
	}

	logger.Info("node heartbeat accepted",
		"status", ack.GetObservedStatus(),
		"running_containers", runningContainers,
		"request_id", logctx.RequestID(reqCtx),
	)
	return nil
}

func (r *Runner) heartbeatStatus() string {
	return "ready"
}

func (r *Runner) resetRuntimeOnce(ctx context.Context, nodeID string) error {
	r.resetMu.Lock()
	defer r.resetMu.Unlock()

	r.mu.Lock()
	if r.runtimeResetDone {
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()

	gcCtx, cancel := context.WithTimeout(ctx, r.timeout(r.cfg.Timeouts.CleanupHard))
	defer cancel()
	if err := r.containerRuntime.ResetNode(gcCtx, nodeID); err != nil {
		return err
	}
	if err := r.containerRuntime.GarbageCollect(gcCtx); err != nil {
		return err
	}
	r.mu.Lock()
	r.runtimeResetDone = true
	r.mu.Unlock()
	return nil
}

func (r *Runner) handleNodeError(nodeID string, err error) {
	if agentclient.IsInvalidSession(err) {
		r.logger.Warn("node session is no longer valid, clearing local state",
			"node_id", nodeID,
			"error", err,
		)
		r.mu.Lock()
		r.nodeID = ""
		r.runtimeResetDone = false
		r.mu.Unlock()
		r.controlClient.SetSessionToken("")
		return
	}
}

func (r *Runner) startWorkloadLogs(req workloadlogs.StartRequest) {
	if r.workloadLogs == nil {
		return
	}
	r.workloadLogs.Start(req)
}

func (r *Runner) timeout(value time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return time.Second
}
