package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	NodeAgentSessionTTL               = 24 * time.Hour
	NodeAgentHeartbeatIntervalSeconds = 15
	NodeAgentWorkIntervalSeconds      = 5
	NodeAgentHostPortMin              = 30000
	NodeAgentHostPortMax              = 60999
	NodeNameSuffix                    = "-node"
	CaddyListenHTTPAddr               = "0.0.0.0:80"
)

type Config struct {
	Path                string                    `yaml:"-"`
	Server              ServerConfig              `yaml:"server"`
	Database            DatabaseConfig            `yaml:"database"`
	Plane               PlaneConfig               `yaml:"plane"`
	ControlPlane        ControlPlaneConfig        `yaml:"controlPlane"`
	NodeAgent           NodeAgentConfig           `yaml:"nodeAgent"`
	Infrastructure      InfrastructureConfig      `yaml:"infrastructure"`
	RuntimeProvisioning RuntimeProvisioningConfig `yaml:"runtimeProvisioning"`
	Ingress             IngressConfig             `yaml:"ingress"`
	Observability       ObservabilityConfig       `yaml:"observability"`
}

type ServerConfig struct {
	ListenGRPCAddr string `yaml:"listenGRPCAddr"`
}

type DatabaseConfig struct {
	URL string `yaml:"url"`
}

type PlaneConfig struct {
	Name string `yaml:"name"`
}

type ControlPlaneConfig struct {
	BearerToken string `yaml:"bearerToken"`
}

type NodeAgentConfig struct {
	ConnectEndpoint string `yaml:"connectEndpoint"`
	BootstrapToken  string `yaml:"bootstrapToken"`
	BinaryURL       string `yaml:"binaryUrl"`
}

type InfrastructureConfig struct {
	Provider string `yaml:"provider"`
	RegionID string `yaml:"regionId"`
	ZoneID   string `yaml:"zoneId"`
}

type RuntimeProvisioningConfig struct {
	InstanceType        string         `yaml:"instanceType"`
	RegistryMirrors     []string       `yaml:"registryMirrors"`
	EgressProxyEndpoint string         `yaml:"egressProxyEndpoint"`
	ProviderSpec        map[string]any `yaml:"providerSpec"`
}

