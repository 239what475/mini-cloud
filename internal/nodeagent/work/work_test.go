package work

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"mini-cloud/internal/contract/nodeagentapi"
	"mini-cloud/internal/nodeagent/runtime"
	"mini-cloud/internal/nodeagent/workloadlogs"
	"mini-cloud/internal/nodeagent/workloadreadiness"
)

// TestExecuteNextReturnsNoWorkWhenPollIsEmpty 验证没有任务时状态机返回无工作结果。
func TestExecuteNextReturnsNoWorkWhenPollIsEmpty(t *testing.T) {
	t.Parallel()

	result, err := ExecuteNext(context.Background(), testLogger(), &fakeWorkClient{}, &fakeRuntime{}, testOptions())
	if err != nil {
		t.Fatalf("ExecuteNext returned error: %v", err)
	}
	if result.WorkFound {
		t.Fatalf("WorkFound = true, want false")
	}
}

// TestExecuteNextReportsFailedWhenRuntimeStartFails 验证容器启动失败会向控制面上报 failed。
func TestExecuteNextReportsFailedWhenRuntimeStartFails(t *testing.T) {
	t.Parallel()

	client := &fakeWorkClient{item: testWorkItem()}
	containerRuntime := &fakeRuntime{runErr: errors.New("image pull failed")}

	result, err := ExecuteNext(context.Background(), testLogger(), client, containerRuntime, testOptions())
	if err == nil || !strings.Contains(err.Error(), "runtime start failed") {
		t.Fatalf("ExecuteNext error = %v, want runtime start failure", err)
	}
	if result.Report == nil || result.Report.Ack.Execution.Status != nodeagentapi.ExecutionStatusFailed {
		t.Fatalf("result.Report = %+v, want failed report", result.Report)
	}
	if len(client.reports) != 1 {
		t.Fatalf("report count = %d, want 1", len(client.reports))
	}
	report := client.reports[0]
	if report.Status != nodeagentapi.ExecutionStatusFailed {
		t.Fatalf("report status = %q, want failed", report.Status)
	}
	if report.ContainerName != "svc-web-0" {
		t.Fatalf("report container name = %q, want svc-web-0", report.ContainerName)
	}
}

// TestExecuteNextReportsRunningWhenReadinessPasses 验证容器启动后启动日志采集，readiness 探测通过后上报 running。
func TestExecuteNextReportsRunningWhenReadinessPasses(t *testing.T) {
	t.Parallel()

	client := &fakeWorkClient{item: testWorkItem()}
	workloadLogs := &fakeWorkloadLogs{}
	readinessWaiter := &fakeReadinessWaiter{result: workloadreadiness.Result{Passed: true}}
	opts := testOptions()
	opts.WorkloadLogs = workloadLogs.Start
	opts.ReadinessWaiter = readinessWaiter
	containerRuntime := &fakeRuntime{runResult: runtime.RunResult{
		ContainerID:   "container-new",
		ContainerName: "svc-web-0",
		HostPort:      32080,
	}}

	result, err := ExecuteNext(context.Background(), testLogger(), client, containerRuntime, opts)
	if err != nil {
		t.Fatalf("ExecuteNext returned error: %v", err)
	}
	if result.Report == nil || result.Report.Ack.Execution.Status != nodeagentapi.ExecutionStatusRunning {
		t.Fatalf("result.Report = %+v, want running report", result.Report)
	}
	if len(client.reports) != 1 {
		t.Fatalf("report count = %d, want 1", len(client.reports))
	}
	report := client.reports[0]
	if report.Status != nodeagentapi.ExecutionStatusRunning {
		t.Fatalf("report status = %q, want running", report.Status)
	}
	if report.HostPort != 32080 {
		t.Fatalf("report host port = %d, want 32080", report.HostPort)
	}
	if len(workloadLogs.starts) != 1 {
		t.Fatalf("workload log start count = %d, want 1", len(workloadLogs.starts))
	}
	if len(readinessWaiter.urls) != 1 {
		t.Fatalf("readiness check count = %d, want 1", len(readinessWaiter.urls))
	}
	if readinessWaiter.urls[0] != "http://127.0.0.1:32080/healthz" {
		t.Fatalf("readiness url = %q, want localhost readiness URL", readinessWaiter.urls[0])
	}
}

