package logquery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBuildLogQL(t *testing.T) {
	t.Parallel()

	query := buildLogQL(Filters{
		Component:    "cloud-plane",
		PlatformName: "mini-cloud-simulated-v2",
		ServiceID:    "app_demo",
		RequestID:    "req_demo",
		Level:        "info",
		Contains:     "http request",
	})

	want := `{component="cloud-plane",job="mini-cloud"} |= "http request" | logfmt | platform_name="mini-cloud-simulated-v2" | service_id="app_demo" | request_id="req_demo" | level="INFO"`
	if query != want {
		t.Fatalf("buildLogQL() = %q, want %q", query, want)
	}
}

func TestServiceQueryRange(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/loki/api/v1/query_range" {
			t.Fatalf("path = %q, want /loki/api/v1/query_range", got)
		}
		if got := r.URL.Query().Get("direction"); got != "backward" {
			t.Fatalf("direction = %q, want backward", got)
		}
		if got := r.URL.Query().Get("limit"); got != "200" {
			t.Fatalf("limit = %q, want 200", got)
		}
		if got := r.URL.Query().Get("query"); !strings.Contains(got, `service_id="app_demo"`) {
			t.Fatalf("query = %q, want service filter", got)
		}

		_, _ = w.Write([]byte(`{
			"status":"success",
			"data":{
				"resultType":"streams",
				"result":[
					{
						"stream":{"job":"mini-cloud","component":"cloud-plane"},
						"values":[
							["1710000002000000000","time=2026-04-14T12:00:02Z level=INFO msg=\"line two\" service_id=app_demo"],
							["1710000001000000000","time=2026-04-14T12:00:01Z level=INFO msg=\"line one\" service_id=app_demo"]
						]
					}
				]
			}
		}`))
	}))
	defer server.Close()

	service := NewService(server.URL, "", 5*time.Second)
	result, err := service.QueryRange(context.Background(), QueryInput{
		Filters: Filters{
			ServiceID: "app_demo",
		},
	})
	if err != nil {
		t.Fatalf("QueryRange returned error: %v", err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(result.Items))
	}
	if got := result.Items[0].Line; !strings.Contains(got, "line two") {
		t.Fatalf("first line = %q, want newest log line", got)
	}
}

func TestNewServiceReturnsNilWithoutBackend(t *testing.T) {
	t.Parallel()

	service := NewService("", "", time.Second)
	if service != nil {
		t.Fatalf("NewService returned %+v, want nil", service)
	}
}
