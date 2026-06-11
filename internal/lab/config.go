package lab

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Path          string          `yaml:"-"`
	Terraform     TerraformConfig `yaml:"terraform"`
	SSH           SSHConfig       `yaml:"ssh"`
	Install       InstallConfig   `yaml:"install"`
	Binaries      BinaryConfig    `yaml:"binaries"`
	Tokens        TokenConfig     `yaml:"tokens"`
	Provider      ProviderConfig  `yaml:"provider"`
	Observability Observability   `yaml:"observability"`
}

type TerraformConfig struct {
	Dir         string   `yaml:"dir"`
	ApplyArgs   []string `yaml:"applyArgs"`
	DestroyArgs []string `yaml:"destroyArgs"`
}

type SSHConfig struct {
	Host    string   `yaml:"host"`
	KeyPath string   `yaml:"keyPath"`
	Options []string `yaml:"options"`
}

type InstallConfig struct {
	Root                 string `yaml:"root"`
	ControlPlaneHTTPAddr string `yaml:"controlPlaneHTTPAddr"`
	IngressBaseDomain    string `yaml:"ingressBaseDomain"`
	RegistryMirror       string `yaml:"registryMirror"`
}

type BinaryConfig struct {
	ControlPlane string `yaml:"controlPlane"`
	CloudPlane   string `yaml:"cloudPlane"`
	NodeAgent    string `yaml:"nodeAgent"`
}

type TokenConfig struct {
	ControlPlaneAdmin      string `yaml:"controlPlaneAdmin"`
	ControlPlaneSouthbound string `yaml:"controlPlaneSouthbound"`
	NodeAgentBootstrap     string `yaml:"nodeAgentBootstrap"`
}

type ProviderConfig struct {
	TencentCredentialFile string `yaml:"tencentCredentialFile"`
}

type Observability struct {
	WorkloadLogLokiURL      string `yaml:"workloadLogLokiURL"`
	WorkloadLogLokiTenantID string `yaml:"workloadLogLokiTenantID"`
	WorkloadOTLPEndpoint    string `yaml:"workloadOTLPEndpoint"`
}

func LoadConfig(path string) (Config, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Config{}, fmt.Errorf("lab config path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read lab config %q: %w", path, err)
	}

	var cfg Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse lab config %q: %w", path, err)
	}
	cfg.Path = path
	cfg.applyDefaults()
	if err := cfg.validateBase(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) applyDefaults() {
	c.Terraform.Dir = defaultString(c.Terraform.Dir, "deploy/terraform/lab")
	if c.Terraform.ApplyArgs == nil {
		c.Terraform.ApplyArgs = []string{"-auto-approve"}
	}
	if c.Terraform.DestroyArgs == nil {
		c.Terraform.DestroyArgs = []string{"-auto-approve"}
	}

	c.Install.Root = defaultString(c.Install.Root, "/opt/mini-cloud")
	c.Install.ControlPlaneHTTPAddr = defaultString(c.Install.ControlPlaneHTTPAddr, "127.0.0.1:18080")
	c.Install.IngressBaseDomain = strings.Trim(strings.TrimSpace(c.Install.IngressBaseDomain), ".")

	c.Binaries.ControlPlane = defaultString(c.Binaries.ControlPlane, "dist/release/linux-amd64/control-plane")
	c.Binaries.CloudPlane = defaultString(c.Binaries.CloudPlane, "dist/release/linux-amd64/cloud-plane")
	c.Binaries.NodeAgent = defaultString(c.Binaries.NodeAgent, "dist/release/linux-amd64/node-agent")

	c.Provider.TencentCredentialFile = defaultString(c.Provider.TencentCredentialFile, filepath.Join(os.Getenv("HOME"), ".tccli/default.credential"))
	c.Provider.TencentCredentialFile = expandHome(c.Provider.TencentCredentialFile)

	c.Tokens.ControlPlaneAdmin = strings.TrimSpace(c.Tokens.ControlPlaneAdmin)
	c.Tokens.ControlPlaneSouthbound = strings.TrimSpace(c.Tokens.ControlPlaneSouthbound)
	c.Tokens.NodeAgentBootstrap = strings.TrimSpace(c.Tokens.NodeAgentBootstrap)
}

func expandHome(path string) string {
	path = strings.TrimSpace(path)
	if path == "~" {
		return os.Getenv("HOME")
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(os.Getenv("HOME"), strings.TrimPrefix(path, "~/"))
	}
	return path
}

func (c Config) validateBase() error {
	if strings.TrimSpace(c.Terraform.Dir) == "" {
		return fmt.Errorf("terraform.dir is required")
	}
	if strings.TrimSpace(c.Install.Root) == "" {
		return fmt.Errorf("install.root is required")
	}
	return nil
}

func (c Config) validateInstall() error {
	if strings.TrimSpace(c.Tokens.ControlPlaneAdmin) == "" {
		return fmt.Errorf("tokens.controlPlaneAdmin is required")
	}
	if strings.TrimSpace(c.Tokens.ControlPlaneSouthbound) == "" {
		return fmt.Errorf("tokens.controlPlaneSouthbound is required")
	}
	if strings.TrimSpace(c.Tokens.NodeAgentBootstrap) == "" {
		return fmt.Errorf("tokens.nodeAgentBootstrap is required")
	}
	if err := requireFile(c.Binaries.ControlPlane); err != nil {
		return err
	}
	if err := requireFile(c.Binaries.CloudPlane); err != nil {
		return err
	}
	if err := requireFile(c.Binaries.NodeAgent); err != nil {
		return err
	}
	return nil
}

func defaultString(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func requireFile(path string) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s does not exist; run make build-release first or update deploy/lab/lab.yaml", path)
		}
		return fmt.Errorf("stat %s: %w", path, err)
	}
	return nil
}
