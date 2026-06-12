package cloudplane

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
)

func TestRegisterWithControlPlane(t *testing.T) {
	var got registrationRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/internal/planes/register" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer southbound-token" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("Decode returned error: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := registerWithControlPlane(context.Background(), registrationTestConfig(server.URL))
	if err != nil {
		t.Fatalf("registerWithControlPlane returned error: %v", err)
	}
	if got.Name != "mini-cloud-lab" || got.GRPCEndpoint != "10.0.0.10:18081" || got.Provider != "tencent" || got.Region != "ap-guangzhou" {
		t.Fatalf("unexpected registration body: %+v", got)
	}
}

func TestControlPlaneRegistrationLoopRetriesUntilRegistered(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n := attempts.Add(1); n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	done := make(chan struct{})
	go func() {
		runControlPlaneRegistrationLoop(
			context.Background(),
			slog.New(slog.NewTextHandler(io.Discard, nil)),
			registrationTestConfig(server.URL),
			time.Millisecond,
		)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("registration loop did not stop after successful retry")
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

func registrationTestConfig(controlPlaneURL string) cloudplaneconfig.Config {
	return cloudplaneconfig.Config{
		Plane: cloudplaneconfig.PlaneConfig{
			Name:         "mini-cloud-lab",
			GRPCEndpoint: "10.0.0.10:18081",
		},
		ControlPlane: cloudplaneconfig.ControlPlaneConfig{
			URL:         controlPlaneURL,
			BearerToken: "southbound-token",
		},
		Infrastructure: cloudplaneconfig.InfrastructureConfig{
			Provider: "tencent",
			RegionID: "ap-guangzhou",
		},
	}
}
