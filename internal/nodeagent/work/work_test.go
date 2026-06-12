package work

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	nodeagentv1 "mini-cloud/internal/gen/proto/minicloud/nodeagent/v1"
	agentclient "mini-cloud/internal/nodeagent/client"
	"mini-cloud/internal/nodeagent/runtime"
	"mini-cloud/internal/nodeagent/workloadlogs"

	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestExecuteNextReturnsNoWorkWhenPollIsEmpty(t *testing.T) {
	t.Parallel()

	result, err := ExecuteNext(context.Background(), testLogger(), newWorkTestClient(t, nil), &fakeRuntime{}, testOptions())
	if err != nil {
		t.Fatalf("ExecuteNext returned error: %v", err)
	}
	if result.WorkFound {
		t.Fatalf("WorkFound = true, want false")
	}
}

func TestExecuteNextReportsFailedWhenRuntimeStartFails(t *testing.T) {
	t.Parallel()

	recorder := &workTestRecorder{item: testWorkItem()}
	client := newWorkTestClient(t, recorder)
	containerRuntime := &fakeRuntime{runErr: errors.New("image pull failed")}

	result, err := ExecuteNext(context.Background(), testLogger(), client, containerRuntime, testOptions())
	if err == nil || !strings.Contains(err.Error(), "runtime start failed") {
		t.Fatalf("ExecuteNext error = %v, want runtime start failure", err)
	}
	if result.Report == nil || result.Report.GetAck().GetExecution().GetStatus() != executionStatusFailed {
		t.Fatalf("result.Report = %+v, want failed report", result.Report)
	}
	if len(recorder.reports) != 1 {
		t.Fatalf("report count = %d, want 1", len(recorder.reports))
	}
	report := recorder.reports[0]
	if report.GetStatus() != executionStatusFailed {
		t.Fatalf("report status = %q, want failed", report.GetStatus())
	}
	if report.GetContainerName() != "svc-web-0" {
		t.Fatalf("report container name = %q, want svc-web-0", report.GetContainerName())
	}
}

