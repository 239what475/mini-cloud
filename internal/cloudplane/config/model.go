package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// Config 是从单一 YAML 配置文件读取、补默认值并校验后的 cloud-plane 有效配置。
type Config struct {
	// Path 是本次读取的配置文件路径，仅用于日志和状态摘要，不来自 YAML 内容。
	Path string
	// Server 描述 cloud-plane 自身服务监听配置。
	Server ServerConfig
	// Database 描述 cloud-plane 状态库连接配置。
	Database DatabaseConfig
	// Plane 描述当前 cloud-plane 实例的身份和归属。
	Plane PlaneConfig
	// ControlPlane 描述 control-plane 访问 cloud-plane southbound gRPC 服务的入口认证配置。
	ControlPlane ControlPlaneConfig
	// NodeAgent 描述 node-agent 接入、bootstrap 和默认运行参数。
	NodeAgent NodeAgentConfig
	// Infrastructure 描述当前 plane 绑定的基础设施 provider 与位置。
	Infrastructure InfrastructureConfig
	// RuntimeProvisioning 描述 provider 专属 runtime node 创建参数。
	RuntimeProvisioning RuntimeProvisioningConfig
	// Ingress 描述外置 ingress 数据面配置。
	Ingress IngressConfig
	// Observability 描述下发给 workload/node-agent 的观测入口。
	Observability ObservabilityConfig
}

// ServerConfig 对应 YAML 中的 server 配置段。
type ServerConfig struct {
	// ListenGRPCAddr 是 cloud-plane 内部 gRPC 服务监听地址，只服务 ControlPlaneSnapshotService、ControlPlaneResourceService、ControlPlaneWorkloadService 和 NodeAgentService。
	ListenGRPCAddr string
}

// DatabaseConfig 对应 YAML 中的 database 配置段。
type DatabaseConfig struct {
	// URL 是 Postgres 数据库连接串。
	URL string
}

// PlaneConfig 对应 YAML 中的 plane 配置段。
type PlaneConfig struct {
	// Identity 描述当前 cloud-plane 实例的稳定身份和归属信息。
	Identity PlaneIdentity
}

// PlaneIdentity 描述当前 cloud-plane 实例的稳定身份和归属信息。
type PlaneIdentity struct {
	// Name 是当前 plane 实例的机器可读稳定标识，用于 ownership tag、日志和 node-agent platform 标识。
	Name string `json:"name" yaml:"name"`
	// Environment 是当前 plane 所处环境，如 dev、staging 或 prod。
	Environment string `json:"environment,omitempty" yaml:"environment"`
	// Owner 是当前 plane 的负责人或归属团队。
	Owner string `json:"owner,omitempty" yaml:"owner"`
}

// ControlPlaneConfig 对应 YAML 中的 controlPlane 配置段。
type ControlPlaneConfig struct {
	// Auth 描述 control-plane 调用 cloud-plane southbound gRPC 服务 时使用的认证配置。
	Auth ControlPlaneAuthConfig
}

// ControlPlaneAuthConfig 描述 control-plane 访问 cloud-plane 的认证配置。
type ControlPlaneAuthConfig struct {
	// BearerToken 是 control-plane 调用 cloud-plane southbound gRPC 服务 时必须携带的内部令牌。
	BearerToken string
}

// NodeAgentConfig 对应 YAML 中的 nodeAgent 配置段。
type NodeAgentConfig struct {
	// ConnectEndpoint 是下发给 node-agent 的 cloud-plane gRPC 连接地址。
	ConnectEndpoint string
	// Auth 描述 node-agent bootstrap 和 session 认证配置。
	Auth NodeAgentAuthConfig
	// Artifact 描述 runtime node bootstrap 下载 node-agent 二进制所需的地址和校验信息。
	Artifact NodeAgentArtifactConfig
	// Defaults 描述动态创建 runtime node 时写入 node-agent 配置的默认参数。
	Defaults NodeAgentDefaultsConfig
}

