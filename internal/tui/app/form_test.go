package app

import (
	"testing"

	controlplanev1 "mini-cloud/internal/gen/proto/minicloud/controlplane/v1"
)

func TestServiceCreateFormBuildsRequest(t *testing.T) {
	form := newServiceCreateForm([]*controlplanev1.Plane{{
		Provider: "tencent",
		Region:   "ap-beijing",
		Status:   &controlplanev1.PlaneStatus{Status: "ready"},
	}})

	values := map[string]string{
		"name":            "cliproxyapi",
		"display_name":    "CLI Proxy API",
		"provider":        "aliyun",
		"region":          "cn-beijing",
		"replicas":        "2",
		"instance_class":  "small",
		"exposure":        "public",
		"image":           "eceasy/cli-proxy-api:latest",
		"default_port":    "8317",
		"readiness_path":  "/healthz",
		"projected_files": "/etc/cliproxy/config.yaml|config_set|cfg_demo|config.yaml;/etc/cliproxy/auth/token|secret_set|sec_demo|token",
		"persistent_dirs": "auth-dir|/var/lib/cliproxy/auth",
	}
	for idx, field := range form.fields {
		if value, ok := values[field.key]; ok {
			form.fields[idx].value = value
		}
	}

	req, err := form.request("prj_demo")
	if err != nil {
		t.Fatalf("request returned error: %v", err)
	}
	if req.GetProjectId() != "prj_demo" {
		t.Fatalf("project id = %q, want prj_demo", req.GetProjectId())
	}
	if req.GetName() != "cliproxyapi" {
		t.Fatalf("name = %q, want cliproxyapi", req.GetName())
	}
	if req.GetSpec().GetReplicas() != 2 {
		t.Fatalf("replicas = %d, want 2", req.GetSpec().GetReplicas())
	}
	if req.GetSpec().GetDefaultPort() != 8317 {
		t.Fatalf("default port = %d, want 8317", req.GetSpec().GetDefaultPort())
	}
	if len(req.GetSpec().GetProjectedFiles()) != 2 {
		t.Fatalf("projected files len = %d, want 2", len(req.GetSpec().GetProjectedFiles()))
	}
	if req.GetSpec().GetProjectedFiles()[0].GetMountPath() != "/etc/cliproxy/config.yaml" {
		t.Fatalf("first projected mountPath = %q", req.GetSpec().GetProjectedFiles()[0].GetMountPath())
	}
	if len(req.GetSpec().GetPersistentDirs()) != 1 {
		t.Fatalf("persistent dirs len = %d, want 1", len(req.GetSpec().GetPersistentDirs()))
	}
	if req.GetSpec().GetPersistentDirs()[0].GetName() != "auth-dir" {
		t.Fatalf("first persistent dir name = %q, want auth-dir", req.GetSpec().GetPersistentDirs()[0].GetName())
	}
}

func TestParseProjectedFilesRejectsMalformedEntry(t *testing.T) {
	t.Parallel()

	_, err := parseProjectedFiles("/etc/app/config.yaml|config_set|cfg_only_three_fields")
	if err == nil {
		t.Fatal("expected parseProjectedFiles to reject malformed entry")
	}
}

func TestParsePersistentDirsRejectsMalformedEntry(t *testing.T) {
	t.Parallel()

	_, err := parsePersistentDirs("auth-dir-only")
	if err == nil {
		t.Fatal("expected parsePersistentDirs to reject malformed entry")
	}
}
