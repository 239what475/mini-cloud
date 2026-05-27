package work

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"mini-cloud/internal/common/logctx"
	"mini-cloud/internal/contract/nodeagentapi"
	"mini-cloud/internal/nodeagent/runtime"
	"mini-cloud/internal/nodeagent/state"
	"mini-cloud/internal/nodeagent/workloadlogs"
	"mini-cloud/internal/nodeagent/workloadreadiness"
	"mini-cloud/internal/nodeagent/workloadtelemetry"
)

// Client 定义执行状态机访问控制面任务和上报结果所需的能力。
type Client interface {
	// PollExecutionWork 拉取指定节点的下一项执行任务。
	PollExecutionWork(context.Context, string) (*nodeagentapi.WorkItem, error)
	// ReportExecution 上报指定执行的运行中或终态结果。
	ReportExecution(context.Context, string, string, nodeagentapi.ReportExecutionRequest) (nodeagentapi.ReportExecutionResponse, error)
}

// ReadinessWaiter 定义等待工作负载 readiness 端点通过的能力。
type ReadinessWaiter interface {
	// Wait 按配置等待工作负载 readiness 端点通过，并返回完整观测结果。
	Wait(context.Context, workloadreadiness.Config) workloadreadiness.Result
}

// Runtime 定义执行状态机启动、停止和读取工作负载容器所需的最小运行时能力。
type Runtime interface {
	// Run 创建并启动一个工作负载容器。
	Run(context.Context, runtime.RunInput) (runtime.RunResult, error)
	// Stop 停止指定容器。
	Stop(context.Context, string) error
	// Logs 返回指定容器尾部日志文本。
	Logs(context.Context, string, int) (string, error)
}

// WorkloadLogStarter 是启动工作负载日志采集的函数。
type WorkloadLogStarter func(workloadlogs.StartRequest)

// ExecutionRecorder 定义执行状态机向本地状态存储写入恢复点所需的能力。
type ExecutionRecorder interface {
	// SaveExecution 保存指定执行的最新恢复状态。
	SaveExecution(state.ExecutionState) error
}

// ExecutionPhase 表示执行状态机阶段，其中部分阶段会作为恢复点持久化。

const (
	// PhasePolled 表示执行任务已从控制面拉取。
	PhasePolled = "polled"
	// PhaseValidated 表示执行任务契约校验已通过。
	PhaseValidated = "validated"
	// PhaseStarting 表示即将启动运行时容器。
	PhaseStarting = "starting"
	// PhaseStarted 表示运行时容器已启动。
	PhaseStarted = "started"
	// PhaseLogsAttached 表示工作负载日志采集已启动或已跳过。
	PhaseLogsAttached = "logs_attached"
	// PhaseReadinessChecking 表示正在等待工作负载 readiness 端点通过。
	PhaseReadinessChecking = "readiness_checking"
	// PhasePromoting 表示新执行 readiness 通过，准备替换旧执行。
	PhasePromoting = "promoting"
	// PhaseReportingRun 表示正在向控制面上报运行中状态。
	PhaseReportingRun = "reporting_running"
	// PhaseRunningReported 表示运行中状态已成功上报。
	PhaseRunningReported = "running_reported"
	// PhaseCleaningFailed 表示 readiness 探测失败后正在清理新容器。
	PhaseCleaningFailed = "cleaning_failed"
	// PhaseFailedReported 表示失败状态已成功上报。
	PhaseFailedReported = "failed_reported"
)

