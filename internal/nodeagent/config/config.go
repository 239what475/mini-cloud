package config

import (
	"bytes"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	nodeagentv1 "mini-cloud/internal/gen/proto/minicloud/nodeagent/v1"
	"mini-cloud/internal/nodeagent/capacity"

	"gopkg.in/yaml.v3"
)

type fileConfig struct {
	Server        serverConfig        `yaml:"server"`
	Auth          authConfig          `yaml:"auth"`
	Platform      platformConfig      `yaml:"platform"`
	Node          nodeConfig          `yaml:"node"`
	Capacity      capacityConfig      `yaml:"capacity"`
	Agent         agentConfig         `yaml:"agent"`
	Runtime       runtimeConfig       `yaml:"runtime"`
	Network       networkConfig       `yaml:"network"`
	Work          workConfig          `yaml:"work"`
	Observability observabilityConfig `yaml:"observability"`
}

type serverConfig struct {
	URL string `yaml:"url"`
}

type authConfig struct {
	BootstrapToken string `yaml:"bootstrapToken"`
}

type platformConfig struct {
	Name string `yaml:"name"`
}

type nodeConfig struct {
	Provider     string `yaml:"provider"`
	Region       string `yaml:"region"`
	Name         string `yaml:"name"`
	PrivateIP    string `yaml:"privateIP"`
	PublicIP     string `yaml:"publicIP"`
	InstanceID   string `yaml:"instanceID"`
	InstanceType string `yaml:"instanceType"`
}

type capacityConfig struct {
	Total            resourceConfig `yaml:"total"`
	SystemReserved   resourceConfig `yaml:"systemReserved"`
	AgentReserved    resourceConfig `yaml:"agentReserved"`
	EvictionReserved resourceConfig `yaml:"evictionReserved"`
}

type resourceConfig struct {
	CPUMilli int `yaml:"cpuMilli"`
	MemoryMi int `yaml:"memoryMi"`
}

type agentConfig struct {
	Version             string `yaml:"version"`
	HeartbeatInterval   string `yaml:"heartbeatInterval"`
	WorkInterval        string `yaml:"workInterval"`
	ShutdownGracePeriod string `yaml:"shutdownGracePeriod"`
	CleanupHardTimeout  string `yaml:"cleanupHardTimeout"`
}

type runtimeConfig struct {
	Type          string              `yaml:"type"`
	HostPortRange hostPortRangeConfig `yaml:"hostPortRange"`
}

type hostPortRangeConfig struct {
	Min int `yaml:"min"`
	Max int `yaml:"max"`
}

type networkConfig struct {
	EgressProxy egressProxyConfig `yaml:"egressProxy"`
}

type egressProxyConfig struct {
	Enabled  bool     `yaml:"enabled"`
	Endpoint string   `yaml:"endpoint"`
	NoProxy  []string `yaml:"noProxy"`
}

type workConfig struct {
	ReadinessAttempts  int    `yaml:"readinessAttempts"`
	ReadinessInterval  string `yaml:"readinessInterval"`
	ReadinessTimeout   string `yaml:"readinessTimeout"`
	RuntimeTimeout     string `yaml:"runtimeTimeout"`
	RuntimeStopTimeout string `yaml:"runtimeStopTimeout"`
	RuntimeLogsTimeout string `yaml:"runtimeLogsTimeout"`
	RegisterTimeout    string `yaml:"registerTimeout"`
	HeartbeatTimeout   string `yaml:"heartbeatTimeout"`
	PollWorkTimeout    string `yaml:"pollWorkTimeout"`
	ReportTimeout      string `yaml:"reportTimeout"`
	LogTail            int    `yaml:"logTail"`
}

type observabilityConfig struct {
	WorkloadLogLokiURL      string `yaml:"workloadLogLokiURL"`
	WorkloadLogLokiTenantID string `yaml:"workloadLogLokiTenantID"`
	WorkloadOTLPEndpoint    string `yaml:"workloadOTLPEndpoint"`
	LogPushTimeout          string `yaml:"logPushTimeout"`
}

type Config struct {
	PlatformName          string
	ServerURL             string
	BootstrapToken        string
	RegisterInput         *nodeagentv1.RegisterNodeRequest
	AgentVersion          string
	CPUMilliAllocatable   int
	MemoryMiAllocatable   int
	HeartbeatInterval     time.Duration
	WorkInterval          time.Duration
	WorkloadLogLokiURL    string
	WorkloadLogLokiTenant string
	Runtime               RuntimeConfig
	Work                  WorkConfig
	Network               NetworkConfig
	Timeouts              TimeoutsConfig
	Logs                  LogsConfig
}

