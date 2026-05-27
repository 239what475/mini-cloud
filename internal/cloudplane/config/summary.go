package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

const (
	SourceUnset      = "unset"
	SourceConfigFile = "config_file"
)

// Summary 描述 cloud-plane 单配置文件和最终有效配置的可观测摘要。
type Summary struct {
	// ObservedAt 是相关事件时间。
	ObservedAt time.Time `json:"observedAt"`
	// Fingerprint 表示脱敏后的摘要视图指纹，不包含配置文件路径、敏感明文和 providerSpec 明细。
	Fingerprint string `json:"fingerprint"`
	// Config 展示 cloud-plane 配置文件路径及其来源。
	Config ConfigSummary `json:"config"`
	// Plane 展示当前 plane 身份的有效值。
	Plane PlaneSummary `json:"plane"`
	// Infrastructure 展示 provider、地域和可用区的有效值。
	Infrastructure InfrastructureSummary `json:"infrastructure"`
	// Server 展示 cloud-plane 监听配置。
	Server ServerSummary `json:"server"`
	// NodeAgent 展示 node-agent 接入、artifact 和 bootstrap 凭据配置情况。
	NodeAgent NodeAgentSummary `json:"nodeAgent"`
	// Observability 展示下发给 node-agent 的 workload 观测配置来源。
	Observability ObservabilitySummary `json:"observability"`
	// RuntimeProvisioning 展示 provider runtime node 创建参数来源。
	RuntimeProvisioning RuntimeProvisioningSummary `json:"runtimeProvisioning"`
	// Ingress 展示外置 ingress 数据面配置来源。
	Ingress IngressSummary `json:"ingress"`
}

// ConfigSummary 描述 cloud-plane 配置文件路径及其配置来源。
type ConfigSummary struct {
	// Path 表示 cloud-plane 单一配置文件路径。
	Path string `json:"path"`
	// Source 标识配置文件路径来源；cloud-plane 只接受命令行 --config 指定本地配置文件，不接受环境变量覆盖。
	Source string `json:"source"`
}

// PlaneSummary 描述 plane 身份元信息的有效值和来源。
type PlaneSummary struct {
	// Name 是当前 plane 实例的机器可读稳定标识。
	Name string `json:"name"`
	// Environment 是当前 plane 环境名称。
	Environment string `json:"environment"`
	// Owner 是当前 plane 负责人或归属团队。
	Owner string `json:"owner"`
	// Source 表示该摘要分组的主要来源。
	Source string `json:"source"`
}

// InfrastructureSummary 描述 provider、地域和可用区的有效值和来源。
type InfrastructureSummary struct {
	// Provider 是基础设施 provider 驱动名称。
	Provider string `json:"provider"`
	// RegionID 表示 Region 的唯一标识。
	RegionID string `json:"regionId"`
	// ZoneID 表示 Zone 的唯一标识。
	ZoneID string `json:"zoneId"`
	// Source 表示该摘要分组的主要来源。
	Source string `json:"source"`
}

// ServerSummary 描述 cloud-plane 自身监听配置。
type ServerSummary struct {
	// ListenGRPCAddr 是 cloud-plane 内部 gRPC 服务监听地址。
	ListenGRPCAddr string `json:"listenGRPCAddr"`
	// Source 标识监听地址来源。
	Source string `json:"source"`
}

// NodeAgentSummary 描述 node-agent 接入和 bootstrap 配置情况。
type NodeAgentSummary struct {
	// ConnectEndpoint 是下发给 node-agent 的 cloud-plane gRPC 连接地址。
	ConnectEndpoint string `json:"connectEndpoint"`
	// ConnectEndpointSource 标识 connect endpoint 来源。
	ConnectEndpointSource string `json:"connectEndpointSource"`
	// BootstrapTokenConfigured 表示 bootstrap token 是否已配置，摘要中不暴露明文。
	BootstrapTokenConfigured bool `json:"bootstrapTokenConfigured"`
	// BootstrapTokenSource 标识 bootstrap token 来源。
	BootstrapTokenSource string `json:"bootstrapTokenSource"`
	// SessionTTLSeconds 是 node-agent session token 有效期秒数。
	SessionTTLSeconds int64 `json:"sessionTTLSeconds"`
	// HostPortRangeMin 是动态 runtime node 上 node-agent hostPort 端口池最小值。
	HostPortRangeMin int `json:"hostPortRangeMin"`
	// HostPortRangeMax 是动态 runtime node 上 node-agent hostPort 端口池最大值。
	HostPortRangeMax int `json:"hostPortRangeMax"`
	// ArtifactBinaryURL 是 runtime node bootstrap 下载 node-agent 的 URL。
	ArtifactBinaryURL string `json:"artifactBinaryUrl"`
	// ArtifactBinaryURLSource 标识 node-agent 下载 URL 的来源。
	ArtifactBinaryURLSource string `json:"artifactBinaryUrlSource"`
}

