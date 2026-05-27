package daemon

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"mini-cloud/internal/common/logctx"
	"mini-cloud/internal/contract/nodeagentapi"
	agentclient "mini-cloud/internal/nodeagent/client"
	agentconfig "mini-cloud/internal/nodeagent/config"
	"mini-cloud/internal/nodeagent/runtime"
	"mini-cloud/internal/nodeagent/state"
	"mini-cloud/internal/nodeagent/work"
	"mini-cloud/internal/nodeagent/workloadlogs"
)

// ControlClient 定义 daemon 与控制面通信所需的完整客户端能力。
type ControlClient interface {
	// Client 提供任务拉取和执行结果上报能力。
	work.Client
	// RegisterNode 使用注册信息向控制面注册当前节点。
	RegisterNode(context.Context, nodeagentapi.RegisterNodeRequest) (nodeagentapi.RegisterNodeResponse, error)
	// SendHeartbeat 向控制面发送指定节点的一次心跳。
	SendHeartbeat(context.Context, string, nodeagentapi.HeartbeatRequest) (nodeagentapi.HeartbeatResponse, error)
	// SetSessionToken 设置后续节点级请求使用的会话令牌。
	SetSessionToken(string)
	// Close 释放控制面客户端资源。
	Close() error
}

// Runtime 定义 daemon 管理本机工作负载运行时所需的完整能力。
type Runtime interface {
	// Runtime 提供执行状态机需要的容器启动、停止和日志读取能力。
	work.Runtime
	// LogFollower 提供工作负载日志采集需要的持续日志跟随能力。
	runtime.LogFollower
	// CountRunning 汇报当前 runtime 可见的所有运行中容器。
	CountRunning(context.Context) (int, error)
	// GarbageCollect 清理 runtime 本地孤儿资源。
	GarbageCollect(context.Context) error
	// Close 释放运行时客户端资源和本地跟踪的临时资源。
	Close() error
}

