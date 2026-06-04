package config

import (
	"bytes"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"mini-cloud/internal/contract/nodeagentapi"
	"mini-cloud/internal/nodeagent/capacity"

	"gopkg.in/yaml.v3"
)

// nodeAgentFileConfig 对应 node-agent YAML 文件的顶层结构。
type nodeAgentFileConfig struct {
	// Server 配置 node-agent 访问控制面的服务端地址。
	Server nodeAgentServerConfig `yaml:"server"`
	// Auth 配置节点首次注册时使用的启动认证信息。
	Auth nodeAgentAuthConfig `yaml:"auth"`
	// Platform 描述当前节点所属的 mini-cloud 平台实例。
	Platform nodeAgentPlatformConfig `yaml:"platform"`
	// Node 描述将要注册到控制面的基础设施节点。
	Node nodeAgentNodeConfig `yaml:"node"`
	// Capacity 配置节点上报的 CPU、内存容量和预留量。
	Capacity nodeAgentCapacityConfig `yaml:"capacity"`
	// Agent 配置 node-agent 进程行为。
	Agent nodeAgentRuntimeConfig `yaml:"agent"`
	// Runtime 选择本机工作负载运行时实现。
	Runtime nodeAgentRuntimeProvider `yaml:"runtime"`
	// Network 配置节点和工作负载网络行为。
	Network nodeAgentNetworkConfig `yaml:"network"`
	// Work 配置执行拉取、启动和 readiness 探测行为。
	Work nodeAgentWorkConfig `yaml:"work"`
	// Observability 配置工作负载日志采集和遥测环境注入。
	Observability nodeAgentObservabilityConfig `yaml:"observability"`
}

// nodeAgentServerConfig 对应 YAML 中的 server 配置段。
type nodeAgentServerConfig struct {
	// URL 是控制面的 HTTP 或 HTTPS 基础地址。
	URL string `yaml:"url"`
}

// nodeAgentAuthConfig 对应 YAML 中的 auth 配置段。
type nodeAgentAuthConfig struct {
	// BootstrapToken 是节点获得会话令牌前用于首次注册的共享令牌。
	BootstrapToken string `yaml:"bootstrapToken"`
}

// nodeAgentPlatformConfig 对应 YAML 中的 platform 配置段。
type nodeAgentPlatformConfig struct {
	// Name 是平台名称，会写入工作负载日志和遥测资源属性。
	Name string `yaml:"name"`
}

// nodeAgentNodeConfig 对应 YAML 中的 node 配置段，用于构造注册请求。
type nodeAgentNodeConfig struct {
	// Provider 是节点所属的云厂商或本地 provider 名称。
	Provider string `yaml:"provider"`
	// Region 是节点所属的云区域或实验环境区域。
	Region string `yaml:"region"`
	// Name 是节点的人类可读名称。
	Name string `yaml:"name"`
	// PrivateIP 是平台内部访问节点时使用的私有地址。
	PrivateIP string `yaml:"privateIP"`
	// PublicIP 是节点可选的公网地址。
	PublicIP string `yaml:"publicIP"`
	// InstanceID 是云厂商实例 ID 或本地节点实例标识。
	InstanceID string `yaml:"instanceID"`
	// InstanceType 是云厂商实例规格或本地运行时类型标签。
	InstanceType string `yaml:"instanceType"`
}

// nodeAgentCapacityConfig 对应 YAML 中的 capacity 配置段。
//
// 容量模型使用静态 allocatable 预算，而不是宿主机实时 free：
//
//	allocatable = total - systemReserved - agentReserved - evictionReserved
//
// total 表示 node-agent 纳入 mini-cloud 资源账本的总资源池，不强制等同于宿主机物理总容量；
// systemReserved 扣除 OS 和 node-agent 管理范围外的服务；
// agentReserved 扣除 node-agent、容器运行时、日志采集等节点管理组件；
// evictionReserved 当前只支持内存，用于压力保护。
type nodeAgentCapacityConfig struct {
	// Total 是 mini-cloud 资源账本总资源池；字段为 0 时自动探测，负数非法。
	Total nodeAgentResourceConfig `yaml:"total"`
	// SystemReserved 是预留给 OS 和 node-agent 管理范围外服务的资源。
	SystemReserved nodeAgentResourceConfig `yaml:"systemReserved"`
	// AgentReserved 是预留给 node-agent 和节点管理组件的资源。
	AgentReserved nodeAgentResourceConfig `yaml:"agentReserved"`
	// EvictionReserved 是预留给压力保护的资源；当前只允许配置内存。
	EvictionReserved nodeAgentResourceConfig `yaml:"evictionReserved"`
}

