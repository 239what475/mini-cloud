package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// cloudPlaneFileConfig 对应 cloud-plane YAML 文件的顶层结构。
type cloudPlaneFileConfig struct {
	// Server 配置 cloud-plane 自身的内部 gRPC 服务监听地址。
	Server cloudPlaneServerConfig `yaml:"server"`
	// Database 配置 cloud-plane 状态库连接。
	Database cloudPlaneDatabaseConfig `yaml:"database"`
	// Plane 描述当前 cloud-plane 实例的身份和归属。
	Plane cloudPlanePlaneConfig `yaml:"plane"`
	// ControlPlane 配置 control-plane 调用 cloud-plane southbound gRPC 服务的认证信息。
	ControlPlane cloudPlaneControlPlaneConfig `yaml:"controlPlane"`
	// NodeAgent 配置 node-agent 接入、bootstrap 和默认运行参数。
	NodeAgent cloudPlaneNodeAgentConfig `yaml:"nodeAgent"`
	// Infrastructure 配置当前 plane 绑定的基础设施 provider 与位置。
	Infrastructure cloudPlaneInfrastructureConfig `yaml:"infrastructure"`
	// RuntimeProvisioning 配置 provider 专属 runtime node 创建参数。
	RuntimeProvisioning cloudPlaneRuntimeProvisioningConfig `yaml:"runtimeProvisioning"`
	// Ingress 配置外置 ingress 数据面。
	Ingress cloudPlaneIngressConfig `yaml:"ingress"`
	// Observability 配置下发给 workload/node-agent 的观测入口。
	Observability cloudPlaneObservabilityConfig `yaml:"observability"`
}

// cloudPlaneServerConfig 对应 YAML 中的 server 配置段。
type cloudPlaneServerConfig struct {
	// ListenGRPCAddr 是 cloud-plane 内部 gRPC 服务监听地址。
	ListenGRPCAddr string `yaml:"listenGRPCAddr"`
}

// cloudPlaneDatabaseConfig 对应 YAML 中的 database 配置段。
type cloudPlaneDatabaseConfig struct {
	// URL 是 Postgres 数据库连接串。
	URL string `yaml:"url"`
}

// cloudPlanePlaneConfig 对应 YAML 中的 plane 配置段。
type cloudPlanePlaneConfig struct {
	// Identity 描述当前 cloud-plane 实例的稳定身份和归属信息。
	Identity PlaneIdentity `yaml:"identity"`
}

// cloudPlaneControlPlaneConfig 对应 YAML 中的 controlPlane 配置段。
type cloudPlaneControlPlaneConfig struct {
	// Auth 描述 control-plane 调用 cloud-plane southbound gRPC 服务的认证配置。
	Auth cloudPlaneControlPlaneAuthConfig `yaml:"auth"`
}

// cloudPlaneControlPlaneAuthConfig 对应 YAML 中的 controlPlane.auth 配置段。
type cloudPlaneControlPlaneAuthConfig struct {
	// BearerToken 是 control-plane 调用 cloud-plane southbound gRPC 服务 时必须携带的内部令牌。
	BearerToken string `yaml:"bearerToken"`
}

// cloudPlaneNodeAgentConfig 对应 YAML 中的 nodeAgent 配置段。
type cloudPlaneNodeAgentConfig struct {
	// ConnectEndpoint 是下发给 node-agent 的 cloud-plane gRPC 连接地址。
	ConnectEndpoint string `yaml:"connectEndpoint"`
	// Auth 描述 node-agent bootstrap 和 session 认证配置。
	Auth cloudPlaneNodeAgentAuthConfig `yaml:"auth"`
	// Artifact 描述 runtime node bootstrap 下载 node-agent 二进制所需的地址和校验信息。
	Artifact cloudPlaneNodeAgentArtifactConfig `yaml:"artifact"`
	// Defaults 描述动态创建 runtime node 时写入 node-agent 配置的默认参数。
	Defaults cloudPlaneNodeAgentDefaultsConfig `yaml:"defaults"`
}

