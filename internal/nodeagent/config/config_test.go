package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadNodeAgentConfig(t *testing.T) {
	t.Parallel()

	cfg, err := Load(writeNodeAgentConfig(t, validNodeAgentYAML()))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Server.URL != "http://127.0.0.1:18081" || cfg.Auth.Token != "node-agent-secret" {
		t.Fatalf("config = %+v", cfg)
	}
	if cfg.ResolvedCapacity.Allocatable.CPUMilli != 3800 || cfg.ResolvedCapacity.Allocatable.MemoryMi != 7424 {
		t.Fatalf("allocatable capacity = %+v", cfg.ResolvedCapacity.Allocatable)
	}
	if len(cfg.Network.WorkloadProxy.NoProxy) != 2 {
		t.Fatalf("workload proxy noProxy = %+v", cfg.Network.WorkloadProxy.NoProxy)
	}
}

func TestLoadNodeAgentConfigAllowsGRPCSWithCA(t *testing.T) {
	t.Parallel()

	yaml := strings.Replace(validNodeAgentYAML(), "url: http://127.0.0.1:18081", "url: grpcs://127.0.0.1:18081\n  tlsCA: test-ca", 1)
	cfg, err := Load(writeNodeAgentConfig(t, yaml))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Server.URL != "grpcs://127.0.0.1:18081" || cfg.Server.TLSCA != "test-ca" {
		t.Fatalf("server config = %+v", cfg.Server)
	}
}

func TestLoadNodeAgentConfigRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	path := writeNodeAgentConfig(t, validNodeAgentYAML()+"\nunknown: true\n")
	if _, err := Load(path); err == nil {
		t.Fatal("Load returned nil error for unknown field")
	}
}

func TestLoadNodeAgentConfigRequiresPath(t *testing.T) {
	t.Parallel()

	if _, err := Load(" "); err == nil || !strings.Contains(err.Error(), "node-agent config path is empty") {
		t.Fatalf("Load error = %v, want empty path error", err)
	}
}

func TestLoadNodeAgentConfigValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "missing server url",
			yaml: strings.Replace(validNodeAgentYAML(), "server:\n  url: http://127.0.0.1:18081\n", "", 1),
		},
		{
			name: "missing token",
			yaml: strings.Replace(validNodeAgentYAML(), "auth:\n  token: node-agent-secret\n", "", 1),
		},
		{
			name: "invalid server url",
			yaml: strings.Replace(validNodeAgentYAML(), "http://127.0.0.1:18081", "https://127.0.0.1:18081", 1),
		},
		{
			name: "grpcs without ca",
			yaml: strings.Replace(validNodeAgentYAML(), "http://127.0.0.1:18081", "grpcs://127.0.0.1:18081", 1),
		},
		{
			name: "internal runtime block",
			yaml: validNodeAgentYAML() + "\nruntime:\n  type: docker\n",
		},
		{
			name: "capacity reservation field",
			yaml: strings.Replace(validNodeAgentYAML(), "capacity:\n  total:", "capacity:\n  agentReserved:\n    cpuMilli: 250\n    memoryMi: 256\n  total:", 1),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := Load(writeNodeAgentConfig(t, tt.yaml)); err == nil {
				t.Fatal("Load returned nil error, want validation error")
			}
		})
	}
}

func validNodeAgentYAML() string {
	return `server:
  url: http://127.0.0.1:18081
auth:
  token: node-agent-secret
platform:
  name: aliyun-plane
node:
  provider: aliyun
  region: cn-beijing
  name: aliyun-node-a
  privateIP: 127.0.0.1
  instanceID: aliyun-node-a
  instanceType: ecs.u1-c1m1.large
capacity:
  total:
    cpuMilli: 4000
    memoryMi: 8192
network:
  workloadProxy:
    endpoint: http://10.1.0.6:3128
    noProxy:
      - 127.0.0.1
      - "  "
      - 10.0.0.0/8
observability:
  workloadOTLPEndpoint: http://127.0.0.1:4318
`
}

func writeNodeAgentConfig(t *testing.T, data string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "node-agent.yaml")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