// NodeAgentAuthConfig 描述 node-agent 接入 cloud-plane 的认证配置。
type NodeAgentAuthConfig struct {
	// BootstrapToken 是 node-agent 首次注册使用的 bootstrap token。
	BootstrapToken string
	// SessionTTL 是 node-agent 会话 token 的有效期。
	SessionTTL time.Duration
}

// NodeAgentArtifactConfig 描述 node-agent 二进制分发配置。
type NodeAgentArtifactConfig struct {
	// BinaryURL 是 bootstrap runtime node 时下载 node-agent 的地址。
	BinaryURL string
	// BinarySHA256 是 node-agent 二进制的可选 SHA256 校验值。
	BinarySHA256 string
}

// NodeAgentDefaultsConfig 描述 runtime node 上 node-agent 的默认运行参数。
type NodeAgentDefaultsConfig struct {
	// NodeNamePrefix 是 runtime node 云资源名称前缀。
	NodeNamePrefix string
	// HeartbeatIntervalSeconds 是 node-agent 心跳上报周期秒数。
	HeartbeatIntervalSeconds int
	// WorkIntervalSeconds 是 node-agent 拉取执行任务的轮询周期秒数。
	WorkIntervalSeconds int
	// HostPortRange 是动态 runtime node 上 node-agent 可分配的宿主机端口范围。
	HostPortRange HostPortRangeConfig
}

// HostPortRangeConfig 描述 node-agent 发布 workload hostPort 的端口池。
type HostPortRangeConfig struct {
	// Min 是 hostPort 端口池最小值。
	Min int `json:"min" yaml:"min"`
	// Max 是 hostPort 端口池最大值。
	Max int `json:"max" yaml:"max"`
}

// InfrastructureConfig 对应 YAML 中的 infrastructure 配置段。
type InfrastructureConfig struct {
	// Provider 是基础设施 provider 驱动名称，只允许 aliyun 或 tencent。
	Provider string
	// Location 描述当前 plane 绑定的 provider region/zone。
	Location InfrastructureLocation
}

// InfrastructureLocation 描述 provider 侧 region/zone 位置。
type InfrastructureLocation struct {
	// RegionID 表示 Region 的唯一标识。
	RegionID string
	// ZoneID 表示 Zone 的唯一标识。
	ZoneID string
}

// RuntimeProvisioningConfig 描述动态 runtime node 创建配置。
type RuntimeProvisioningConfig struct {
	// ImagePull 描述 Docker 镜像拉取配置。
	ImagePull ImagePullConfig
	// Egress 描述 runtime node 出公网代理配置。
	Egress EgressConfig
	// ProviderSpec 保存云厂商专属 runtime node 创建参数，按 provider 延迟解析。
	ProviderSpec json.RawMessage
}

// ImagePullConfig 描述 runtime node 拉取容器镜像时的通用配置。
type ImagePullConfig struct {
	// RegistryMirrors 是写入 Docker daemon 的 registry mirror 列表。
	RegistryMirrors []string
}

// EgressConfig 描述 runtime node 出公网配置。
type EgressConfig struct {
	// Proxy 描述 HTTP/HTTPS forward proxy。
	Proxy EgressProxyConfig
}

// EgressProxyConfig 描述 runtime node 使用的 HTTP/HTTPS forward proxy。
type EgressProxyConfig struct {
	// Enabled 表示是否为 runtime node 注入代理配置。
	Enabled bool
	// Endpoint 是 runtime node 可通过私网访问的 proxy URL。
	Endpoint string
	// NoProxy 是不走 proxy 的地址、域名或网段列表。
	NoProxy []string
}

// IngressConfig 描述外置 ingress 数据面配置。
type IngressConfig struct {
	// Enabled 表示是否由 cloud-plane 管理外置 ingress 配置。
	Enabled bool
	// BaseDomain 是 public service 默认入口域名后缀。
	BaseDomain string
	// Caddy 描述外置 Caddy 配置。
	Caddy CaddyIngressConfig
}

// CaddyIngressConfig 描述外置 Caddy 的配置文件和 reload 命令。
type CaddyIngressConfig struct {
	// ListenHTTPAddr 是 Caddy HTTP 入口监听地址。
	ListenHTTPAddr string
	// ConfigPath 是 cloud-plane 写入的 Caddyfile 路径。
	ConfigPath string
	// ReloadCommand 是写入 Caddyfile 后执行的 reload 命令及参数。
	ReloadCommand []string
}

