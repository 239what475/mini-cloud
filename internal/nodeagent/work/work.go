package work

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"mini-cloud/internal/common/logctx"
	"mini-cloud/internal/common/projectedfile"
	nodeagentv1 "mini-cloud/internal/gen/proto/minicloud/nodeagent/v1"
	agentclient "mini-cloud/internal/nodeagent/client"
	"mini-cloud/internal/nodeagent/runtime"
	"mini-cloud/internal/nodeagent/workloadlogs"
)

// ReadinessWaiter 定义等待工作负载 readiness 端点通过的能力。
type ReadinessWaiter interface {
	// Wait 按配置等待工作负载 readiness 端点通过，并返回完整观测结果。
	Wait(context.Context, ReadinessConfig) ReadinessResult
}

// WorkloadLogStarter 是启动工作负载日志采集的函数。
type WorkloadLogStarter func(workloadlogs.StartRequest)

// ExecutionPhase 表示执行状态机阶段。
const (
	workActionDelete = "delete"

	executionStatusFailed     = "failed"
	executionStatusRunning    = "running"
	executionStatusSuperseded = "superseded"

	// PhasePolled 表示执行任务已从控制面拉取。
	PhasePolled = "polled"
	// PhaseValidated 表示执行任务契约校验已通过。
	PhaseValidated = "validated"
	// PhaseDeleting 表示正在清理已存在的工作负载容器。
	PhaseDeleting = "deleting"
	// PhaseDeleteReported 表示删除清理结果已成功上报。
	PhaseDeleteReported = "delete_reported"
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
}

// Result 描述一次 ExecuteNext 调用的执行结果和最终阶段。
type Result struct {
	// NodeID 是本次执行状态机所属节点 ID。
	NodeID string `json:"nodeID"`
	// WorkFound 表示本次轮询是否拿到了执行任务。
	WorkFound bool `json:"workFound"`
	// WorkItem 是控制面返回的执行任务。
	WorkItem *nodeagentv1.WorkItem `json:"workItem,omitempty"`
	// RuntimeRun 是运行时容器启动结果。
	RuntimeRun *runtime.RunResult `json:"runtimeRun,omitempty"`
	// Readiness 是工作负载 readiness 探测结果。
	Readiness *ReadinessResult `json:"readiness,omitempty"`
	// Report 是最后一次执行结果上报响应。
	Report *nodeagentv1.ReportExecutionResponse `json:"report,omitempty"`
	// string 是本次状态机推进到的最后阶段。
	Phase string `json:"phase,omitempty"`
}

// Executor 执行单个任务拉取和工作负载启动状态机。
type Executor struct {
	// logger 记录执行状态机日志。
	logger *slog.Logger
	// client 访问控制面任务和上报接口。
	client *agentclient.Client
	// containerRuntime 启动、停止和读取工作负载容器。
	containerRuntime runtime.Runtime
	// readinessWaiter 等待工作负载 readiness 端点通过。
	readinessWaiter ReadinessWaiter
	// opts 保存状态机配置和可选依赖。
	opts Options
}