// Options 配置一次执行状态机运行所需的上下文、超时和可选能力。
type Options struct {
	// NodeID 是当前 node-agent 节点 ID。
	NodeID string
	// NodePrivateIP 是 workload hostPort 绑定的节点私网地址，供外置 Caddy 从私网访问。
	NodePrivateIP string
	// HostPortMin 是 workload hostPort 显式分配范围下限。
	HostPortMin int
	// HostPortMax 是 workload hostPort 显式分配范围上限。
	HostPortMax int
	// PlatformName 是注入工作负载遥测元数据的平台名称。
	PlatformName string
	// ReadinessAttempts 是工作负载 readiness 探测尝试次数。
	ReadinessAttempts int
	// ReadinessInterval 是 readiness 探测尝试间隔。
	ReadinessInterval time.Duration
	// ReadinessTimeout 是单次 readiness HTTP 请求超时时间。
	ReadinessTimeout time.Duration
	// RuntimeTimeout 是运行时创建并启动工作负载的超时时间。
	RuntimeTimeout time.Duration
	// RuntimeStopTimeout 是停止工作负载容器的超时时间。
	RuntimeStopTimeout time.Duration
	// RuntimeLogsTimeout 是读取容器日志的超时时间。
	RuntimeLogsTimeout time.Duration
	// PollWorkTimeout 是拉取执行任务的超时时间。
	PollWorkTimeout time.Duration
	// ReportTimeout 是上报执行结果和保存恢复点的超时时间。
	ReportTimeout time.Duration
	// CleanupHardTimeout 是其他超时未配置时的兜底超时时间。
	CleanupHardTimeout time.Duration
	// LogTail 是失败报告中附带的容器日志尾部行数。
	LogTail int
	// WorkloadLogs 是可选的工作负载日志采集启动器。
	WorkloadLogs WorkloadLogStarter
	// WorkloadOTLPEndpoint 是注入工作负载环境变量的 OTLP endpoint。
	WorkloadOTLPEndpoint string
	// EgressProxyEnabled 表示是否向 workload 注入 HTTP/HTTPS proxy 环境变量。
	EgressProxyEnabled bool
	// EgressProxyEndpoint 是 workload 使用的 HTTP/HTTPS proxy URL。
	EgressProxyEndpoint string
	// EgressProxyNoProxy 是 workload 不走 proxy 的地址、域名或网段列表。
	EgressProxyNoProxy []string
	// ReadinessWaiter 是可注入的 workload readiness 等待实现。
	ReadinessWaiter ReadinessWaiter
	// Recorder 是可选的执行恢复状态记录器。
	Recorder ExecutionRecorder
}

// Result 描述一次 ExecuteNext 调用的执行结果和最终阶段。
type Result struct {
	// NodeID 是本次执行状态机所属节点 ID。
	NodeID string `json:"nodeID"`
	// WorkFound 表示本次轮询是否拿到了执行任务。
	WorkFound bool `json:"workFound"`
	// WorkItem 是控制面返回的执行任务。
	WorkItem *nodeagentapi.WorkItem `json:"workItem,omitempty"`
	// RuntimeRun 是运行时容器启动结果。
	RuntimeRun *runtime.RunResult `json:"runtimeRun,omitempty"`
	// Readiness 是工作负载 readiness 探测结果。
	Readiness *workloadreadiness.Result `json:"readiness,omitempty"`
	// Report 是最后一次执行结果上报响应。
	Report *nodeagentapi.ReportExecutionResponse `json:"report,omitempty"`
	// string 是本次状态机推进到的最后阶段。
	Phase string `json:"phase,omitempty"`
}

// Executor 执行单个任务拉取和工作负载启动状态机。
type Executor struct {
	// logger 记录执行状态机日志。
	logger *slog.Logger
	// client 访问控制面任务和上报接口。
	client Client
	// containerRuntime 启动、停止和读取工作负载容器。
	containerRuntime Runtime
	// readinessWaiter 等待工作负载 readiness 端点通过。
	readinessWaiter ReadinessWaiter
	// opts 保存状态机配置和可选依赖。
	opts Options
}

// NewExecutor 创建执行状态机实例，并补齐默认 readiness 探测器。
func NewExecutor(logger *slog.Logger, client Client, containerRuntime Runtime, opts Options) Executor {
	readinessWaiter := opts.ReadinessWaiter
	if readinessWaiter == nil {
		readinessWaiter = workloadreadiness.NewChecker(nil)
	}
	return Executor{
		logger:           logger,
		client:           client,
		containerRuntime: containerRuntime,
		readinessWaiter:  readinessWaiter,
		opts:             opts,
	}
}

