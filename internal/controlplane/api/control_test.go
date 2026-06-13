package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	controlplaneconfig "mini-cloud/internal/controlplane/config"
	"mini-cloud/internal/controlplane/coordination"
)

func TestDNSCheckRequiresAdminToken(t *testing.T) {
	t.Parallel()

	handler := newTestControlHandler(t)
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/control/dns/check", nil)

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body = %s, want unauthorized", resp.Code, resp.Body.String())
	}
}

func TestDNSCheckReturnsOK(t *testing.T) {
	t.Parallel()

	handler := newTestControlHandler(t)
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/control/dns/check", nil)
	req.Header.Set("Authorization", "Bearer admin-token")

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want ok", resp.Code, resp.Body.String())
	}
}

func newTestControlHandler(t *testing.T) http.Handler {
	t.Helper()

	catalog, err := coordination.NewPlaneCatalog([]controlplaneconfig.PlaneConfig{{
		ID:           "pln_test",
		Name:         "test-plane",
		DisplayName:  "Test Plane",
		Provider:     "tencent",
		Region:       "ap-guangzhou",
		GRPCEndpoint: "127.0.0.1:18081",
	}})
	if err != nil {
		t.Fatalf("NewPlaneCatalog returned error: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	syncer := coordination.NewPlaneSyncer(logger, catalog, "southbound-token", nil)
	services := coordination.NewServiceOperations(logger, catalog, "southbound-token", "apps.example.test", syncer)
	handler, err := NewMux(Options{
		AdminToken:        "admin-token",
		ServiceOperations: services,
		PlaneSyncer:       syncer,
	}, logger)
	if err != nil {
		t.Fatalf("NewMux returned error: %v", err)
	}
	return handler
}