// NewExecutor 创建执行状态机实例，并补齐默认 readiness 探测器。
func NewExecutor(logger *slog.Logger, client *agentclient.Client, containerRuntime runtime.Runtime, opts Options) Executor {
	readinessWaiter := opts.ReadinessWaiter
	if readinessWaiter == nil {
		readinessWaiter = NewReadinessChecker(nil)
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
func ExecuteNext(ctx context.Context, logger *slog.Logger, client *agentclient.Client, containerRuntime runtime.Runtime, opts Options) (Result, error) {
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

	if err := validateWorkItem(item); err != nil {
		report, reason, reportErr := e.reportValidationFailure(ctx, item, err)
		if reportErr != nil {
			return result, fmt.Errorf("report failed execution after invalid work item: %w", reportErr)
		}
		result.Report = report
		result.Phase = PhaseFailedReported
		e.logReport(workLogger, report)
		return result, fmt.Errorf("%s", reason)
	}
	result.Phase = PhaseValidated
	if normalizeWorkAction(item.GetAction()) == workActionDelete {
		result.Phase = PhaseDeleting
		report, err := e.deleteWorkItem(ctx, item)
		if err != nil {
			result.Report = report
			return result, err
		}
		result.Report = report
		result.Phase = PhaseDeleteReported
		e.logReport(workLogger, report)
		return result, nil
	}

	result.Phase = PhaseStarting
	runResult, err := e.runWorkItem(ctx, item)
	if err != nil {
		report, reason, reportErr := e.reportRuntimeStartFailure(ctx, item, err)
		if reportErr != nil {
			return result, fmt.Errorf("report failed execution after runtime start error: %w", reportErr)
		}
		result.Report = report
		e.logReport(workLogger, report)
		return result, fmt.Errorf("%s", reason)
	}

	result.RuntimeRun = &runResult
	result.Phase = PhaseStarted
	workLogger.Info("runtime container started",
		"container_name", runResult.ContainerName,
		"container_id", runResult.ContainerID,
		"host_port", runResult.HostPort,
	)
	e.startWorkloadLogForwarding(item, runResult)
	result.Phase = PhaseLogsAttached

	readinessHost := strings.TrimSpace(opts.NodePrivateIP)
	if readinessHost == "" {
		readinessHost = "127.0.0.1"
	}
	readinessURL := fmt.Sprintf("http://%s%s", net.JoinHostPort(readinessHost, fmt.Sprintf("%d", runResult.HostPort)), item.GetReadinessPath())
	result.Phase = PhaseReadinessChecking
	readinessResult := e.readinessWaiter.Wait(ctx, ReadinessConfig{
		URL:      readinessURL,
		Attempts: opts.ReadinessAttempts,
		Interval: opts.ReadinessInterval,
		Timeout:  opts.ReadinessTimeout,
	})
	result.Readiness = &readinessResult

	if readinessResult.Passed {
		result.Phase = PhasePromoting
		result.Phase = PhaseReportingRun
		report, err := e.reportRunning(ctx, item, runResult, readinessURL)
		if err != nil {
			result.Report = report
			return result, err
		}
		result.Report = report
		result.Phase = PhaseRunningReported
		e.logReport(workLogger, report)
		return result, nil
	}

	result.Phase = PhaseCleaningFailed
	report, reason, err := e.cleanupFailedRun(ctx, item, runResult, readinessResult)
	if err != nil {
		return result, err
	}
	result.Report = report
	result.Phase = PhaseFailedReported
	e.logReport(workLogger, report)
	return result, fmt.Errorf("%s", reason)
}

// deleteWorkItem 停止已有容器并将 execution 标记为 superseded；失败时上报 failed，供控制面保留删除中状态继续重试。
func (e Executor) deleteWorkItem(ctx context.Context, item *nodeagentv1.WorkItem) (*nodeagentv1.ReportExecutionResponse, error) {
	stopCtx, cancelStop := context.WithTimeout(e.detachedWorkContext(item), e.timeout(e.opts.RuntimeStopTimeout))
	stopErr := e.containerRuntime.Stop(stopCtx, item.GetContainerId())
	cancelStop()
	if stopErr != nil {
		report, reportErr := e.reportFailed(ctx, item, &nodeagentv1.ReportExecutionRequest{
			Reason:        TruncateReason(fmt.Sprintf("delete execution failed while stopping container %s: %v", item.GetContainerName(), stopErr)),
			ContainerId:   item.GetContainerId(),
			ContainerName: item.GetContainerName(),
			HostPort:      item.GetHostPort(),
		})
		if reportErr != nil {
			return nil, fmt.Errorf("report failed delete execution: %w", reportErr)
		}
		return report, stopErr
	}
	report, err := e.report(ctx, item, &nodeagentv1.ReportExecutionRequest{
		Status:        executionStatusSuperseded,
		Reason:        fmt.Sprintf("service deletion stopped container %s", item.GetContainerName()),
		ContainerId:   item.GetContainerId(),
		ContainerName: item.GetContainerName(),
		HostPort:      item.GetHostPort(),
	})
	if err != nil {
		return nil, fmt.Errorf("report deleted execution: %w", err)
	}
	return report, nil
}

// pollWork 带超时和日志上下文字段拉取下一项执行任务。
func (e Executor) pollWork(ctx context.Context) (*nodeagentv1.WorkItem, context.Context, error) {
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
func (e Executor) runWorkItem(ctx context.Context, item *nodeagentv1.WorkItem) (runtime.RunResult, error) {
	runCtx, cancelRun := context.WithTimeout(e.workContext(ctx, item), e.timeout(e.opts.RuntimeTimeout))
	defer cancelRun()

	env := injectTelemetryEnv(item.GetEnv(), item, telemetryEnvOptions{
		PlatformName:         e.opts.PlatformName,
		WorkloadOTLPEndpoint: e.opts.WorkloadOTLPEndpoint,
	})
	env = injectEgressProxyEnv(env, e.opts)
	return e.containerRuntime.Run(runCtx, runtime.RunInput{
		ContainerName:   item.GetContainerName(),
		NodeID:          e.opts.NodeID,
		ExecutionID:     item.GetExecutionId(),
		PlanID:          item.GetPlanId(),
		ServiceID:       item.GetServiceId(),
		ProjectionRef:   item.GetExecutionId(),
		Image:           item.GetImage(),
		Command:         append([]string(nil), item.GetCommand()...),
		Args:            append([]string(nil), item.GetArgs()...),
		Env:             env,
		ProjectedFiles:  projectedFilesFromProto(item.GetProjectedFiles()),
		ImageCredential: convertExecutionImageCredential(item.GetImageCredential()),
		ContainerPort:   int(item.GetContainerPort()),
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
func (e Executor) reportRunning(ctx context.Context, item *nodeagentv1.WorkItem, runResult runtime.RunResult, readinessURL string) (*nodeagentv1.ReportExecutionResponse, error) {
	if err := e.stopSuperseded(ctx, item, runResult); err != nil {
		report, reportErr := e.reportFailed(ctx, item, &nodeagentv1.ReportExecutionRequest{
			Reason:        err.Error(),
			ContainerId:   runResult.ContainerID,
			ContainerName: runResult.ContainerName,
			HostPort:      int32(runResult.HostPort),
		})
		if reportErr != nil {
			return nil, fmt.Errorf("report failed execution after superseded stop error: %w", reportErr)
		}
		return report, err
	}

	report, err := e.report(ctx, item, &nodeagentv1.ReportExecutionRequest{
		Status:                executionStatusRunning,
		Reason:                fmt.Sprintf("readiness check passed at %s", readinessURL),
		ContainerId:           runResult.ContainerID,
		ContainerName:         runResult.ContainerName,
		HostPort:              int32(runResult.HostPort),
		SupersededExecutionId: supersededExecutionID(item.GetSupersededExecution()),
	})
	if err != nil {
		return nil, fmt.Errorf("report running execution: %w", err)
	}
	return report, nil
}

// stopSuperseded 在新容器 readiness 通过后停止被替换的旧容器；失败时清理新容器。
func (e Executor) stopSuperseded(ctx context.Context, item *nodeagentv1.WorkItem, runResult runtime.RunResult) error {
	if item.GetSupersededExecution() == nil || item.GetSupersededExecution().GetContainerId() == "" {
		return nil
	}

	stopCtx, cancelStop := context.WithTimeout(e.detachedWorkContext(item), e.timeout(e.opts.RuntimeStopTimeout))
	stopErr := e.containerRuntime.Stop(stopCtx, item.GetSupersededExecution().GetContainerId())
	cancelStop()
	if stopErr == nil {
		return nil
	}

	cleanupCtx, cancelCleanup := context.WithTimeout(e.detachedWorkContext(item), e.timeout(e.opts.RuntimeStopTimeout))
	_ = e.containerRuntime.Stop(cleanupCtx, runResult.ContainerID)
	cancelCleanup()

	return fmt.Errorf("%s", TruncateReason(fmt.Sprintf(
		"candidate passed readiness, but stopping superseded container %s failed: %v",
		item.GetSupersededExecution().GetContainerName(),
		stopErr,
	)))
}

// cleanupFailedRun 在 readiness 探测失败后采集日志、停止容器并上报失败。
func (e Executor) cleanupFailedRun(ctx context.Context, item *nodeagentv1.WorkItem, runResult runtime.RunResult, readinessResult ReadinessResult) (*nodeagentv1.ReportExecutionResponse, string, error) {
	logSnippet := ""
	logCtx, cancelLogs := context.WithTimeout(e.detachedWorkContext(item), e.timeout(e.opts.RuntimeLogsTimeout))
	logSnippet, _ = e.containerRuntime.Logs(logCtx, runResult.ContainerID, e.opts.LogTail)
	cancelLogs()

	stopCtx, cancelStop := context.WithTimeout(e.detachedWorkContext(item), e.timeout(e.opts.RuntimeStopTimeout))
	stopErr := e.containerRuntime.Stop(stopCtx, runResult.ContainerID)
	cancelStop()

	reason := BuildFailedExecutionReason(readinessResult, logSnippet, stopErr)
	report, reportErr := e.reportFailed(ctx, item, &nodeagentv1.ReportExecutionRequest{
		Reason:        reason,
		ContainerId:   runResult.ContainerID,
		ContainerName: runResult.ContainerName,
		HostPort:      int32(runResult.HostPort),
	})
	if reportErr != nil {
		return nil, "", fmt.Errorf("report failed execution: %w", reportErr)
	}
	return report, reason, nil
}

// reportRuntimeStartFailure 将容器启动错误转换为失败上报。
func (e Executor) reportRuntimeStartFailure(ctx context.Context, item *nodeagentv1.WorkItem, runErr error) (*nodeagentv1.ReportExecutionResponse, string, error) {
	reason := TruncateReason(fmt.Sprintf("runtime start failed: %v", runErr))
	report, err := e.reportFailed(ctx, item, &nodeagentv1.ReportExecutionRequest{
		Reason:        reason,
		ContainerName: item.GetContainerName(),
	})
	if report == nil {
		return nil, reason, err
	}
	return report, reason, err
}

// reportValidationFailure 将任务校验错误转换为失败上报。
func (e Executor) reportValidationFailure(ctx context.Context, item *nodeagentv1.WorkItem, validationErr error) (*nodeagentv1.ReportExecutionResponse, string, error) {
	reason := TruncateReason(fmt.Sprintf("invalid work item: %v", validationErr))
	report, err := e.reportFailed(ctx, item, &nodeagentv1.ReportExecutionRequest{
		Reason:        reason,
		ContainerName: item.GetContainerName(),
	})
	if report == nil {
		return nil, reason, err
	}
	return report, reason, err
}

// reportFailed 补齐 failed 状态和默认原因后上报执行结果。
func (e Executor) reportFailed(ctx context.Context, item *nodeagentv1.WorkItem, req *nodeagentv1.ReportExecutionRequest) (*nodeagentv1.ReportExecutionResponse, error) {
	req.Status = executionStatusFailed
	if req.GetReason() == "" {
		req.Reason = "execution failed"
	}
	return e.report(ctx, item, req)
}

// report 带超时和日志上下文字段向控制面上报执行结果。
func (e Executor) report(ctx context.Context, item *nodeagentv1.WorkItem, req *nodeagentv1.ReportExecutionRequest) (*nodeagentv1.ReportExecutionResponse, error) {
	reportCtx, cancel := context.WithTimeout(e.detachedWorkContext(item), e.timeout(e.opts.ReportTimeout))
	defer cancel()
	reportCtx = logctx.WithFields(reportCtx, logctx.Fields{
		RequestID:   logctx.EnsureRequestID(""),
		NodeID:      e.opts.NodeID,
		ServiceID:   item.GetServiceId(),
		PlanID:      item.GetPlanId(),
		ExecutionID: item.GetExecutionId(),
	})
	req.NodeId = e.opts.NodeID
	req.ExecutionId = item.GetExecutionId()
	return e.client.ReportExecution(reportCtx, req)
}

// startWorkloadLogForwarding 在日志采集依赖存在时尝试启动当前执行的工作负载日志采集。
func (e Executor) startWorkloadLogForwarding(item *nodeagentv1.WorkItem, runResult runtime.RunResult) {
	if e.opts.WorkloadLogs == nil {
		return
	}
	e.opts.WorkloadLogs(workloadlogs.StartRequest{
		ServiceID:     item.GetServiceId(),
		ServiceName:   item.GetServiceName(),
		PlanID:        item.GetPlanId(),
		ExecutionID:   item.GetExecutionId(),
		NodeID:        e.opts.NodeID,
		ContainerID:   runResult.ContainerID,
		ContainerName: runResult.ContainerName,
	})
}

// workLogger 返回带节点和执行上下文字段的 logger。
func (e Executor) workLogger(item *nodeagentv1.WorkItem) *slog.Logger {
	return logctx.WithLoggerFields(e.logger, logctx.Fields{
		NodeID:      e.opts.NodeID,
		ServiceID:   item.GetServiceId(),
		PlanID:      item.GetPlanId(),
		ExecutionID: item.GetExecutionId(),
	})
}

// workContext 在调用方上下文中追加节点和执行字段。
func (e Executor) workContext(ctx context.Context, item *nodeagentv1.WorkItem) context.Context {
	return logctx.WithFields(ctx, logctx.Fields{
		NodeID:      e.opts.NodeID,
		ServiceID:   item.GetServiceId(),
		PlanID:      item.GetPlanId(),
		ExecutionID: item.GetExecutionId(),
	})
}

// detachedWorkContext 创建不继承调用方取消、deadline 和 value，只重新注入节点和执行字段的后台上下文。
func (e Executor) detachedWorkContext(item *nodeagentv1.WorkItem) context.Context {
	return logctx.WithFields(context.Background(), logctx.Fields{
		NodeID:      e.opts.NodeID,
		ServiceID:   item.GetServiceId(),
		PlanID:      item.GetPlanId(),
		ExecutionID: item.GetExecutionId(),
	})
}

// logReport 记录控制面确认后的执行状态。
func (e Executor) logReport(logger *slog.Logger, report *nodeagentv1.ReportExecutionResponse) {
	logger.Info("reported execution status", "status", report.GetAck().GetExecution().GetStatus())
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
func BuildFailedExecutionReason(readinessResult ReadinessResult, logs string, stopErr error) string {
	lastObservation := ReadinessObservation{}
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
func supersededExecutionID(item *nodeagentv1.SupersededExecution) string {
	if item == nil {
		return ""
	}
	return item.GetExecutionId()
}

// convertExecutionImageCredential 将控制面镜像凭据转换为运行时镜像凭据。
func convertExecutionImageCredential(value *nodeagentv1.ImageCredential) *runtime.ImageCredential {
	if value == nil {
		return nil
	}
	return &runtime.ImageCredential{
		Server:   value.GetServer(),
		Username: value.GetUsername(),
		Password: value.GetPassword(),
	}
}

func normalizeWorkAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "", "run":
		return "run"
	case workActionDelete:
		return workActionDelete
	default:
		return strings.ToLower(strings.TrimSpace(action))
	}
}

func validateWorkItem(item *nodeagentv1.WorkItem) error {
	if item == nil {
		return fmt.Errorf("work item is required")
	}
	action := normalizeWorkAction(item.GetAction())
	if action != "run" && action != workActionDelete {
		return fmt.Errorf("work action must be one of run, delete")
	}
	if strings.TrimSpace(item.GetExecutionId()) == "" {
		return fmt.Errorf("executionID is required")
	}
	if strings.TrimSpace(item.GetPlanId()) == "" {
		return fmt.Errorf("planID is required")
	}
	if strings.TrimSpace(item.GetNodeId()) == "" {
		return fmt.Errorf("nodeID is required")
	}
	if strings.TrimSpace(item.GetServiceId()) == "" {
		return fmt.Errorf("serviceID is required")
	}
	if strings.TrimSpace(item.GetContainerName()) == "" {
		return fmt.Errorf("containerName is required")
	}
	if action == workActionDelete {
		if strings.TrimSpace(item.GetContainerId()) == "" {
			return fmt.Errorf("containerID is required")
		}
		return nil
	}
	if strings.TrimSpace(item.GetImage()) == "" {
		return fmt.Errorf("image is required")
	}
	if item.GetContainerPort() <= 0 {
		return fmt.Errorf("containerPort must be greater than 0")
	}
	readinessPath := strings.TrimSpace(item.GetReadinessPath())
	if readinessPath == "" {
		return fmt.Errorf("readinessPath is required")
	}
	if !strings.HasPrefix(readinessPath, "/") {
		return fmt.Errorf("readinessPath must start with /")
	}
	for _, item := range item.GetProjectedFiles() {
		if item == nil {
			continue
		}
		file := projectedfile.File{
			MountPath: item.GetMountPath(),
			Content:   item.GetContent(),
			Mode:      item.GetMode(),
			Sensitive: item.GetSensitive(),
		}
		if err := file.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func projectedFilesFromProto(items []*nodeagentv1.ProjectedFile) []projectedfile.File {
	if len(items) == 0 {
		return nil
	}
	out := make([]projectedfile.File, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, projectedfile.File{
			MountPath: item.GetMountPath(),
			Content:   item.GetContent(),
			Mode:      item.GetMode(),
			Sensitive: item.GetSensitive(),
		})
	}
	return projectedfile.CloneFiles(out)
}