// ExecuteNext 使用临时 Executor 拉取并执行下一项任务。
func ExecuteNext(ctx context.Context, logger *slog.Logger, client Client, containerRuntime Runtime, opts Options) (Result, error) {
	return NewExecutor(logger, client, containerRuntime, opts).ExecuteNext(ctx)
}

// ExecuteNext 推进一次完整的任务拉取、启动、readiness 探测和结果上报流程。
func (e Executor) ExecuteNext(ctx context.Context) (Result, error) {
	opts := e.opts
	result := Result{
		NodeID: opts.NodeID,
	}

	item, pollCtx, err := e.pollWork(ctx)
	if err != nil {
		return result, err
	}
	if item == nil {
		return result, nil
	}

	result.WorkFound = true
	result.WorkItem = item
	result.Phase = PhasePolled
	workLogger := e.workLogger(item)
	workLogger.Info("claimed execution work", "request_id", logctx.RequestID(pollCtx))
	if err := e.record(item, PhasePolled, runtime.RunResult{}, ""); err != nil {
		return result, err
	}

	if err := item.Validate(); err != nil {
		report, reason, reportErr := e.reportValidationFailure(ctx, item, err)
		if reportErr != nil {
			return result, fmt.Errorf("report failed execution after invalid work item: %w", reportErr)
		}
		result.Report = &report
		result.Phase = PhaseFailedReported
		e.logReport(workLogger, report)
		return result, fmt.Errorf("%s", reason)
	}
	result.Phase = PhaseValidated
	if err := e.record(item, PhaseValidated, runtime.RunResult{}, ""); err != nil {
		return result, err
	}

	result.Phase = PhaseStarting
	if err := e.record(item, PhaseStarting, runtime.RunResult{}, ""); err != nil {
		return result, err
	}
	runResult, err := e.runWorkItem(ctx, item)
	if err != nil {
		report, reason, reportErr := e.reportRuntimeStartFailure(ctx, item, err)
		if reportErr != nil {
			return result, fmt.Errorf("report failed execution after runtime start error: %w", reportErr)
		}
		result.Report = &report
		e.logReport(workLogger, report)
		return result, fmt.Errorf("%s", reason)
	}

	result.RuntimeRun = &runResult
	result.Phase = PhaseStarted
	if err := e.record(item, PhaseStarted, runResult, ""); err != nil {
		return result, err
	}
	workLogger.Info("runtime container started",
		"container_name", runResult.ContainerName,
		"container_id", runResult.ContainerID,
		"host_port", runResult.HostPort,
	)
	e.startWorkloadLogForwarding(item, runResult)
	result.Phase = PhaseLogsAttached
	if err := e.record(item, PhaseLogsAttached, runResult, ""); err != nil {
		return result, err
	}

	readinessHost := strings.TrimSpace(opts.NodePrivateIP)
	if readinessHost == "" {
		readinessHost = "127.0.0.1"
	}
	readinessURL := fmt.Sprintf("http://%s%s", net.JoinHostPort(readinessHost, fmt.Sprintf("%d", runResult.HostPort)), item.ReadinessPath)
	result.Phase = PhaseReadinessChecking
	if err := e.record(item, PhaseReadinessChecking, runResult, ""); err != nil {
		return result, err
	}
	readinessResult := e.readinessWaiter.Wait(ctx, workloadreadiness.Config{
		URL:      readinessURL,
		Attempts: opts.ReadinessAttempts,
		Interval: opts.ReadinessInterval,
		Timeout:  opts.ReadinessTimeout,
	})
	result.Readiness = &readinessResult

	if readinessResult.Passed {
		result.Phase = PhasePromoting
		if err := e.record(item, PhasePromoting, runResult, ""); err != nil {
			return result, err
		}
		result.Phase = PhaseReportingRun
		if err := e.record(item, PhaseReportingRun, runResult, ""); err != nil {
			return result, err
		}
		report, err := e.reportRunning(ctx, item, runResult, readinessURL)
		if err != nil {
			result.Report = &report
			return result, err
		}
		result.Report = &report
		result.Phase = PhaseRunningReported
		if err := e.record(item, PhaseRunningReported, runResult, string(report.Ack.Execution.Status)); err != nil {
			return result, err
		}
		e.logReport(workLogger, report)
		return result, nil
	}

	result.Phase = PhaseCleaningFailed
	if err := e.record(item, PhaseCleaningFailed, runResult, ""); err != nil {
		return result, err
	}
	report, reason, err := e.cleanupFailedRun(ctx, item, runResult, readinessResult)
	if err != nil {
		return result, err
	}
	result.Report = &report
	result.Phase = PhaseFailedReported
	if err := e.record(item, PhaseFailedReported, runResult, string(report.Ack.Execution.Status)); err != nil {
		return result, err
	}
	e.logReport(workLogger, report)
	return result, fmt.Errorf("%s", reason)
}