// cloudPlaneNodeAgentAuthConfig 对应 YAML 中的 nodeAgent.auth 配置段。
type cloudPlaneNodeAgentAuthConfig struct {
	// BootstrapToken 是 node-agent 首次注册使用的 bootstrap token。
	BootstrapToken string `yaml:"bootstrapToken"`
	// SessionTTL 是 node-agent 会话 token 的有效期，例如 24h。
	SessionTTL string `yaml:"sessionTTL"`
}

// cloudPlaneNodeAgentArtifactConfig 对应 YAML 中的 nodeAgent.artifact 配置段。
type cloudPlaneNodeAgentArtifactConfig struct {
	// BinaryURL 是 bootstrap runtime node 时下载 node-agent 的地址。
	BinaryURL string `yaml:"binaryUrl"`
	// BinarySHA256 是 node-agent 二进制的可选 SHA256 校验值。
	BinarySHA256 string `yaml:"binarySha256"`
}

// cloudPlaneNodeAgentDefaultsConfig 对应 YAML 中的 nodeAgent.defaults 配置段。
type cloudPlaneNodeAgentDefaultsConfig struct {
	// NodeNamePrefix 是 runtime node 云资源名称前缀。
	NodeNamePrefix string `yaml:"nodeNamePrefix"`
	// HeartbeatIntervalSeconds 是 node-agent 心跳上报周期秒数。
	HeartbeatIntervalSeconds int `yaml:"heartbeatIntervalSeconds"`
	// WorkIntervalSeconds 是 node-agent 拉取执行任务的轮询周期秒数。
	WorkIntervalSeconds int `yaml:"workIntervalSeconds"`
	// HostPortRange 是写入动态 runtime node 的 node-agent hostPort 端口池。
	HostPortRange HostPortRangeConfig `yaml:"hostPortRange"`
}

// cloudPlaneInfrastructureConfig 对应 YAML 中的 infrastructure 配置段。
type cloudPlaneInfrastructureConfig struct {
	// Provider 是基础设施 provider 驱动名称。
	Provider string `yaml:"provider"`
	// Location 描述当前 plane 绑定的 provider region/zone。
	Location cloudPlaneInfrastructureLocationConfig `yaml:"location"`
}

// cloudPlaneInfrastructureLocationConfig 对应 YAML 中的 infrastructure.location 配置段。
type cloudPlaneInfrastructureLocationConfig struct {
	// RegionID 表示 Region 的唯一标识。
	RegionID string `yaml:"regionId"`
	// ZoneID 表示 Zone 的唯一标识。
	ZoneID string `yaml:"zoneId"`
}

// cloudPlaneRuntimeProvisioningConfig 对应 YAML 中的 runtimeProvisioning 配置段。
type cloudPlaneRuntimeProvisioningConfig struct {
	// ImagePull 描述 runtime node 拉取镜像时的通用配置。
	ImagePull cloudPlaneImagePullConfig `yaml:"imagePull"`
	// Egress 描述 runtime node 出公网代理配置。
	Egress cloudPlaneEgressConfig `yaml:"egress"`
	// ProviderSpec 保存 provider 专属配置原始 YAML 节点，由 build 转成 JSON RawMessage。
	ProviderSpec yaml.Node `yaml:"providerSpec"`
}

// cloudPlaneImagePullConfig 对应 YAML 中的 runtimeProvisioning.imagePull 配置段。
type cloudPlaneImagePullConfig struct {
	// RegistryMirrors 是写入 Docker daemon 的 registry mirror 列表。
	RegistryMirrors []string `yaml:"registryMirrors"`
}

// cloudPlaneEgressConfig 对应 YAML 中的 runtimeProvisioning.egress 配置段。
type cloudPlaneEgressConfig struct {
	// Proxy 描述 runtime node 使用的 HTTP/HTTPS 出公网代理。
	Proxy cloudPlaneEgressProxyConfig `yaml:"proxy"`
}