// nodeAgentResourceConfig 表示 YAML 中一组 CPU / 内存资源数量。
type nodeAgentResourceConfig struct {
	// CPUMilli 是 CPU 数量，单位为 millicore。
	CPUMilli int `yaml:"cpuMilli"`
	// MemoryMi 是内存数量，单位为 MiB。
	MemoryMi int `yaml:"memoryMi"`
}

// nodeAgentRuntimeConfig 对应 YAML 中的 agent 配置段。
type nodeAgentRuntimeConfig struct {
	// Version 是通过心跳上报的 node-agent 版本。
	Version string `yaml:"version"`
	// HeartbeatInterval 是心跳循环的间隔。
	HeartbeatInterval string `yaml:"heartbeatInterval"`
	// WorkInterval 是执行拉取循环的间隔。
	WorkInterval string `yaml:"workInterval"`
	// ShutdownGracePeriod 是 daemon 等待后台循环退出的宽限时间。
	ShutdownGracePeriod string `yaml:"shutdownGracePeriod"`
	// CleanupHardTimeout 是恢复和清理操作的硬超时时间。
	CleanupHardTimeout string `yaml:"cleanupHardTimeout"`
}

// nodeAgentRuntimeProvider 对应 YAML 中的 runtime 配置段。
type nodeAgentRuntimeProvider struct {
	// Type 是运行时 provider 名称；生产路径当前使用 docker。
	Type string `yaml:"type"`
	// HostPortRange 限定 Docker 发布给外置 ingress 访问的宿主机端口范围。
	HostPortRange nodeAgentHostPortRangeConfig `yaml:"hostPortRange"`
}

// nodeAgentHostPortRangeConfig 对应 YAML 中的 runtime.hostPortRange 配置段。
type nodeAgentHostPortRangeConfig struct {
	// Min 是 node-agent 可分配 hostPort 的最小值。
	Min int `yaml:"min"`
	// Max 是 node-agent 可分配 hostPort 的最大值。
	Max int `yaml:"max"`
}

// nodeAgentNetworkConfig 对应 YAML 中的 network 配置段。
type nodeAgentNetworkConfig struct {
	// EgressProxy 描述注入工作负载容器的 HTTP/HTTPS 出公网代理。
	EgressProxy nodeAgentEgressProxyConfig `yaml:"egressProxy"`
}

// nodeAgentEgressProxyConfig 对应 YAML 中的 network.egressProxy 配置段。
type nodeAgentEgressProxyConfig struct {
	// Enabled 表示是否向 workload 容器注入 proxy 环境变量。
	Enabled bool `yaml:"enabled"`
	// Endpoint 是 workload 应使用的 HTTP/HTTPS proxy URL。
	Endpoint string `yaml:"endpoint"`
	// NoProxy 是不走 proxy 的地址、域名或网段列表。
	NoProxy []string `yaml:"noProxy"`
}

// nodeAgentWorkConfig 对应 YAML 中的 work 配置段。
type nodeAgentWorkConfig struct {
	// ReadinessAttempts 是容器启动后的 readiness 探测尝试次数。
	ReadinessAttempts int `yaml:"readinessAttempts"`
	// ReadinessInterval 是两次 readiness 探测之间的间隔。
	ReadinessInterval string `yaml:"readinessInterval"`
	// ReadinessTimeout 是单次 readiness 探测 HTTP 请求超时时间。
	ReadinessTimeout string `yaml:"readinessTimeout"`
	// RuntimeTimeout 是启动工作负载容器的超时时间。
	RuntimeTimeout string `yaml:"runtimeTimeout"`
	// RuntimeStopTimeout 是停止工作负载容器的超时时间。
	RuntimeStopTimeout string `yaml:"runtimeStopTimeout"`
	// RuntimeLogsTimeout 是读取运行时日志和统计容器数的超时时间。
	RuntimeLogsTimeout string `yaml:"runtimeLogsTimeout"`
	// RegisterTimeout 是节点注册请求的超时时间。
	RegisterTimeout string `yaml:"registerTimeout"`
	// HeartbeatTimeout 是心跳请求的超时时间。
	HeartbeatTimeout string `yaml:"heartbeatTimeout"`
	// PollWorkTimeout 是拉取执行任务请求的超时时间。
	PollWorkTimeout string `yaml:"pollWorkTimeout"`
	// ReportTimeout 是上报执行结果请求的超时时间。
	ReportTimeout string `yaml:"reportTimeout"`
	// LogTail 是执行失败时附带到上报结果中的容器日志行数。
	LogTail int `yaml:"logTail"`
}

