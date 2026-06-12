package daemon

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	nodeagentv1 "mini-cloud/internal/gen/proto/minicloud/nodeagent/v1"
	agentclient "mini-cloud/internal/nodeagent/client"
	agentconfig "mini-cloud/internal/nodeagent/config"
	"mini-cloud/internal/nodeagent/work"
	"mini-cloud/internal/nodeagent/workloadlogs"
	"mini-cloud/internal/transport"
)

type Runner struct {
	logger           *slog.Logger
	cfg              agentconfig.Config
	controlClient    *agentclient.Client
	containerRuntime containerRuntime
	workloadLogs     *workloadlogs.Collector

	registerMu       sync.Mutex
	mu               sync.Mutex
	nodeID           string
	resetMu          sync.Mutex
	runtimeResetDone bool
	closeOnce        sync.Once
	closeErr         error
}

type containerRuntime interface {
	work.ContainerRuntime
	ResetNode(context.Context, string) error
	GarbageCollect(context.Context) error
	Close() error
}

func NewRunner(
	logger *slog.Logger,
	cfg agentconfig.Config,
	controlClient *agentclient.Client,
	containerRuntime containerRuntime,
	workloadLogs *workloadlogs.Collector,
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
		r.logger.Warn("node agent initial registration failed", "instance_id", r.cfg.Node.InstanceID, "error", err)
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

	shutdownTimer := time.NewTimer(agentconfig.ShutdownGraceTimeout)
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
			closeCtx, cancel := context.WithTimeout(context.Background(), agentconfig.CleanupHardTimeout)
			if err := r.workloadLogs.Close(closeCtx); err != nil {
				r.logger.Warn("close workload log collector failed", "error", err)
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
	ticker := time.NewTicker(agentconfig.HeartbeatInterval)
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
	ticker := time.NewTicker(agentconfig.WorkInterval)
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
		r.logger.Warn("node agent registration failed", "instance_id", r.cfg.Node.InstanceID, "error", err)
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
		r.logger.Warn("node agent registration failed before work poll", "instance_id", r.cfg.Node.InstanceID, "error", err)
		return
	}
	if err := r.resetRuntimeOnce(ctx, nodeID); err != nil {
		r.logger.Warn("node runtime reset failed before work poll", "node_id", nodeID, "error", err)
		return
	}

	result, err := work.ExecuteNext(ctx, r.logger, r.controlClient, r.containerRuntime, work.Options{
		Node: work.NodeOptions{
			ID:        nodeID,
			PrivateIP: r.cfg.Node.PrivateIP,
		},
		Runtime: work.RuntimeOptions{
			HostPortMin: agentconfig.RuntimeHostPortMin,
			HostPortMax: agentconfig.RuntimeHostPortMax,
		},
		Readiness: work.ReadinessOptions{
			Attempts: agentconfig.ReadinessAttempts,
			Interval: agentconfig.ReadinessInterval,
			Timeout:  agentconfig.ReadinessTimeout,
		},
		Timeouts: work.TimeoutOptions{
			RuntimeStart: agentconfig.RuntimeStartTimeout,
			RuntimeStop:  agentconfig.RuntimeStopTimeout,
			RuntimeLogs:  agentconfig.RuntimeLogsTimeout,
			PollWork:     agentconfig.PollWorkRequestTimeout,
			Report:       agentconfig.ReportExecutionTimeout,
			CleanupHard:  agentconfig.CleanupHardTimeout,
		},
		Observability: work.ObservabilityOptions{
			PlatformName:         r.cfg.Platform.Name,
			WorkloadLogs:         r.startWorkloadLogs,
			WorkloadOTLPEndpoint: r.cfg.Observability.WorkloadOTLPEndpoint,
			LogTail:              agentconfig.WorkloadLogTailLineCount,
		},
		Network: work.NetworkOptions{
			EgressProxy: work.EgressProxyOptions{
				Endpoint: r.cfg.Network.EgressProxy.Endpoint,
				NoProxy:  r.cfg.Network.EgressProxy.NoProxy,
			},
		},
	})
	if err != nil {
		workLogger := r.logger.With("node_id", nodeID)
		if result.WorkItem != nil {
			workLogger = withWorkItemLogFields(workLogger, result.WorkItem)
		}
		workLogger.Warn("node work execution failed", "error", err)
		r.handleNodeError(nodeID, err)
		return
	}
	if result.WorkFound {
		workLogger := r.logger.With("node_id", nodeID)
		reportStatus := ""
		if result.WorkItem != nil {
			workLogger = withWorkItemLogFields(workLogger, result.WorkItem)
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
	reqCtx, cancel := context.WithTimeout(ctx, agentconfig.RegisterTimeout)
	defer cancel()
	requestID := transport.EnsureRequestID("")
	reqCtx = transport.ContextWithRequestID(reqCtx, requestID)
	registered, err := r.controlClient.RegisterNode(reqCtx, r.registerRequest())
	if err != nil {
		return "", err
	}

	r.mu.Lock()
	r.nodeID = registered.GetNodeId()
	r.runtimeResetDone = false
	r.mu.Unlock()

	r.logger.With("node_id", registered.GetNodeId()).Info("node agent registered",
		"instance_id", r.cfg.Node.InstanceID,
		"request_id", requestID,
	)
	return registered.GetNodeId(), nil
}

func (r *Runner) sendHeartbeat(ctx context.Context, nodeID string) error {
	logger := r.logger.With("node_id", nodeID)

	reqCtx, cancel := context.WithTimeout(ctx, agentconfig.HeartbeatRequestTimeout)
	defer cancel()
	requestID := transport.EnsureRequestID("")
	reqCtx = transport.ContextWithRequestID(reqCtx, requestID)
	ack, err := r.controlClient.SendHeartbeat(reqCtx, &nodeagentv1.HeartbeatRequest{
		NodeId:              nodeID,
		CpuMilliAllocatable: int32(r.cfg.ResolvedCapacity.Allocatable.CPUMilli),
		MemoryMiAllocatable: int32(r.cfg.ResolvedCapacity.Allocatable.MemoryMi),
	})
	if err != nil {
		return err
	}

	logger.Info("node heartbeat accepted",
		"status", ack.GetObservedStatus(),
		"request_id", requestID,
	)
	return nil
}

func withWorkItemLogFields(logger *slog.Logger, item *nodeagentv1.WorkItem) *slog.Logger {
	if item == nil {
		return logger
	}
	return logger.With(
		"service_id", item.GetServiceId(),
		"plan_id", item.GetPlanId(),
		"execution_id", item.GetExecutionId(),
	)
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

	gcCtx, cancel := context.WithTimeout(ctx, agentconfig.CleanupHardTimeout)
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
	if agentclient.IsUnknownNode(err) {
		r.logger.Warn("node is no longer known by cloud-plane, clearing local state",
			"node_id", nodeID,
			"error", err,
		)
		r.mu.Lock()
		r.nodeID = ""
		r.runtimeResetDone = false
		r.mu.Unlock()
		return
	}
}

func (r *Runner) startWorkloadLogs(req workloadlogs.StartRequest) {
	if r.workloadLogs == nil {
		return
	}
	r.workloadLogs.Start(req)
}

func (r *Runner) registerRequest() *nodeagentv1.RegisterNodeRequest {
	return &nodeagentv1.RegisterNodeRequest{
		Provider:      r.cfg.Node.Provider,
		Region:        r.cfg.Node.Region,
		Name:          r.cfg.Node.Name,
		PrivateIp:     r.cfg.Node.PrivateIP,
		InstanceId:    r.cfg.Node.InstanceID,
		InstanceType:  r.cfg.Node.InstanceType,
		CpuMilliTotal: int32(r.cfg.ResolvedCapacity.Total.CPUMilli),
		MemoryMiTotal: int32(r.cfg.ResolvedCapacity.Total.MemoryMi),
	}
}