// ObservabilitySummary 描述 cloud-plane 下发给 runtime node 的 workload 观测配置。
type ObservabilitySummary struct {
	// LokiURL 表示 workload 日志推送到 Loki 的入口。
	LokiURL string `json:"lokiURL"`
	// LokiURLSource 标识 Loki URL 来源。
	LokiURLSource string `json:"lokiURLSource"`
	// LokiTenantConfigured 表示是否配置了 Loki tenant。
	LokiTenantConfigured bool `json:"lokiTenantConfigured"`
	// LokiTenantSource 标识 Loki tenant 配置来源。
	LokiTenantSource string `json:"lokiTenantSource"`
	// OTLPEndpoint 表示 workload OTLP 上报入口。
	OTLPEndpoint string `json:"otlpEndpoint"`
	// OTLPEndpointSource 标识 workload OTLP 上报入口来源。
	OTLPEndpointSource string `json:"otlpEndpointSource"`
}

// RuntimeProvisioningSummary 描述 runtime node 动态创建相关配置。
type RuntimeProvisioningSummary struct {
	// RegistryMirrorsCount 表示配置的 registry mirror 数量。
	RegistryMirrorsCount int `json:"registryMirrorsCount"`
	// EgressProxyEnabled 表示是否启用 runtime node egress proxy。
	EgressProxyEnabled bool `json:"egressProxyEnabled"`
	// EgressProxyEndpoint 表示 runtime node 使用的 egress proxy endpoint。
	EgressProxyEndpoint string `json:"egressProxyEndpoint"`
	// ProviderSpecSource 标识 provider spec 的来源。
	ProviderSpecSource string `json:"providerSpecSource"`
}

// IngressSummary 描述外置 ingress 数据面配置摘要。
type IngressSummary struct {
	// Enabled 表示是否启用外置 ingress 配置管理。
	Enabled bool `json:"enabled"`
	// BaseDomain 是 public service 默认入口域名后缀。
	BaseDomain string `json:"baseDomain"`
	// CaddyConfigPath 是 cloud-plane 写入的 Caddyfile 路径。
	CaddyConfigPath string `json:"caddyConfigPath"`
	// CaddyConfigPathSource 标识 Caddyfile 路径来源。
	CaddyConfigPathSource string `json:"caddyConfigPathSource"`
}

