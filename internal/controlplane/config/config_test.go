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
auth:
  adminToken: admin-secret
  southboundToken: southbound-secret
  southboundTLS:
    caCert: test-ca
    cert: test-cert
    key: test-key
dns:
  serviceBaseDomain: apps.example.com
  dnspod:
    domain: example.com
planes:
  - id: pln_test
    name: test-plane
    displayName: Test Plane
    provider: aliyun
    region: cn-beijing
    grpcEndpoint: 127.0.0.1:18081
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
	if cfg.Auth.AdminToken != "admin-secret" {
		t.Fatalf("auth.adminToken = %q", cfg.Auth.AdminToken)
	}
	if cfg.Auth.SouthboundToken != "southbound-secret" {
		t.Fatalf("auth.southboundToken = %q", cfg.Auth.SouthboundToken)
	}
	if cfg.DNS.ServiceBaseDomain != "apps.example.com" {
		t.Fatalf("dns.serviceBaseDomain = %q", cfg.DNS.ServiceBaseDomain)
	}
	if cfg.DNS.DNSPod.Domain != "example.com" {
		t.Fatalf("unexpected DNSPod config: %+v", cfg.DNS.DNSPod)
	}
	if len(cfg.Planes) != 1 || cfg.Planes[0].ID != "pln_test" || cfg.Planes[0].Provider != "aliyun" {
		t.Fatalf("unexpected planes: %+v", cfg.Planes)
	}
}

func TestLoadRequiresConfigPath(t *testing.T) {
	if _, err := Load(""); err == nil {
		t.Fatalf("Load returned nil error, want config path error")
	}
}

func TestLoadRequiresAdminTokenAndPlanes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control-plane.yaml")
	if err := os.WriteFile(path, []byte(`
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

func TestLoadRequiresDNSPodConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control-plane.yaml")
	if err := os.WriteFile(path, []byte(`
auth:
  adminToken: admin-secret
  southboundToken: southbound-secret
  southboundTLS:
    caCert: test-ca
    cert: test-cert
    key: test-key
dns:
  serviceBaseDomain: apps.example.test
planes:
  - id: pln_test
    name: test-plane
    provider: aliyun
    region: cn-beijing
    grpcEndpoint: 127.0.0.1:18081
`), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatalf("Load returned nil error, want DNSPod validation error")
	}
}

func TestLoadReadsDNSPodDomainOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control-plane.yaml")
	if err := os.WriteFile(path, []byte(`
auth:
  adminToken: admin-secret
  southboundToken: southbound-secret
  southboundTLS:
    caCert: test-ca
    cert: test-cert
    key: test-key
dns:
  serviceBaseDomain: apps.example.test
  dnspod:
    domain: example.test
planes:
  - id: pln_test
    name: test-plane
    provider: aliyun
    region: cn-beijing
    grpcEndpoint: 127.0.0.1:18081
`), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.DNS.DNSPod.Domain != "example.test" {
		t.Fatalf("dns.dnspod.domain = %q", cfg.DNS.DNSPod.Domain)
	}
}

func TestLoadRejectsDNSPodStaticCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control-plane.yaml")
	if err := os.WriteFile(path, []byte(`
auth:
  adminToken: admin-secret
  southboundToken: southbound-secret
  southboundTLS:
    caCert: test-ca
    cert: test-cert
    key: test-key
dns:
  serviceBaseDomain: apps.example.test
  dnspod:
    domain: example.test
    legacyField: value
planes:
  - id: pln_test
    name: test-plane
    provider: aliyun
    region: cn-beijing
    grpcEndpoint: 127.0.0.1:18081
`), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatalf("Load returned nil error, want unknown field error")
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control-plane.yaml")
	if err := os.WriteFile(path, []byte(`
auth:
  adminToken: admin-secret
  southboundToken: southbound-secret
  southboundTLS:
    caCert: test-ca
    cert: test-cert
    key: test-key
dns:
  serviceBaseDomain: apps.example.test
  dnspod:
    domain: example.test
planes:
  - id: pln_test
    name: test-plane
    provider: aliyun
    region: cn-beijing
    grpcEndpoint: 127.0.0.1:18081
sync:
  planeIntervalSeconds: 30
`), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatalf("Load returned nil error, want unknown field error")
	}
}