// TestExecuteNextCleansUpAndReportsFailedWhenReadinessFails 验证 readiness 探测失败后会停止容器并上报 failed。
func TestExecuteNextCleansUpAndReportsFailedWhenReadinessFails(t *testing.T) {
	t.Parallel()

	client := &fakeWorkClient{item: testWorkItem()}
	containerRuntime := &fakeRuntime{
		runResult: runtime.RunResult{
			ContainerID:   "container-new",
			ContainerName: "svc-web-0",
			HostPort:      32080,
		},
		logs: "workload boot failed",
	}
	opts := testOptions()
	opts.ReadinessWaiter = &fakeReadinessWaiter{result: workloadreadiness.Result{
		URL: "http://127.0.0.1:32080/healthz",
		Observations: []workloadreadiness.Observation{
			{Attempt: 1, StatusCode: http.StatusInternalServerError, Error: "unexpected status 500"},
		},
	}}

	result, err := ExecuteNext(context.Background(), testLogger(), client, containerRuntime, opts)
	if err == nil || !strings.Contains(err.Error(), "readiness check never passed") {
		t.Fatalf("ExecuteNext error = %v, want readiness failure", err)
	}
	if result.Report == nil || result.Report.Ack.Execution.Status != nodeagentapi.ExecutionStatusFailed {
		t.Fatalf("result.Report = %+v, want failed report", result.Report)
	}
	if len(containerRuntime.stops) != 1 || containerRuntime.stops[0] != "container-new" {
		t.Fatalf("stopped containers = %v, want [container-new]", containerRuntime.stops)
	}
	if len(client.reports) != 1 || !strings.Contains(client.reports[0].Reason, "logs: workload boot failed") {
		t.Fatalf("report = %+v, want log snippet in failed reason", client.reports)
	}
}

// TestExecuteNextStopsCandidateAndReportsFailedWhenSupersededStopFails 验证旧容器停止失败时会清理新容器并上报 failed。
func TestExecuteNextStopsCandidateAndReportsFailedWhenSupersededStopFails(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	item.SupersededExecution = &nodeagentapi.SupersededExecution{
		ExecutionID:   "exec-old",
		ContainerID:   "container-old",
		ContainerName: "svc-web-old",
	}
	client := &fakeWorkClient{item: item}
	containerRuntime := &fakeRuntime{
		runResult: runtime.RunResult{
			ContainerID:   "container-new",
			ContainerName: "svc-web-0",
			HostPort:      32080,
		},
		stopErrByID: map[string]error{
			"container-old": errors.New("docker stop timeout"),
		},
	}
	opts := testOptions()
	opts.ReadinessWaiter = &fakeReadinessWaiter{result: workloadreadiness.Result{Passed: true}}

	result, err := ExecuteNext(context.Background(), testLogger(), client, containerRuntime, opts)
	if err == nil || !strings.Contains(err.Error(), "stopping superseded container") {
		t.Fatalf("ExecuteNext error = %v, want superseded stop failure", err)
	}
	if result.Report == nil || result.Report.Ack.Execution.Status != nodeagentapi.ExecutionStatusFailed {
		t.Fatalf("result.Report = %+v, want failed report", result.Report)
	}
	if strings.Join(containerRuntime.stops, ",") != "container-old,container-new" {
		t.Fatalf("stopped containers = %v, want old then new", containerRuntime.stops)
	}
	if len(client.reports) != 1 || client.reports[0].Status != nodeagentapi.ExecutionStatusFailed {
		t.Fatalf("reports = %+v, want failed report", client.reports)
	}
}

// TestExecuteNextReturnsReportError 验证执行结果上报失败会返回错误。
func TestExecuteNextReturnsReportError(t *testing.T) {
	t.Parallel()

	client := &fakeWorkClient{
		item:      testWorkItem(),
		reportErr: errors.New("control plane unavailable"),
	}
	opts := testOptions()
	opts.ReadinessWaiter = &fakeReadinessWaiter{result: workloadreadiness.Result{Passed: true}}

	_, err := ExecuteNext(context.Background(), testLogger(), client, &fakeRuntime{runResult: runtime.RunResult{
		ContainerID:   "container-new",
		ContainerName: "svc-web-0",
		HostPort:      32080,
	}}, opts)
	if err == nil || !strings.Contains(err.Error(), "report running execution") {
		t.Fatalf("ExecuteNext error = %v, want report running error", err)
	}
}