// ObservabilityConfig 对应 YAML 中的 observability 配置段。
type ObservabilityConfig struct {
	// Logs 描述 workload 日志上报配置。
	Logs ObservabilityLogsConfig
	// Traces 描述 workload trace 上报配置。
	Traces ObservabilityTracesConfig
}

// ObservabilityLogsConfig 描述 workload 日志上报配置。
type ObservabilityLogsConfig struct {
	// LokiURL 是 workload 日志推送到 Loki 的入口。
	LokiURL string
	// LokiTenantID 是 Loki tenant 标识。
	LokiTenantID string
}

// ObservabilityTracesConfig 描述 workload trace 上报配置。
type ObservabilityTracesConfig struct {
	// OTLPEndpoint 是 workload OTLP 上报入口。
	OTLPEndpoint string
}

// ProviderRuntimeConfig 是 provider 创建 runtime node 所需的最小配置视图。
type ProviderRuntimeConfig struct {
	// PlaneIdentity 描述当前 plane 的 ownership 身份。
	PlaneIdentity PlaneIdentity
	// Infrastructure 描述 provider 驱动和 region/zone。
	Infrastructure InfrastructureConfig
	// NodeAgent 描述写入 runtime node 的 node-agent bootstrap 配置。
	NodeAgent NodeAgentConfig
	// RuntimeProvisioning 描述 provider 专属 runtime node 创建参数。
	RuntimeProvisioning RuntimeProvisioningConfig
	// Observability 描述写入 runtime node 的 workload 观测配置。
	Observability ObservabilityConfig
}

// ToProviderRuntimeConfig 返回 provider 创建 runtime node 所需的最小配置视图。
func (c Config) ToProviderRuntimeConfig() ProviderRuntimeConfig {
	return ProviderRuntimeConfig{
		PlaneIdentity:       c.Plane.Identity,
		Infrastructure:      c.Infrastructure,
		NodeAgent:           c.NodeAgent,
		RuntimeProvisioning: c.RuntimeProvisioning,
		Observability:       c.Observability,
	}
}

// ParseProviderSpec 解析 runtimeProvisioning.providerSpec。
// 参数说明：target 是 provider 专属配置目标对象，本函数只负责严格反序列化。
func (c RuntimeProvisioningConfig) ParseProviderSpec(target any) error {
	// providerSpec 缺失时无法构造云厂商 driver，直接返回配置错误。
	if len(c.ProviderSpec) == 0 {
		return fmt.Errorf("runtimeProvisioning.providerSpec is empty")
	}
	// providerSpec 已在 YAML 读取阶段转成 JSON raw message，这里按目标 provider 结构解码。
	decoder := json.NewDecoder(bytes.NewReader(c.ProviderSpec))
	// 禁止未知字段，避免配置拼写错误被静默忽略。
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		// 包装 providerSpec 解码错误，保留配置字段路径。
		return fmt.Errorf("parse runtimeProvisioning.providerSpec: %w", err)
	}
	// 本函数只负责结构解码；provider 专属字段合法性由 driver ParseRuntimeConfig 校验。
	return nil
}

