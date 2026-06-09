package work

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	nodeagentv1 "mini-cloud/internal/gen/proto/minicloud/nodeagent/v1"
	"mini-cloud/internal/logctx"
	agentclient "mini-cloud/internal/nodeagent/client"
	"mini-cloud/internal/nodeagent/runtime"
	"mini-cloud/internal/nodeagent/workloadlogs"
	"mini-cloud/internal/projectedfile"
)

type ReadinessWaiter interface {
	Wait(context.Context, ReadinessConfig) ReadinessResult
}

type WorkloadLogStarter func(workloadlogs.StartRequest)

const (
	workActionDelete = "delete"

	executionStatusFailed     = "failed"
	executionStatusRunning    = "running"
	executionStatusSuperseded = "superseded"

	PhasePolled            = "polled"
	PhaseValidated         = "validated"
	PhaseDeleting          = "deleting"
	PhaseDeleteReported    = "delete_reported"
	PhaseStarting          = "starting"
	PhaseStarted           = "started"
	PhaseLogsAttached      = "logs_attached"
	PhaseReadinessChecking = "readiness_checking"
	PhasePromoting         = "promoting"
	PhaseReportingRun      = "reporting_running"
	PhaseRunningReported   = "running_reported"
	PhaseCleaningFailed    = "cleaning_failed"
	PhaseFailedReported    = "failed_reported"
)

type Options struct {
	Node            NodeOptions
	Runtime         RuntimeOptions
	Readiness       ReadinessOptions
	Timeouts        TimeoutOptions
	Observability   ObservabilityOptions
	Network         NetworkOptions
	ReadinessWaiter ReadinessWaiter
}

type NodeOptions struct {
	ID        string
	PrivateIP string
}

type RuntimeOptions struct {
	HostPortMin int
	HostPortMax int
}

type ReadinessOptions struct {
	Attempts int
	Interval time.Duration
	Timeout  time.Duration
}

type TimeoutOptions struct {
	RuntimeStart time.Duration
	RuntimeStop  time.Duration
	RuntimeLogs  time.Duration
	PollWork     time.Duration
	Report       time.Duration
	CleanupHard  time.Duration
}

type ObservabilityOptions struct {
	PlatformName         string
	WorkloadLogs         WorkloadLogStarter
	WorkloadOTLPEndpoint string
	LogTail              int
}

type NetworkOptions struct {
	EgressProxy EgressProxyOptions
}

type EgressProxyOptions struct {
	Enabled  bool
	Endpoint string
	NoProxy  []string
}

type Result struct {
	NodeID     string                               `json:"nodeID"`
	WorkFound  bool                                 `json:"workFound"`
	WorkItem   *nodeagentv1.WorkItem                `json:"workItem,omitempty"`
	RuntimeRun *runtime.RunResult                   `json:"runtimeRun,omitempty"`
	Readiness  *ReadinessResult                     `json:"readiness,omitempty"`
	Report     *nodeagentv1.ReportExecutionResponse `json:"report,omitempty"`
	Phase      string                               `json:"phase,omitempty"`
}

type Executor struct {
	logger           *slog.Logger
	client           *agentclient.Client
	containerRuntime runtime.Runtime
	readinessWaiter  ReadinessWaiter
	opts             Options
}

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

func ExecuteNext(ctx context.Context, logger *slog.Logger, client *agentclient.Client, containerRuntime runtime.Runtime, opts Options) (Result, error) {
	return NewExecutor(logger, client, containerRuntime, opts).ExecuteNext(ctx)
}