// pollWork 带超时和日志上下文字段拉取下一项执行任务。
func (e Executor) pollWork(ctx context.Context) (*nodeagentapi.WorkItem, context.Context, error) {
	pollCtx, cancel := context.WithTimeout(ctx, e.timeout(e.opts.PollWorkTimeout))
	defer cancel()
	pollCtx = logctx.WithFields(pollCtx, logctx.Fields{
		RequestID: logctx.EnsureRequestID(""),
		NodeID:    e.opts.NodeID,
	})
	item, err := e.client.PollExecutionWork(pollCtx, e.opts.NodeID)
	return item, pollCtx, err
}

// runWorkItem 将执行任务转换为运行时输入并启动容器。
func (e Executor) runWorkItem(ctx context.Context, item *nodeagentapi.WorkItem) (runtime.RunResult, error) {
	runCtx, cancelRun := context.WithTimeout(e.workContext(ctx, item), e.timeout(e.opts.RuntimeTimeout))
	defer cancelRun()

	env := workloadtelemetry.InjectWorkloadTelemetryEnv(item.Env, item, workloadtelemetry.WorkloadTelemetryEnvOptions{
		PlatformName:         e.opts.PlatformName,
		WorkloadOTLPEndpoint: e.opts.WorkloadOTLPEndpoint,
	})
	env = injectEgressProxyEnv(env, e.opts)
	return e.containerRuntime.Run(runCtx, runtime.RunInput{
		ContainerName:   item.ContainerName,
		NodeID:          e.opts.NodeID,
		ExecutionID:     item.ExecutionID,
		DeploymentID:    item.DeploymentID,
		ProjectID:       item.ProjectID,
		ServiceID:       item.ServiceID,
		RevisionID:      item.RevisionID,
		ProjectionRef:   item.ExecutionID,
		Image:           item.Image,
		Command:         item.Command,
		Args:            item.Args,
		Env:             env,
		ProjectedFiles:  item.ProjectedFiles,
		PersistentDirs:  item.PersistentDirs,
		ImageCredential: convertExecutionImageCredential(item.ImageCredential),
		ContainerPort:   item.ContainerPort,
		HostBindIP:      e.opts.NodePrivateIP,
		HostPortMin:     e.opts.HostPortMin,
		HostPortMax:     e.opts.HostPortMax,
	})
}

// injectEgressProxyEnv 向 workload 环境变量注入 runtime node 的统一 HTTP/HTTPS 出口代理。
func injectEgressProxyEnv(env map[string]string, opts Options) map[string]string {
	if !opts.EgressProxyEnabled || strings.TrimSpace(opts.EgressProxyEndpoint) == "" {
		return env
	}
	out := make(map[string]string, len(env)+6)
	for key, value := range env {
		out[key] = value
	}
	endpoint := strings.TrimSpace(opts.EgressProxyEndpoint)
	noProxy := strings.Join(opts.EgressProxyNoProxy, ",")
	out["HTTP_PROXY"] = endpoint
	out["HTTPS_PROXY"] = endpoint
	out["http_proxy"] = endpoint
	out["https_proxy"] = endpoint
	out["NO_PROXY"] = noProxy
	out["no_proxy"] = noProxy
	return out
}

