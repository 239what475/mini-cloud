package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestLoadAppliesDefaults 验证配置加载会补齐 cloud-plane 默认值。
func TestLoadAppliesDefaults(t *testing.T) {
	t.Parallel()

	// 准备只包含必填字段的配置文件，用来验证 Load 会补齐派生默认值。
	dir := t.TempDir()
	path := filepath.Join(dir, "cloud-plane.yaml")
	data := []byte(`
server:
  listenGRPCAddr: 0.0.0.0:18081
database:
  url: postgres://mini_cloud:mini_cloud@127.0.0.1:5432/mini_cloud_cloud_plane?sslmode=disable
plane:
  identity:
    name: mini-cloud-lab
controlPlane:
  auth:
    bearerToken: southbound-token
nodeAgent:
  connectEndpoint: 10.0.0.10:18081
  auth:
    bootstrapToken: bootstrap-token
  artifact:
    binaryUrl: https://artifact.example/node-agent-linux-amd64
infrastructure:
  provider: aliyun
  location:
    regionId: cn-beijing
runtimeProvisioning:
  providerSpec:
    instanceType: ecs.u1-c1m1.large
    imageId: m-test
    vSwitchId: vsw-test
    securityGroupId: sg-test
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	// 加载配置后断言 node-agent 相关默认值来自 plane name 和内置默认策略。
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.NodeAgent.Defaults.NodeNamePrefix != "mini-cloud-lab-runtime-node" {
		t.Fatalf("NodeNamePrefix = %q, want mini-cloud-lab-runtime-node", cfg.NodeAgent.Defaults.NodeNamePrefix)
	}
	if cfg.NodeAgent.Defaults.HeartbeatIntervalSeconds != 15 {
		t.Fatalf("HeartbeatIntervalSeconds = %d, want 15", cfg.NodeAgent.Defaults.HeartbeatIntervalSeconds)
	}
	if cfg.NodeAgent.Defaults.WorkIntervalSeconds != 5 {
		t.Fatalf("WorkIntervalSeconds = %d, want 5", cfg.NodeAgent.Defaults.WorkIntervalSeconds)
	}
	if cfg.NodeAgent.Defaults.HostPortRange.Min != 30000 || cfg.NodeAgent.Defaults.HostPortRange.Max != 60999 {
		t.Fatalf("HostPortRange = %d-%d, want 30000-60999", cfg.NodeAgent.Defaults.HostPortRange.Min, cfg.NodeAgent.Defaults.HostPortRange.Max)
	}
	if cfg.NodeAgent.Auth.SessionTTL.String() != "24h0m0s" {
		t.Fatalf("SessionTTL = %s, want 24h0m0s", cfg.NodeAgent.Auth.SessionTTL)
	}
}

// TestValidateAcceptsHTTPNodeAgentConnectEndpoint 验证 node-agent 连接地址允许携带 HTTP scheme。
func TestValidateAcceptsHTTPNodeAgentConnectEndpoint(t *testing.T) {
	t.Parallel()

	// 构造完整有效配置，并显式使用带 scheme 的 node-agent 连接地址。
	cfg := Config{
		Server:       ServerConfig{ListenGRPCAddr: "0.0.0.0:18081"},
		Database:     DatabaseConfig{URL: "postgres://mini_cloud:mini_cloud@127.0.0.1:5432/mini_cloud_cloud_plane?sslmode=disable"},
		Plane:        PlaneConfig{Identity: PlaneIdentity{Name: "mini-cloud-lab"}},
		ControlPlane: ControlPlaneConfig{Auth: ControlPlaneAuthConfig{BearerToken: "southbound-token"}},
		NodeAgent: NodeAgentConfig{
			ConnectEndpoint: "https://10.0.0.10:18081",
			Auth:            NodeAgentAuthConfig{BootstrapToken: "bootstrap-token", SessionTTL: 1},
			Artifact:        NodeAgentArtifactConfig{BinaryURL: "https://artifact.example/node-agent-linux-amd64"},
			Defaults:        NodeAgentDefaultsConfig{NodeNamePrefix: "mini-cloud-lab-runtime-node", HeartbeatIntervalSeconds: 15, WorkIntervalSeconds: 5, HostPortRange: HostPortRangeConfig{Min: 30000, Max: 60999}},
		},
		Infrastructure:      InfrastructureConfig{Provider: "aliyun", Location: InfrastructureLocation{RegionID: "cn-beijing"}},
		RuntimeProvisioning: RuntimeProvisioningConfig{ProviderSpec: json.RawMessage(`{"instanceType":"ecs.u1-c1m1.large"}`)},
	}
	// Validate 应接受 http/https 形式的 connectEndpoint。
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate error: %v", err)
	}
}

// TestValidateRejectsLocalProvider 验证 cloud-plane 配置拒绝 local provider。
func TestValidateRejectsLocalProvider(t *testing.T) {
	t.Parallel()

	// local provider 已从 cloud-plane runtime driver 中移除，配置校验必须拒绝。
	cfg := Config{
		Server:       ServerConfig{ListenGRPCAddr: "0.0.0.0:18081"},
		Database:     DatabaseConfig{URL: "postgres://mini_cloud:mini_cloud@127.0.0.1:5432/mini_cloud_cloud_plane?sslmode=disable"},
		Plane:        PlaneConfig{Identity: PlaneIdentity{Name: "mini-cloud-lab"}},
		ControlPlane: ControlPlaneConfig{Auth: ControlPlaneAuthConfig{BearerToken: "southbound-token"}},
		NodeAgent: NodeAgentConfig{
			ConnectEndpoint: "10.0.0.10:18081",
			Auth:            NodeAgentAuthConfig{BootstrapToken: "bootstrap-token", SessionTTL: 1},
			Artifact:        NodeAgentArtifactConfig{BinaryURL: "https://artifact.example/node-agent-linux-amd64"},
			Defaults:        NodeAgentDefaultsConfig{NodeNamePrefix: "mini-cloud-lab-runtime-node", HeartbeatIntervalSeconds: 15, WorkIntervalSeconds: 5, HostPortRange: HostPortRangeConfig{Min: 30000, Max: 60999}},
		},
		Infrastructure:      InfrastructureConfig{Provider: "local", Location: InfrastructureLocation{RegionID: "local"}},
		RuntimeProvisioning: RuntimeProvisioningConfig{ProviderSpec: json.RawMessage(`{"instanceType":"local"}`)},
	}
	// provider=local 不能通过校验，避免重新引入手动扩容语义。
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate error = nil, want local provider rejection")
	}
}

// TestLoadRejectsRuntimeProvisioningEnabled 验证加载配置时拒绝 legacy enabled 开关。
func TestLoadRejectsRuntimeProvisioningEnabled(t *testing.T) {
	t.Parallel()

	// 写入仍包含 legacy enabled 开关的 YAML，用来确认新配置模型不接受该字段。
	dir := t.TempDir()
	path := filepath.Join(dir, "cloud-plane.yaml")
	data := []byte(`
server:
  listenGRPCAddr: 0.0.0.0:18081
database:
  url: postgres://mini_cloud:mini_cloud@127.0.0.1:5432/mini_cloud_cloud_plane?sslmode=disable
plane:
  identity:
    name: mini-cloud-lab
controlPlane:
  auth:
    bearerToken: southbound-token
nodeAgent:
  connectEndpoint: 10.0.0.10:18081
  auth:
    bootstrapToken: bootstrap-token
  artifact:
    binaryUrl: https://artifact.example/node-agent-linux-amd64
infrastructure:
  provider: aliyun
  location:
    regionId: cn-beijing
runtimeProvisioning:
  enabled: true
  providerSpec:
    instanceType: ecs.u1-c1m1.large
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}
	// Load 使用严格 YAML 解码和语义校验，应拒绝 runtimeProvisioning.enabled。
	if _, err := Load(path); err == nil {
		t.Fatal("Load error = nil, want runtimeProvisioning.enabled rejection")
	}
}