// BuildSummary 根据有效配置构建 cloud-plane 可观测的脱敏摘要视图。
// 参数说明：cfg 是从单一配置文件读取的有效配置；observedAt 是观测到该状态的时间。
func BuildSummary(cfg Config, observedAt time.Time) Summary {
	// 先组装除 fingerprint 之外的摘要字段，fingerprint 需要基于这些有效值计算。
	summary := Summary{
		// 统一转成 UTC，避免同一事件在不同时区序列化出不同摘要。
		ObservedAt: observedAt.UTC(),
		Config: ConfigSummary{
			// Path 仅用于观测展示，后续 fingerprint 会主动排除本地路径。
			Path:   strings.TrimSpace(cfg.Path),
			Source: sourceForConfigValue(cfg.Path),
		},
		Plane: PlaneSummary{
			// plane identity 字段来自配置文件；这里再次 trim，避免摘要暴露外围空白。
			Name:        strings.TrimSpace(cfg.Plane.Identity.Name),
			Environment: strings.TrimSpace(cfg.Plane.Identity.Environment),
			Owner:       strings.TrimSpace(cfg.Plane.Identity.Owner),
			// identity 字段没有其它来源，统一标记为配置文件来源。
			Source: SourceConfigFile,
		},
		Infrastructure: InfrastructureSummary{
			// provider 和位置决定当前 plane 绑定的云基础设施范围。
			Provider: strings.TrimSpace(cfg.Infrastructure.Provider),
			RegionID: strings.TrimSpace(cfg.Infrastructure.Location.RegionID),
			ZoneID:   strings.TrimSpace(cfg.Infrastructure.Location.ZoneID),
			// infrastructure 当前只能来自单一配置文件。
			Source: SourceConfigFile,
		},
		Server: ServerSummary{
			// gRPC 监听地址只用于展示，不在摘要层重新校验可绑定性。
			ListenGRPCAddr: strings.TrimSpace(cfg.Server.ListenGRPCAddr),
			Source:         SourceConfigFile,
		},
		NodeAgent: NodeAgentSummary{
			// connect endpoint 会下发给 node-agent，摘要展示清理后的最终值。
			ConnectEndpoint:       strings.TrimSpace(cfg.NodeAgent.ConnectEndpoint),
			ConnectEndpointSource: sourceForConfigValue(cfg.NodeAgent.ConnectEndpoint),
			// token 类字段只展示是否已配置，避免把敏感值写入日志或 proto Struct。
			BootstrapTokenConfigured: strings.TrimSpace(cfg.NodeAgent.Auth.BootstrapToken) != "",
			BootstrapTokenSource:     sourceForConfigValue(cfg.NodeAgent.Auth.BootstrapToken),
			// TTL 用秒展示，便于控制面或排障工具以稳定数值消费。
			SessionTTLSeconds: int64(cfg.NodeAgent.Auth.SessionTTL.Seconds()),
			// hostPortRange 需要和平台安全组放行范围保持一致，摘要中直接展示便于排障。
			HostPortRangeMin: cfg.NodeAgent.Defaults.HostPortRange.Min,
			HostPortRangeMax: cfg.NodeAgent.Defaults.HostPortRange.Max,
			// 当前摘要会展示 artifact URL，部署时应避免在 URL 中嵌入敏感凭据。
			ArtifactBinaryURL:       strings.TrimSpace(cfg.NodeAgent.Artifact.BinaryURL),
			ArtifactBinaryURLSource: sourceForConfigValue(cfg.NodeAgent.Artifact.BinaryURL),
		},
		Observability: ObservabilitySummary{
			// 观测端点是可选下发配置，摘要中保留最终值和来源。
			LokiURL:       strings.TrimSpace(cfg.Observability.Logs.LokiURL),
			LokiURLSource: sourceForConfigValue(cfg.Observability.Logs.LokiURL),
			// tenant 可能是内部标识，只暴露是否配置，不暴露明文。
			LokiTenantConfigured: strings.TrimSpace(cfg.Observability.Logs.LokiTenantID) != "",
			LokiTenantSource:     sourceForConfigValue(cfg.Observability.Logs.LokiTenantID),
			// trace endpoint 同样作为可选下发配置展示。
			OTLPEndpoint:       strings.TrimSpace(cfg.Observability.Traces.OTLPEndpoint),
			OTLPEndpointSource: sourceForConfigValue(cfg.Observability.Traces.OTLPEndpoint),
		},
		RuntimeProvisioning: RuntimeProvisioningSummary{
			// providerSpec 本身可能包含云资源参数，只展示是否配置，不展示内容。
			RegistryMirrorsCount: len(cfg.RuntimeProvisioning.ImagePull.RegistryMirrors),
			EgressProxyEnabled:   cfg.RuntimeProvisioning.Egress.Proxy.Enabled,
			EgressProxyEndpoint:  strings.TrimSpace(cfg.RuntimeProvisioning.Egress.Proxy.Endpoint),
			ProviderSpecSource:   sourceForProviderSpec(cfg.RuntimeProvisioning),
		},
		Ingress: IngressSummary{
			Enabled:               cfg.Ingress.Enabled,
			BaseDomain:            strings.TrimSpace(cfg.Ingress.BaseDomain),
			CaddyConfigPath:       strings.TrimSpace(cfg.Ingress.Caddy.ConfigPath),
			CaddyConfigPathSource: sourceForConfigValue(cfg.Ingress.Caddy.ConfigPath),
		},
	}
	// fingerprint 基于有效配置摘要计算，必须在 summary 字段组装完成后再生成。
	summary.Fingerprint = fingerprint(summary)
	// 返回可被 control-plane 快照或日志消费的配置摘要。
	return summary
}

