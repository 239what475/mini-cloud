package config

import (
	"bytes"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	NodeNameSuffix          = "-node"
	CaddyListenHTTPAddr     = "0.0.0.0:80"
	CaddyArtifactListenAddr = "0.0.0.0:18082"
	CaddyArtifactRoot       = "/opt/mini-cloud/artifacts"
)

type Config struct {
	Path             string                 `yaml:"-"`
	Server           ServerConfig           `yaml:"server"`
	Database         DatabaseConfig         `yaml:"database"`
	Plane            PlaneConfig            `yaml:"plane"`
	ControlPlane     ControlPlaneConfig     `yaml:"controlPlane"`
	NodeAgent        NodeAgentConfig        `yaml:"nodeAgent"`
	Infrastructure   InfrastructureConfig   `yaml:"infrastructure"`
	NodeProvisioning NodeProvisioningConfig `yaml:"nodeProvisioning"`
	Ingress          IngressConfig          `yaml:"ingress"`
	Observability    ObservabilityConfig    `yaml:"observability"`
}

type ServerConfig struct {
	ListenGRPCAddr string `yaml:"listenGRPCAddr"`
}

type DatabaseConfig struct {
	URL string `yaml:"url"`
}

type PlaneConfig struct {
	Name         string `yaml:"name"`
	GRPCEndpoint string `yaml:"grpcEndpoint"`
}

type ControlPlaneConfig struct {
	URL         string `yaml:"url"`
	BearerToken string `yaml:"bearerToken"`
}

type NodeAgentConfig struct {
	ConnectEndpoint string `yaml:"connectEndpoint"`
	Token           string `yaml:"token"`
	BinaryURL       string `yaml:"binaryUrl"`
}

type InfrastructureConfig struct {
	Provider          string                  `yaml:"provider"`
	RegionID          string                  `yaml:"regionId"`
	ZoneID            string                  `yaml:"zoneId"`
	TencentCredential TencentCredentialConfig `yaml:"tencentCredential"`
}

type TencentCredentialConfig struct {
	SecretID  string `yaml:"secretId"`
	SecretKey string `yaml:"secretKey"`
	Token     string `yaml:"token"`
}

type NodeProvisioningConfig struct {
	InstanceType                string            `yaml:"instanceType"`
	RegistryMirrors             []string          `yaml:"registryMirrors"`
	WorkloadEgressProxyEndpoint string            `yaml:"workloadEgressProxyEndpoint"`
	Aliyun                      AliyunNodeConfig  `yaml:"aliyun"`
	Tencent                     TencentNodeConfig `yaml:"tencent"`
}

type AliyunNodeConfig struct {
	ImageID            string `yaml:"imageId"`
	KeyPairName        string `yaml:"keyPairName"`
	VSwitchID          string `yaml:"vSwitchId"`
	SecurityGroupID    string `yaml:"securityGroupId"`
	SystemDiskCategory string `yaml:"systemDiskCategory"`
	SystemDiskSizeGiB  int    `yaml:"systemDiskSizeGiB"`
}

type TencentNodeConfig struct {
	ImageID           string   `yaml:"imageId"`
	KeyIDs            []string `yaml:"keyIds"`
	VPCID             string   `yaml:"vpcId"`
	SubnetID          string   `yaml:"subnetId"`
	SecurityGroupIDs  []string `yaml:"securityGroupIds"`
	SystemDiskType    string   `yaml:"systemDiskType"`
	SystemDiskSizeGiB int64    `yaml:"systemDiskSizeGiB"`
}

type IngressConfig struct {
	BaseDomain    string `yaml:"baseDomain"`
	CaddyAdminURL string `yaml:"caddyAdminURL"`
	PublicOrigin  string `yaml:"publicOrigin"`
}

