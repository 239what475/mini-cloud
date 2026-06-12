package lab

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigAcceptsMultiPlaneExample(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join("..", "..", "deploy", "lab", "lab.yaml.example"))
	if err != nil {
		t.Fatalf("LoadConfig example returned error: %v", err)
	}
	if cfg.ControlPlane.SSH.Host != "myserver-control" {
		t.Fatalf("control-plane host = %q", cfg.ControlPlane.SSH.Host)
	}
	if len(cfg.Planes) != 2 {
		t.Fatalf("planes = %d, want 2", len(cfg.Planes))
	}
	if !filepath.IsAbs(cfg.Planes[0].Terraform.VarFile) {
		t.Fatalf("plane var file was not normalized to absolute path: %q", cfg.Planes[0].Terraform.VarFile)
	}
}

func TestLoadConfigRejectsTwoPlanesOnSameHost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lab.yaml")
	content := []byte(`
controlPlane:
  ssh:
    host: control
  url: http://control:18080
planes:
  - name: a
    provider: aliyun
    region: cn-beijing
    ssh:
      host: same
    terraform:
      workspace: a
      varFile: a.tfvars
  - name: b
    provider: tencent
    region: ap-guangzhou
    ssh:
      host: same
    terraform:
      workspace: b
      varFile: b.tfvars
`)
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "same ssh.host") {
		t.Fatalf("LoadConfig error = %v, want same host rejection", err)
	}
}

func TestLoadConfigRejectsTerraformArgs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lab.yaml")
	content := []byte(`
controlPlane:
  ssh:
    host: control
  url: http://control:18080
planes:
  - name: a
    provider: aliyun
    region: cn-beijing
    ssh:
      host: plane-a
    terraform:
      workspace: a
      varFile: a.tfvars
      applyArgs: ["-auto-approve"]
`)
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "field applyArgs not found") {
		t.Fatalf("LoadConfig error = %v, want unknown applyArgs rejection", err)
	}
}

func TestValidateInstallRequiresIngressBaseDomain(t *testing.T) {
	cfg := Config{
		ControlPlane: ControlPlane{
			SSH: SSHConfig{Host: "control"},
			URL: "http://control:18080",
		},
		Install: InstallConfig{
			Root: "/opt/mini-cloud",
		},
		Tokens: TokenConfig{
			ControlPlaneAdmin:      "admin",
			ControlPlaneSouthbound: "southbound",
			NodeAgent:              "node-agent",
		},
	}
	if err := cfg.validateInstall(); err == nil || !strings.Contains(err.Error(), "install.ingressBaseDomain") {
		t.Fatalf("validateInstall error = %v, want ingressBaseDomain error", err)
	}
}