// cloudPlaneEgressProxyConfig 对应 YAML 中的 runtimeProvisioning.egress.proxy 配置段。
type cloudPlaneEgressProxyConfig struct {
	// Enabled 表示是否为 runtime node bootstrap、Docker daemon 和 workload 注入代理配置。
	Enabled bool `yaml:"enabled"`
	// Endpoint 是 runtime node 可通过私网访问的 forward proxy URL。
	Endpoint string `yaml:"endpoint"`
	// NoProxy 是不走 forward proxy 的主机、域名后缀或网段列表。
	NoProxy []string `yaml:"noProxy"`
}

// cloudPlaneIngressConfig 对应 YAML 中的 ingress 配置段。
type cloudPlaneIngressConfig struct {
	// Enabled 表示是否由 cloud-plane 后台 reconciler 管理外置 Caddy 配置。
	Enabled bool `yaml:"enabled"`
	// BaseDomain 是 public service 默认入口域名后缀。
	BaseDomain string `yaml:"baseDomain"`
	// Caddy 描述外置 Caddy 的配置文件和 reload 命令。
	Caddy cloudPlaneCaddyIngressConfig `yaml:"caddy"`
}

// cloudPlaneCaddyIngressConfig 对应 YAML 中的 ingress.caddy 配置段。
type cloudPlaneCaddyIngressConfig struct {
	// ListenHTTPAddr 是 Caddy HTTP 入口监听地址。
	ListenHTTPAddr string `yaml:"listenHTTPAddr"`
	// ConfigPath 是 cloud-plane 写入的 Caddyfile 路径。
	ConfigPath string `yaml:"configPath"`
	// ReloadCommand 是 Caddyfile 写入后执行的 reload 命令及参数。
	ReloadCommand []string `yaml:"reloadCommand"`
}

// cloudPlaneObservabilityConfig 对应 YAML 中的 observability 配置段。
type cloudPlaneObservabilityConfig struct {
	// Logs 描述 workload 日志上报配置。
	Logs cloudPlaneObservabilityLogsConfig `yaml:"logs"`
	// Traces 描述 workload trace 上报配置。
	Traces cloudPlaneObservabilityTracesConfig `yaml:"traces"`
}

// cloudPlaneObservabilityLogsConfig 对应 YAML 中的 observability.logs 配置段。
type cloudPlaneObservabilityLogsConfig struct {
	// LokiURL 是 workload 日志推送到 Loki 的入口。
	LokiURL string `yaml:"lokiURL"`
	// LokiTenantID 是 Loki tenant 标识。
	LokiTenantID string `yaml:"lokiTenantID"`
}

// cloudPlaneObservabilityTracesConfig 对应 YAML 中的 observability.traces 配置段。
type cloudPlaneObservabilityTracesConfig struct {
	// OTLPEndpoint 是 workload OTLP 上报入口。
	OTLPEndpoint string `yaml:"otlpEndpoint"`
}

// defaultCloudPlaneFileConfig 返回严格解码用户 YAML 前使用的默认配置。
func defaultCloudPlaneFileConfig() cloudPlaneFileConfig {
	// 先构造完整的顶层配置对象，让 YAML 解码只覆盖用户显式填写的字段。
	return cloudPlaneFileConfig{
		// 当前只有 node-agent 相关字段需要在文件缺省时提供安全默认值。
		NodeAgent: cloudPlaneNodeAgentConfig{
			// session token 必须有有效期；默认 24h 让用户未配置时仍有明确 TTL。
			Auth: cloudPlaneNodeAgentAuthConfig{
				SessionTTL: "24h",
			},
			// 默认轮询参数只影响后续下发给动态 runtime node 的 node-agent 配置。
			Defaults: cloudPlaneNodeAgentDefaultsConfig{
				HeartbeatIntervalSeconds: 15,
				WorkIntervalSeconds:      5,
				HostPortRange: HostPortRangeConfig{
					Min: 30000,
					Max: 60999,
				},
			},
		},
	}
}