// nodeAgentObservabilityConfig 对应 YAML 中的 observability 配置段。
type nodeAgentObservabilityConfig struct {
	// WorkloadLogLokiURL 是工作负载容器日志推送到 Loki 的基础地址。
	WorkloadLogLokiURL string `yaml:"workloadLogLokiURL"`
	// WorkloadLogLokiTenantID 是可选的 Loki 租户头 X-Scope-OrgID。
	WorkloadLogLokiTenantID string `yaml:"workloadLogLokiTenantID"`
	// WorkloadOTLPEndpoint 是注入工作负载环境变量的 OTLP HTTP endpoint。
	WorkloadOTLPEndpoint string `yaml:"workloadOTLPEndpoint"`
	// LogPushTimeout 是单次 Loki push 请求的超时时间。
	LogPushTimeout string `yaml:"logPushTimeout"`
}

// Config 是 daemon 使用或保留的、已校验和归一化后的 node-agent 配置。
type Config struct {
	// PlatformName 是平台名称，用于日志标签和工作负载遥测资源属性。
	PlatformName string
	// ServerURL 是已校验的 cloud-plane 明文 gRPC endpoint；可使用 host:port 或 http://host:port。
	ServerURL string
	// BootstrapToken 是节点获得会话令牌前使用的注册令牌。
	BootstrapToken string
	// RegisterInput 是完整校验后的节点注册请求体。
	RegisterInput nodeagentapi.RegisterNodeRequest
	// AgentVersion 是通过心跳上报的 agent 版本。
	AgentVersion string
	// CPUMilliAllocatable 是 node-agent 承诺给 mini-cloud 调度器使用的静态 CPU 预算。
	CPUMilliAllocatable int
	// MemoryMiAllocatable 是 node-agent 承诺给 mini-cloud 调度器使用的静态内存预算。
	MemoryMiAllocatable int
	// HeartbeatInterval 是心跳循环间隔。
	HeartbeatInterval time.Duration
	// WorkInterval 是执行拉取循环间隔。
	WorkInterval time.Duration
	// WorkloadLogLokiURL 是工作负载日志 Loki 基础地址。
	WorkloadLogLokiURL string
	// WorkloadLogLokiTenant 是可选的 Loki 租户值。
	WorkloadLogLokiTenant string
	// Runtime 配置本机工作负载运行时。
	Runtime RuntimeConfig
	// Work 配置执行启动和 readiness 探测行为。
	Work WorkConfig
	// Network 配置 node-agent 注入到 workload 的网络环境。
	Network NetworkConfig
	// Timeouts 汇总 daemon 各类外部操作超时。
	Timeouts TimeoutsConfig
	// Logs 汇总工作负载日志推送参数。
	Logs LogsConfig
}

// RuntimeConfig 配置本机工作负载运行时 provider。
type RuntimeConfig struct {
	// Type 是运行时 provider 名称。
	Type string
	// HostPortMin 是 workload 宿主机端口池最小端口。
	HostPortMin int
	// HostPortMax 是 workload 宿主机端口池最大端口。
	HostPortMax int
}

// WorkConfig 配置工作负载执行行为。
type WorkConfig struct {
	// PlatformName 是传递给工作负载遥测元数据的平台名称。
	PlatformName string
	// ReadinessAttempts 是工作负载启动后的 readiness 探测尝试次数。
	ReadinessAttempts int
	// ReadinessInterval 是两次 readiness 探测之间的间隔。
	ReadinessInterval time.Duration
	// ReadinessTimeout 是单次 readiness 探测 HTTP 请求超时时间。
	ReadinessTimeout time.Duration
	// LogTail 是执行失败时采集的容器日志尾部行数。
	LogTail int
	// WorkloadOTLPEndpoint 是注入工作负载环境变量的 OTLP endpoint。
	WorkloadOTLPEndpoint string
}