func TestExecuteNextReportsRunningWhenReadinessPasses(t *testing.T) {
	t.Parallel()

	recorder := &workTestRecorder{item: testWorkItem()}
	client := newWorkTestClient(t, recorder)
	workloadLogs := &fakeWorkloadLogs{}
	readinessWaiter := &fakeReadinessWaiter{result: ReadinessResult{Passed: true}}
	opts := testOptions()
	opts.Observability.WorkloadLogs = workloadLogs.Start
	containerRuntime := &fakeRuntime{runResult: runtime.RunResult{
		ContainerID:   "container-new",
		ContainerName: "svc-web-0",
		HostPort:      32080,
	}}

	result, err := executeNext(context.Background(), testLogger(), client, containerRuntime, opts, readinessWaiter)
	if err != nil {
		t.Fatalf("ExecuteNext returned error: %v", err)
	}
	if result.Report == nil || result.Report.GetAck().GetExecution().GetStatus() != executionStatusRunning {
		t.Fatalf("result.Report = %+v, want running report", result.Report)
	}
	if len(recorder.reports) != 1 {
		t.Fatalf("report count = %d, want 1", len(recorder.reports))
	}
	report := recorder.reports[0]
	if report.GetStatus() != executionStatusRunning {
		t.Fatalf("report status = %q, want running", report.GetStatus())
	}
	if report.GetHostPort() != 32080 {
		t.Fatalf("report host port = %d, want 32080", report.GetHostPort())
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

func TestExecuteNextCleansUpAndReportsFailedWhenReadinessFails(t *testing.T) {
	t.Parallel()

	recorder := &workTestRecorder{item: testWorkItem()}
	client := newWorkTestClient(t, recorder)
	containerRuntime := &fakeRuntime{
		runResult: runtime.RunResult{
			ContainerID:   "container-new",
			ContainerName: "svc-web-0",
			HostPort:      32080,
		},
		logs: "workload boot failed",
	}
	opts := testOptions()
	readinessWaiter := &fakeReadinessWaiter{result: ReadinessResult{
		URL: "http://127.0.0.1:32080/healthz",
		Observations: []ReadinessObservation{
			{Attempt: 1, StatusCode: http.StatusInternalServerError, Error: "unexpected status 500"},
		},
	}}

	result, err := executeNext(context.Background(), testLogger(), client, containerRuntime, opts, readinessWaiter)
	if err == nil || !strings.Contains(err.Error(), "readiness check never passed") {
		t.Fatalf("ExecuteNext error = %v, want readiness failure", err)
	}
	if result.Report == nil || result.Report.GetAck().GetExecution().GetStatus() != executionStatusFailed {
		t.Fatalf("result.Report = %+v, want failed report", result.Report)
	}
	if len(containerRuntime.stops) != 1 || containerRuntime.stops[0] != "container-new" {
		t.Fatalf("stopped containers = %v, want [container-new]", containerRuntime.stops)
	}
	if len(recorder.reports) != 1 || !strings.Contains(recorder.reports[0].GetReason(), "logs: workload boot failed") {
		t.Fatalf("report = %+v, want log snippet in failed reason", recorder.reports)
	}
}

func TestExecuteNextStopsCandidateAndReportsFailedWhenSupersededStopFails(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	item.SupersededExecution = &nodeagentv1.SupersededExecution{
		ExecutionId:   "exec-old",
		ContainerId:   "container-old",
		ContainerName: "svc-web-old",
	}
	recorder := &workTestRecorder{item: item}
	client := newWorkTestClient(t, recorder)
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
	readinessWaiter := &fakeReadinessWaiter{result: ReadinessResult{Passed: true}}

	result, err := executeNext(context.Background(), testLogger(), client, containerRuntime, opts, readinessWaiter)
	if err == nil || !strings.Contains(err.Error(), "stopping superseded container") {
		t.Fatalf("ExecuteNext error = %v, want superseded stop failure", err)
	}
	if result.Report == nil || result.Report.GetAck().GetExecution().GetStatus() != executionStatusFailed {
		t.Fatalf("result.Report = %+v, want failed report", result.Report)
	}
	if strings.Join(containerRuntime.stops, ",") != "container-old,container-new" {
		t.Fatalf("stopped containers = %v, want old then new", containerRuntime.stops)
	}
	if len(recorder.reports) != 1 || recorder.reports[0].GetStatus() != executionStatusFailed {
		t.Fatalf("reports = %+v, want failed report", recorder.reports)
	}
}

func TestExecuteNextDeleteWorkStopsContainerAndReportsSucceeded(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	item.Action = workActionDelete
	item.ContainerId = "container-old"
	item.ContainerName = "svc-web-old"
	item.HostPort = 32080
	recorder := &workTestRecorder{item: item}
	client := newWorkTestClient(t, recorder)
	containerRuntime := &fakeRuntime{}

	result, err := ExecuteNext(context.Background(), testLogger(), client, containerRuntime, testOptions())
	if err != nil {
		t.Fatalf("ExecuteNext returned error: %v", err)
	}
	if result.Report == nil || result.Report.GetAck().GetExecution().GetStatus() != executionStatusSucceeded {
		t.Fatalf("result.Report = %+v, want succeeded report", result.Report)
	}
	if len(containerRuntime.stops) != 1 || containerRuntime.stops[0] != "container-old" {
		t.Fatalf("stopped containers = %v, want [container-old]", containerRuntime.stops)
	}
	if len(recorder.reports) != 1 || recorder.reports[0].GetStatus() != executionStatusSucceeded {
		t.Fatalf("reports = %+v, want succeeded report", recorder.reports)
	}
}

func TestExecuteNextReturnsReportError(t *testing.T) {
	t.Parallel()

	client := newWorkTestClient(t, &workTestRecorder{
		item:      testWorkItem(),
		reportErr: errors.New("control plane unavailable"),
	})
	opts := testOptions()
	readinessWaiter := &fakeReadinessWaiter{result: ReadinessResult{Passed: true}}

	_, err := executeNext(context.Background(), testLogger(), client, &fakeRuntime{runResult: runtime.RunResult{
		ContainerID:   "container-new",
		ContainerName: "svc-web-0",
		HostPort:      32080,
	}}, opts, readinessWaiter)
	if err == nil || !strings.Contains(err.Error(), "report running execution") {
		t.Fatalf("ExecuteNext error = %v, want report running error", err)
	}
}

func TestExecuteNextCleansUpAndReportsAfterContextCanceledDuringReadiness(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	recorder := &workTestRecorder{item: testWorkItem()}
	client := newWorkTestClient(t, recorder)
	opts := testOptions()
	readinessWaiter := &fakeReadinessWaiter{beforeWait: cancel, result: ReadinessResult{
		URL: "http://127.0.0.1:32080/healthz",
		Observations: []ReadinessObservation{
			{Attempt: 1, Error: context.Canceled.Error()},
		},
	}}
	containerRuntime := &fakeRuntime{runResult: runtime.RunResult{
		ContainerID:   "container-new",
		ContainerName: "svc-web-0",
		HostPort:      32080,
	}}

	result, err := executeNext(ctx, testLogger(), client, containerRuntime, opts, readinessWaiter)
	if err == nil || !strings.Contains(err.Error(), "readiness check never passed") {
		t.Fatalf("ExecuteNext error = %v, want readiness failure after cancellation", err)
	}
	if result.Report == nil || result.Report.GetAck().GetExecution().GetStatus() != executionStatusFailed {
		t.Fatalf("result.Report = %+v, want failed report", result.Report)
	}
	if len(containerRuntime.stops) != 1 || containerRuntime.stops[0] != "container-new" {
		t.Fatalf("stopped containers = %v, want [container-new]", containerRuntime.stops)
	}
}

func TestBuildFailedExecutionReasonIncludesUsefulContext(t *testing.T) {
	t.Parallel()

	reason := buildFailedExecutionReason(ReadinessResult{
		URL: "http://127.0.0.1:32774/",
		Observations: []ReadinessObservation{
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

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testOptions() Options {
	return Options{
		Node: NodeOptions{
			ID: "node-a",
		},
		Readiness: ReadinessOptions{
			Attempts: 1,
			Interval: time.Millisecond,
			Timeout:  time.Millisecond,
		},
		Timeouts: TimeoutOptions{
			RuntimeStart: time.Second,
		},
		Observability: ObservabilityOptions{
			PlatformName: "test-platform",
			LogTail:      20,
		},
	}
}

func testWorkItem() *nodeagentv1.WorkItem {
	return &nodeagentv1.WorkItem{
		Action:        workActionRun,
		ExecutionId:   "exec-new",
		PlanId:        "plan-a",
		NodeId:        "node-a",
		ServiceId:     "service-a",
		ServiceName:   "web",
		Image:         "example/web:latest",
		Env:           map[string]string{"APP_ENV": "test"},
		ContainerPort: 8080,
		ReadinessPath: "/healthz",
		ContainerName: "svc-web-0",
	}
}

type workTestService struct {
	nodeagentv1.UnimplementedNodeAgentServiceServer

	recorder *workTestRecorder
}

func (s *workTestService) PollWork(context.Context, *nodeagentv1.PollWorkRequest) (*nodeagentv1.PollWorkResponse, error) {
	if s.recorder == nil {
		return &nodeagentv1.PollWorkResponse{}, nil
	}
	if s.recorder.pollErr != nil {
		return nil, s.recorder.pollErr
	}
	return &nodeagentv1.PollWorkResponse{Item: s.recorder.item}, nil
}

func (s *workTestService) ReportExecution(_ context.Context, req *nodeagentv1.ReportExecutionRequest) (*nodeagentv1.ReportExecutionResponse, error) {
	if s.recorder == nil {
		return &nodeagentv1.ReportExecutionResponse{Ack: protoReportAck(req.GetExecutionId(), req.GetStatus())}, nil
	}
	s.recorder.reports = append(s.recorder.reports, req)
	if s.recorder.reportErr != nil {
		return nil, s.recorder.reportErr
	}
	return &nodeagentv1.ReportExecutionResponse{Ack: protoReportAck(req.GetExecutionId(), req.GetStatus())}, nil
}

func newWorkTestClient(t *testing.T, recorder *workTestRecorder) *agentclient.Client {
	t.Helper()

	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	nodeagentv1.RegisterNodeAgentServiceServer(grpcServer, &workTestService{recorder: recorder})
	go func() {
		_ = grpcServer.Serve(listener)
	}()
	t.Cleanup(func() {
		grpcServer.Stop()
		if err := listener.Close(); err != nil {
			t.Logf("close bufconn listener: %v", err)
		}
	})

	return agentclient.New(agentclient.Config{
		ServerURL: "http://bufconn",
		Dialer: func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		},
	})
}

func protoReportAck(executionID string, status string) *nodeagentv1.ReportExecutionAck {
	return &nodeagentv1.ReportExecutionAck{
		Execution: &nodeagentv1.ExecutionRecord{
			Id:     executionID,
			Status: status,
		},
		ObservedAt: timestamppb.Now(),
	}
}

type workTestRecorder struct {
	item      *nodeagentv1.WorkItem
	pollErr   error
	reportErr error
	reports   []*nodeagentv1.ReportExecutionRequest
}

type fakeRuntime struct {
	runResult   runtime.RunResult
	runErr      error
	logs        string
	stops       []string
	stopErrByID map[string]error
}

func (f *fakeRuntime) Run(context.Context, runtime.RunInput) (runtime.RunResult, error) {
	return f.runResult, f.runErr
}

func (f *fakeRuntime) Stop(_ context.Context, containerID string) error {
	f.stops = append(f.stops, containerID)
	if f.stopErrByID == nil {
		return nil
	}
	return f.stopErrByID[containerID]
}

func (f *fakeRuntime) Logs(context.Context, string, int) (string, error) {
	return f.logs, nil
}

type fakeReadinessWaiter struct {
	result     ReadinessResult
	beforeWait func()
	urls       []string
}

func (f *fakeReadinessWaiter) Wait(_ context.Context, cfg ReadinessConfig) ReadinessResult {
	f.urls = append(f.urls, cfg.URL)
	if f.beforeWait != nil {
		f.beforeWait()
	}
	if f.result.URL == "" {
		f.result.URL = cfg.URL
	}
	return f.result
}

type fakeWorkloadLogs struct {
	starts []workloadlogs.StartRequest
}

func (f *fakeWorkloadLogs) Start(req workloadlogs.StartRequest) {
	f.starts = append(f.starts, req)
}

func TestTruncateReasonLimitsLength(t *testing.T) {
	t.Parallel()

	value := strings.Repeat("a", 500)
	got := truncateReason(value)

	if len(got) != 400 {
		t.Fatalf("TruncateReason length = %d, want 400", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected truncated reason to end with ellipsis, got %q", got[len(got)-10:])
	}
}