// Runner 持有 node-agent daemon 主循环运行所需的组件和内存状态。
type Runner struct {
	// logger 记录 daemon 生命周期、心跳和执行日志。
	logger *slog.Logger
	// cfg 是已校验的 node-agent 配置。
	cfg agentconfig.Config
	// controlClient 访问控制面的注册、心跳、任务和上报接口。
	controlClient ControlClient
	// containerRuntime 启动、停止和观测本机工作负载容器。
	containerRuntime Runtime
	// workloadLogs 管理工作负载日志采集。
	workloadLogs *workloadlogs.Manager

	// mu 保护 state。
	mu sync.Mutex
	// state 保存当前节点会话和执行恢复状态。
	state state.State
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
		QueueSize:    cfg.Logs.QueueSize,
		BatchSize:    cfg.Logs.BatchMaxEntries,
		BatchWait:    cfg.Logs.BatchMaxWait,
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
	controlClient ControlClient,
	containerRuntime Runtime,
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

// Run 加载本地状态、尝试注册和恢复，然后运行心跳与任务循环直到上下文取消。
func (r *Runner) Run(ctx context.Context) error {
	// 无论 Run 以什么路径返回，都先释放控制面客户端连接。
	defer func() {
		// 关闭失败不改变 daemon 退出结果，只记录日志，避免掩盖主流程错误。
		if err := r.controlClient.Close(); err != nil {
			r.logger.Warn("close node control client failed", "error", err)
		}
	}()
	// 无论 Run 以什么路径返回，都释放本机容器运行时资源。
	defer func() {
		// 运行时关闭失败同样只记录日志；此时 daemon 已经进入退出路径。
		if err := r.containerRuntime.Close(); err != nil {
			r.logger.Warn("close node runtime failed", "error", err)
		}
	}()
	// 工作负载日志采集是可选能力；没有配置时不需要关闭。
	if r.workloadLogs != nil {
		// 确保所有日志采集 goroutine 在 daemon 退出前有机会 flush 和停止。
		defer func() {
			// 关闭日志管理器使用独立后台 context，避免外部 ctx 已取消导致无法清理。
			closeCtx, cancel := context.WithTimeout(context.Background(), r.timeout(r.cfg.Timeouts.CleanupHard))
			// 释放 timeout context 的 timer 资源。
			defer cancel()
			// 日志关闭失败不阻止 daemon 退出，只记录 warning。
			if err := r.workloadLogs.Close(closeCtx); err != nil {
				r.logger.Warn("close workload log manager failed", "error", err)
			}
		}()
	}

	// 从本地状态文件读取节点会话和执行恢复点；文件不存在时底层会返回空状态。
	storedState, err := state.Load(r.cfg.StateFile)
	if err != nil {
		// 状态文件读取失败会影响注册身份和恢复语义，因此直接返回错误。
		return err
	}
	// 有 NodeID 但没有 session token 的状态不可用于后续请求，需要强制重新注册。
	if storedState.NodeID != "" && strings.TrimSpace(storedState.SessionToken) == "" {
		r.logger.Info("node agent state is missing session token, forcing re-registration",
			"state_file", r.cfg.StateFile,
			"node_id", storedState.NodeID,
		)
		// 清空内存状态，使后面的 ensureNodeRegistration 走注册路径。
		storedState = state.State{}
	}
	// Runner.state 会被心跳循环、任务循环和恢复逻辑访问，写入时需要加锁。
	r.mu.Lock()
	// 将磁盘状态同步到 Runner 内存状态。
	r.state = storedState
	// 内存状态写入完成后立即释放锁。
	r.mu.Unlock()
	// 把本地恢复出的 session token 同步到控制面客户端，供心跳和任务请求使用。
	r.controlClient.SetSessionToken(storedState.SessionToken)

	// 启动时先尝试注册一次，尽早发现配置或控制面问题。
	if _, err := r.ensureNodeRegistration(ctx); err != nil {
		// 初始注册失败不退出；后续心跳和任务循环会继续重试注册。
		r.logger.Warn("node agent initial registration failed", "instance_id", r.cfg.RegisterInput.InstanceID, "error", err)
	}
	// 启动后执行一次运行时垃圾回收，例如清理孤儿投影文件目录。
	r.reconcileRuntime(ctx)
	// 根据本地执行恢复点恢复日志跟随，或把中断的执行上报为失败。
	r.recoverExecutions(ctx)

	// 为两个后台循环派生一个可单独取消的运行 context。
	runCtx, cancelRun := context.WithCancel(ctx)
	// 防御性兜底：即使 Run 在其他路径返回，也确保后台循环收到取消信号。
	defer cancelRun()
	// WaitGroup 用于等待心跳循环和任务循环都退出。
	var wg sync.WaitGroup
	// 当前会启动两个后台 goroutine：heartbeat loop 和 worker loop。
	wg.Add(2)
	// 启动心跳循环 goroutine。
	go func() {
		// 心跳循环退出时标记一个后台任务完成。
		defer wg.Done()
		// 心跳循环会立即执行一次，然后按 HeartbeatInterval 周期运行。
		r.runHeartbeatLoop(runCtx)
	}()
	// 启动任务执行循环 goroutine。
	go func() {
		// 任务循环退出时标记一个后台任务完成。
		defer wg.Done()
		// 任务循环会立即执行一次，然后按 WorkInterval 周期运行。
		r.runWorkerLoop(runCtx)
	}()

	// 阻塞等待外部取消 daemon，例如进程收到退出信号或上层 context 超时。
	<-ctx.Done()
	// 记录主循环停止原因，通常是 context canceled 或 context deadline exceeded。
	r.logger.Info("node agent loop stopped", "reason", ctx.Err())
	// 主 context 已结束，显式取消派生 context，通知两个后台循环尽快退出。
	cancelRun()

	// 创建关闭宽限期 timer，避免后台循环卡住时 Run 永久阻塞。
	shutdownTimer := time.NewTimer(r.timeout(r.cfg.Timeouts.ShutdownGrace))
	// Run 返回前停止 timer，释放 timer 资源。
	defer shutdownTimer.Stop()
	// stopped 用于把 wg.Wait() 的完成事件转换成 select 可以等待的 channel。
	stopped := make(chan struct{})
	// 在单独 goroutine 中等待两个后台循环退出。
	go func() {
		// wg.Wait() 返回后关闭 stopped，通知下面的 select：所有后台循环已停止。
		defer close(stopped)
		// 等待 heartbeat loop 和 worker loop 都执行完 wg.Done()。
		wg.Wait()
	}()
	// 等待“两个后台循环都退出”或“关闭宽限期超时”两者之一先发生。
	select {
	case <-stopped:
		// 两个后台循环都已正常退出，Run 可以继续返回。
	case <-shutdownTimer.C:
		// 超过关闭宽限期仍未全部退出，不再等待，避免 daemon 退出被无限阻塞。
		r.logger.Warn("node agent shutdown grace period elapsed")
		return nil
	}
	// daemon 主循环完成正常关闭。
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
		r.logger.Warn("node agent registration failed", "instance_id", r.cfg.RegisterInput.InstanceID, "error", err)
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
		r.logger.Warn("node agent registration failed before work poll", "instance_id", r.cfg.RegisterInput.InstanceID, "error", err)
		return
	}

	result, err := work.ExecuteNext(ctx, r.logger, r.controlClient, r.containerRuntime, work.Options{
		NodeID:               nodeID,
		NodePrivateIP:        r.cfg.RegisterInput.PrivateIP,
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
		Recorder:             r,
	})
	if err != nil {
		workLogger := logctx.WithLoggerFields(r.logger, logctx.Fields{NodeID: nodeID})
		if result.WorkItem != nil {
			workLogger = logctx.WithLoggerFields(workLogger, logctx.Fields{
				ServiceID:    result.WorkItem.ServiceID,
				DeploymentID: result.WorkItem.DeploymentID,
				ExecutionID:  result.WorkItem.ExecutionID,
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
				ServiceID:    result.WorkItem.ServiceID,
				DeploymentID: result.WorkItem.DeploymentID,
				ExecutionID:  result.WorkItem.ExecutionID,
			})
		}
		if result.Report != nil {
			reportStatus = string(result.Report.Ack.Execution.Status)
		}
		workLogger.Info("node work execution finished",
			"report_status", reportStatus,
		)
	}
}

