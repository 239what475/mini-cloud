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
  southboundToken: southbound-secret
dns:
  serviceBaseDomain: apps.whatcloud.cn
  dnspod:
    domain: whatcloud.cn
    secretId: sid
    secretKey: skey
    token: stok
logs:
  loki:
    url: http://127.0.0.1:3100
    tenantID: tenant-a
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
	if cfg.Server.HTTPAddr != "127.0.0.1:18080" {
		t.Fatalf("server.httpAddr = %q", cfg.Server.HTTPAddr)
	}
	if cfg.UI.Dir != "/opt/mini-cloud/control-plane/web" {
		t.Fatalf("ui.dir = %q", cfg.UI.Dir)
	}
	if cfg.Database.URL == "" {
		t.Fatalf("database.url is empty")
	}
	if cfg.Auth.AdminToken != "admin-secret" {
		t.Fatalf("auth.adminToken = %q", cfg.Auth.AdminToken)
	}
	if cfg.Auth.SouthboundToken != "southbound-secret" {
		t.Fatalf("auth.southboundToken = %q", cfg.Auth.SouthboundToken)
	}
	if cfg.DNS.ServiceBaseDomain != "apps.whatcloud.cn" {
		t.Fatalf("dns.serviceBaseDomain = %q", cfg.DNS.ServiceBaseDomain)
	}
	if cfg.DNS.DNSPod.Domain != "whatcloud.cn" || cfg.DNS.DNSPod.SecretID != "sid" || cfg.DNS.DNSPod.SecretKey != "skey" || cfg.DNS.DNSPod.Token != "stok" {
		t.Fatalf("unexpected DNSPod config: %+v", cfg.DNS.DNSPod)
	}
	if cfg.Logs.Loki.URL != "http://127.0.0.1:3100" || cfg.Logs.Loki.TenantID != "tenant-a" {
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
  southboundToken: ""
`), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatalf("Load returned nil error, want validation error")
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control-plane.yaml")
	if err := os.WriteFile(path, []byte(`
database:
  url: postgres://mini_cloud:mini_cloud@127.0.0.1:5432/mini_cloud_control_plane?sslmode=disable
auth:
  adminToken: admin-secret
  southboundToken: southbound-secret
dns:
  serviceBaseDomain: apps.example.test
sync:
  planeIntervalSeconds: 30
`), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatalf("Load returned nil error, want unknown field error")
	}
}