func (c RuntimeProvisioningConfig) ParseProviderSpec(target any) error {
	if len(c.ProviderSpec) == 0 {
		return fmt.Errorf("runtimeProvisioning.providerSpec is empty")
	}
	data, err := json.Marshal(c.ProviderSpec)
	if err != nil {
		return fmt.Errorf("parse runtimeProvisioning.providerSpec: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("parse runtimeProvisioning.providerSpec: %w", err)
	}
	return nil
}

type IngressConfig struct {
	BaseDomain    string `yaml:"baseDomain"`
	CaddyAdminURL string `yaml:"caddyAdminURL"`
}

type ObservabilityConfig struct {
	LokiURL      string `yaml:"lokiURL"`
	OTLPEndpoint string `yaml:"otlpEndpoint"`
}

func Load(path string) (Config, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Config{}, fmt.Errorf("cloud-plane config path is empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read cloud-plane config %q: %w", path, err)
	}

	var cfg Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse cloud-plane config %q: %w", path, err)
	}

	cfg.Path = path
	cfg.normalize()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) normalize() {
	c.Plane.Name = strings.TrimSpace(c.Plane.Name)
	c.ControlPlane.BearerToken = strings.TrimSpace(c.ControlPlane.BearerToken)
	c.Infrastructure.Provider = strings.TrimSpace(c.Infrastructure.Provider)
	c.Infrastructure.RegionID = strings.TrimSpace(c.Infrastructure.RegionID)
	c.Infrastructure.ZoneID = strings.TrimSpace(c.Infrastructure.ZoneID)
	c.NodeAgent.ConnectEndpoint = strings.TrimSpace(c.NodeAgent.ConnectEndpoint)
	c.NodeAgent.BootstrapToken = strings.TrimSpace(c.NodeAgent.BootstrapToken)
	c.NodeAgent.BinaryURL = strings.TrimSpace(c.NodeAgent.BinaryURL)
	c.RuntimeProvisioning.InstanceType = strings.TrimSpace(c.RuntimeProvisioning.InstanceType)
	c.RuntimeProvisioning.RegistryMirrors = trimStringList(c.RuntimeProvisioning.RegistryMirrors)
	c.RuntimeProvisioning.EgressProxyEndpoint = strings.TrimSpace(c.RuntimeProvisioning.EgressProxyEndpoint)
	c.Ingress.BaseDomain = strings.Trim(strings.TrimSpace(c.Ingress.BaseDomain), ".")
	c.Ingress.CaddyAdminURL = strings.TrimSpace(c.Ingress.CaddyAdminURL)
	c.Observability.LokiURL = strings.TrimSpace(c.Observability.LokiURL)
	c.Observability.OTLPEndpoint = strings.TrimSpace(c.Observability.OTLPEndpoint)
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Server.ListenGRPCAddr) == "" {
		return fmt.Errorf("server.listenGRPCAddr is required")
	}
	if strings.TrimSpace(c.Database.URL) == "" {
		return fmt.Errorf("database.url is required")
	}
	if strings.TrimSpace(c.Plane.Name) == "" {
		return fmt.Errorf("plane.name is required")
	}
	if strings.TrimSpace(c.ControlPlane.BearerToken) == "" {
		return fmt.Errorf("controlPlane.bearerToken is required")
	}
	if strings.TrimSpace(c.NodeAgent.BootstrapToken) == "" {
		return fmt.Errorf("nodeAgent.bootstrapToken is required")
	}
	if strings.TrimSpace(c.NodeAgent.ConnectEndpoint) == "" {
		return fmt.Errorf("nodeAgent.connectEndpoint is required")
	}
	if err := validateConnectEndpoint(c.NodeAgent.ConnectEndpoint); err != nil {
		return err
	}
	provider := strings.ToLower(strings.TrimSpace(c.Infrastructure.Provider))
	if provider == "" {
		return fmt.Errorf("infrastructure.provider is required")
	}
	if provider != "aliyun" && provider != "tencent" {
		return fmt.Errorf("infrastructure.provider must be aliyun or tencent")
	}
	if strings.TrimSpace(c.Infrastructure.RegionID) == "" {
		return fmt.Errorf("infrastructure.regionId is required")
	}
	if strings.TrimSpace(c.RuntimeProvisioning.InstanceType) == "" {
		return fmt.Errorf("runtimeProvisioning.instanceType is required")
	}
	if len(c.RuntimeProvisioning.ProviderSpec) == 0 {
		return fmt.Errorf("runtimeProvisioning.providerSpec is required")
	}
	if strings.TrimSpace(c.RuntimeProvisioning.EgressProxyEndpoint) != "" {
		if err := validateProxyEndpoint(c.RuntimeProvisioning.EgressProxyEndpoint); err != nil {
			return err
		}
	}
	if strings.TrimSpace(c.Ingress.BaseDomain) != "" {
		if strings.TrimSpace(c.Ingress.CaddyAdminURL) == "" {
			return fmt.Errorf("ingress.caddyAdminURL is required when ingress is enabled")
		}
		if err := validateCaddyAdminURL(c.Ingress.CaddyAdminURL); err != nil {
			return err
		}
	}
	if strings.TrimSpace(c.NodeAgent.BinaryURL) == "" {
		return fmt.Errorf("nodeAgent.binaryUrl is required")
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

func validateProxyEndpoint(value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("parse runtimeProvisioning.egressProxyEndpoint: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("runtimeProvisioning.egressProxyEndpoint must use http or https")
	}
	if parsed.Host == "" {
		return fmt.Errorf("runtimeProvisioning.egressProxyEndpoint must include host")
	}
	return nil
}

func validateCaddyAdminURL(value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("parse ingress.caddyAdminURL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("ingress.caddyAdminURL must use http or https")
	}
	if parsed.Host == "" {
		return fmt.Errorf("ingress.caddyAdminURL must include host")
	}
	if parsed.Port() == "" {
		return fmt.Errorf("ingress.caddyAdminURL must include port")
	}
	host := strings.ToLower(parsed.Hostname())
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("ingress.caddyAdminURL must point to localhost")
	}
	return nil
}

func validateConnectEndpoint(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("nodeAgent.connectEndpoint is required")
	}
	if host, port, err := net.SplitHostPort(trimmed); err == nil && strings.TrimSpace(host) != "" && strings.TrimSpace(port) != "" {
		return nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("parse nodeAgent.connectEndpoint: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("nodeAgent.connectEndpoint must be host:port or use http/https gRPC URI")
	}
	if parsed.Host == "" {
		return fmt.Errorf("nodeAgent.connectEndpoint must include host")
	}
	return nil
}
