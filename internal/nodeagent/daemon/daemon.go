package daemon

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"mini-cloud/internal/common/logctx"
	nodeagentv1 "mini-cloud/internal/gen/proto/minicloud/nodeagent/v1"
	agentclient "mini-cloud/internal/nodeagent/client"
	agentconfig "mini-cloud/internal/nodeagent/config"
	"mini-cloud/internal/nodeagent/runtime"
	"mini-cloud/internal/nodeagent/work"
	"mini-cloud/internal/nodeagent/workloadlogs"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// Runner 持有 node-agent daemon 主循环运行所需的组件和内存状态。
type Runner struct {
	// logger 记录 daemon 生命周期、心跳和执行日志。
	logger *slog.Logger
	// cfg 是已校验的 node-agent 配置。
	cfg agentconfig.Config
	// controlClient 访问控制面的注册、心跳、任务和上报接口。
	controlClient *agentclient.Client
	// containerRuntime 启动、停止和观测本机工作负载容器。
	containerRuntime runtime.Runtime
	// workloadLogs 管理工作负载日志采集。
	workloadLogs *workloadlogs.Manager

	// registerMu 串行化进程内注册，避免 heartbeat/work 并发重新注册。
	registerMu sync.Mutex
	// mu 保护 nodeID。
	mu sync.Mutex
	// nodeID 是本次进程启动后 cloud-plane 返回的稳定节点 ID。
	nodeID string
	// resetMu 串行化启动期 runtime reset，避免 heartbeat/work 并发重置。
	resetMu sync.Mutex
	// runtimeResetDone 表示本进程拿到当前 nodeID 后已经清理过旧 workload 容器。
	runtimeResetDone bool
}

// Run 根据配置创建生产组件，并运行 node-agent daemon 主循环。
func Run(ctx context.Context, logger *slog.Logger, cfg agentconfig.Config) error {
	if logger == nil {
		logger = slog.Default()
	}
	client := agentclient.New(agentclient.Config{
		ServerURL:      cfg.ServerURL,
		BootstrapToken: cfg.BootstrapToken,
	})
	containerRuntime, err := runtime.New(logger, runtime.Config{
		Type: cfg.Runtime.Type,
	})
	if err != nil {
		return err
	}
	workloadLogs, err := workloadlogs.NewManager(logger, workloadlogs.Config{
		LokiURL:      cfg.WorkloadLogLokiURL,
		LokiTenantID: cfg.WorkloadLogLokiTenant,
		PlatformName: cfg.PlatformName,
		PushTimeout:  cfg.Logs.PushTimeout,
	}, containerRuntime)
	if err != nil {
		if closeErr := containerRuntime.Close(); closeErr != nil {
			logger.Warn("close runtime after workload log manager setup failed", "error", closeErr)
		}
		return err
	}
	return NewRunner(logger, cfg, client, containerRuntime, workloadLogs).Run(ctx)
}

// NewRunner 组装一个使用显式组件的 Runner。
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

// Run 注册当前进程、重置本地 runtime，并运行心跳与任务循环直到上下文取消。
func (r *Runner) Run(ctx context.Context) error {
	defer func() {
		if err := r.controlClient.Close(); err != nil {
			r.logger.Warn("close node control client failed", "error", err)
		}
	}()
	defer func() {
		if err := r.containerRuntime.Close(); err != nil {
			r.logger.Warn("close node runtime failed", "error", err)
		}
	}()
	if r.workloadLogs != nil {
		defer func() {
			closeCtx, cancel := context.WithTimeout(context.Background(), r.timeout(r.cfg.Timeouts.CleanupHard))
			defer cancel()
			if err := r.workloadLogs.Close(closeCtx); err != nil {
				r.logger.Warn("close workload log manager failed", "error", err)
			}
		}()
	}

	// 后台循环启动前先注册并重置本机 runtime，避免心跳和任务循环并发触发首次注册。
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

// runHeartbeatLoop 立即执行一次心跳周期，然后按配置间隔持续发送心跳。
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

// runWorkerLoop 立即执行一次任务周期，然后按配置间隔持续拉取任务。
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

// tryHeartbeatCycle 确保节点已注册并发送一次心跳。
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

// tryWorkCycle 确保节点已注册并尝试执行下一项任务。
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
		NodeID:               nodeID,
		NodePrivateIP:        r.cfg.RegisterInput.GetPrivateIp(),
		HostPortMin:          r.cfg.Runtime.HostPortMin,
		HostPortMax:          r.cfg.Runtime.HostPortMax,
		PlatformName:         r.cfg.Work.PlatformName,
		ReadinessAttempts:    r.cfg.Work.ReadinessAttempts,
		ReadinessInterval:    r.cfg.Work.ReadinessInterval,
		ReadinessTimeout:     r.cfg.Work.ReadinessTimeout,
		RuntimeTimeout:       r.cfg.Timeouts.RuntimeStart,
		RuntimeStopTimeout:   r.cfg.Timeouts.RuntimeStop,
		RuntimeLogsTimeout:   r.cfg.Timeouts.RuntimeLogs,
		PollWorkTimeout:      r.cfg.Timeouts.PollWork,
		ReportTimeout:        r.cfg.Timeouts.Report,
		CleanupHardTimeout:   r.cfg.Timeouts.CleanupHard,
		LogTail:              r.cfg.Work.LogTail,
		WorkloadLogs:         r.startWorkloadLogs,
		WorkloadOTLPEndpoint: r.cfg.Work.WorkloadOTLPEndpoint,
		EgressProxyEnabled:   r.cfg.Network.EgressProxy.Enabled,
		EgressProxyEndpoint:  r.cfg.Network.EgressProxy.Endpoint,
		EgressProxyNoProxy:   r.cfg.Network.EgressProxy.NoProxy,
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

// ensureNodeRegistration 返回当前进程内节点 ID；没有有效会话时向控制面重新注册。
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

// registerNode 使用 bootstrap token 注册当前进程，并只在内存中保存 nodeID 和 session token。
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

// sendHeartbeat 尝试统计运行中容器数量并向控制面发送心跳。
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
		NodeId:       nodeID,
		ReportedAt:   timestamppb.New(time.Now().UTC()),
		AgentVersion: r.cfg.AgentVersion,
		// 心跳上报的是静态 allocatable 预算：
		// total - systemReserved - agentReserved - evictionReserved。
		// 它不是宿主机实时 CPU idle，也不是 /proc/meminfo 的 MemAvailable。
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

// heartbeatStatus 返回当前心跳应上报的节点运行态。
//
// 当前 node-agent 只在主循环可运行时发送心跳，因此固定上报 ready。
// draining/offline 由控制面维护；后续接入本机 runtime/node 自检后，再在这里派生 not_ready。
func (r *Runner) heartbeatStatus() string {
	return "ready"
}

// resetRuntimeOnce 在当前进程注册后停止本节点旧 workload 容器，并清理孤儿本地资源。
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

// handleNodeError 在控制面认为节点会话无效时清除本地注册状态。
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

// startWorkloadLogs 在日志管理器存在时启动指定执行的日志采集。
func (r *Runner) startWorkloadLogs(req workloadlogs.StartRequest) {
	if r.workloadLogs == nil {
		return
	}
	r.workloadLogs.Start(req)
}

// timeout 返回正数超时值；未配置时使用 1 秒兜底。
func (r *Runner) timeout(value time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return time.Second
}
