package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadNodeAgentConfig(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "node-agent.yaml")
	data := []byte(`server:
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
  egressProxy:
    endpoint: http://10.1.0.6:3128
    noProxy:
      - 127.0.0.1
      - "  "
      - 10.0.0.0/8
observability:
  workloadLogLokiURL: http://127.0.0.1:3100
  workloadLogLokiTenantID: tenant-a
  workloadOTLPEndpoint: http://127.0.0.1:4318
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Server.URL != "http://127.0.0.1:18081" {
		t.Fatalf("server.url = %q", cfg.Server.URL)
	}
	if cfg.Auth.Token != "node-agent-secret" {
		t.Fatalf("auth.token = %q", cfg.Auth.Token)
	}
	if cfg.Node.Provider != "aliyun" || cfg.Node.InstanceID != "aliyun-node-a" {
		t.Fatalf("node config = %+v", cfg.Node)
	}
	if cfg.ResolvedCapacity.Allocatable.CPUMilli != 3800 {
		t.Fatalf("allocatable cpuMilli = %d, want 3800", cfg.ResolvedCapacity.Allocatable.CPUMilli)
	}
	if cfg.ResolvedCapacity.Allocatable.MemoryMi != 7424 {
		t.Fatalf("allocatable memoryMi = %d, want 7424", cfg.ResolvedCapacity.Allocatable.MemoryMi)
	}
	if cfg.Network.EgressProxy.Endpoint != "http://10.1.0.6:3128" {
		t.Fatalf("egress proxy = %+v", cfg.Network.EgressProxy)
	}
	if len(cfg.Network.EgressProxy.NoProxy) != 2 {
		t.Fatalf("egress proxy noProxy = %+v", cfg.Network.EgressProxy.NoProxy)
	}
	if cfg.Observability.WorkloadLogLokiURL != "http://127.0.0.1:3100" || cfg.Observability.WorkloadLogLokiTenantID != "tenant-a" {
		t.Fatalf("log config = %q/%q", cfg.Observability.WorkloadLogLokiURL, cfg.Observability.WorkloadLogLokiTenantID)
	}
}

func TestLoadNodeAgentConfigRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "node-agent.yaml")
	data := []byte(`server:
  url: http://127.0.0.1:18081
unknown: true
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

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

func TestLoadNodeAgentConfigRejectsInternalTuningBlocks(t *testing.T) {
	t.Parallel()

	for _, block := range []string{
		`agent:
  version: test-version
`,
		`runtime:
  type: docker
`,
		`work:
  readinessAttempts: 4
`,
	} {
		block := block
		t.Run(strings.Split(block, ":")[0], func(t *testing.T) {
			t.Parallel()

			path := writeNodeAgentConfigForTest(t, `server:
  url: http://127.0.0.1:18081
auth:
  token: node-agent-secret
node:
  region: cn-beijing
  name: aliyun-node-a
  privateIP: 127.0.0.1
  instanceID: aliyun-node-a
  instanceType: ecs.u1-c1m1.large
capacity:
  total:
    cpuMilli: 4000
    memoryMi: 8192
`+block)

			if _, err := Load(path); err == nil {
				t.Fatalf("Load returned nil error for %s block", strings.Split(block, ":")[0])
			}
		})
	}
}

func TestLoadNodeAgentConfigRejectsCapacityReservationFields(t *testing.T) {
	t.Parallel()

	path := writeNodeAgentConfigForTest(t, `server:
  url: http://127.0.0.1:18081
auth:
  token: node-agent-secret
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
  agentReserved:
    cpuMilli: 250
    memoryMi: 256
`)

	if _, err := Load(path); err == nil {
		t.Fatal("Load returned nil error for capacity.agentReserved")
	}
}

func TestLoadNodeAgentConfigRejectsMissingServerURL(t *testing.T) {
	t.Parallel()

	path := writeNodeAgentConfigForTest(t, `auth:
  token: node-agent-secret
node:
  region: cn-beijing
  name: aliyun-node-a
  privateIP: 127.0.0.1
  instanceID: aliyun-node-a
  instanceType: ecs.u1-c1m1.large
capacity:
  total:
    cpuMilli: 4000
    memoryMi: 8192
`)

	if _, err := Load(path); err == nil {
		t.Fatal("Load returned nil error for missing server.url")
	}
}

func TestLoadNodeAgentConfigRejectsMissingToken(t *testing.T) {
	t.Parallel()

	path := writeNodeAgentConfigForTest(t, `server:
  url: http://127.0.0.1:18081
node:
  region: cn-beijing
  name: aliyun-node-a
  privateIP: 127.0.0.1
  instanceID: aliyun-node-a
  instanceType: ecs.u1-c1m1.large
capacity:
  total:
    cpuMilli: 4000
    memoryMi: 8192
`)

	if _, err := Load(path); err == nil {
		t.Fatal("Load returned nil error for missing auth.token")
	}
}

func TestLoadNodeAgentConfigRejectsInvalidServerURL(t *testing.T) {
	t.Parallel()

	for _, rawURL := range []string{"ftp://127.0.0.1:18081", "https://127.0.0.1:18081"} {
		rawURL := rawURL
		t.Run(rawURL, func(t *testing.T) {
			t.Parallel()

			path := writeNodeAgentConfigForTest(t, `server:
  url: `+rawURL+`
auth:
  token: node-agent-secret
node:
  region: cn-beijing
  name: aliyun-node-a
  privateIP: 127.0.0.1
  instanceID: aliyun-node-a
  instanceType: ecs.u1-c1m1.large
capacity:
  total:
    cpuMilli: 4000
    memoryMi: 8192
`)

			if _, err := Load(path); err == nil {
				t.Fatal("Load returned nil error for invalid server.url")
			}
		})
	}
}

func writeNodeAgentConfigForTest(t *testing.T, data string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "node-agent.yaml")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
