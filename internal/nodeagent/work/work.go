package work

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	nodeagentv1 "mini-cloud/internal/gen/proto/minicloud/nodeagent/v1"
	agentclient "mini-cloud/internal/nodeagent/client"
	"mini-cloud/internal/nodeagent/runtime"
	"mini-cloud/internal/transport"
)

type ContainerRuntime interface {
	Run(context.Context, runtime.RunInput) (runtime.RunResult, error)
	Stop(context.Context, string) error
	Logs(context.Context, string, int) (string, error)
}

const (
	workActionRun    = "run"
	workActionDelete = "delete"

	executionStatusFailed    = "failed"
	executionStatusRunning   = "running"
	executionStatusSucceeded = "succeeded"
)

type Options struct {
	Node          NodeOptions
	Runtime       RuntimeOptions
	Readiness     ReadinessOptions
	Timeouts      TimeoutOptions
	Observability ObservabilityOptions
	Network       NetworkOptions
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
	WorkloadOTLPEndpoint string
	LogTail              int
}

type NetworkOptions struct {
	EgressProxy EgressProxyOptions
}

type EgressProxyOptions struct {
	Endpoint string
	NoProxy  []string
}

type Result struct {
	WorkFound bool
	WorkItem  *nodeagentv1.WorkItem
	Report    *nodeagentv1.ReportExecutionResponse
}

type executor struct {
	logger           *slog.Logger
	client           *agentclient.Client
	containerRuntime ContainerRuntime
	opts             Options
}

func ExecuteNext(ctx context.Context, logger *slog.Logger, client *agentclient.Client, containerRuntime ContainerRuntime, opts Options) (Result, error) {
	return executor{
		logger:           logger,
		client:           client,
		containerRuntime: containerRuntime,
		opts:             opts,
	}.executeNext(ctx)
}

func (e executor) executeNext(ctx context.Context) (Result, error) {
	opts := e.opts
	result := Result{}

	item, pollCtx, err := e.pollWork(ctx)
	if err != nil {
		return result, err
	}
	if item == nil {
		return result, nil
	}

	result.WorkFound = true
	result.WorkItem = item
	workLogger := e.workLogger(item)
	workLogger.Info("claimed execution work", "request_id", transport.RequestIDFromContext(pollCtx))

	if err := validateWorkItem(item); err != nil {
		report, reason, reportErr := e.reportValidationFailure(ctx, item, err)
		if reportErr != nil {
			return result, fmt.Errorf("report failed execution after invalid work item: %w", reportErr)
		}
		result.Report = report
		e.logReport(workLogger, report)
		return result, errors.New(reason)
	}
	if strings.TrimSpace(item.GetAction()) == workActionDelete {
		report, err := e.deleteWorkItem(ctx, item)
		if err != nil {
			result.Report = report
			return result, err
		}
		result.Report = report
		e.logReport(workLogger, report)
		return result, nil
	}

	runResult, err := e.runWorkItem(ctx, item)
	if err != nil {
		report, reason, reportErr := e.reportRuntimeStartFailure(ctx, item, err)
		if reportErr != nil {
			return result, fmt.Errorf("report failed execution after runtime start error: %w", reportErr)
		}
		result.Report = report
		e.logReport(workLogger, report)
		return result, errors.New(reason)
	}

	workLogger.Info("runtime container started",
		"container_name", runResult.ContainerName,
		"container_id", runResult.ContainerID,
		"host_port", runResult.HostPort,
	)

	readinessHost := strings.TrimSpace(opts.Node.PrivateIP)
	if readinessHost == "" {
		readinessHost = "127.0.0.1"
	}
	readinessURL := fmt.Sprintf("http://%s%s", net.JoinHostPort(readinessHost, fmt.Sprintf("%d", runResult.HostPort)), item.GetReadinessPath())
	readinessResult := WaitReadiness(ctx, ReadinessConfig{
		URL:      readinessURL,
		Attempts: opts.Readiness.Attempts,
		Interval: opts.Readiness.Interval,
		Timeout:  opts.Readiness.Timeout,
	})

	if readinessResult.Passed {
		report, err := e.reportRunning(ctx, item, runResult, readinessURL)
		if err != nil {
			result.Report = report
			return result, err
		}
		result.Report = report
		e.logReport(workLogger, report)
		return result, nil
	}

	report, reason, err := e.cleanupFailedRun(ctx, item, runResult, readinessResult)
	if err != nil {
		return result, err
	}
	result.Report = report
	e.logReport(workLogger, report)
	return result, errors.New(reason)
}