// reportRunning 在新执行 readiness 通过后尝试停止被替换执行；成功后上报 running，失败则上报 failed。
func (e Executor) reportRunning(ctx context.Context, item *nodeagentapi.WorkItem, runResult runtime.RunResult, readinessURL string) (nodeagentapi.ReportExecutionResponse, error) {
	if err := e.stopSuperseded(ctx, item, runResult); err != nil {
		report, reportErr := e.reportFailed(ctx, item, nodeagentapi.ReportExecutionRequest{
			Reason:        err.Error(),
			ContainerID:   runResult.ContainerID,
			ContainerName: runResult.ContainerName,
			HostPort:      runResult.HostPort,
		})
		if reportErr != nil {
			return nodeagentapi.ReportExecutionResponse{}, fmt.Errorf("report failed execution after superseded stop error: %w", reportErr)
		}
		return report, err
	}

	report, err := e.report(ctx, item, nodeagentapi.ReportExecutionRequest{
		Status:                nodeagentapi.ExecutionStatusRunning,
		Reason:                fmt.Sprintf("readiness check passed at %s", readinessURL),
		ContainerID:           runResult.ContainerID,
		ContainerName:         runResult.ContainerName,
		HostPort:              runResult.HostPort,
		SupersededExecutionID: supersededExecutionID(item.SupersededExecution),
	})
	if err != nil {
		return nodeagentapi.ReportExecutionResponse{}, fmt.Errorf("report running execution: %w", err)
	}
	return report, nil
}

// stopSuperseded 在新容器 readiness 通过后停止被替换的旧容器；失败时清理新容器。
func (e Executor) stopSuperseded(ctx context.Context, item *nodeagentapi.WorkItem, runResult runtime.RunResult) error {
	if item.SupersededExecution == nil || item.SupersededExecution.ContainerID == "" {
		return nil
	}

	stopCtx, cancelStop := context.WithTimeout(e.detachedWorkContext(item), e.timeout(e.opts.RuntimeStopTimeout))
	stopErr := e.containerRuntime.Stop(stopCtx, item.SupersededExecution.ContainerID)
	cancelStop()
	if stopErr == nil {
		return nil
	}

	cleanupCtx, cancelCleanup := context.WithTimeout(e.detachedWorkContext(item), e.timeout(e.opts.RuntimeStopTimeout))
	_ = e.containerRuntime.Stop(cleanupCtx, runResult.ContainerID)
	cancelCleanup()

	return fmt.Errorf("%s", TruncateReason(fmt.Sprintf(
		"candidate passed readiness, but stopping superseded container %s failed: %v",
		item.SupersededExecution.ContainerName,
		stopErr,
	)))
}

// cleanupFailedRun 在 readiness 探测失败后采集日志、停止容器并上报失败。
func (e Executor) cleanupFailedRun(ctx context.Context, item *nodeagentapi.WorkItem, runResult runtime.RunResult, readinessResult workloadreadiness.Result) (nodeagentapi.ReportExecutionResponse, string, error) {
	logSnippet := ""
	logCtx, cancelLogs := context.WithTimeout(e.detachedWorkContext(item), e.timeout(e.opts.RuntimeLogsTimeout))
	logSnippet, _ = e.containerRuntime.Logs(logCtx, runResult.ContainerID, e.opts.LogTail)
	cancelLogs()

	stopCtx, cancelStop := context.WithTimeout(e.detachedWorkContext(item), e.timeout(e.opts.RuntimeStopTimeout))
	stopErr := e.containerRuntime.Stop(stopCtx, runResult.ContainerID)
	cancelStop()

	reason := BuildFailedExecutionReason(readinessResult, logSnippet, stopErr)
	report, reportErr := e.reportFailed(ctx, item, nodeagentapi.ReportExecutionRequest{
		Reason:        reason,
		ContainerID:   runResult.ContainerID,
		ContainerName: runResult.ContainerName,
		HostPort:      runResult.HostPort,
	})
	if reportErr != nil {
		return nodeagentapi.ReportExecutionResponse{}, "", fmt.Errorf("report failed execution: %w", reportErr)
	}
	return report, reason, nil
}