type ObservabilityConfig struct {
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
	c.Plane.GRPCEndpoint = strings.TrimSpace(c.Plane.GRPCEndpoint)
	c.ControlPlane.URL = strings.TrimRight(strings.TrimSpace(c.ControlPlane.URL), "/")
	c.ControlPlane.BearerToken = strings.TrimSpace(c.ControlPlane.BearerToken)
	c.Infrastructure.Provider = strings.ToLower(strings.TrimSpace(c.Infrastructure.Provider))
	c.Infrastructure.RegionID = strings.TrimSpace(c.Infrastructure.RegionID)
	c.Infrastructure.ZoneID = strings.TrimSpace(c.Infrastructure.ZoneID)
	c.Infrastructure.TencentCredential.SecretID = strings.TrimSpace(c.Infrastructure.TencentCredential.SecretID)
	c.Infrastructure.TencentCredential.SecretKey = strings.TrimSpace(c.Infrastructure.TencentCredential.SecretKey)
	c.Infrastructure.TencentCredential.Token = strings.TrimSpace(c.Infrastructure.TencentCredential.Token)
	c.NodeAgent.ConnectEndpoint = strings.TrimSpace(c.NodeAgent.ConnectEndpoint)
	c.NodeAgent.Token = strings.TrimSpace(c.NodeAgent.Token)
	c.NodeAgent.BinaryURL = strings.TrimSpace(c.NodeAgent.BinaryURL)
	c.NodeProvisioning.InstanceType = strings.TrimSpace(c.NodeProvisioning.InstanceType)
	c.NodeProvisioning.RegistryMirrors = trimStringList(c.NodeProvisioning.RegistryMirrors)
	c.NodeProvisioning.WorkloadEgressProxyEndpoint = strings.TrimSpace(c.NodeProvisioning.WorkloadEgressProxyEndpoint)
	c.NodeProvisioning.Aliyun.ImageID = strings.TrimSpace(c.NodeProvisioning.Aliyun.ImageID)
	c.NodeProvisioning.Aliyun.KeyPairName = strings.TrimSpace(c.NodeProvisioning.Aliyun.KeyPairName)
	c.NodeProvisioning.Aliyun.VSwitchID = strings.TrimSpace(c.NodeProvisioning.Aliyun.VSwitchID)
	c.NodeProvisioning.Aliyun.SecurityGroupID = strings.TrimSpace(c.NodeProvisioning.Aliyun.SecurityGroupID)
	c.NodeProvisioning.Aliyun.SystemDiskCategory = strings.TrimSpace(c.NodeProvisioning.Aliyun.SystemDiskCategory)
	c.NodeProvisioning.Tencent.ImageID = strings.TrimSpace(c.NodeProvisioning.Tencent.ImageID)
	c.NodeProvisioning.Tencent.KeyIDs = trimStringList(c.NodeProvisioning.Tencent.KeyIDs)
	c.NodeProvisioning.Tencent.VPCID = strings.TrimSpace(c.NodeProvisioning.Tencent.VPCID)
	c.NodeProvisioning.Tencent.SubnetID = strings.TrimSpace(c.NodeProvisioning.Tencent.SubnetID)
	c.NodeProvisioning.Tencent.SecurityGroupIDs = trimStringList(c.NodeProvisioning.Tencent.SecurityGroupIDs)
	c.NodeProvisioning.Tencent.SystemDiskType = strings.TrimSpace(c.NodeProvisioning.Tencent.SystemDiskType)
	c.Ingress.BaseDomain = strings.Trim(strings.ToLower(strings.TrimSpace(c.Ingress.BaseDomain)), ".")
	c.Ingress.CaddyAdminURL = strings.TrimSpace(c.Ingress.CaddyAdminURL)
	c.Ingress.PublicOrigin = strings.TrimSpace(c.Ingress.PublicOrigin)
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
	if strings.TrimSpace(c.Plane.GRPCEndpoint) == "" {
		return fmt.Errorf("plane.grpcEndpoint is required")
	}
	if err := validateGRPCEndpoint(c.Plane.GRPCEndpoint); err != nil {
		return err
	}
	if strings.TrimSpace(c.ControlPlane.URL) == "" {
		return fmt.Errorf("controlPlane.url is required")
	}
	if err := validateControlPlaneURL(c.ControlPlane.URL); err != nil {
		return err
	}
	if strings.TrimSpace(c.ControlPlane.BearerToken) == "" {
		return fmt.Errorf("controlPlane.bearerToken is required")
	}
	if strings.TrimSpace(c.NodeAgent.Token) == "" {
		return fmt.Errorf("nodeAgent.token is required")
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
	if provider == "tencent" {
		if strings.TrimSpace(c.Infrastructure.TencentCredential.SecretID) == "" {
			return fmt.Errorf("infrastructure.tencentCredential.secretId is required when provider is tencent")
		}
		if strings.TrimSpace(c.Infrastructure.TencentCredential.SecretKey) == "" {
			return fmt.Errorf("infrastructure.tencentCredential.secretKey is required when provider is tencent")
		}
	}
	if strings.TrimSpace(c.Infrastructure.RegionID) == "" {
		return fmt.Errorf("infrastructure.regionId is required")
	}
	if strings.TrimSpace(c.NodeProvisioning.InstanceType) == "" {
		return fmt.Errorf("nodeProvisioning.instanceType is required")
	}
	if provider == "aliyun" {
		if err := c.NodeProvisioning.Aliyun.Validate(); err != nil {
			return err
		}
	}
	if provider == "tencent" {
		if err := c.NodeProvisioning.Tencent.Validate(); err != nil {
			return err
		}
	}
	if strings.TrimSpace(c.NodeProvisioning.WorkloadEgressProxyEndpoint) != "" {
		if err := validateProxyEndpoint(c.NodeProvisioning.WorkloadEgressProxyEndpoint); err != nil {
			return err
		}
	}
	publicOriginConfigured := strings.TrimSpace(c.Ingress.PublicOrigin) != ""
	if strings.TrimSpace(c.Ingress.CaddyAdminURL) != "" {
		if err := validateCaddyAdminURL(c.Ingress.CaddyAdminURL); err != nil {
			return err
		}
	}
	if strings.TrimSpace(c.Ingress.BaseDomain) != "" {
		if strings.TrimSpace(c.Ingress.CaddyAdminURL) == "" {
			return fmt.Errorf("ingress.caddyAdminURL is required when ingress is enabled")
		}
	} else if publicOriginConfigured {
		return fmt.Errorf("ingress.baseDomain is required when ingress.publicOrigin is configured")
	}
	if strings.TrimSpace(c.NodeAgent.BinaryURL) == "" {
		return fmt.Errorf("nodeAgent.binaryUrl is required")
	}
	return nil
}

func (c AliyunNodeConfig) Validate() error {
	if strings.TrimSpace(c.ImageID) == "" {
		return fmt.Errorf("nodeProvisioning.aliyun.imageId is required")
	}
	if strings.TrimSpace(c.VSwitchID) == "" {
		return fmt.Errorf("nodeProvisioning.aliyun.vSwitchId is required")
	}
	if strings.TrimSpace(c.SecurityGroupID) == "" {
		return fmt.Errorf("nodeProvisioning.aliyun.securityGroupId is required")
	}
	if c.SystemDiskSizeGiB < 0 {
		return fmt.Errorf("nodeProvisioning.aliyun.systemDiskSizeGiB must not be negative")
	}
	return nil
}

func (c TencentNodeConfig) Validate() error {
	if strings.TrimSpace(c.ImageID) == "" {
		return fmt.Errorf("nodeProvisioning.tencent.imageId is required")
	}
	if strings.TrimSpace(c.VPCID) == "" {
		return fmt.Errorf("nodeProvisioning.tencent.vpcId is required")
	}
	if strings.TrimSpace(c.SubnetID) == "" {
		return fmt.Errorf("nodeProvisioning.tencent.subnetId is required")
	}
	if len(c.SecurityGroupIDs) == 0 {
		return fmt.Errorf("nodeProvisioning.tencent.securityGroupIds is required")
	}
	if c.SystemDiskSizeGiB < 0 {
		return fmt.Errorf("nodeProvisioning.tencent.systemDiskSizeGiB must not be negative")
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
		return fmt.Errorf("parse nodeProvisioning.workloadEgressProxyEndpoint: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("nodeProvisioning.workloadEgressProxyEndpoint must use http or https")
	}
	if parsed.Host == "" {
		return fmt.Errorf("nodeProvisioning.workloadEgressProxyEndpoint must include host")
	}
	return nil
}

func validateControlPlaneURL(value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("parse controlPlane.url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("controlPlane.url must use http or https")
	}
	if parsed.Host == "" {
		return fmt.Errorf("controlPlane.url must include host")
	}
	return nil
}

func validateGRPCEndpoint(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("plane.grpcEndpoint is required")
	}
	if strings.ContainsAny(trimmed, " \t\r\n") {
		return fmt.Errorf("plane.grpcEndpoint must not contain whitespace")
	}
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return fmt.Errorf("plane.grpcEndpoint must be a gRPC target, not an HTTP URL")
	}
	if strings.HasPrefix(lower, "grpc://") || strings.HasPrefix(lower, "grpcs://") {
		_, port, err := net.SplitHostPort(strings.TrimSpace(trimmed[strings.Index(trimmed, "://")+3:]))
		if err != nil || strings.TrimSpace(port) == "" {
			return fmt.Errorf("plane.grpcEndpoint must include host:port")
		}
		return nil
	}
	if strings.HasPrefix(lower, "dns:///") || strings.HasPrefix(lower, "unix:///") {
		return nil
	}
	if _, port, err := net.SplitHostPort(trimmed); err == nil && strings.TrimSpace(port) != "" {
		return nil
	}
	return fmt.Errorf("plane.grpcEndpoint must include host:port")
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
		return fmt.Errorf("nodeAgent.connectEndpoint must be host:port or an http/https target")
	}
	if parsed.Host == "" {
		return fmt.Errorf("nodeAgent.connectEndpoint must include host")
	}
	return nil
}
