package workloadlogs

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mini-cloud/internal/nodeagent/runtime"
)

// pushPayload 是测试中解码 Loki push 请求体的最小结构。
type pushPayload struct {
	// Streams 是请求体中的 Loki stream 列表。
	Streams []pushPayloadStream `json:"streams"`
}

// pushPayloadStream 是测试中解码的单个 Loki stream。
type pushPayloadStream struct {
	// Stream 是 Loki stream labels。
	Stream map[string]string `json:"stream"`
	// Values 是 Loki 日志值数组。
	Values [][]string `json:"values"`
}

// TestFormatLogfmtLineIncludesExecutionContext 验证 logfmt 行包含执行上下文字段。
func TestFormatLogfmtLineIncludesExecutionContext(t *testing.T) {
	t.Parallel()

	got := formatLogfmtLine("mini-cloud-lab", StartRequest{
		ProjectID:     "prj_demo",
		ServiceID:     "svc_demo",
		ServiceName:   "hello",
		DeploymentID:  "dep_demo",
		ExecutionID:   "exec_demo",
		ReplicaIndex:  3,
		NodeID:        "node_demo",
		ContainerName: "mini-cloud-dep-demo-r3",
	}, "stdout", time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC), "hello world")

	for _, want := range []string{
		`platform_name="mini-cloud-lab"`,
		`project_id="prj_demo"`,
		`service_id="svc_demo"`,
		`deployment_id="dep_demo"`,
		`execution_id="exec_demo"`,
		`replica_index="3"`,
		`stream="stdout"`,
		`msg="hello world"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatted line = %q, want substring %q", got, want)
		}
	}
}

// TestPushLineSendsStructuredPayloadToLoki 验证单行日志会按 Loki push payload 结构发送。
func TestPushLineSendsStructuredPayloadToLoki(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotTenant string
	var gotPayload pushPayload

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := r.Body.Close(); err != nil {
				t.Logf("close request body: %v", err)
			}
		}()
		gotPath = r.URL.Path
		gotTenant = r.Header.Get("X-Scope-OrgID")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if err := json.Unmarshal(body, &gotPayload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	manager := &Manager{
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		platform: "mini-cloud-lab",
		loki:     newLokiClient(server.URL, "tenant-demo", server.Client()),
	}

	manager.pushLine(StartRequest{
		ProjectID:     "prj_demo",
		ServiceID:     "svc_demo",
		ServiceName:   "hello",
		DeploymentID:  "dep_demo",
		ExecutionID:   "exec_demo",
		ReplicaIndex:  1,
		NodeID:        "node_demo",
		ContainerName: "mini-cloud-dep-demo-r1",
	}, "stderr", time.Date(2026, 4, 16, 12, 34, 56, 0, time.UTC), "boom")

	if gotPath != "/loki/api/v1/push" {
		t.Fatalf("request path = %q, want /loki/api/v1/push", gotPath)
	}
	if gotTenant != "tenant-demo" {
		t.Fatalf("tenant header = %q, want tenant-demo", gotTenant)
	}
	if len(gotPayload.Streams) != 1 {
		t.Fatalf("streams length = %d, want 1", len(gotPayload.Streams))
	}
	stream := gotPayload.Streams[0]
	if stream.Stream["component"] != "workload" {
		t.Fatalf("stream component = %q, want workload", stream.Stream["component"])
	}
	if stream.Stream["log_source"] != "container" {
		t.Fatalf("stream log_source = %q, want container", stream.Stream["log_source"])
	}
	if stream.Stream["stream"] != "stderr" {
		t.Fatalf("stream label = %q, want stderr", stream.Stream["stream"])
	}
	if len(stream.Values) != 1 || len(stream.Values[0]) != 2 {
		t.Fatalf("values = %+v, want one Loki entry", stream.Values)
	}
	if !strings.Contains(stream.Values[0][1], `msg="boom"`) {
		t.Fatalf("payload line = %q, want msg field", stream.Values[0][1])
	}
	if !strings.Contains(stream.Values[0][1], `execution_id="exec_demo"`) {
		t.Fatalf("payload line = %q, want execution context", stream.Values[0][1])
	}
}

// fakeLogFollower 是测试用的固定日志记录 LogFollower。
type fakeLogFollower struct {
	// records 是 FollowLogs 调用时依次发出的日志记录。
	records []runtime.LogRecord
	// done 在所有测试日志记录发出后关闭。
	done chan struct{}
}

// FollowLogs 实现测试用日志跟随接口。
func (f fakeLogFollower) FollowLogs(ctx context.Context, containerID string, emit runtime.LogEmitter) error {
	defer func() {
		if f.done != nil {
			close(f.done)
		}
	}()
	for _, record := range f.records {
		emit(record)
	}
	return nil
}

// TestPushBatchSendsMultipleStreamsInOneLokiRequest 验证批量推送会按 stdout/stderr 分成多个 Loki stream。
func TestPushBatchSendsMultipleStreamsInOneLokiRequest(t *testing.T) {
	t.Parallel()

	var gotPayload pushPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { _ = r.Body.Close() }()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if err := json.Unmarshal(body, &gotPayload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	manager := &Manager{
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		platform: "mini-cloud-lab",
		loki:     newLokiClient(server.URL, "", server.Client()),
	}
	manager.pushBatch(StartRequest{ExecutionID: "exec_demo"}, []queuedLog{
		{stream: "stdout", timestamp: time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC), line: "one"},
		{stream: "stdout", timestamp: time.Date(2026, 4, 16, 12, 0, 1, 0, time.UTC), line: "two"},
		{stream: "stderr", timestamp: time.Date(2026, 4, 16, 12, 0, 2, 0, time.UTC), line: "err"},
	})

	if len(gotPayload.Streams) != 2 {
		t.Fatalf("streams length = %d, want 2", len(gotPayload.Streams))
	}
	counts := map[string]int{}
	for _, stream := range gotPayload.Streams {
		counts[stream.Stream["stream"]] = len(stream.Values)
	}
	if counts["stdout"] != 2 || counts["stderr"] != 1 {
		t.Fatalf("stream value counts = %+v, want stdout=2 stderr=1", counts)
	}
}

// TestStartReturnsHandleAndCloseFlushesQueuedLogs 验证关闭日志采集句柄会刷新队列中日志。
func TestStartReturnsHandleAndCloseFlushesQueuedLogs(t *testing.T) {
	t.Parallel()

	payloads := make(chan pushPayload, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { _ = r.Body.Close() }()
		var payload pushPayload
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		payloads <- payload
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	followDone := make(chan struct{})
	manager, err := NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		LokiURL:     server.URL,
		BatchSize:   10,
		BatchWait:   time.Hour,
		PushTimeout: time.Second,
	}, fakeLogFollower{records: []runtime.LogRecord{
		{Timestamp: time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC), Stream: "stdout", Line: "one"},
		{Timestamp: time.Date(2026, 4, 16, 12, 0, 1, 0, time.UTC), Stream: "stdout", Line: "two"},
	}, done: followDone})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	handle := manager.Start(StartRequest{ExecutionID: "exec_demo", ContainerID: "container_demo"})
	<-followDone
	closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := handle.Close(closeCtx); err != nil {
		t.Fatalf("handle.Close: %v", err)
	}

	select {
	case payload := <-payloads:
		if len(payload.Streams) != 1 || len(payload.Streams[0].Values) != 2 {
			t.Fatalf("payload streams = %+v, want one stdout stream with two values", payload.Streams)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for flushed Loki payload")
	}
}