// Validate 校验配置文件中的有效值是否满足 cloud-plane 运行约束。
func (c Config) Validate() error {
	// cloud-plane 要求显式配置内部 gRPC 监听地址，避免启动时使用不明确的默认监听行为。
	if strings.TrimSpace(c.Server.ListenGRPCAddr) == "" {
		return fmt.Errorf("server.listenGRPCAddr is required")
	}
	// 状态库是 cloud-plane 的本地持久化边界，配置层先要求连接串非空。
	if strings.TrimSpace(c.Database.URL) == "" {
		return fmt.Errorf("database.url is required")
	}
	// plane name 是 ownership、日志和 runtime node 标识的基础身份，必须显式配置。
	if strings.TrimSpace(c.Plane.Identity.Name) == "" {
		return fmt.Errorf("plane.identity.name is required")
	}
	// control-plane 调用内部接口必须带 token；这里要求配置存在，不在摘要中暴露明文。
	if strings.TrimSpace(c.ControlPlane.Auth.BearerToken) == "" {
		return fmt.Errorf("controlPlane.auth.bearerToken is required")
	}
	// node-agent 首次注册依赖 bootstrap token，缺失时无法建立初始信任。
	if strings.TrimSpace(c.NodeAgent.Auth.BootstrapToken) == "" {
		return fmt.Errorf("nodeAgent.auth.bootstrapToken is required")
	}
	// session TTL 必须为正数，避免生成立即过期或语义反转的会话 token。
	if c.NodeAgent.Auth.SessionTTL <= 0 {
		return fmt.Errorf("nodeAgent.auth.sessionTTL must be greater than 0")
	}
	// connectEndpoint 会写入 node-agent 配置，空值会导致 agent 无法回连 cloud-plane。
	if strings.TrimSpace(c.NodeAgent.ConnectEndpoint) == "" {
		return fmt.Errorf("nodeAgent.connectEndpoint is required")
	}
	// 对 endpoint 做基础格式校验；不在这里验证 DNS、端口或 gRPC 连通性。
	if _, err := validateConnectEndpoint(c.NodeAgent.ConnectEndpoint); err != nil {
		return err
	}
	// 动态创建 runtime node 时需要非空名称前缀；默认值可由 normalize 根据 plane name 派生。
	if strings.TrimSpace(c.NodeAgent.Defaults.NodeNamePrefix) == "" {
		return fmt.Errorf("nodeAgent.defaults.nodeNamePrefix is required")
	}
	// 心跳间隔必须为正数；具体是否过大或过小由运维配置策略决定。
	if c.NodeAgent.Defaults.HeartbeatIntervalSeconds <= 0 {
		return fmt.Errorf("nodeAgent.defaults.heartbeatIntervalSeconds must be greater than 0")
	}
	// 拉取任务轮询间隔也必须为正数，避免 busy loop 或永不轮询。
	if c.NodeAgent.Defaults.WorkIntervalSeconds <= 0 {
		return fmt.Errorf("nodeAgent.defaults.workIntervalSeconds must be greater than 0")
	}
	// hostPort 端口池必须显式有效，因为 Terraform 安全组会按同一范围放行外置 Caddy 到 runtime node 的访问。
	if c.NodeAgent.Defaults.HostPortRange.Min <= 0 || c.NodeAgent.Defaults.HostPortRange.Max <= 0 {
		return fmt.Errorf("nodeAgent.defaults.hostPortRange min and max must be greater than 0")
	}
	if c.NodeAgent.Defaults.HostPortRange.Min > c.NodeAgent.Defaults.HostPortRange.Max {
		return fmt.Errorf("nodeAgent.defaults.hostPortRange.min must be less than or equal to nodeAgent.defaults.hostPortRange.max")
	}
	if c.NodeAgent.Defaults.HostPortRange.Max > 65535 {
		return fmt.Errorf("nodeAgent.defaults.hostPortRange.max must be less than or equal to 65535")
	}
	// provider 名称大小写不作为语义差异，统一 trim/lower 后做白名单判断。
	provider := strings.ToLower(strings.TrimSpace(c.Infrastructure.Provider))
	// provider 为空时无法选择内置 RuntimeDriver。
	if provider == "" {
		return fmt.Errorf("infrastructure.provider is required")
	}
	// 当前架构只保留多云 provider：aliyun 和 tencent，不再支持 local/manual runtime。
	if provider != "aliyun" && provider != "tencent" {
		return fmt.Errorf("infrastructure.provider must be aliyun or tencent")
	}
	// region 是云厂商创建 runtime node 的必要位置参数。
	if strings.TrimSpace(c.Infrastructure.Location.RegionID) == "" {
		return fmt.Errorf("infrastructure.location.regionId is required")
	}
	// providerSpec 由具体 provider driver 解析；config 层只要求用户提供非空配置。
	if len(c.RuntimeProvisioning.ProviderSpec) == 0 {
		return fmt.Errorf("runtimeProvisioning.providerSpec is required")
	}
	// egress proxy 启用时必须提供可被 runtime node 访问的 http/https URL。
	if c.RuntimeProvisioning.Egress.Proxy.Enabled {
		if strings.TrimSpace(c.RuntimeProvisioning.Egress.Proxy.Endpoint) == "" {
			return fmt.Errorf("runtimeProvisioning.egress.proxy.endpoint is required when proxy is enabled")
		}
		if err := validateProxyEndpoint(c.RuntimeProvisioning.Egress.Proxy.Endpoint); err != nil {
			return err
		}
	}
	// ingress 启用时必须提供 base domain、Caddyfile 路径和 reload 命令。
	if c.Ingress.Enabled {
		if strings.TrimSpace(c.Ingress.BaseDomain) == "" {
			return fmt.Errorf("ingress.baseDomain is required when ingress is enabled")
		}
		if strings.TrimSpace(c.Ingress.Caddy.ListenHTTPAddr) == "" {
			return fmt.Errorf("ingress.caddy.listenHTTPAddr is required when ingress is enabled")
		}
		if _, _, err := net.SplitHostPort(c.Ingress.Caddy.ListenHTTPAddr); err != nil {
			return fmt.Errorf("ingress.caddy.listenHTTPAddr must be host:port: %w", err)
		}
		if strings.TrimSpace(c.Ingress.Caddy.ConfigPath) == "" {
			return fmt.Errorf("ingress.caddy.configPath is required when ingress is enabled")
		}
		if len(c.Ingress.Caddy.ReloadCommand) == 0 {
			return fmt.Errorf("ingress.caddy.reloadCommand is required when ingress is enabled")
		}
	}
	// 动态 runtime node bootstrap 必须知道从哪里下载 node-agent 二进制。
	if strings.TrimSpace(c.NodeAgent.Artifact.BinaryURL) == "" {
		return fmt.Errorf("nodeAgent.artifact.binaryUrl is required")
	}
	// 所有 config 层基础约束通过，外部资源可用性由后续启动和 provider 流程确认。
	return nil
}