// TestValidateRequiresRuntimeProvisioningProviderSpec 验证 runtime provisioning 必须提供 providerSpec。
func TestValidateRequiresRuntimeProvisioningProviderSpec(t *testing.T) {
	t.Parallel()

	// 构造缺少 providerSpec 的配置，其它必填项保持有效，确保失败点集中在 providerSpec。
	cfg := Config{
		Server:       ServerConfig{ListenGRPCAddr: "0.0.0.0:18081"},
		Database:     DatabaseConfig{URL: "postgres://mini_cloud:mini_cloud@127.0.0.1:5432/mini_cloud_cloud_plane?sslmode=disable"},
		Plane:        PlaneConfig{Identity: PlaneIdentity{Name: "mini-cloud-lab"}},
		ControlPlane: ControlPlaneConfig{Auth: ControlPlaneAuthConfig{BearerToken: "southbound-token"}},
		NodeAgent: NodeAgentConfig{
			ConnectEndpoint: "10.0.0.10:18081",
			Auth:            NodeAgentAuthConfig{BootstrapToken: "bootstrap-token", SessionTTL: 1},
			Artifact:        NodeAgentArtifactConfig{BinaryURL: "https://artifact.example/node-agent-linux-amd64"},
			Defaults:        NodeAgentDefaultsConfig{NodeNamePrefix: "mini-cloud-lab-runtime-node", HeartbeatIntervalSeconds: 15, WorkIntervalSeconds: 5, HostPortRange: HostPortRangeConfig{Min: 30000, Max: 60999}},
		},
		Infrastructure:      InfrastructureConfig{Provider: "aliyun", Location: InfrastructureLocation{RegionID: "cn-beijing"}},
		RuntimeProvisioning: RuntimeProvisioningConfig{},
	}
	// runtime driver 必须依赖 providerSpec 构造，因此缺失时校验失败。
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate error = nil, want runtime provisioning providerSpec requirement")
	}
}

