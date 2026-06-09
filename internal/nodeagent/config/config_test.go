package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLoadNodeAgentConfig 验证显式 YAML 字段和默认值会共同归一化为 daemon 配置。
func TestLoadNodeAgentConfig(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "node-agent.yaml")
	data := []byte(`server:
  url: http://127.0.0.1:18081
auth:
  bootstrapToken: bootstrap-secret
platform:
  name: aliyun-plane
node:
  provider: aliyun
  region: cn-beijing
  name: aliyun-node-a
  privateIP: 127.0.0.1
  publicIP: 127.0.0.1
  instanceID: aliyun-node-a
  instanceType: ecs.u1-c1m1.large
capacity:
  total:
    cpuMilli: 4000
    memoryMi: 8192
  systemReserved:
    cpuMilli: 500
    memoryMi: 1024
  agentReserved:
    cpuMilli: 250
    memoryMi: 256
  evictionReserved:
    memoryMi: 512
agent:
  version: test-version
  heartbeatInterval: 10s
  workInterval: 3s
runtime:
  type: docker
  hostPortRange:
    min: 31000
    max: 31999
work:
  readinessAttempts: 4
  readinessInterval: 250ms
  readinessTimeout: 500ms
  runtimeTimeout: 30s
  logTail: 12
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

	if cfg.ServerURL != "http://127.0.0.1:18081" {
		t.Fatalf("ServerURL = %q", cfg.ServerURL)
	}
	if cfg.BootstrapToken != "bootstrap-secret" {
		t.Fatalf("BootstrapToken = %q", cfg.BootstrapToken)
	}
	if cfg.RegisterInput.GetProvider() != "aliyun" || cfg.RegisterInput.GetInstanceId() != "aliyun-node-a" {
		t.Fatalf("RegisterInput = %+v", cfg.RegisterInput)
	}
	if cfg.CPUMilliAllocatable != 3250 {
		t.Fatalf("CPUMilliAllocatable = %d, want 3250", cfg.CPUMilliAllocatable)
	}
	if cfg.MemoryMiAllocatable != 6400 {
		t.Fatalf("MemoryMiAllocatable = %d, want 6400", cfg.MemoryMiAllocatable)
	}
	if cfg.AgentVersion != "test-version" {
		t.Fatalf("agent version = %q", cfg.AgentVersion)
	}
	if cfg.HeartbeatInterval != 10*time.Second || cfg.WorkInterval != 3*time.Second {
		t.Fatalf("intervals = %s/%s", cfg.HeartbeatInterval, cfg.WorkInterval)
	}
	if cfg.Runtime.Type != "docker" {
		t.Fatalf("runtime type = %q, want docker", cfg.Runtime.Type)
	}
	if cfg.Runtime.HostPortMin != 31000 || cfg.Runtime.HostPortMax != 31999 {
		t.Fatalf("runtime hostPortRange = %d-%d, want 31000-31999", cfg.Runtime.HostPortMin, cfg.Runtime.HostPortMax)
	}
	if cfg.Work.ReadinessAttempts != 4 || cfg.Work.ReadinessInterval != 250*time.Millisecond {
		t.Fatalf("work options = %+v", cfg.Work)
	}
	if cfg.WorkloadLogLokiURL != "http://127.0.0.1:3100" || cfg.WorkloadLogLokiTenant != "tenant-a" {
		t.Fatalf("log config = %q/%q", cfg.WorkloadLogLokiURL, cfg.WorkloadLogLokiTenant)
	}
}

// TestLoadNodeAgentConfigRejectsUnknownFields 验证严格 YAML 解码会拒绝未知字段。
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

// TestLoadNodeAgentConfigRejectsOldCapacityFields 验证旧 capacity 字段不再被兼容接受。
func TestLoadNodeAgentConfigRejectsOldCapacityFields(t *testing.T) {
	t.Parallel()

	path := writeNodeAgentConfigForTest(t, `server:
  url: http://127.0.0.1:18081
auth:
  bootstrapToken: bootstrap-secret
node:
  region: cn-beijing
  name: aliyun-node-a
  privateIP: 127.0.0.1
  instanceID: aliyun-node-a
  instanceType: ecs.u1-c1m1.large
capacity:
  cpuMilliCapacity: 4000
  memoryMiCapacity: 8192
  cpuMilliReserve: 500
  memoryMiReserve: 1024
  cpuMilliAllocatable: 3000
  memoryMiAllocatable: 6000
`)

	if _, err := Load(path); err == nil {
		t.Fatal("Load returned nil error for old capacity fields")
	}
}

// TestLoadNodeAgentConfigRejectsOldHealthFields 验证旧 health 命名的 work 字段不再被兼容接受。
func TestLoadNodeAgentConfigRejectsOldHealthFields(t *testing.T) {
	t.Parallel()

	path := writeNodeAgentConfigForTest(t, `server:
  url: http://127.0.0.1:18081
auth:
  bootstrapToken: bootstrap-secret
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
work:
  healthAttempts: 4
  healthInterval: 250ms
  healthTimeout: 500ms
`)

	if _, err := Load(path); err == nil {
		t.Fatal("Load returned nil error for old health work fields")
	}
}

// TestLoadNodeAgentConfigRejectsAgentStatus 验证节点运行态不再允许从配置文件声明。
func TestLoadNodeAgentConfigRejectsAgentStatus(t *testing.T) {
	t.Parallel()

	path := writeNodeAgentConfigForTest(t, `server:
  url: http://127.0.0.1:18081
auth:
  bootstrapToken: bootstrap-secret
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
agent:
  status: ready
`)

	if _, err := Load(path); err == nil {
		t.Fatal("Load returned nil error for agent.status")
	}
}

// TestLoadNodeAgentConfigRejectsMissingServerURL 验证 server.url 必填。
func TestLoadNodeAgentConfigRejectsMissingServerURL(t *testing.T) {
	t.Parallel()

	path := writeNodeAgentConfigForTest(t, `auth:
  bootstrapToken: bootstrap-secret
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

// TestLoadNodeAgentConfigRejectsMissingBootstrapToken 验证 auth.bootstrapToken 必填。
func TestLoadNodeAgentConfigRejectsMissingBootstrapToken(t *testing.T) {
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
		t.Fatal("Load returned nil error for missing auth.bootstrapToken")
	}
}

// TestLoadNodeAgentConfigRejectsInvalidServerURL 验证 server.url 必须是明文 gRPC endpoint。
func TestLoadNodeAgentConfigRejectsInvalidServerURL(t *testing.T) {
	t.Parallel()

	for _, rawURL := range []string{"ftp://127.0.0.1:18081", "https://127.0.0.1:18081"} {
		rawURL := rawURL
		t.Run(rawURL, func(t *testing.T) {
			t.Parallel()

			path := writeNodeAgentConfigForTest(t, `server:
  url: `+rawURL+`
auth:
  bootstrapToken: bootstrap-secret
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

// writeNodeAgentConfigForTest 为 config 测试写入临时 node-agent YAML 配置。
func writeNodeAgentConfigForTest(t *testing.T, data string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "node-agent.yaml")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