// TestExecuteNextCleansUpAndReportsAfterContextCanceledDuringReadiness 验证 readiness 探测期间上下文取消后仍会清理并上报失败。
func TestExecuteNextCleansUpAndReportsAfterContextCanceledDuringReadiness(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	client := &fakeWorkClient{item: testWorkItem()}
	opts := testOptions()
	opts.ReadinessWaiter = &fakeReadinessWaiter{beforeWait: cancel, result: workloadreadiness.Result{
		URL: "http://127.0.0.1:32080/healthz",
		Observations: []workloadreadiness.Observation{
			{Attempt: 1, Error: context.Canceled.Error()},
		},
	}}
	containerRuntime := &fakeRuntime{runResult: runtime.RunResult{
		ContainerID:   "container-new",
		ContainerName: "svc-web-0",
		HostPort:      32080,
	}}

	result, err := ExecuteNext(ctx, testLogger(), client, containerRuntime, opts)
	if err == nil || !strings.Contains(err.Error(), "readiness check never passed") {
		t.Fatalf("ExecuteNext error = %v, want readiness failure after cancellation", err)
	}
	if result.Report == nil || result.Report.Ack.Execution.Status != nodeagentapi.ExecutionStatusFailed {
		t.Fatalf("result.Report = %+v, want failed report", result.Report)
	}
	if len(containerRuntime.stops) != 1 || containerRuntime.stops[0] != "container-new" {
		t.Fatalf("stopped containers = %v, want [container-new]", containerRuntime.stops)
	}
}

// TestBuildFailedExecutionReasonIncludesUsefulContext 验证失败原因包含 readiness 探测、日志和清理错误上下文。
func TestBuildFailedExecutionReasonIncludesUsefulContext(t *testing.T) {
	t.Parallel()

	reason := BuildFailedExecutionReason(workloadreadiness.Result{
		URL: "http://127.0.0.1:32774/",
		Observations: []workloadreadiness.Observation{
			{
				Attempt:    1,
				StatusCode: http.StatusBadGateway,
				Error:      "unexpected status 502",
			},
		},
	}, " line one \n  line two ", errors.New("docker stop timeout"))

	if !strings.Contains(reason, "readiness check never passed") {
		t.Fatalf("expected failure reason to mention readiness, got %q", reason)
	}
	if !strings.Contains(reason, "last status: 502") {
		t.Fatalf("expected failure reason to include last status, got %q", reason)
	}
	if !strings.Contains(reason, "logs: line one line two") {
		t.Fatalf("expected failure reason to include compacted logs, got %q", reason)
	}
	if !strings.Contains(reason, "cleanup warning: docker stop timeout") {
		t.Fatalf("expected failure reason to include cleanup warning, got %q", reason)
	}
}

// testLogger 返回丢弃输出的测试 logger。
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// testOptions 返回执行状态机测试使用的基础选项。
func testOptions() Options {
	return Options{
		NodeID:            "node-a",
		PlatformName:      "test-platform",
		ReadinessAttempts: 1,
		ReadinessInterval: time.Millisecond,
		ReadinessTimeout:  time.Millisecond,
		RuntimeTimeout:    time.Second,
		LogTail:           20,
	}
}

// testWorkItem 返回执行状态机测试使用的基础任务。
func testWorkItem() *nodeagentapi.WorkItem {
	return &nodeagentapi.WorkItem{
		ExecutionID:   "exec-new",
		DeploymentID:  "deploy-a",
		NodeID:        "node-a",
		ProjectID:     "project-a",
		ServiceID:     "service-a",
		ServiceName:   "web",
		ReplicaIndex:  0,
		Image:         "example/web:latest",
		Env:           map[string]string{"APP_ENV": "test"},
		ContainerPort: 8080,
		ReadinessPath: "/healthz",
		ContainerName: "svc-web-0",
	}
}

