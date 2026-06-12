package config

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Path     string         `yaml:"-"`
	Server   ServerConfig   `yaml:"server"`
	UI       UIConfig       `yaml:"ui"`
	Database DatabaseConfig `yaml:"database"`
	Auth     AuthConfig     `yaml:"auth"`
	DNS      DNSConfig      `yaml:"dns"`
	Logs     LogsConfig     `yaml:"logs"`
}

type ServerConfig struct {
	HTTPAddr string `yaml:"httpAddr"`
}

type UIConfig struct {
	Dir string `yaml:"dir"`
}

type DatabaseConfig struct {
	URL string `yaml:"url"`
}

type AuthConfig struct {
	AdminToken      string `yaml:"adminToken"`
	SouthboundToken string `yaml:"southboundToken"`
}

type DNSConfig struct {
	ServiceBaseDomain string       `yaml:"serviceBaseDomain"`
	DNSPod            DNSPodConfig `yaml:"dnspod"`
}

type DNSPodConfig struct {
	Domain    string `yaml:"domain"`
	SecretID  string `yaml:"secretId"`
	SecretKey string `yaml:"secretKey"`
	Token     string `yaml:"token"`
}

type LogsConfig struct {
	Loki LokiConfig `yaml:"loki"`
}

type LokiConfig struct {
	URL      string `yaml:"url"`
	TenantID string `yaml:"tenantID"`
}

func Load(path string) (Config, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Config{}, fmt.Errorf("config path is required")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config file: %w", err)
	}

	var cfg Config
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config file: %w", err)
	}

	cfg.Path = path
	cfg.normalize()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) normalize() {
	c.Server.HTTPAddr = strings.TrimSpace(c.Server.HTTPAddr)
	if c.Server.HTTPAddr == "" {
		c.Server.HTTPAddr = ":8080"
	}
	c.UI.Dir = strings.TrimSpace(c.UI.Dir)
	if c.UI.Dir == "" {
		c.UI.Dir = "web/dist"
	}
	c.Database.URL = strings.TrimSpace(c.Database.URL)
	c.Auth.AdminToken = strings.TrimSpace(c.Auth.AdminToken)
	c.Auth.SouthboundToken = strings.TrimSpace(c.Auth.SouthboundToken)
	c.DNS.ServiceBaseDomain = strings.Trim(strings.ToLower(strings.TrimSpace(c.DNS.ServiceBaseDomain)), ".")
	c.DNS.DNSPod.Domain = strings.Trim(strings.ToLower(strings.TrimSpace(c.DNS.DNSPod.Domain)), ".")
	c.DNS.DNSPod.SecretID = strings.TrimSpace(c.DNS.DNSPod.SecretID)
	c.DNS.DNSPod.SecretKey = strings.TrimSpace(c.DNS.DNSPod.SecretKey)
	c.DNS.DNSPod.Token = strings.TrimSpace(c.DNS.DNSPod.Token)
	c.Logs.Loki.URL = strings.TrimSpace(c.Logs.Loki.URL)
	c.Logs.Loki.TenantID = strings.TrimSpace(c.Logs.Loki.TenantID)
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Database.URL) == "" {
		return fmt.Errorf("database.url is required")
	}
	if strings.TrimSpace(c.Auth.AdminToken) == "" {
		return fmt.Errorf("auth.adminToken is required")
	}
	if strings.TrimSpace(c.Auth.SouthboundToken) == "" {
		return fmt.Errorf("auth.southboundToken is required")
	}
	if strings.TrimSpace(c.DNS.ServiceBaseDomain) == "" {
		return fmt.Errorf("dns.serviceBaseDomain is required")
	}
	dnsConfigured := strings.TrimSpace(c.DNS.DNSPod.Domain) != "" ||
		strings.TrimSpace(c.DNS.DNSPod.SecretID) != "" ||
		strings.TrimSpace(c.DNS.DNSPod.SecretKey) != ""
	if dnsConfigured {
		if strings.TrimSpace(c.DNS.DNSPod.Domain) == "" {
			return fmt.Errorf("dns.dnspod.domain is required when dns.dnspod is configured")
		}
		if strings.TrimSpace(c.DNS.DNSPod.SecretID) == "" {
			return fmt.Errorf("dns.dnspod.secretId is required when dns.dnspod is configured")
		}
		if strings.TrimSpace(c.DNS.DNSPod.SecretKey) == "" {
			return fmt.Errorf("dns.dnspod.secretKey is required when dns.dnspod is configured")
		}
	}
	return nil
}