// ensureNodeRegistration 返回当前节点 ID；本地无有效节点时向控制面注册。
func (r *Runner) ensureNodeRegistration(ctx context.Context) (string, error) {
	r.mu.Lock()
	if r.state.NodeID != "" {
		nodeID := r.state.NodeID
		r.mu.Unlock()
		return nodeID, nil
	}
	r.mu.Unlock()

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
	r.state.NodeID = registered.NodeID
	r.state.SessionToken = registered.SessionToken
	r.state.LastSyncedAt = time.Now().UTC()
	nextState := r.state
	r.mu.Unlock()

	if err := state.Save(r.cfg.StateFile, nextState); err != nil {
		return "", err
	}
	r.controlClient.SetSessionToken(registered.SessionToken)

	logctx.WithLoggerFields(r.logger, logctx.Fields{NodeID: registered.NodeID}).Info("node agent registered",
		"instance_id", r.cfg.RegisterInput.InstanceID,
		"request_id", logctx.RequestID(reqCtx),
	)
	return registered.NodeID, nil
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
	ack, err := r.controlClient.SendHeartbeat(reqCtx, nodeID, nodeagentapi.HeartbeatRequest{
		ReportedAt:   time.Now().UTC(),
		AgentVersion: r.cfg.AgentVersion,
		// 心跳上报的是静态 allocatable 预算：
		// total - systemReserved - agentReserved - evictionReserved。
		// 它不是宿主机实时 CPU idle，也不是 /proc/meminfo 的 MemAvailable。
		CPUMilliAllocatable: r.cfg.CPUMilliAllocatable,
		MemoryMiAllocatable: r.cfg.MemoryMiAllocatable,
		RunningContainers:   runningContainers,
		Status:              r.heartbeatStatus(),
	})
	if err != nil {
		return err
	}

	logger.Info("node heartbeat accepted",
		"status", ack.ObservedStatus,
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
	return nodeagentapi.NodeStatusReady
}

// reconcileRuntime 执行启动期 runtime 垃圾回收。
func (r *Runner) reconcileRuntime(ctx context.Context) {
	gcCtx, cancel := context.WithTimeout(ctx, r.timeout(r.cfg.Timeouts.CleanupHard))
	defer cancel()
	if err := r.containerRuntime.GarbageCollect(gcCtx); err != nil {
		r.logger.Warn("node runtime garbage collection failed", "error", err)
	}
}

// recoverExecutions 根据本地恢复点恢复日志跟随、清理终态或失败上报。
func (r *Runner) recoverExecutions(ctx context.Context) {
	r.mu.Lock()
	executions := make([]state.ExecutionState, 0, len(r.state.Executions))
	for _, execution := range r.state.Executions {
		executions = append(executions, execution)
	}
	r.mu.Unlock()

	for _, execution := range executions {
		switch execution.Phase {
		case string(work.PhaseRunningReported):
			r.recoverRunningExecution(execution)
		case string(work.PhaseFailedReported):
			if err := r.ClearExecution(execution.ExecutionID); err != nil {
				r.logger.Warn("clear recovered terminal execution state failed", "execution_id", execution.ExecutionID, "error", err)
			}
		default:
			r.failRecoveredExecution(ctx, execution)
		}
	}
}

// recoverRunningExecution 为已上报 running 的执行恢复工作负载日志跟随。
func (r *Runner) recoverRunningExecution(execution state.ExecutionState) {
	if r.workloadLogs == nil || strings.TrimSpace(execution.ContainerID) == "" {
		return
	}
	r.startWorkloadLogs(workloadlogs.StartRequest{
		ProjectID:     execution.ProjectID,
		ServiceID:     execution.ServiceID,
		ServiceName:   execution.ServiceName,
		DeploymentID:  execution.DeploymentID,
		ExecutionID:   execution.ExecutionID,
		ReplicaIndex:  execution.ReplicaIndex,
		NodeID:        execution.NodeID,
		ContainerID:   execution.ContainerID,
		ContainerName: execution.ContainerName,
	})
	r.logger.Info("recovered running execution log follower",
		"execution_id", execution.ExecutionID,
		"container_id", execution.ContainerID,
	)
}

// failRecoveredExecution 尝试清理重启前未到达已上报终态的执行，并向控制面上报 failed。
func (r *Runner) failRecoveredExecution(ctx context.Context, execution state.ExecutionState) {
	if strings.TrimSpace(execution.ContainerID) != "" {
		stopCtx, cancel := context.WithTimeout(context.Background(), r.timeout(r.cfg.Timeouts.RuntimeStop))
		if err := r.containerRuntime.Stop(stopCtx, execution.ContainerID); err != nil {
			r.logger.Warn("stop recovered in-flight execution failed",
				"execution_id", execution.ExecutionID,
				"container_id", execution.ContainerID,
				"error", err,
			)
		}
		cancel()
	}

	nodeID := execution.NodeID
	if strings.TrimSpace(nodeID) == "" {
		r.mu.Lock()
		nodeID = r.state.NodeID
		r.mu.Unlock()
	}
	reportCtx, cancel := context.WithTimeout(context.Background(), r.timeout(r.cfg.Timeouts.Report))
	defer cancel()
	reportCtx = logctx.WithFields(reportCtx, logctx.Fields{
		RequestID:    logctx.EnsureRequestID(""),
		NodeID:       nodeID,
		ServiceID:    execution.ServiceID,
		DeploymentID: execution.DeploymentID,
		ExecutionID:  execution.ExecutionID,
	})
	_, err := r.controlClient.ReportExecution(reportCtx, nodeID, execution.ExecutionID, nodeagentapi.ReportExecutionRequest{
		Status:        nodeagentapi.ExecutionStatusFailed,
		Reason:        "node-agent restarted before execution reached a reported terminal state",
		ContainerID:   execution.ContainerID,
		ContainerName: fallback(execution.ContainerName, execution.ExecutionID),
		HostPort:      execution.HostPort,
	})
	if err != nil {
		r.logger.Warn("report recovered in-flight execution failed",
			"execution_id", execution.ExecutionID,
			"error", err,
		)
		return
	}
	if err := r.ClearExecution(execution.ExecutionID); err != nil {
		r.logger.Warn("clear recovered failed execution state failed", "execution_id", execution.ExecutionID, "error", err)
	}
}

// handleNodeError 在控制面认为节点会话无效时清除本地注册状态。
func (r *Runner) handleNodeError(nodeID string, err error) {
	if agentclient.IsInvalidSession(err) {
		r.logger.Warn("node session is no longer valid, clearing local state",
			"node_id", nodeID,
			"state_file", r.cfg.StateFile,
			"error", err,
		)
		if clearErr := state.Clear(r.cfg.StateFile); clearErr != nil {
			r.logger.Warn("clear node state failed", "state_file", r.cfg.StateFile, "error", clearErr)
		}
		r.mu.Lock()
		r.state = state.State{}
		r.mu.Unlock()
		r.controlClient.SetSessionToken("")
		return
	}
}

// SaveExecution 保存执行恢复点，供 daemon 重启后恢复处理。
func (r *Runner) SaveExecution(execution state.ExecutionState) error {
	r.mu.Lock()
	if r.state.Executions == nil {
		r.state.Executions = map[string]state.ExecutionState{}
	}
	r.state.Executions[execution.ExecutionID] = execution
	nextState := r.state
	r.mu.Unlock()
	return state.Save(r.cfg.StateFile, nextState)
}

// ClearExecution 删除指定执行的本地恢复点。
func (r *Runner) ClearExecution(executionID string) error {
	r.mu.Lock()
	if r.state.Executions != nil {
		delete(r.state.Executions, executionID)
	}
	nextState := r.state
	r.mu.Unlock()
	return state.Save(r.cfg.StateFile, nextState)
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

// fallback 返回非空 value，否则返回 fallbackValue。
func fallback(value string, fallbackValue string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallbackValue
}