// SummaryMap 把配置摘要转换成 map，便于 proto Struct 输出。
// 参数说明：summary 是有效配置摘要。
func SummaryMap(summary Summary) (map[string]any, error) {
	// 先创建目标 map，后续 json.Unmarshal 会把结构体字段转成 map key/value。
	out := map[string]any{}
	// 复用 JSON tag 作为字段名规则，避免手写 map 时与摘要结构漂移。
	data, err := json.Marshal(summary)
	if err != nil {
		return nil, err
	}
	// 将 JSON 重新反序列化成通用 map，便于转换为 protobuf Struct。
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	// 返回的 map 不包含未导出字段，字段名与 Summary 的 json tag 保持一致；该结果用于展示/proto Struct。
	return out, nil
}

// sourceForConfigValue 标记配置字段来自配置文件还是未配置。
// 参数说明：value 是需要写入、比较或转换的值。
func sourceForConfigValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return SourceUnset
	}
	return SourceConfigFile
}

// sourceForProviderSpec 标记 provider spec 是否来自配置文件。
// 参数说明：cfg 提供 runtime provisioning 配置。
func sourceForProviderSpec(cfg RuntimeProvisioningConfig) string {
	if len(cfg.ProviderSpec) > 0 {
		return SourceConfigFile
	}
	return SourceUnset
}

// fingerprint 为脱敏后的配置摘要生成稳定指纹，不包含本地路径、观测时间、敏感明文和 providerSpec 明细。
// 参数说明：summary 是有效配置摘要。
func fingerprint(summary Summary) string {
	// 明确列出参与指纹的字段，避免把 ObservedAt 或本地配置路径纳入漂移判断。
	payload := struct {
		Plane               PlaneSummary               `json:"plane"`
		Infrastructure      InfrastructureSummary      `json:"infrastructure"`
		Server              ServerSummary              `json:"server"`
		NodeAgent           NodeAgentSummary           `json:"nodeAgent"`
		Observability       ObservabilitySummary       `json:"observability"`
		RuntimeProvisioning RuntimeProvisioningSummary `json:"runtimeProvisioning"`
		Ingress             IngressSummary             `json:"ingress"`
	}{
		// plane identity 变化会影响 ownership 语义，需要进入指纹。
		Plane: summary.Plane,
		// provider/region/zone 变化会影响 runtime node 创建范围，需要进入指纹。
		Infrastructure: summary.Infrastructure,
		// server 地址变化影响内部服务入口，需要进入指纹。
		Server: summary.Server,
		// node-agent 接入、TTL、bootstrap token 配置状态和 artifact URL 来源进入指纹。
		NodeAgent: summary.NodeAgent,
		// 摘要中可见的观测端点和 tenant 配置状态进入指纹。
		Observability: summary.Observability,
		// providerSpec 只以来源状态进入指纹，不把 provider 专属明细写入摘要指纹输入。
		RuntimeProvisioning: summary.RuntimeProvisioning,
		// ingress 配置影响 public service 数据面发布路径，需要进入指纹。
		Ingress: summary.Ingress,
	}
	// JSON 编码为稳定结构；字段顺序由结构体定义控制。
	data, err := json.Marshal(payload)
	if err != nil {
		// payload 只包含可 JSON 编码的摘要类型，失败时返回空指纹作为保守降级。
		return ""
	}
	// 使用 SHA256 生成固定长度摘要，便于日志和快照比较。
	sum := sha256.Sum256(data)
	// 以十六进制字符串返回，避免二进制字节进入 JSON/proto 展示层。
	return hex.EncodeToString(sum[:])
}