func (e executor) deleteWorkItem(ctx context.Context, item *nodeagentv1.WorkItem) (*nodeagentv1.ReportExecutionResponse, error) {
	stopCtx, cancelStop := context.WithTimeout(context.WithoutCancel(ctx), e.timeout(e.opts.Timeouts.RuntimeStop))
	stopErr := e.containerRuntime.Stop(stopCtx, item.GetContainerId())
	cancelStop()
	if stopErr != nil {
		report, reportErr := e.reportFailed(ctx, item, &nodeagentv1.ReportExecutionRequest{
			Reason:        truncateReason(fmt.Sprintf("delete execution failed while stopping container %s: %v", item.GetContainerName(), stopErr)),
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
		Status:        executionStatusSucceeded,
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

func (e executor) pollWork(ctx context.Context) (*nodeagentv1.WorkItem, context.Context, error) {
	pollCtx, cancel := context.WithTimeout(ctx, e.timeout(e.opts.Timeouts.PollWork))
	defer cancel()
	pollCtx = transport.ContextWithRequestID(pollCtx, transport.EnsureRequestID(""))
	item, err := e.client.PollExecutionWork(pollCtx, e.opts.Node.ID)
	return item, pollCtx, err
}

func (e executor) runWorkItem(ctx context.Context, item *nodeagentv1.WorkItem) (runtime.RunResult, error) {
	runCtx, cancelRun := context.WithTimeout(ctx, e.timeout(e.opts.Timeouts.RuntimeStart))
	defer cancelRun()

	env := injectTelemetryEnv(item.GetEnv(), item, telemetryEnvOptions{
		PlatformName:         e.opts.Observability.PlatformName,
		WorkloadOTLPEndpoint: e.opts.Observability.WorkloadOTLPEndpoint,
	})
	env = injectEgressProxyEnv(env, e.opts)
	return e.containerRuntime.Run(runCtx, runtime.RunInput{
		ContainerName: item.GetContainerName(),
		NodeID:        e.opts.Node.ID,
		ExecutionID:   item.GetExecutionId(),
		PlanID:        item.GetPlanId(),
		ServiceID:     item.GetServiceId(),
		Image:         item.GetImage(),
		Command:       append([]string(nil), item.GetCommand()...),
		Args:          append([]string(nil), item.GetArgs()...),
		Env:           env,
		ContainerPort: int(item.GetContainerPort()),
		HostBindIP:    e.opts.Node.PrivateIP,
		HostPortMin:   e.opts.Runtime.HostPortMin,
		HostPortMax:   e.opts.Runtime.HostPortMax,
	})
}

func injectEgressProxyEnv(env map[string]string, opts Options) map[string]string {
	proxy := opts.Network.EgressProxy
	if strings.TrimSpace(proxy.Endpoint) == "" {
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

func (e executor) reportRunning(ctx context.Context, item *nodeagentv1.WorkItem, runResult runtime.RunResult, readinessURL string) (*nodeagentv1.ReportExecutionResponse, error) {
	report, err := e.report(ctx, item, &nodeagentv1.ReportExecutionRequest{
		Status:        executionStatusRunning,
		Reason:        fmt.Sprintf("readiness check passed at %s", readinessURL),
		ContainerId:   runResult.ContainerID,
		ContainerName: runResult.ContainerName,
		HostPort:      int32(runResult.HostPort),
	})
	if err != nil {
		return nil, fmt.Errorf("report running execution: %w", err)
	}
	return report, nil
}

func (e executor) cleanupFailedRun(ctx context.Context, item *nodeagentv1.WorkItem, runResult runtime.RunResult, readinessResult ReadinessResult) (*nodeagentv1.ReportExecutionResponse, string, error) {
	logSnippet := ""
	logCtx, cancelLogs := context.WithTimeout(context.WithoutCancel(ctx), e.timeout(e.opts.Timeouts.RuntimeLogs))
	logSnippet, _ = e.containerRuntime.Logs(logCtx, runResult.ContainerID, e.opts.Observability.LogTail)
	cancelLogs()

	stopCtx, cancelStop := context.WithTimeout(context.WithoutCancel(ctx), e.timeout(e.opts.Timeouts.RuntimeStop))
	stopErr := e.containerRuntime.Stop(stopCtx, runResult.ContainerID)
	cancelStop()

	reason := buildFailedExecutionReason(readinessResult, logSnippet, stopErr)
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

func (e executor) reportRuntimeStartFailure(ctx context.Context, item *nodeagentv1.WorkItem, runErr error) (*nodeagentv1.ReportExecutionResponse, string, error) {
	reason := truncateReason(fmt.Sprintf("runtime start failed: %v", runErr))
	report, err := e.reportFailed(ctx, item, &nodeagentv1.ReportExecutionRequest{
		Reason:        reason,
		ContainerName: item.GetContainerName(),
	})
	if report == nil {
		return nil, reason, err
	}
	return report, reason, err
}

func (e executor) reportValidationFailure(ctx context.Context, item *nodeagentv1.WorkItem, validationErr error) (*nodeagentv1.ReportExecutionResponse, string, error) {
	reason := truncateReason(fmt.Sprintf("invalid work item: %v", validationErr))
	report, err := e.reportFailed(ctx, item, &nodeagentv1.ReportExecutionRequest{
		Reason:        reason,
		ContainerName: item.GetContainerName(),
	})
	if report == nil {
		return nil, reason, err
	}
	return report, reason, err
}

func (e executor) reportFailed(ctx context.Context, item *nodeagentv1.WorkItem, req *nodeagentv1.ReportExecutionRequest) (*nodeagentv1.ReportExecutionResponse, error) {
	req.Status = executionStatusFailed
	if req.GetReason() == "" {
		req.Reason = "execution failed"
	}
	return e.report(ctx, item, req)
}

func (e executor) report(ctx context.Context, item *nodeagentv1.WorkItem, req *nodeagentv1.ReportExecutionRequest) (*nodeagentv1.ReportExecutionResponse, error) {
	reportCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), e.timeout(e.opts.Timeouts.Report))
	defer cancel()
	reportCtx = transport.ContextWithRequestID(reportCtx, transport.EnsureRequestID(transport.RequestIDFromContext(reportCtx)))
	req.NodeId = e.opts.Node.ID
	req.ExecutionId = item.GetExecutionId()
	return e.client.ReportExecution(reportCtx, req)
}

func (e executor) workLogger(item *nodeagentv1.WorkItem) *slog.Logger {
	return e.logger.With(
		"node_id", e.opts.Node.ID,
		"service_id", item.GetServiceId(),
		"plan_id", item.GetPlanId(),
		"execution_id", item.GetExecutionId(),
	)
}

func (e executor) logReport(logger *slog.Logger, report *nodeagentv1.ReportExecutionResponse) {
	if report == nil {
		return
	}
	logger.Info("reported execution status", "status", report.GetAck().GetExecution().GetStatus())
}

func (e executor) timeout(value time.Duration) time.Duration {
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

func buildFailedExecutionReason(readinessResult ReadinessResult, logs string, stopErr error) string {
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
	return truncateReason(strings.Join(parts, "; "))
}

func compactLogSnippet(value string) string {
	compact := strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	return truncateReason(compact)
}

func truncateReason(value string) string {
	const maxLen = 400
	value = strings.TrimSpace(value)
	if len(value) <= maxLen {
		return value
	}
	return value[:maxLen-3] + "..."
}

func validateWorkItem(item *nodeagentv1.WorkItem) error {
	if item == nil {
		return fmt.Errorf("work item is required")
	}
	action := strings.TrimSpace(item.GetAction())
	if action != workActionRun && action != workActionDelete {
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
	return nil
}