// Load 读取、严格解码、校验并归一化 cloud-plane YAML 配置文件。
// 参数说明：path 是本地 cloud-plane YAML 配置文件路径。
func Load(path string) (Config, error) {
	// 配置路径来自 CLI 参数，先去掉首尾空白，避免把空白字符串当成真实路径。
	path = strings.TrimSpace(path)
	// cloud-plane 不再从环境变量或固定路径隐式读取配置，因此空路径必须立即报错。
	if path == "" {
		return Config{}, fmt.Errorf("cloud-plane config path is empty")
	}

	// 一次性读取 YAML 文件；读取失败时保留路径，方便定位启动配置问题。
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read cloud-plane config %q: %w", path, err)
	}

	// 先填入文件级默认值，再让 YAML 覆盖，确保缺省字段仍能进入后续统一构建流程。
	fileCfg := defaultCloudPlaneFileConfig()
	// 使用 yaml.Decoder 而不是 yaml.Unmarshal，是为了启用 KnownFields 严格字段检查。
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	// 禁止已声明配置段中的未知字段，避免拼错配置键后被静默忽略；providerSpec 子树由 provider driver 后续校验。
	decoder.KnownFields(true)
	// 将用户 YAML 解码到带默认值的中间结构；providerSpec 保留为 yaml.Node，sessionTTL 保留为字符串。
	if err := decoder.Decode(&fileCfg); err != nil {
		return Config{}, fmt.Errorf("parse cloud-plane config %q: %w", path, err)
	}

	// 将文件结构转换成运行时 Config，并在 build 内完成解析、归一化和校验。
	cfg, err := build(path, fileCfg)
	if err != nil {
		return Config{}, err
	}
	// 返回已经校验通过的配置，调用方不需要再次补默认值或清理已知字段的外围空白。
	return cfg, nil
}

