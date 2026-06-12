package caddy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	cloudmodel "mini-cloud/internal/cloudplane/model"
)

func TestBuildConfigBuildsReverseProxyRoute(t *testing.T) {
	config, err := buildConfig("0.0.0.0:80", "0.0.0.0:18082", "/opt/mini-cloud/artifacts", "http://127.0.0.1:2019", []cloudmodel.Route{{
		Host:     "api.team.apps.example.test",
		Backends: []string{"10.0.1.20:32768", "10.0.1.21:32769"},
	}})
	if err != nil {
		t.Fatalf("BuildConfig returned error: %v", err)
	}

	if config.Admin.Listen != "127.0.0.1:2019" {
		t.Fatalf("admin listen = %q, want 127.0.0.1:2019", config.Admin.Listen)
	}
	server := config.Apps.HTTP.Servers["mini_cloud_ingress"]
	if len(server.Listen) != 1 || server.Listen[0] != "0.0.0.0:80" {
		t.Fatalf("server listen = %+v, want 0.0.0.0:80", server.Listen)
	}
	if !server.AutomaticHTTPS.Disable {
		t.Fatal("automatic HTTPS is not disabled")
	}
	if len(server.Routes) != 2 {
		t.Fatalf("routes len = %d, want host route plus fallback", len(server.Routes))
	}
	route := server.Routes[0]
	if got := route.Match[0].Host[0]; got != "api.team.apps.example.test" {
		t.Fatalf("route host = %q, want api.team.apps.example.test", got)
	}
	handler := route.Handle[0]
	if handler.Handler != "reverse_proxy" {
		t.Fatalf("handler = %q, want reverse_proxy", handler.Handler)
	}
	if len(handler.Upstreams) != 2 || handler.Upstreams[0].Dial != "10.0.1.20:32768" || handler.Upstreams[1].Dial != "10.0.1.21:32769" {
		t.Fatalf("upstreams = %+v, want both backends", handler.Upstreams)
	}
	artifactServer := config.Apps.HTTP.Servers["mini_cloud_artifacts"]
	if len(artifactServer.Listen) != 1 || artifactServer.Listen[0] != "0.0.0.0:18082" {
		t.Fatalf("artifact listen = %+v, want 0.0.0.0:18082", artifactServer.Listen)
	}
	artifactHandler := artifactServer.Routes[0].Handle[0]
	if artifactHandler.Handler != "file_server" || artifactHandler.Root != "/opt/mini-cloud/artifacts" {
		t.Fatalf("artifact handler = %+v, want file_server rooted at artifacts dir", artifactHandler)
	}
}

func TestBuildConfigUses503ForRouteWithoutBackends(t *testing.T) {
	config, err := buildConfig("0.0.0.0:80", "0.0.0.0:18082", "/opt/mini-cloud/artifacts", "http://127.0.0.1:2019", []cloudmodel.Route{{Host: "api.team.apps.example.test"}})
	if err != nil {
		t.Fatalf("BuildConfig returned error: %v", err)
	}

	handler := config.Apps.HTTP.Servers["mini_cloud_ingress"].Routes[0].Handle[0]
	if handler.Handler != "static_response" || handler.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("handler = %+v, want 503 static response", handler)
	}
}

func TestSinkSkipsLoadAfterSuccessfulUnchangedSync(t *testing.T) {
	t.Parallel()

	var loadCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/load" {
			t.Fatalf("request path = %q, want /load", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("request method = %q, want POST", r.Method)
		}
		var config caddyConfig
		if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		loadCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sink := NewSink(nil, Config{ListenHTTPAddr: "0.0.0.0:80", ArtifactListenAddr: "0.0.0.0:18082", ArtifactDocumentRoot: "/opt/mini-cloud/artifacts", AdminURL: server.URL})
	routes := []cloudmodel.Route{{Host: "api.team.apps.example.test", Backends: []string{"10.0.1.20:30080"}}}

	if err := sink.SyncRoutes(context.Background(), routes); err != nil {
		t.Fatalf("first SyncRoutes returned error: %v", err)
	}
	if err := sink.SyncRoutes(context.Background(), routes); err != nil {
		t.Fatalf("second SyncRoutes returned error: %v", err)
	}
	if got := loadCount.Load(); got != 1 {
		t.Fatalf("load count = %d, want 1", got)
	}
}

func TestSinkRetriesLoadAfterFailure(t *testing.T) {
	t.Parallel()

	var loadCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := loadCount.Add(1)
		if attempt == 1 {
			http.Error(w, "load failed", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sink := NewSink(nil, Config{ListenHTTPAddr: "0.0.0.0:80", ArtifactListenAddr: "0.0.0.0:18082", ArtifactDocumentRoot: "/opt/mini-cloud/artifacts", AdminURL: server.URL})
	routes := []cloudmodel.Route{{Host: "api.team.apps.example.test", Backends: []string{"10.0.1.20:30080"}}}

	if err := sink.SyncRoutes(context.Background(), routes); err == nil {
		t.Fatal("first Apply returned nil error, want load failure")
	}
	if err := sink.SyncRoutes(context.Background(), routes); err != nil {
		t.Fatalf("second SyncRoutes returned error: %v", err)
	}
	if got := loadCount.Load(); got != 2 {
		t.Fatalf("load count = %d, want retry", got)
	}
}