// validateProxyEndpoint 校验 egress proxy endpoint 使用 http/https URL 且包含 host。
func validateProxyEndpoint(value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("parse runtimeProvisioning.egress.proxy.endpoint: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("runtimeProvisioning.egress.proxy.endpoint must use http or https")
	}
	if parsed.Host == "" {
		return fmt.Errorf("runtimeProvisioning.egress.proxy.endpoint must include host")
	}
	return nil
}

// validateConnectEndpoint 校验 nodeAgent.connectEndpoint 非空，并允许 host:port 或带 http/https scheme 的 gRPC endpoint。
func validateConnectEndpoint(value string) (string, error) {
	// 先清理外围空白，保证后续解析和返回值使用同一份规范化字符串。
	trimmed := strings.TrimSpace(value)
	// 空 endpoint 与 Validate 中的必填错误保持一致，方便单独测试该 helper。
	if trimmed == "" {
		return "", fmt.Errorf("nodeAgent.connectEndpoint is required")
	}
	// 优先接受 host:port 形式，这是 gRPC dial 最常见的内部地址表达。
	if host, port, err := net.SplitHostPort(trimmed); err == nil && strings.TrimSpace(host) != "" && strings.TrimSpace(port) != "" {
		return trimmed, nil
	}
	// 如果不是 host:port，再尝试按 URI 解析，支持显式 http/https scheme。
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse nodeAgent.connectEndpoint: %w", err)
	}
	// 只允许 http/https URI，避免把任意 scheme 静默下发给 node-agent。
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("nodeAgent.connectEndpoint must be host:port or use http/https gRPC URI")
	}
	// URI 形式至少必须包含 URL Host 组件；这里不进一步校验 hostname/port 是否可用。
	if parsed.Host == "" {
		return "", fmt.Errorf("nodeAgent.connectEndpoint must include host")
	}
	// 返回 trim 后的原始 endpoint，保留用户选择的 host:port 或 URI 表达。
	return trimmed, nil
}