// build 将已解码的 YAML 配置转换为 cloud-plane 可直接使用的有效配置。
func build(path string, fileCfg cloudPlaneFileConfig) (Config, error) {
	// sessionTTL 在 YAML 中是字符串，先解析成运行期直接使用的 time.Duration。
	sessionTTL, err := parsePositiveDuration("nodeAgent.auth.sessionTTL", fileCfg.NodeAgent.Auth.SessionTTL)
	if err != nil {
		return Config{}, err
	}

	// providerSpec 是 provider 专属结构，config 层只负责保持为 JSON RawMessage。
	providerSpec, err := rawJSONFromYAMLNode(fileCfg.RuntimeProvisioning.ProviderSpec)
	if err != nil {
		return Config{}, fmt.Errorf("parse runtimeProvisioning.providerSpec: %w", err)
	}

	// 构造对外的 Config；这一层会把 YAML 字段名转换为业务语义更明确的结构。
	cfg := Config{
		// 记录配置文件路径，便于日志、诊断和后续组件知道本次启动来源。
		Path: strings.TrimSpace(path),
		Server: ServerConfig{
			// gRPC 监听地址允许用户在 YAML 中写入空白，进入 Config 前统一清理。
			ListenGRPCAddr: strings.TrimSpace(fileCfg.Server.ListenGRPCAddr),
		},
		Database: DatabaseConfig{
			// 数据库 URL 只做空白清理；Validate 检查必填，可连接性由数据库打开阶段确认。
			URL: strings.TrimSpace(fileCfg.Database.URL),
		},
		Plane: PlaneConfig{
			// identity 是结构体值，具体字符串字段会在 normalize 中统一清理。
			Identity: fileCfg.Plane.Identity,
		},
		ControlPlane: ControlPlaneConfig{
			Auth: ControlPlaneAuthConfig{
				// control-plane bearer token 是本地认证材料，构建时只清理外围空白。
				BearerToken: strings.TrimSpace(fileCfg.ControlPlane.Auth.BearerToken),
			},
		},
		NodeAgent: NodeAgentConfig{
			// connectEndpoint 会被下发给 node-agent，这里先清理外围空白，格式由 Validate 校验。
			ConnectEndpoint: strings.TrimSpace(fileCfg.NodeAgent.ConnectEndpoint),
			Auth: NodeAgentAuthConfig{
				// bootstrap token 用于首次注册，不能在这里生成或派生，只接受显式配置。
				BootstrapToken: strings.TrimSpace(fileCfg.NodeAgent.Auth.BootstrapToken),
				// sessionTTL 已在前面解析为正 duration，后续无需再次解析字符串。
				SessionTTL: sessionTTL,
			},
			Artifact: NodeAgentArtifactConfig{
				// bootstrap 脚本使用 BinaryURL 下载 node-agent 二进制。
				BinaryURL: strings.TrimSpace(fileCfg.NodeAgent.Artifact.BinaryURL),
				// SHA256 是可选校验值；为空表示后续下载阶段不要求做该项校验。
				BinarySHA256: strings.TrimSpace(fileCfg.NodeAgent.Artifact.BinarySHA256),
			},
			Defaults: NodeAgentDefaultsConfig{
				// node name 前缀可以缺省，normalize 会根据 plane identity 派生。
				NodeNamePrefix: strings.TrimSpace(fileCfg.NodeAgent.Defaults.NodeNamePrefix),
				// interval 字段保持整数秒，范围检查交给 Config.Validate。
				HeartbeatIntervalSeconds: fileCfg.NodeAgent.Defaults.HeartbeatIntervalSeconds,
				WorkIntervalSeconds:      fileCfg.NodeAgent.Defaults.WorkIntervalSeconds,
				HostPortRange:            fileCfg.NodeAgent.Defaults.HostPortRange,
			},
		},
		Infrastructure: InfrastructureConfig{
			// provider 名称决定后续选择哪个内置 RuntimeDriver。
			Provider: strings.TrimSpace(fileCfg.Infrastructure.Provider),
			Location: InfrastructureLocation{
				// region/zone 是 provider 侧位置标识，config 层不做 provider 专属合法性判断。
				RegionID: strings.TrimSpace(fileCfg.Infrastructure.Location.RegionID),
				ZoneID:   strings.TrimSpace(fileCfg.Infrastructure.Location.ZoneID),
			},
		},
		RuntimeProvisioning: RuntimeProvisioningConfig{
			// 保存已经 JSON 化的 providerSpec，具体 schema 由对应 provider driver 解析。
			ProviderSpec: providerSpec,
			ImagePull: ImagePullConfig{
				RegistryMirrors: trimStringList(fileCfg.RuntimeProvisioning.ImagePull.RegistryMirrors),
			},
			Egress: EgressConfig{
				Proxy: EgressProxyConfig{
					Enabled:  fileCfg.RuntimeProvisioning.Egress.Proxy.Enabled,
					Endpoint: strings.TrimSpace(fileCfg.RuntimeProvisioning.Egress.Proxy.Endpoint),
					NoProxy:  trimStringList(fileCfg.RuntimeProvisioning.Egress.Proxy.NoProxy),
				},
			},
		},
		Ingress: IngressConfig{
			Enabled:    fileCfg.Ingress.Enabled,
			BaseDomain: strings.TrimSpace(fileCfg.Ingress.BaseDomain),
			Caddy: CaddyIngressConfig{
				ListenHTTPAddr: strings.TrimSpace(fileCfg.Ingress.Caddy.ListenHTTPAddr),
				ConfigPath:     strings.TrimSpace(fileCfg.Ingress.Caddy.ConfigPath),
				ReloadCommand:  trimStringList(fileCfg.Ingress.Caddy.ReloadCommand),
			},
		},
		Observability: ObservabilityConfig{
			Logs: ObservabilityLogsConfig{
				// Loki 配置会透传给 node-agent/workload 侧使用，这里只清理外围空白。
				LokiURL:      strings.TrimSpace(fileCfg.Observability.Logs.LokiURL),
				LokiTenantID: strings.TrimSpace(fileCfg.Observability.Logs.LokiTenantID),
			},
			Traces: ObservabilityTracesConfig{
				// OTLP endpoint 同样作为下发配置保存，不在 config 层建立网络连接验证。
				OTLPEndpoint: strings.TrimSpace(fileCfg.Observability.Traces.OTLPEndpoint),
			},
		},
	}
	// 统一处理嵌套字段的二次空白清理和派生默认值。
	cfg.normalize()
	// 最后执行配置层面的必填项和基础约束校验；外部资源可用性由启动流程继续确认。
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	// 成功返回前已完成解析、归一化和配置层校验。
	return cfg, nil
}