// NetworkConfig 配置工作负载网络环境注入。
type NetworkConfig struct {
	// EgressProxy 描述注入 workload 容器的 HTTP/HTTPS 代理环境变量。
	EgressProxy EgressProxyConfig
}

// EgressProxyConfig 描述 workload 出公网代理配置。
type EgressProxyConfig struct {
	// Enabled 表示是否向 workload 容器注入 proxy 环境变量。
	Enabled bool
	// Endpoint 是 HTTP/HTTPS proxy URL。
	Endpoint string
	// NoProxy 是不走 proxy 的地址、域名或网段列表。
	NoProxy []string
}

// TimeoutsConfig 汇总 daemon 外部操作超时配置。
type TimeoutsConfig struct {
	// Register 限制节点注册请求耗时。
	Register time.Duration
	// Heartbeat 限制心跳请求耗时。
	Heartbeat time.Duration
	// PollWork 限制执行拉取请求耗时。
	PollWork time.Duration
	// Report 限制执行结果上报请求耗时。
	Report time.Duration
	// RuntimeStart 限制工作负载容器启动耗时。
	RuntimeStart time.Duration
	// RuntimeStop 限制工作负载容器停止耗时。
	RuntimeStop time.Duration
	// RuntimeLogs 限制读取运行时日志和容器计数耗时。
	RuntimeLogs time.Duration
	// ShutdownGrace 限制 daemon 关闭时等待后台循环退出的时间。
	ShutdownGrace time.Duration
	// CleanupHard 限制恢复和清理类操作耗时。
	CleanupHard time.Duration
}

// LogsConfig 汇总工作负载日志推送配置。
type LogsConfig struct {
	// PushTimeout 限制单次 Loki push 请求耗时。
	PushTimeout time.Duration
}

// defaultNodeAgentFileConfig 返回严格解码用户 YAML 前使用的默认配置。
func defaultNodeAgentFileConfig() nodeAgentFileConfig {
	return nodeAgentFileConfig{
		Node: nodeAgentNodeConfig{
			Provider: "aliyun",
		},
		Agent: nodeAgentRuntimeConfig{
			Version:             "0.1.0",
			HeartbeatInterval:   "15s",
			WorkInterval:        "5s",
			ShutdownGracePeriod: "30s",
			CleanupHardTimeout:  "30s",
		},
		Runtime: nodeAgentRuntimeProvider{
			Type: "docker",
			HostPortRange: nodeAgentHostPortRangeConfig{
				Min: 30000,
				Max: 60999,
			},
		},
		Work: nodeAgentWorkConfig{
			ReadinessAttempts:  10,
			ReadinessInterval:  "1s",
			ReadinessTimeout:   "1s",
			RuntimeTimeout:     "2m",
			RuntimeStopTimeout: "30s",
			RuntimeLogsTimeout: "10s",
			RegisterTimeout:    "10s",
			HeartbeatTimeout:   "5s",
			PollWorkTimeout:    "10s",
			ReportTimeout:      "10s",
			LogTail:            20,
		},
		Observability: nodeAgentObservabilityConfig{
			LogPushTimeout: "5s",
		},
	}
}

// Load 读取、严格解码、校验并归一化 node-agent YAML 配置文件。
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read node-agent config %q: %w", path, err)
	}

	fileCfg := defaultNodeAgentFileConfig()
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&fileCfg); err != nil {
		return Config{}, fmt.Errorf("parse node-agent config %q: %w", path, err)
	}

	return build(fileCfg)
}