type RuntimeConfig struct {
	Type        string
	HostPortMin int
	HostPortMax int
}

type WorkConfig struct {
	PlatformName         string
	ReadinessAttempts    int
	ReadinessInterval    time.Duration
	ReadinessTimeout     time.Duration
	LogTail              int
	WorkloadOTLPEndpoint string
}

type NetworkConfig struct {
	EgressProxy EgressProxyConfig
}

type EgressProxyConfig struct {
	Enabled  bool
	Endpoint string
	NoProxy  []string
}

type TimeoutsConfig struct {
	Register      time.Duration
	Heartbeat     time.Duration
	PollWork      time.Duration
	Report        time.Duration
	RuntimeStart  time.Duration
	RuntimeStop   time.Duration
	RuntimeLogs   time.Duration
	ShutdownGrace time.Duration
	CleanupHard   time.Duration
}

type LogsConfig struct {
	PushTimeout time.Duration
}

func defaultFileConfig() fileConfig {
	return fileConfig{
		Node: nodeConfig{
			Provider: "aliyun",
		},
		Agent: agentConfig{
			Version:             "0.1.0",
			HeartbeatInterval:   "15s",
			WorkInterval:        "5s",
			ShutdownGracePeriod: "30s",
			CleanupHardTimeout:  "30s",
		},
		Runtime: runtimeConfig{
			Type: "docker",
			HostPortRange: hostPortRangeConfig{
				Min: 30000,
				Max: 60999,
			},
		},
		Work: workConfig{
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
		Observability: observabilityConfig{
			LogPushTimeout: "5s",
		},
	}
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read node-agent config %q: %w", path, err)
	}

	fileCfg := defaultFileConfig()
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&fileCfg); err != nil {
		return Config{}, fmt.Errorf("parse node-agent config %q: %w", path, err)
	}

	return build(fileCfg)
}

func build(fileCfg fileConfig) (Config, error) {
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
	registerInput := &nodeagentv1.RegisterNodeRequest{
		Provider:      fileCfg.Node.Provider,
		Region:        fileCfg.Node.Region,
		Name:          fileCfg.Node.Name,
		PrivateIp:     fileCfg.Node.PrivateIP,
		PublicIp:      fileCfg.Node.PublicIP,
		InstanceId:    fileCfg.Node.InstanceID,
		InstanceType:  fileCfg.Node.InstanceType,
		CpuMilliTotal: int32(resolvedCapacity.Total.CPUMilli),
		MemoryMiTotal: int32(resolvedCapacity.Total.MemoryMi),
	}
	if err := validateRegisterInput(registerInput); err != nil {
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

func toResource(value resourceConfig) capacity.Resources {
	return capacity.Resources{
		CPUMilli: value.CPUMilli,
		MemoryMi: value.MemoryMi,
	}
}

func validateRegisterInput(input *nodeagentv1.RegisterNodeRequest) error {
	if strings.TrimSpace(input.GetProvider()) == "" {
		return fmt.Errorf("node.provider is required")
	}
	if strings.TrimSpace(input.GetRegion()) == "" {
		return fmt.Errorf("node.region is required")
	}
	if strings.TrimSpace(input.GetName()) == "" {
		return fmt.Errorf("node.name is required")
	}
	privateIP := strings.TrimSpace(input.GetPrivateIp())
	if privateIP == "" {
		return fmt.Errorf("node.privateIP is required")
	}
	if net.ParseIP(privateIP) == nil {
		return fmt.Errorf("node.privateIP must be a valid IP address")
	}
	if publicIP := strings.TrimSpace(input.GetPublicIp()); publicIP != "" && net.ParseIP(publicIP) == nil {
		return fmt.Errorf("node.publicIP must be a valid IP address")
	}
	if strings.TrimSpace(input.GetInstanceId()) == "" {
		return fmt.Errorf("node.instanceID is required")
	}
	if strings.TrimSpace(input.GetInstanceType()) == "" {
		return fmt.Errorf("node.instanceType is required")
	}
	if input.GetCpuMilliTotal() <= 0 {
		return fmt.Errorf("capacity total cpuMilli must be greater than 0")
	}
	if input.GetMemoryMiTotal() <= 0 {
		return fmt.Errorf("capacity total memoryMi must be greater than 0")
	}
	return nil
}

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