// normalize 清理字符串空白并补齐派生默认值。
func (c *Config) normalize() {
	// plane identity 来自 YAML 嵌套结构，统一在这里清理，避免 build 中遗漏子字段。
	c.Plane.Identity.Name = strings.TrimSpace(c.Plane.Identity.Name)
	c.Plane.Identity.Environment = strings.TrimSpace(c.Plane.Identity.Environment)
	c.Plane.Identity.Owner = strings.TrimSpace(c.Plane.Identity.Owner)
	// provider 和位置信息会用于 driver 选择和 runtime node 创建，先去掉外围空白。
	c.Infrastructure.Provider = strings.TrimSpace(c.Infrastructure.Provider)
	c.Infrastructure.Location.RegionID = strings.TrimSpace(c.Infrastructure.Location.RegionID)
	c.Infrastructure.Location.ZoneID = strings.TrimSpace(c.Infrastructure.Location.ZoneID)
	// node-agent 接入地址、认证材料和 artifact 信息都必须保持空白清理后一致。
	c.NodeAgent.ConnectEndpoint = strings.TrimSpace(c.NodeAgent.ConnectEndpoint)
	c.NodeAgent.Auth.BootstrapToken = strings.TrimSpace(c.NodeAgent.Auth.BootstrapToken)
	c.NodeAgent.Artifact.BinaryURL = strings.TrimSpace(c.NodeAgent.Artifact.BinaryURL)
	c.NodeAgent.Artifact.BinarySHA256 = strings.TrimSpace(c.NodeAgent.Artifact.BinarySHA256)
	// node name 前缀允许用户显式配置，也允许根据 plane identity 自动派生。
	c.NodeAgent.Defaults.NodeNamePrefix = strings.TrimSpace(c.NodeAgent.Defaults.NodeNamePrefix)
	// 只有用户未配置前缀且 plane name 可用时才派生，避免生成以空字符串开头的模糊名称。
	if c.NodeAgent.Defaults.NodeNamePrefix == "" && c.Plane.Identity.Name != "" {
		c.NodeAgent.Defaults.NodeNamePrefix = c.Plane.Identity.Name + "-runtime-node"
	}
	// 观测端点是可选下发配置，normalize 只清理空白，不把空值改成默认地址。
	c.Observability.Logs.LokiURL = strings.TrimSpace(c.Observability.Logs.LokiURL)
	c.Observability.Logs.LokiTenantID = strings.TrimSpace(c.Observability.Logs.LokiTenantID)
	c.Observability.Traces.OTLPEndpoint = strings.TrimSpace(c.Observability.Traces.OTLPEndpoint)
	c.RuntimeProvisioning.ImagePull.RegistryMirrors = trimStringList(c.RuntimeProvisioning.ImagePull.RegistryMirrors)
	c.RuntimeProvisioning.Egress.Proxy.Endpoint = strings.TrimSpace(c.RuntimeProvisioning.Egress.Proxy.Endpoint)
	c.RuntimeProvisioning.Egress.Proxy.NoProxy = mergeNoProxyDefaults(c.RuntimeProvisioning.Egress.Proxy.Enabled, c.RuntimeProvisioning.Egress.Proxy.NoProxy)
	c.Ingress.BaseDomain = strings.Trim(strings.TrimSpace(c.Ingress.BaseDomain), ".")
	c.Ingress.Caddy.ListenHTTPAddr = strings.TrimSpace(c.Ingress.Caddy.ListenHTTPAddr)
	if c.Ingress.Enabled && c.Ingress.Caddy.ListenHTTPAddr == "" {
		c.Ingress.Caddy.ListenHTTPAddr = "0.0.0.0:80"
	}
	c.Ingress.Caddy.ConfigPath = strings.TrimSpace(c.Ingress.Caddy.ConfigPath)
	c.Ingress.Caddy.ReloadCommand = trimStringList(c.Ingress.Caddy.ReloadCommand)
}