// reportRuntimeStartFailure 将容器启动错误转换为失败上报。
func (e Executor) reportRuntimeStartFailure(ctx context.Context, item *nodeagentapi.WorkItem, runErr error) (nodeagentapi.ReportExecutionResponse, string, error) {
	reason := TruncateReason(fmt.Sprintf("runtime start failed: %v", runErr))
	report, err := e.reportFailed(ctx, item, nodeagentapi.ReportExecutionRequest{
		Reason:        reason,
		ContainerName: item.ContainerName,
	})
	return report, reason, err
}

// reportValidationFailure 将任务校验错误转换为失败上报。
func (e Executor) reportValidationFailure(ctx context.Context, item *nodeagentapi.WorkItem, validationErr error) (nodeagentapi.ReportExecutionResponse, string, error) {
	reason := TruncateReason(fmt.Sprintf("invalid work item: %v", validationErr))
	report, err := e.reportFailed(ctx, item, nodeagentapi.ReportExecutionRequest{
		Reason:        reason,
		ContainerName: item.ContainerName,
	})
	return report, reason, err
}

// reportFailed 补齐 failed 状态和默认原因后上报执行结果。
func (e Executor) reportFailed(ctx context.Context, item *nodeagentapi.WorkItem, req nodeagentapi.ReportExecutionRequest) (nodeagentapi.ReportExecutionResponse, error) {
	req.Status = nodeagentapi.ExecutionStatusFailed
	if req.Reason == "" {
		req.Reason = "execution failed"
	}
	return e.report(ctx, item, req)
}

// report 带超时和日志上下文字段向控制面上报执行结果。
func (e Executor) report(ctx context.Context, item *nodeagentapi.WorkItem, req nodeagentapi.ReportExecutionRequest) (nodeagentapi.ReportExecutionResponse, error) {
	reportCtx, cancel := context.WithTimeout(e.detachedWorkContext(item), e.timeout(e.opts.ReportTimeout))
	defer cancel()
	reportCtx = logctx.WithFields(reportCtx, logctx.Fields{
		RequestID:    logctx.EnsureRequestID(""),
		NodeID:       e.opts.NodeID,
		ServiceID:    item.ServiceID,
		DeploymentID: item.DeploymentID,
		ExecutionID:  item.ExecutionID,
	})
	return e.client.ReportExecution(reportCtx, e.opts.NodeID, item.ExecutionID, req)
}

// startWorkloadLogForwarding 在日志采集依赖存在时尝试启动当前执行的工作负载日志采集。
func (e Executor) startWorkloadLogForwarding(item *nodeagentapi.WorkItem, runResult runtime.RunResult) {
	if e.opts.WorkloadLogs == nil {
		return
	}
	e.opts.WorkloadLogs(workloadlogs.StartRequest{
		ProjectID:     item.ProjectID,
		ServiceID:     item.ServiceID,
		ServiceName:   item.ServiceName,
		DeploymentID:  item.DeploymentID,
		ExecutionID:   item.ExecutionID,
		ReplicaIndex:  item.ReplicaIndex,
		NodeID:        e.opts.NodeID,
		ContainerID:   runResult.ContainerID,
		ContainerName: runResult.ContainerName,
	})
}

// workLogger 返回带节点和执行上下文字段的 logger。
func (e Executor) workLogger(item *nodeagentapi.WorkItem) *slog.Logger {
	return logctx.WithLoggerFields(e.logger, logctx.Fields{
		NodeID:       e.opts.NodeID,
		ServiceID:    item.ServiceID,
		DeploymentID: item.DeploymentID,
		ExecutionID:  item.ExecutionID,
	})
}

// workContext 在调用方上下文中追加节点和执行字段。
func (e Executor) workContext(ctx context.Context, item *nodeagentapi.WorkItem) context.Context {
	return logctx.WithFields(ctx, logctx.Fields{
		NodeID:       e.opts.NodeID,
		ServiceID:    item.ServiceID,
		DeploymentID: item.DeploymentID,
		ExecutionID:  item.ExecutionID,
	})
}

