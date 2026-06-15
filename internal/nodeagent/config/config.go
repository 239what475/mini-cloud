package config

import (
	"bytes"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"mini-cloud/internal/nodeagent/capacity"

	"gopkg.in/yaml.v3"
)

const (
	HeartbeatInterval = 15 * time.Second
	WorkInterval      = 5 * time.Second

	ShutdownGraceTimeout = 30 * time.Second
	CleanupHardTimeout   = 30 * time.Second

	RuntimeHostPortMin       = 30000
	RuntimeHostPortMax       = 60999
	RuntimeStartTimeout      = 2 * time.Minute
	RuntimeStopTimeout       = 30 * time.Second
	RuntimeLogsTimeout       = 10 * time.Second
	ReadinessAttempts        = 10
	ReadinessInterval        = time.Second
	ReadinessTimeout         = time.Second
	RegisterTimeout          = 10 * time.Second
	HeartbeatRequestTimeout  = 5 * time.Second
	PollWorkRequestTimeout   = 10 * time.Second
	ReportExecutionTimeout   = 10 * time.Second
	WorkloadLogTailLineCount = 20
)

type ServerConfig struct {
	URL   string `yaml:"url"`
	TLSCA string `yaml:"tlsCA"`
}

type AuthConfig struct {
	Token string `yaml:"token"`
}

type PlatformConfig struct {
	Name string `yaml:"name"`
}

type NodeConfig struct {
	Provider     string `yaml:"provider"`
	Region       string `yaml:"region"`
	Name         string `yaml:"name"`
	PrivateIP    string `yaml:"privateIP"`
	InstanceID   string `yaml:"instanceID"`
	InstanceType string `yaml:"instanceType"`
}

type CapacityConfig struct {
	Total ResourceConfig `yaml:"total"`
}

type ResourceConfig struct {
	CPUMilli int `yaml:"cpuMilli"`
	MemoryMi int `yaml:"memoryMi"`
}

type ObservabilityConfig struct {
	WorkloadOTLPEndpoint string `yaml:"workloadOTLPEndpoint"`
}

type Config struct {
	Path          string              `yaml:"-"`
	Server        ServerConfig        `yaml:"server"`
	Auth          AuthConfig          `yaml:"auth"`
	Platform      PlatformConfig      `yaml:"platform"`
	Node          NodeConfig          `yaml:"node"`
	Capacity      CapacityConfig      `yaml:"capacity"`
	Network       NetworkConfig       `yaml:"network"`
	Observability ObservabilityConfig `yaml:"observability"`

	ResolvedCapacity capacity.Resolved `yaml:"-"`
}

type NetworkConfig struct {
	WorkloadProxy WorkloadProxyConfig `yaml:"workloadProxy"`
}

type WorkloadProxyConfig struct {
	Endpoint string   `yaml:"endpoint"`
	NoProxy  []string `yaml:"noProxy"`
}

func Load(path string) (Config, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Config{}, fmt.Errorf("node-agent config path is empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read node-agent config %q: %w", path, err)
	}

	var cfg Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse node-agent config %q: %w", path, err)
	}

	cfg.Path = path
	return build(cfg)
}

func build(cfg Config) (Config, error) {
	serverURL, err := validateServerURL(cfg.Server.URL)
	if err != nil {
		return Config{}, err
	}
	tlsCA := strings.TrimSpace(cfg.Server.TLSCA)
	if strings.HasPrefix(strings.ToLower(serverURL), "grpcs://") && tlsCA == "" {
		return Config{}, fmt.Errorf("server.tlsCA is required when server.url uses grpcs")
	}
	token := strings.TrimSpace(cfg.Auth.Token)
	if token == "" {
		return Config{}, fmt.Errorf("auth.token is required")
	}

	resolvedCapacity, err := capacity.ResolveNodeCapacity(toResource(cfg.Capacity.Total))
	if err != nil {
		return Config{}, err
	}

	endpoint := strings.TrimSpace(cfg.Network.WorkloadProxy.Endpoint)
	if endpoint != "" {
		parsed, err := url.ParseRequestURI(endpoint)
		if err != nil {
			return Config{}, fmt.Errorf("network.workloadProxy.endpoint must be a valid URL: %w", err)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return Config{}, fmt.Errorf("network.workloadProxy.endpoint must use http or https")
		}
		if parsed.Host == "" {
			return Config{}, fmt.Errorf("network.workloadProxy.endpoint must include host")
		}
	}
	cfg.Platform.Name = strings.TrimSpace(cfg.Platform.Name)
	cfg.Node.Provider = strings.ToLower(strings.TrimSpace(cfg.Node.Provider))
	cfg.Node.Region = strings.TrimSpace(cfg.Node.Region)
	cfg.Node.Name = strings.TrimSpace(cfg.Node.Name)
	cfg.Node.PrivateIP = strings.TrimSpace(cfg.Node.PrivateIP)
	cfg.Node.InstanceID = strings.TrimSpace(cfg.Node.InstanceID)
	cfg.Node.InstanceType = strings.TrimSpace(cfg.Node.InstanceType)
	if err := validateNodeConfig(cfg.Node, resolvedCapacity.Total); err != nil {
		return Config{}, err
	}

	cfg.Network.WorkloadProxy.Endpoint = endpoint
	cfg.Network.WorkloadProxy.NoProxy = trimStringList(cfg.Network.WorkloadProxy.NoProxy)
	cfg.Observability.WorkloadOTLPEndpoint = strings.TrimSpace(cfg.Observability.WorkloadOTLPEndpoint)

	cfg.Server.URL = serverURL
	cfg.Server.TLSCA = tlsCA
	cfg.Auth.Token = token
	cfg.ResolvedCapacity = resolvedCapacity
	return cfg, nil
}

func toResource(value ResourceConfig) capacity.Resources {
	return capacity.Resources{
		CPUMilli: value.CPUMilli,
		MemoryMi: value.MemoryMi,
	}
}

func validateNodeConfig(input NodeConfig, total capacity.Resources) error {
	if strings.TrimSpace(input.Provider) == "" {
		return fmt.Errorf("node.provider is required")
	}
	if strings.TrimSpace(input.Region) == "" {
		return fmt.Errorf("node.region is required")
	}
	if strings.TrimSpace(input.Name) == "" {
		return fmt.Errorf("node.name is required")
	}
	privateIP := strings.TrimSpace(input.PrivateIP)
	if privateIP == "" {
		return fmt.Errorf("node.privateIP is required")
	}
	if net.ParseIP(privateIP) == nil {
		return fmt.Errorf("node.privateIP must be a valid IP address")
	}
	if strings.TrimSpace(input.InstanceID) == "" {
		return fmt.Errorf("node.instanceID is required")
	}
	if strings.TrimSpace(input.InstanceType) == "" {
		return fmt.Errorf("node.instanceType is required")
	}
	if total.CPUMilli <= 0 {
		return fmt.Errorf("capacity total cpuMilli must be greater than 0")
	}
	if total.MemoryMi <= 0 {
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
	if parsed.Scheme != "http" && parsed.Scheme != "grpcs" {
		return "", fmt.Errorf("server.url must be host:port, http gRPC URI, or grpcs URI")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("server.url must include host")
	}
	return trimmed, nil
}
