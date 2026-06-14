package config

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Path   string        `yaml:"-"`
	Server ServerConfig  `yaml:"server"`
	UI     UIConfig      `yaml:"ui"`
	Auth   AuthConfig    `yaml:"auth"`
	DNS    DNSConfig     `yaml:"dns"`
	Planes []PlaneConfig `yaml:"planes"`
}

type ServerConfig struct {
	HTTPAddr string `yaml:"httpAddr"`
}

type UIConfig struct {
	Dir string `yaml:"dir"`
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
	Domain string `yaml:"domain"`
}

type PlaneConfig struct {
	ID           string `yaml:"id"`
	Name         string `yaml:"name"`
	DisplayName  string `yaml:"displayName"`
	Provider     string `yaml:"provider"`
	Region       string `yaml:"region"`
	GRPCEndpoint string `yaml:"grpcEndpoint"`
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
	c.Auth.AdminToken = strings.TrimSpace(c.Auth.AdminToken)
	c.Auth.SouthboundToken = strings.TrimSpace(c.Auth.SouthboundToken)
	c.DNS.ServiceBaseDomain = strings.Trim(strings.ToLower(strings.TrimSpace(c.DNS.ServiceBaseDomain)), ".")
	c.DNS.DNSPod.Domain = strings.Trim(strings.ToLower(strings.TrimSpace(c.DNS.DNSPod.Domain)), ".")
	for i := range c.Planes {
		plane := &c.Planes[i]
		plane.ID = strings.TrimSpace(plane.ID)
		plane.Name = strings.TrimSpace(plane.Name)
		plane.DisplayName = strings.TrimSpace(plane.DisplayName)
		if plane.DisplayName == "" {
			plane.DisplayName = plane.Name
		}
		plane.Provider = strings.ToLower(strings.TrimSpace(plane.Provider))
		plane.Region = strings.TrimSpace(plane.Region)
		plane.GRPCEndpoint = strings.TrimSpace(plane.GRPCEndpoint)
	}
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Auth.AdminToken) == "" {
		return fmt.Errorf("auth.adminToken is required")
	}
	if strings.TrimSpace(c.Auth.SouthboundToken) == "" {
		return fmt.Errorf("auth.southboundToken is required")
	}
	if strings.TrimSpace(c.DNS.ServiceBaseDomain) == "" {
		return fmt.Errorf("dns.serviceBaseDomain is required")
	}
	if strings.TrimSpace(c.DNS.DNSPod.Domain) == "" {
		return fmt.Errorf("dns.dnspod.domain is required")
	}
	if len(c.Planes) == 0 {
		return fmt.Errorf("planes is required")
	}
	seenIDs := map[string]bool{}
	seenNames := map[string]bool{}
	for _, plane := range c.Planes {
		if strings.TrimSpace(plane.ID) == "" {
			return fmt.Errorf("planes.id is required")
		}
		if seenIDs[plane.ID] {
			return fmt.Errorf("duplicate plane id %q", plane.ID)
		}
		seenIDs[plane.ID] = true
		if strings.TrimSpace(plane.Name) == "" {
			return fmt.Errorf("plane %q name is required", plane.ID)
		}
		if seenNames[plane.Name] {
			return fmt.Errorf("duplicate plane name %q", plane.Name)
		}
		seenNames[plane.Name] = true
		if plane.Provider != "aliyun" && plane.Provider != "tencent" {
			return fmt.Errorf("plane %q provider must be aliyun or tencent", plane.ID)
		}
		if strings.TrimSpace(plane.Region) == "" {
			return fmt.Errorf("plane %q region is required", plane.ID)
		}
		if strings.TrimSpace(plane.GRPCEndpoint) == "" {
			return fmt.Errorf("plane %q grpcEndpoint is required", plane.ID)
		}
		if strings.ContainsAny(plane.GRPCEndpoint, " \t\r\n") {
			return fmt.Errorf("plane %q grpcEndpoint must not contain whitespace", plane.ID)
		}
		lower := strings.ToLower(plane.GRPCEndpoint)
		if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
			return fmt.Errorf("plane %q grpcEndpoint must be a gRPC target, not an HTTP URL", plane.ID)
		}
	}
	return nil
}