func (e Executor) ExecuteNext(ctx context.Context) (Result, error) {
	opts := e.opts
	result := Result{
		NodeID: opts.Node.ID,
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

	readinessHost := strings.TrimSpace(opts.Node.PrivateIP)
	if readinessHost == "" {
		readinessHost = "127.0.0.1"
	}
	readinessURL := fmt.Sprintf("http://%s%s", net.JoinHostPort(readinessHost, fmt.Sprintf("%d", runResult.HostPort)), item.GetReadinessPath())
	result.Phase = PhaseReadinessChecking
	readinessResult := e.readinessWaiter.Wait(ctx, ReadinessConfig{
		URL:      readinessURL,
		Attempts: opts.Readiness.Attempts,
		Interval: opts.Readiness.Interval,
		Timeout:  opts.Readiness.Timeout,
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

func (e Executor) deleteWorkItem(ctx context.Context, item *nodeagentv1.WorkItem) (*nodeagentv1.ReportExecutionResponse, error) {
	stopCtx, cancelStop := context.WithTimeout(e.detachedWorkContext(item), e.timeout(e.opts.Timeouts.RuntimeStop))
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

func (e Executor) pollWork(ctx context.Context) (*nodeagentv1.WorkItem, context.Context, error) {
	pollCtx, cancel := context.WithTimeout(ctx, e.timeout(e.opts.Timeouts.PollWork))
	defer cancel()
	pollCtx = logctx.WithFields(pollCtx, logctx.Fields{
		RequestID: logctx.EnsureRequestID(""),
		NodeID:    e.opts.Node.ID,
	})
	item, err := e.client.PollExecutionWork(pollCtx, e.opts.Node.ID)
	return item, pollCtx, err
}

func (e Executor) runWorkItem(ctx context.Context, item *nodeagentv1.WorkItem) (runtime.RunResult, error) {
	runCtx, cancelRun := context.WithTimeout(e.workContext(ctx, item), e.timeout(e.opts.Timeouts.RuntimeStart))
	defer cancelRun()

	env := injectTelemetryEnv(item.GetEnv(), item, telemetryEnvOptions{
		PlatformName:         e.opts.Observability.PlatformName,
		WorkloadOTLPEndpoint: e.opts.Observability.WorkloadOTLPEndpoint,
	})
	env = injectEgressProxyEnv(env, e.opts)
	return e.containerRuntime.Run(runCtx, runtime.RunInput{
		ContainerName:   item.GetContainerName(),
		NodeID:          e.opts.Node.ID,
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
		HostBindIP:      e.opts.Node.PrivateIP,
		HostPortMin:     e.opts.Runtime.HostPortMin,
		HostPortMax:     e.opts.Runtime.HostPortMax,
	})
}

func injectEgressProxyEnv(env map[string]string, opts Options) map[string]string {
	proxy := opts.Network.EgressProxy
	if !proxy.Enabled || strings.TrimSpace(proxy.Endpoint) == "" {
		return env
	}
	out := make(map[string]string, len(env)+6)
	for key, value := range env {
		out[key] = value
	}
	endpoint := strings.TrimSpace(proxy.Endpoint)
	noProxy := strings.Join(proxy.NoProxy, ",")
	out["HTTP_PROXY"] = endpoint
	out["HTTPS_PROXY"] = endpoint
	out["http_proxy"] = endpoint
	out["https_proxy"] = endpoint
	out["NO_PROXY"] = noProxy
	out["no_proxy"] = noProxy
	return out
}

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

func (e Executor) stopSuperseded(ctx context.Context, item *nodeagentv1.WorkItem, runResult runtime.RunResult) error {
	if item.GetSupersededExecution() == nil || item.GetSupersededExecution().GetContainerId() == "" {
		return nil
	}

	stopCtx, cancelStop := context.WithTimeout(e.detachedWorkContext(item), e.timeout(e.opts.Timeouts.RuntimeStop))
	stopErr := e.containerRuntime.Stop(stopCtx, item.GetSupersededExecution().GetContainerId())
	cancelStop()
	if stopErr == nil {
		return nil
	}

	cleanupCtx, cancelCleanup := context.WithTimeout(e.detachedWorkContext(item), e.timeout(e.opts.Timeouts.RuntimeStop))
	_ = e.containerRuntime.Stop(cleanupCtx, runResult.ContainerID)
	cancelCleanup()

	return fmt.Errorf("%s", TruncateReason(fmt.Sprintf(
		"candidate passed readiness, but stopping superseded container %s failed: %v",
		item.GetSupersededExecution().GetContainerName(),
		stopErr,
	)))
}

func (e Executor) cleanupFailedRun(ctx context.Context, item *nodeagentv1.WorkItem, runResult runtime.RunResult, readinessResult ReadinessResult) (*nodeagentv1.ReportExecutionResponse, string, error) {
	logSnippet := ""
	logCtx, cancelLogs := context.WithTimeout(e.detachedWorkContext(item), e.timeout(e.opts.Timeouts.RuntimeLogs))
	logSnippet, _ = e.containerRuntime.Logs(logCtx, runResult.ContainerID, e.opts.Observability.LogTail)
	cancelLogs()

	stopCtx, cancelStop := context.WithTimeout(e.detachedWorkContext(item), e.timeout(e.opts.Timeouts.RuntimeStop))
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

func (e Executor) reportFailed(ctx context.Context, item *nodeagentv1.WorkItem, req *nodeagentv1.ReportExecutionRequest) (*nodeagentv1.ReportExecutionResponse, error) {
	req.Status = executionStatusFailed
	if req.GetReason() == "" {
		req.Reason = "execution failed"
	}
	return e.report(ctx, item, req)
}

func (e Executor) report(ctx context.Context, item *nodeagentv1.WorkItem, req *nodeagentv1.ReportExecutionRequest) (*nodeagentv1.ReportExecutionResponse, error) {
	reportCtx, cancel := context.WithTimeout(e.detachedWorkContext(item), e.timeout(e.opts.Timeouts.Report))
	defer cancel()
	reportCtx = logctx.WithFields(reportCtx, logctx.Fields{
		RequestID:   logctx.EnsureRequestID(""),
		NodeID:      e.opts.Node.ID,
		ServiceID:   item.GetServiceId(),
		PlanID:      item.GetPlanId(),
		ExecutionID: item.GetExecutionId(),
	})
	req.NodeId = e.opts.Node.ID
	req.ExecutionId = item.GetExecutionId()
	return e.client.ReportExecution(reportCtx, req)
}

func (e Executor) startWorkloadLogForwarding(item *nodeagentv1.WorkItem, runResult runtime.RunResult) {
	if e.opts.Observability.WorkloadLogs == nil {
		return
	}
	e.opts.Observability.WorkloadLogs(workloadlogs.StartRequest{
		ServiceID:     item.GetServiceId(),
		ServiceName:   item.GetServiceName(),
		PlanID:        item.GetPlanId(),
		ExecutionID:   item.GetExecutionId(),
		NodeID:        e.opts.Node.ID,
		ContainerID:   runResult.ContainerID,
		ContainerName: runResult.ContainerName,
	})
}

func (e Executor) workLogger(item *nodeagentv1.WorkItem) *slog.Logger {
	return logctx.WithLoggerFields(e.logger, logctx.Fields{
		NodeID:      e.opts.Node.ID,
		ServiceID:   item.GetServiceId(),
		PlanID:      item.GetPlanId(),
		ExecutionID: item.GetExecutionId(),
	})
}

func (e Executor) workContext(ctx context.Context, item *nodeagentv1.WorkItem) context.Context {
	return logctx.WithFields(ctx, logctx.Fields{
		NodeID:      e.opts.Node.ID,
		ServiceID:   item.GetServiceId(),
		PlanID:      item.GetPlanId(),
		ExecutionID: item.GetExecutionId(),
	})
}

func (e Executor) detachedWorkContext(item *nodeagentv1.WorkItem) context.Context {
	return logctx.WithFields(context.Background(), logctx.Fields{
		NodeID:      e.opts.Node.ID,
		ServiceID:   item.GetServiceId(),
		PlanID:      item.GetPlanId(),
		ExecutionID: item.GetExecutionId(),
	})
}

func (e Executor) logReport(logger *slog.Logger, report *nodeagentv1.ReportExecutionResponse) {
	logger.Info("reported execution status", "status", report.GetAck().GetExecution().GetStatus())
}

func (e Executor) timeout(value time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	if e.opts.Timeouts.CleanupHard > 0 {
		return e.opts.Timeouts.CleanupHard
	}
	if e.opts.Timeouts.RuntimeStart > 0 {
		return e.opts.Timeouts.RuntimeStart
	}
	return time.Second
}

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

func compactLogSnippet(value string) string {
	compact := strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	return TruncateReason(compact)
}

func TruncateReason(value string) string {
	const maxLen = 400
	value = strings.TrimSpace(value)
	if len(value) <= maxLen {
		return value
	}
	return value[:maxLen-3] + "..."
}

func supersededExecutionID(item *nodeagentv1.SupersededExecution) string {
	if item == nil {
		return ""
	}
	return item.GetExecutionId()
}

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