// fakeWorkClient 是执行状态机测试用的控制面客户端。
type fakeWorkClient struct {
	// item 是 PollExecutionWork 返回的预设任务。
	item *nodeagentapi.WorkItem
	// pollErr 是 PollExecutionWork 返回的预设错误。
	pollErr error
	// reportErr 是 ReportExecution 返回的预设错误。
	reportErr error
	// reports 记录所有执行结果上报请求。
	reports []nodeagentapi.ReportExecutionRequest
}

// PollExecutionWork 返回预设任务或错误。
func (f *fakeWorkClient) PollExecutionWork(context.Context, string) (*nodeagentapi.WorkItem, error) {
	return f.item, f.pollErr
}

// ReportExecution 记录上报请求，并按配置返回成功响应或错误。
func (f *fakeWorkClient) ReportExecution(_ context.Context, _ string, executionID string, req nodeagentapi.ReportExecutionRequest) (nodeagentapi.ReportExecutionResponse, error) {
	f.reports = append(f.reports, req)
	if f.reportErr != nil {
		return nodeagentapi.ReportExecutionResponse{}, f.reportErr
	}
	return nodeagentapi.ReportExecutionResponse{
		Ack: nodeagentapi.ReportExecutionAck{
			Execution: nodeagentapi.ExecutionRecord{
				ID:     executionID,
				Status: req.Status,
			},
		},
	}, nil
}

// fakeRuntime 是执行状态机测试用的容器运行时。
type fakeRuntime struct {
	// runResult 是 Run 返回的预设结果。
	runResult runtime.RunResult
	// runErr 是 Run 返回的预设错误。
	runErr error
	// logs 是 Logs 返回的预设日志文本。
	logs string
	// stops 记录 Stop 调用收到的容器 ID。
	stops []string
	// stopErrByID 按容器 ID 配置 Stop 返回的错误。
	stopErrByID map[string]error
}

// Run 返回预设的运行时启动结果或错误。
func (f *fakeRuntime) Run(context.Context, runtime.RunInput) (runtime.RunResult, error) {
	return f.runResult, f.runErr
}

// Stop 记录停止的容器 ID，并按容器 ID 返回预设错误。
func (f *fakeRuntime) Stop(_ context.Context, containerID string) error {
	f.stops = append(f.stops, containerID)
	if f.stopErrByID == nil {
		return nil
	}
	return f.stopErrByID[containerID]
}

// Logs 返回预设的容器日志文本。
func (f *fakeRuntime) Logs(context.Context, string, int) (string, error) {
	return f.logs, nil
}

// fakeReadinessWaiter 是执行状态机测试用的 readiness 探测器。
type fakeReadinessWaiter struct {
	// result 是 Wait 返回的预设 readiness 探测结果。
	result workloadreadiness.Result
	// beforeWait 是返回结果前执行的测试钩子。
	beforeWait func()
	// urls 记录 Wait 收到的 readiness URL。
	urls []string
}

// Wait 记录 readiness 配置，并返回预设结果。
func (f *fakeReadinessWaiter) Wait(_ context.Context, cfg workloadreadiness.Config) workloadreadiness.Result {
	f.urls = append(f.urls, cfg.URL)
	if f.beforeWait != nil {
		f.beforeWait()
	}
	if f.result.URL == "" {
		f.result.URL = cfg.URL
	}
	return f.result
}

// fakeWorkloadLogs 是执行状态机测试用的工作负载日志启动器。
type fakeWorkloadLogs struct {
	// starts 记录所有日志采集启动请求。
	starts []workloadlogs.StartRequest
}

// Start 记录日志采集启动请求。
func (f *fakeWorkloadLogs) Start(req workloadlogs.StartRequest) {
	f.starts = append(f.starts, req)
}

// TestTruncateReasonLimitsLength 验证失败原因会被裁剪到固定长度。
func TestTruncateReasonLimitsLength(t *testing.T) {
	t.Parallel()

	value := strings.Repeat("a", 500)
	got := TruncateReason(value)

	if len(got) != 400 {
		t.Fatalf("TruncateReason length = %d, want 400", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected truncated reason to end with ellipsis, got %q", got[len(got)-10:])
	}
}