// trimStringList 清理字符串列表中的空白项，并保持原有顺序。
func trimStringList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

// mergeNoProxyDefaults 在启用 egress proxy 时补入 runtime node 必须直连的本地、私网和 metadata 地址。
func mergeNoProxyDefaults(enabled bool, values []string) []string {
	values = trimStringList(values)
	if !enabled {
		return values
	}
	defaults := []string{
		"127.0.0.1",
		"localhost",
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"169.254.169.254",
		"100.100.100.200",
		"metadata.tencentyun.com",
	}
	seen := make(map[string]struct{}, len(defaults)+len(values))
	out := make([]string, 0, len(defaults)+len(values))
	for _, value := range append(defaults, values...) {
		key := strings.ToLower(strings.TrimSpace(value))
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, strings.TrimSpace(value))
	}
	return out
}

// parsePositiveDuration 解析必须大于 0 的 duration 字符串。
func parsePositiveDuration(field string, value string) (time.Duration, error) {
	// duration 字段允许 YAML 中存在外围空白，但不允许实际内容为空。
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, fmt.Errorf("%s is required", field)
	}
	// 使用 Go 标准 duration 语法，例如 30s、5m、24h。
	parsed, err := time.ParseDuration(trimmed)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid duration: %w", field, err)
	}
	// 0 或负数会让 session 立即过期或产生反直觉行为，因此在配置阶段拒绝。
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be greater than 0", field)
	}
	// 返回解析后的 duration，调用方不再保留原始字符串语义。
	return parsed, nil
}

// rawJSONFromYAMLNode 将 providerSpec 的 YAML 子树转换成 provider 可继续解析的 JSON RawMessage。
func rawJSONFromYAMLNode(node yaml.Node) (json.RawMessage, error) {
	// 未配置 providerSpec 时 yaml.Node.Kind 为 0，表示没有 provider 专属配置。
	if node.Kind == 0 {
		return nil, nil
	}
	// 显式写 providerSpec: null 时与未配置保持同一语义，避免传递 JSON 字面量 null 给 driver。
	if node.Kind == yaml.ScalarNode && strings.EqualFold(strings.TrimSpace(node.Value), "null") {
		return nil, nil
	}

	// 先让 yaml.v3 将任意 YAML 子树解码成 Go 值，保留列表、map、标量等结构。
	var value any
	if err := node.Decode(&value); err != nil {
		return nil, err
	}
	// YAML 允许非字符串 map key，但 JSON 不允许；递归转换后才能安全 json.Marshal。
	normalized := normalizeYAMLValue(value)
	// 用 encoding/json 生成可交给 provider driver 继续解析的 RawMessage。
	data, err := json.Marshal(normalized)
	if err != nil {
		return nil, err
	}
	// RawMessage 持有 JSON 字节，后续 provider 负责按自己的 schema 反序列化。
	return json.RawMessage(data), nil
}

// normalizeYAMLValue 递归转换 YAML 解码值，确保后续可以被 encoding/json 编码。
func normalizeYAMLValue(value any) any {
	// 根据 yaml 解码得到的实际 Go 类型递归处理容器节点。
	switch typed := value.(type) {
	case map[string]any:
		// 字符串 key map 已经符合 JSON 要求，但子值仍可能包含 YAML 专属类型。
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = normalizeYAMLValue(item)
		}
		return out
	case map[any]any:
		// 非字符串 key map 不能直接 JSON 编码，统一用 fmt.Sprint 转成字符串 key。
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[fmt.Sprint(key)] = normalizeYAMLValue(item)
		}
		return out
	case []any:
		// 数组本身可以 JSON 编码，但每个元素仍需要递归规范化。
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, normalizeYAMLValue(item))
		}
		return out
	default:
		// 标量值直接返回，交给 encoding/json 处理具体编码。
		return value
	}
}