// build 将已解码的 YAML 配置转换为 daemon 可直接使用的配置。
func build(fileCfg nodeAgentFileConfig) (Config, error) {
	serverURL, err := validateServerURL(fileCfg.Server.URL)
	if err != nil {
		return Config{}, err
	}
	bootstrapToken := strings.TrimSpace(fileCfg.Auth.BootstrapToken)
	if bootstrapToken == "" {
		return Config{}, fmt.Errorf("auth.bootstrapToken is required")
	}

	resolvedCapacity, err := capacity.ResolveNodeCapacity(capacity.Input{
		Total:            toResource(fileCfg.Capacity.Total),
		SystemReserved:   toResource(fileCfg.Capacity.SystemReserved),
		AgentReserved:    toResource(fileCfg.Capacity.AgentReserved),
		EvictionReserved: toResource(fileCfg.Capacity.EvictionReserved),
	})
	if err != nil {
		return Config{}, err
	}

	heartbeatInterval, err := parsePositiveDuration("agent.heartbeatInterval", fileCfg.Agent.HeartbeatInterval)
	if err != nil {
		return Config{}, err
	}
	workInterval, err := parsePositiveDuration("agent.workInterval", fileCfg.Agent.WorkInterval)
	if err != nil {
		return Config{}, err
	}
	readinessInterval, err := parsePositiveDuration("work.readinessInterval", fileCfg.Work.ReadinessInterval)
	if err != nil {
		return Config{}, err
	}
	readinessTimeout, err := parsePositiveDuration("work.readinessTimeout", fileCfg.Work.ReadinessTimeout)
	if err != nil {
		return Config{}, err
	}
	runtimeType := strings.TrimSpace(fileCfg.Runtime.Type)
	if runtimeType == "" {
		runtimeType = "docker"
	}
	if fileCfg.Runtime.HostPortRange.Min <= 0 || fileCfg.Runtime.HostPortRange.Max <= 0 {
		return Config{}, fmt.Errorf("runtime.hostPortRange min and max must be greater than 0")
	}
	if fileCfg.Runtime.HostPortRange.Min > fileCfg.Runtime.HostPortRange.Max {
		return Config{}, fmt.Errorf("runtime.hostPortRange.min must be less than or equal to runtime.hostPortRange.max")
	}
	if fileCfg.Runtime.HostPortRange.Max > 65535 {
		return Config{}, fmt.Errorf("runtime.hostPortRange.max must be less than or equal to 65535")
	}

	runtimeTimeout, err := parsePositiveDuration("work.runtimeTimeout", fileCfg.Work.RuntimeTimeout)
	if err != nil {
		return Config{}, err
	}
	runtimeStopTimeout, err := parsePositiveDuration("work.runtimeStopTimeout", fileCfg.Work.RuntimeStopTimeout)
	if err != nil {
		return Config{}, err
	}
	runtimeLogsTimeout, err := parsePositiveDuration("work.runtimeLogsTimeout", fileCfg.Work.RuntimeLogsTimeout)
	if err != nil {
		return Config{}, err
	}
	registerTimeout, err := parsePositiveDuration("work.registerTimeout", fileCfg.Work.RegisterTimeout)
	if err != nil {
		return Config{}, err
	}
	heartbeatTimeout, err := parsePositiveDuration("work.heartbeatTimeout", fileCfg.Work.HeartbeatTimeout)
	if err != nil {
		return Config{}, err
	}
	pollWorkTimeout, err := parsePositiveDuration("work.pollWorkTimeout", fileCfg.Work.PollWorkTimeout)
	if err != nil {
		return Config{}, err
	}
	reportTimeout, err := parsePositiveDuration("work.reportTimeout", fileCfg.Work.ReportTimeout)
	if err != nil {
		return Config{}, err
	}
	shutdownGrace, err := parsePositiveDuration("agent.shutdownGracePeriod", fileCfg.Agent.ShutdownGracePeriod)
	if err != nil {
		return Config{}, err
	}
	cleanupHard, err := parsePositiveDuration("agent.cleanupHardTimeout", fileCfg.Agent.CleanupHardTimeout)
	if err != nil {
		return Config{}, err
	}
	logPushTimeout, err := parsePositiveDuration("observability.logPushTimeout", fileCfg.Observability.LogPushTimeout)
	if err != nil {
		return Config{}, err
	}
	if fileCfg.Work.ReadinessAttempts <= 0 {
		return Config{}, fmt.Errorf("work.readinessAttempts must be greater than 0")
	}
	if fileCfg.Work.LogTail < 0 {
		return Config{}, fmt.Errorf("work.logTail must be greater than or equal to 0")
	}
	if fileCfg.Network.EgressProxy.Enabled {
		endpoint := strings.TrimSpace(fileCfg.Network.EgressProxy.Endpoint)
		if endpoint == "" {
			return Config{}, fmt.Errorf("network.egressProxy.endpoint is required when egress proxy is enabled")
		}
		parsed, err := url.ParseRequestURI(endpoint)
		if err != nil {
			return Config{}, fmt.Errorf("network.egressProxy.endpoint must be a valid URL: %w", err)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return Config{}, fmt.Errorf("network.egressProxy.endpoint must use http or https")
		}
		if parsed.Host == "" {
			return Config{}, fmt.Errorf("network.egressProxy.endpoint must include host")
		}
	}
	registerInput := nodeagentapi.RegisterNodeRequest{
		Provider:      fileCfg.Node.Provider,
		Region:        fileCfg.Node.Region,
		Name:          fileCfg.Node.Name,
		PrivateIP:     fileCfg.Node.PrivateIP,
		PublicIP:      fileCfg.Node.PublicIP,
		InstanceID:    fileCfg.Node.InstanceID,
		InstanceType:  fileCfg.Node.InstanceType,
		CPUMilliTotal: resolvedCapacity.Total.CPUMilli,
		MemoryMiTotal: resolvedCapacity.Total.MemoryMi,
	}
	if err := registerInput.Validate(); err != nil {
		return Config{}, err
	}

	return Config{
		PlatformName:          fileCfg.Platform.Name,
		ServerURL:             serverURL,
		BootstrapToken:        bootstrapToken,
		RegisterInput:         registerInput,
		AgentVersion:          fileCfg.Agent.Version,
		CPUMilliAllocatable:   resolvedCapacity.Allocatable.CPUMilli,
		MemoryMiAllocatable:   resolvedCapacity.Allocatable.MemoryMi,
		HeartbeatInterval:     heartbeatInterval,
		WorkInterval:          workInterval,
		WorkloadLogLokiURL:    fileCfg.Observability.WorkloadLogLokiURL,
		WorkloadLogLokiTenant: fileCfg.Observability.WorkloadLogLokiTenantID,
		Runtime: RuntimeConfig{
			Type:        runtimeType,
			HostPortMin: fileCfg.Runtime.HostPortRange.Min,
			HostPortMax: fileCfg.Runtime.HostPortRange.Max,
		},
		Work: WorkConfig{
			PlatformName:         fileCfg.Platform.Name,
			ReadinessAttempts:    fileCfg.Work.ReadinessAttempts,
			ReadinessInterval:    readinessInterval,
			ReadinessTimeout:     readinessTimeout,
			LogTail:              fileCfg.Work.LogTail,
			WorkloadOTLPEndpoint: fileCfg.Observability.WorkloadOTLPEndpoint,
		},
		Network: NetworkConfig{
			EgressProxy: EgressProxyConfig{
				Enabled:  fileCfg.Network.EgressProxy.Enabled,
				Endpoint: strings.TrimSpace(fileCfg.Network.EgressProxy.Endpoint),
				NoProxy:  trimStringList(fileCfg.Network.EgressProxy.NoProxy),
			},
		},
		Timeouts: TimeoutsConfig{
			Register:      registerTimeout,
			Heartbeat:     heartbeatTimeout,
			PollWork:      pollWorkTimeout,
			Report:        reportTimeout,
			RuntimeStart:  runtimeTimeout,
			RuntimeStop:   runtimeStopTimeout,
			RuntimeLogs:   runtimeLogsTimeout,
			ShutdownGrace: shutdownGrace,
			CleanupHard:   cleanupHard,
		},
		Logs: LogsConfig{
			PushTimeout: logPushTimeout,
		},
	}, nil
}

// toResource 将 YAML 中的资源数量转换为容量计算包使用的结构。
func toResource(value nodeAgentResourceConfig) capacity.Resources {
	return capacity.Resources{
		CPUMilli: value.CPUMilli,
		MemoryMi: value.MemoryMi,
	}
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

// validateServerURL 校验 server.url 非空，并允许 host:port 或 http://host:port 明文 gRPC endpoint。
func validateServerURL(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("server.url is required")
	}
	if host, port, err := net.SplitHostPort(trimmed); err == nil && strings.TrimSpace(host) != "" && strings.TrimSpace(port) != "" {
		return trimmed, nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse server.url: %w", err)
	}
	if parsed.Scheme != "http" {
		return "", fmt.Errorf("server.url must be host:port or use http gRPC URI")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("server.url must include host")
	}
	return trimmed, nil
}

// parsePositiveDuration 解析 duration 字符串，并拒绝零值或负值。
func parsePositiveDuration(name string, value string) (time.Duration, error) {
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be greater than 0", name)
	}
	return parsed, nil
}