// TestParseProviderSpec 验证 providerSpec 可以严格解析到 provider 专属结构。
func TestParseProviderSpec(t *testing.T) {
	t.Parallel()

	// providerSpec 原始 JSON 包含目标结构体中声明的两个字段。
	cfg := RuntimeProvisioningConfig{
		ProviderSpec: json.RawMessage(`{"instanceType":"ecs.u1-c1m1.large","systemDiskSizeGiB":40}`),
	}

	// 使用临时结构体模拟 provider driver 的专属配置结构。
	var spec struct {
		InstanceType      string `json:"instanceType"`
		SystemDiskSizeGiB int    `json:"systemDiskSizeGiB"`
	}
	if err := cfg.ParseProviderSpec(&spec); err != nil {
		t.Fatalf("ParseProviderSpec error: %v", err)
	}
	// 解码后逐项断言，确认 raw JSON 被写入目标结构体。
	if spec.InstanceType != "ecs.u1-c1m1.large" {
		t.Fatalf("InstanceType = %q, want ecs.u1-c1m1.large", spec.InstanceType)
	}
	if spec.SystemDiskSizeGiB != 40 {
		t.Fatalf("SystemDiskSizeGiB = %d, want 40", spec.SystemDiskSizeGiB)
	}
}

// TestParseProviderSpecRejectsUnknownField 验证 providerSpec 拒绝未知字段。
func TestParseProviderSpecRejectsUnknownField(t *testing.T) {
	t.Parallel()

	// providerSpec 含有目标结构体未声明的字段，用来验证 DisallowUnknownFields 生效。
	cfg := RuntimeProvisioningConfig{ProviderSpec: json.RawMessage(`{"instanceType":"ecs.u1-c1m1.large","unexpected":true}`)}

	// 目标结构体只声明 instanceType，unexpected 必须触发解析错误。
	var spec struct {
		InstanceType string `json:"instanceType"`
	}
	if err := cfg.ParseProviderSpec(&spec); err == nil {
		t.Fatal("ParseProviderSpec error = nil, want unknown field rejection")
	}
}

