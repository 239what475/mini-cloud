package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadReadsYAMLConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control-plane.yaml")
	if err := os.WriteFile(path, []byte(`
server:
  httpAddr: 127.0.0.1:18080
ui:
  dir: /opt/mini-cloud/control-plane/web
database:
  url: postgres://mini_cloud:mini_cloud@127.0.0.1:5432/mini_cloud_control_plane?sslmode=disable
auth:
  adminToken: admin-secret
sync:
  planeIntervalSeconds: 15
service:
  reconcileTimeoutSeconds: 300
logs:
  loki:
    url: http://127.0.0.1:3100
    tenantID: tenant-a
    queryTimeoutSeconds: 9
`), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Path != path {
		t.Fatalf("Path = %q, want %q", cfg.Path, path)
	}
	if cfg.HTTPAddr != "127.0.0.1:18080" {
		t.Fatalf("HTTPAddr = %q", cfg.HTTPAddr)
	}
	if cfg.UIDir != "/opt/mini-cloud/control-plane/web" {
		t.Fatalf("UIDir = %q", cfg.UIDir)
	}
	if cfg.DatabaseURL == "" {
		t.Fatalf("DatabaseURL is empty")
	}
	if cfg.AdminToken != "admin-secret" {
		t.Fatalf("AdminToken = %q", cfg.AdminToken)
	}
	if cfg.PlaneSyncIntervalSeconds != 15 {
		t.Fatalf("PlaneSyncIntervalSeconds = %d", cfg.PlaneSyncIntervalSeconds)
	}
	if cfg.ServiceReconcileTimeoutSeconds != 300 {
		t.Fatalf("ServiceReconcileTimeoutSeconds = %d", cfg.ServiceReconcileTimeoutSeconds)
	}
	if cfg.LokiURL != "http://127.0.0.1:3100" || cfg.LokiTenantID != "tenant-a" || cfg.LokiQueryTimeoutSeconds != 9 {
		t.Fatalf("unexpected Loki config: %+v", cfg)
	}
}

func TestLoadRequiresConfigPath(t *testing.T) {
	if _, err := Load(""); err == nil {
		t.Fatalf("Load returned nil error, want config path error")
	}
}

func TestLoadRequiresDatabaseAndAdminToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control-plane.yaml")
	if err := os.WriteFile(path, []byte(`
database:
  url: ""
auth:
  adminToken: ""
`), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatalf("Load returned nil error, want validation error")
	}
}

func TestLoadRejectsNegativeDurations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control-plane.yaml")
	if err := os.WriteFile(path, []byte(`
database:
  url: postgres://mini_cloud:mini_cloud@127.0.0.1:5432/mini_cloud_control_plane?sslmode=disable
auth:
  adminToken: admin-secret
sync:
  planeIntervalSeconds: -1
`), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatalf("Load returned nil error, want validation error")
	}
}