// detachedWorkContext 创建不继承调用方取消、deadline 和 value，只重新注入节点和执行字段的后台上下文。
func (e Executor) detachedWorkContext(item *nodeagentapi.WorkItem) context.Context {
	return logctx.WithFields(context.Background(), logctx.Fields{
		NodeID:       e.opts.NodeID,
		ServiceID:    item.ServiceID,
		DeploymentID: item.DeploymentID,
		ExecutionID:  item.ExecutionID,
	})
}

// logReport 记录控制面确认后的执行状态。
func (e Executor) logReport(logger *slog.Logger, report nodeagentapi.ReportExecutionResponse) {
	logger.Info("reported execution status", "status", report.Ack.Execution.Status)
}

// record 将当前执行阶段保存为本地恢复点。
func (e Executor) record(item *nodeagentapi.WorkItem, phase string, runResult runtime.RunResult, reportStatus string) error {
	if e.opts.Recorder == nil || item == nil {
		return nil
	}
	return e.opts.Recorder.SaveExecution(state.ExecutionState{
		ExecutionID:      item.ExecutionID,
		DeploymentID:     item.DeploymentID,
		ProjectID:        item.ProjectID,
		ServiceID:        item.ServiceID,
		ServiceName:      item.ServiceName,
		ReplicaIndex:     item.ReplicaIndex,
		RevisionID:       item.RevisionID,
		NodeID:           e.opts.NodeID,
		Phase:            phase,
		ContainerID:      runResult.ContainerID,
		ContainerName:    runResult.ContainerName,
		HostPort:         runResult.HostPort,
		ProjectionRef:    item.ExecutionID,
		LastReportStatus: reportStatus,
		UpdatedAt:        time.Now().UTC(),
	})
}

// timeout 返回显式超时，或按清理、运行时、1 秒的顺序选择兜底值。
func (e Executor) timeout(value time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	if e.opts.CleanupHardTimeout > 0 {
		return e.opts.CleanupHardTimeout
	}
	if e.opts.RuntimeTimeout > 0 {
		return e.opts.RuntimeTimeout
	}
	return time.Second
}

// BuildFailedExecutionReason 根据 readiness 探测、日志片段和清理错误构造失败原因。
func BuildFailedExecutionReason(readinessResult workloadreadiness.Result, logs string, stopErr error) string {
	lastObservation := workloadreadiness.Observation{}
	if len(readinessResult.Observations) > 0 {
		lastObservation = readinessResult.Observations[len(readinessResult.Observations)-1]
	}

	parts := []string{
		fmt.Sprintf("readiness check never passed for %s", readinessResult.URL),
	}
	if lastObservation.Error != "" {
		parts = append(parts, fmt.Sprintf("last error: %s", lastObservation.Error))
	}
	if lastObservation.StatusCode != 0 {
		parts = append(parts, fmt.Sprintf("last status: %d", lastObservation.StatusCode))
	}
	if strings.TrimSpace(logs) != "" {
		parts = append(parts, fmt.Sprintf("logs: %s", compactLogSnippet(logs)))
	}
	if stopErr != nil {
		parts = append(parts, fmt.Sprintf("cleanup warning: %v", stopErr))
	}
	return TruncateReason(strings.Join(parts, "; "))
}

// compactLogSnippet 压缩日志空白字符并限制失败原因长度。
func compactLogSnippet(value string) string {
	compact := strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	return TruncateReason(compact)
}

// TruncateReason 将执行状态原因裁剪到控制面可接受的长度。
func TruncateReason(value string) string {
	// maxLen 是控制面执行状态原因允许的最大长度。
	const maxLen = 400
	value = strings.TrimSpace(value)
	if len(value) <= maxLen {
		return value
	}
	return value[:maxLen-3] + "..."
}

// supersededExecutionID 安全返回被替换执行 ID。
func supersededExecutionID(item *nodeagentapi.SupersededExecution) string {
	if item == nil {
		return ""
	}
	return item.ExecutionID
}

// convertExecutionImageCredential 将控制面镜像凭据转换为运行时镜像凭据。
func convertExecutionImageCredential(value *nodeagentapi.ImageCredential) *runtime.ImageCredential {
	if value == nil {
		return nil
	}
	return &runtime.ImageCredential{
		Server:   value.Server,
		Username: value.Username,
		Password: value.Password,
	}
}