// TestLoadRejectsUnknownField 验证 YAML 配置拒绝未知顶层字段。
func TestLoadRejectsUnknownField(t *testing.T) {
	t.Parallel()

	// YAML 顶层包含未知字段，验证配置文件不允许静默忽略拼写或遗留字段。
	dir := t.TempDir()
	path := filepath.Join(dir, "cloud-plane.yaml")
	data := []byte(`
server:
  listenGRPCAddr: 0.0.0.0:18081
database:
  url: postgres://mini_cloud:mini_cloud@127.0.0.1:5432/mini_cloud_cloud_plane?sslmode=disable
plane:
  identity:
    name: mini-cloud-lab
controlPlane:
  auth:
    bearerToken: southbound-token
nodeAgent:
  connectEndpoint: 10.0.0.10:18081
  auth:
    bootstrapToken: bootstrap-token
infrastructure:
  provider: aliyun
  location:
    regionId: cn-beijing
legacyEnvFallback: true
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}
	// Load 应在严格解码阶段拒绝 unknown field。
	if _, err := Load(path); err == nil {
		t.Fatal("Load error = nil, want unknown field rejection")
	}
}

// TestLoadRejectsLegacyCloudPlaneBootstrapToken 验证 YAML 配置拒绝旧 cloudPlane token 字段。
func TestLoadRejectsLegacyCloudPlaneBootstrapToken(t *testing.T) {
	t.Parallel()

	// 构造包含旧 cloudPlane.nodeAgentBootstrapToken 的 YAML，确保迁移后不再兼容旧位置。
	dir := t.TempDir()
	path := filepath.Join(dir, "cloud-plane.yaml")
	data := []byte(`
server:
  listenGRPCAddr: 0.0.0.0:18081
database:
  url: postgres://mini_cloud:mini_cloud@127.0.0.1:5432/mini_cloud_cloud_plane?sslmode=disable
controlPlane:
  auth:
    bearerToken: southbound-token
nodeAgent:
  connectEndpoint: 10.0.0.10:18081
  auth:
    bootstrapToken: bootstrap-token
cloudPlane:
  internalGRPCEndpoint: 10.0.0.10:18081
  nodeAgentBootstrapToken: legacy-location-token
plane:
  identity:
    name: mini-cloud-lab
infrastructure:
  provider: aliyun
  location:
    regionId: cn-beijing
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}
	// 严格配置模型应拒绝 legacy cloudPlane block。
	if _, err := Load(path); err == nil {
		t.Fatal("Load error = nil, want legacy cloudPlane block rejection")
	}
}

// TestLoadRejectsInvalidSessionTTL 验证 node-agent session TTL 必须为正值。
func TestLoadRejectsInvalidSessionTTL(t *testing.T) {
	t.Parallel()

	// sessionTTL=0s 不具备有效会话窗口，配置加载应拒绝。
	dir := t.TempDir()
	path := filepath.Join(dir, "cloud-plane.yaml")
	data := []byte(`
server:
  listenGRPCAddr: 0.0.0.0:18081
database:
  url: postgres://mini_cloud:mini_cloud@127.0.0.1:5432/mini_cloud_cloud_plane?sslmode=disable
plane:
  identity:
    name: mini-cloud-lab
controlPlane:
  auth:
    bearerToken: southbound-token
nodeAgent:
  connectEndpoint: 10.0.0.10:18081
  auth:
    bootstrapToken: bootstrap-token
    sessionTTL: 0s
infrastructure:
  provider: aliyun
  location:
    regionId: cn-beijing
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}
	// Load 会解析 duration 并校验必须为正值。
	if _, err := Load(path); err == nil {
		t.Fatal("Load error = nil, want invalid session TTL rejection")
	}
}

// TestLoadDeployExamples 验证部署示例配置能被当前配置模型加载。
func TestLoadDeployExamples(t *testing.T) {
	t.Parallel()

	// 部署示例是用户实际复制的入口，测试确保 aliyun/tencent 两份示例都能被当前配置模型加载。
	for _, path := range []string{
		"../../../deploy/cloud-plane/cloud-plane.aliyun.yaml.example",
		"../../../deploy/cloud-plane/cloud-plane.tencent.yaml.example",
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			// 每个子测试只验证配置加载成功，不连接真实云厂商或数据库。
			if _, err := Load(path); err != nil {
				t.Fatalf("Load(%q) error: %v", path, err)
			}
		})
	}
}
