package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadAcceptsMinimalCloudPlaneConfig 验证 cloud-plane YAML 只需要保留真正需要人工配置的字段。
func TestLoadAcceptsMinimalCloudPlaneConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "cloud-plane.yaml")
	data := []byte(`
server:
  listenGRPCAddr: 0.0.0.0:18081
database:
  url: postgres://mini_cloud:mini_cloud@127.0.0.1:5432/mini_cloud_cloud_plane?sslmode=disable
plane:
  name: mini-cloud-lab
  grpcEndpoint: 10.0.0.10:18081
controlPlane:
  url: http://127.0.0.1:18080
  bearerToken: southbound-token
nodeAgent:
  connectEndpoint: 10.0.0.10:18081
  bootstrapToken: bootstrap-token
  binaryUrl: https://artifact.example/node-agent-linux-amd64
infrastructure:
  provider: aliyun
  regionId: cn-beijing
runtimeProvisioning:
  instanceType: ecs.u1-c1m1.large
  providerSpec:
    imageId: m-test
    vSwitchId: vsw-test
    securityGroupId: sg-test
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Path != path {
		t.Fatalf("Path = %q, want %q", cfg.Path, path)
	}
	if cfg.Plane.Name != "mini-cloud-lab" {
		t.Fatalf("Plane.Name = %q, want mini-cloud-lab", cfg.Plane.Name)
	}
	if cfg.Infrastructure.Provider != "aliyun" {
		t.Fatalf("Infrastructure.Provider = %q, want aliyun", cfg.Infrastructure.Provider)
	}
}

// TestValidateAcceptsHTTPNodeAgentConnectEndpoint 验证 node-agent 连接地址允许携带 HTTP scheme。
func TestValidateAcceptsHTTPNodeAgentConnectEndpoint(t *testing.T) {
	t.Parallel()

	// 构造完整有效配置，并显式使用带 scheme 的 node-agent 连接地址。
	cfg := Config{
		Server:       ServerConfig{ListenGRPCAddr: "0.0.0.0:18081"},
		Database:     DatabaseConfig{URL: "postgres://mini_cloud:mini_cloud@127.0.0.1:5432/mini_cloud_cloud_plane?sslmode=disable"},
		Plane:        PlaneConfig{Name: "mini-cloud-lab", GRPCEndpoint: "10.0.0.10:18081"},
		ControlPlane: ControlPlaneConfig{URL: "http://127.0.0.1:18080", BearerToken: "southbound-token"},
		NodeAgent: NodeAgentConfig{
			ConnectEndpoint: "https://10.0.0.10:18081",
			BootstrapToken:  "bootstrap-token",
			BinaryURL:       "https://artifact.example/node-agent-linux-amd64",
		},
		Infrastructure: InfrastructureConfig{Provider: "aliyun", RegionID: "cn-beijing"},
		RuntimeProvisioning: RuntimeProvisioningConfig{
			InstanceType: "ecs.u1-c1m1.large",
			ProviderSpec: map[string]any{"imageId": "m-test"},
		},
	}
	// Validate 应接受 http/https 形式的 connectEndpoint。
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate error: %v", err)
	}
}

// TestValidateRejectsLocalProvider 验证 cloud-plane 配置拒绝 local provider。
func TestValidateRejectsLocalProvider(t *testing.T) {
	t.Parallel()

	// local provider 已从 cloud-plane node provider driver 中移除，配置校验必须拒绝。
	cfg := Config{
		Server:       ServerConfig{ListenGRPCAddr: "0.0.0.0:18081"},
		Database:     DatabaseConfig{URL: "postgres://mini_cloud:mini_cloud@127.0.0.1:5432/mini_cloud_cloud_plane?sslmode=disable"},
		Plane:        PlaneConfig{Name: "mini-cloud-lab", GRPCEndpoint: "10.0.0.10:18081"},
		ControlPlane: ControlPlaneConfig{URL: "http://127.0.0.1:18080", BearerToken: "southbound-token"},
		NodeAgent: NodeAgentConfig{
			ConnectEndpoint: "10.0.0.10:18081",
			BootstrapToken:  "bootstrap-token",
			BinaryURL:       "https://artifact.example/node-agent-linux-amd64",
		},
		Infrastructure: InfrastructureConfig{Provider: "local", RegionID: "local"},
		RuntimeProvisioning: RuntimeProvisioningConfig{
			InstanceType: "local",
			ProviderSpec: map[string]any{"imageId": "m-test"},
		},
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
  name: mini-cloud-lab
  grpcEndpoint: 10.0.0.10:18081
controlPlane:
  url: http://127.0.0.1:18080
  bearerToken: southbound-token
nodeAgent:
  connectEndpoint: 10.0.0.10:18081
  bootstrapToken: bootstrap-token
  binaryUrl: https://artifact.example/node-agent-linux-amd64
infrastructure:
  provider: aliyun
  regionId: cn-beijing
runtimeProvisioning:
  enabled: true
  instanceType: ecs.u1-c1m1.large
  providerSpec:
    imageId: m-test
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
		Plane:        PlaneConfig{Name: "mini-cloud-lab", GRPCEndpoint: "10.0.0.10:18081"},
		ControlPlane: ControlPlaneConfig{URL: "http://127.0.0.1:18080", BearerToken: "southbound-token"},
		NodeAgent: NodeAgentConfig{
			ConnectEndpoint: "10.0.0.10:18081",
			BootstrapToken:  "bootstrap-token",
			BinaryURL:       "https://artifact.example/node-agent-linux-amd64",
		},
		Infrastructure:      InfrastructureConfig{Provider: "aliyun", RegionID: "cn-beijing"},
		RuntimeProvisioning: RuntimeProvisioningConfig{InstanceType: "ecs.u1-c1m1.large"},
	}
	// node provider driver 必须依赖 providerSpec 构造，因此缺失时校验失败。
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate error = nil, want runtime provisioning providerSpec requirement")
	}
}

// TestValidateRequiresNodeProvisioningInstanceType 验证 node pool 必须声明公共实例规格。
func TestValidateRequiresNodeProvisioningInstanceType(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Server:       ServerConfig{ListenGRPCAddr: "0.0.0.0:18081"},
		Database:     DatabaseConfig{URL: "postgres://mini_cloud:mini_cloud@127.0.0.1:5432/mini_cloud_cloud_plane?sslmode=disable"},
		Plane:        PlaneConfig{Name: "mini-cloud-lab", GRPCEndpoint: "10.0.0.10:18081"},
		ControlPlane: ControlPlaneConfig{URL: "http://127.0.0.1:18080", BearerToken: "southbound-token"},
		NodeAgent: NodeAgentConfig{
			ConnectEndpoint: "10.0.0.10:18081",
			BootstrapToken:  "bootstrap-token",
			BinaryURL:       "https://artifact.example/node-agent-linux-amd64",
		},
		Infrastructure:      InfrastructureConfig{Provider: "aliyun", RegionID: "cn-beijing"},
		RuntimeProvisioning: RuntimeProvisioningConfig{ProviderSpec: map[string]any{"imageId": "m-test"}},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate error = nil, want runtime provisioning instanceType requirement")
	}
}

func TestValidateRequiresCaddyAdminURLWhenIngressEnabled(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Server:       ServerConfig{ListenGRPCAddr: "0.0.0.0:18081"},
		Database:     DatabaseConfig{URL: "postgres://mini_cloud:mini_cloud@127.0.0.1:5432/mini_cloud_cloud_plane?sslmode=disable"},
		Plane:        PlaneConfig{Name: "mini-cloud-lab", GRPCEndpoint: "10.0.0.10:18081"},
		ControlPlane: ControlPlaneConfig{URL: "http://127.0.0.1:18080", BearerToken: "southbound-token"},
		NodeAgent: NodeAgentConfig{
			ConnectEndpoint: "10.0.0.10:18081",
			BootstrapToken:  "bootstrap-token",
			BinaryURL:       "https://artifact.example/node-agent-linux-amd64",
		},
		Infrastructure: InfrastructureConfig{Provider: "aliyun", RegionID: "cn-beijing"},
		RuntimeProvisioning: RuntimeProvisioningConfig{
			InstanceType: "ecs.u1-c1m1.large",
			ProviderSpec: map[string]any{"imageId": "m-test"},
		},
		Ingress: IngressConfig{BaseDomain: "apps.example.test"},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate error = nil, want caddy admin URL requirement")
	}
}

// TestParseProviderSpec 验证 providerSpec 可以严格解析到 provider 专属结构。
func TestParseProviderSpec(t *testing.T) {
	t.Parallel()

	// providerSpec 原始 JSON 包含目标结构体中声明的两个字段。
	cfg := RuntimeProvisioningConfig{
		ProviderSpec: map[string]any{"imageId": "m-test", "systemDiskSizeGiB": 40},
	}

	// 使用临时结构体模拟 provider driver 的专属配置结构。
	var spec struct {
		ImageID           string `json:"imageId"`
		SystemDiskSizeGiB int    `json:"systemDiskSizeGiB"`
	}
	if err := cfg.ParseProviderSpec(&spec); err != nil {
		t.Fatalf("ParseProviderSpec error: %v", err)
	}
	// 解码后逐项断言，确认 raw JSON 被写入目标结构体。
	if spec.ImageID != "m-test" {
		t.Fatalf("ImageID = %q, want m-test", spec.ImageID)
	}
	if spec.SystemDiskSizeGiB != 40 {
		t.Fatalf("SystemDiskSizeGiB = %d, want 40", spec.SystemDiskSizeGiB)
	}
}

// TestParseProviderSpecRejectsUnknownField 验证 providerSpec 拒绝未知字段。
func TestParseProviderSpecRejectsUnknownField(t *testing.T) {
	t.Parallel()

	// providerSpec 含有目标结构体未声明的字段，用来验证 DisallowUnknownFields 生效。
	cfg := RuntimeProvisioningConfig{ProviderSpec: map[string]any{"imageId": "m-test", "unexpected": true}}

	// 目标结构体只声明 imageId，unexpected 必须触发解析错误。
	var spec struct {
		ImageID string `json:"imageId"`
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
  name: mini-cloud-lab
controlPlane:
  bearerToken: southbound-token
nodeAgent:
  connectEndpoint: 10.0.0.10:18081
  bootstrapToken: bootstrap-token
infrastructure:
  provider: aliyun
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
  bearerToken: southbound-token
nodeAgent:
  connectEndpoint: 10.0.0.10:18081
  bootstrapToken: bootstrap-token
cloudPlane:
  internalGRPCEndpoint: 10.0.0.10:18081
  nodeAgentBootstrapToken: legacy-location-token
plane:
  name: mini-cloud-lab
infrastructure:
  provider: aliyun
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
