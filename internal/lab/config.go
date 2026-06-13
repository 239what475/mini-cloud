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
	Path          string         `yaml:"-"`
	ControlPlane  ControlPlane   `yaml:"controlPlane"`
	Planes        []Plane        `yaml:"planes"`
	SSH           SSHConfig      `yaml:"ssh"`
	Install       InstallConfig  `yaml:"install"`
	Binaries      BinaryConfig   `yaml:"binaries"`
	Tokens        TokenConfig    `yaml:"tokens"`
	Provider      ProviderConfig `yaml:"provider"`
	Observability Observability  `yaml:"observability"`
}

type TerraformConfig struct {
	Dir       string `yaml:"dir"`
	Workspace string `yaml:"workspace"`
	VarFile   string `yaml:"varFile"`
}

type SSHConfig struct {
	Host    string   `yaml:"host"`
	KeyPath string   `yaml:"keyPath"`
	Options []string `yaml:"options"`
}

type ControlPlane struct {
	SSH            SSHConfig `yaml:"ssh"`
	ListenHTTPAddr string    `yaml:"listenHTTPAddr"`
	URL            string    `yaml:"url"`
}

type Plane struct {
	Name           string          `yaml:"name"`
	Provider       string          `yaml:"provider"`
	Region         string          `yaml:"region"`
	SSH            SSHConfig       `yaml:"ssh"`
	Terraform      TerraformConfig `yaml:"terraform"`
	RegistryMirror string          `yaml:"registryMirror"`
}

type InstallConfig struct {
	Root              string `yaml:"root"`
	IngressBaseDomain string `yaml:"ingressBaseDomain"`
	RegistryMirror    string `yaml:"registryMirror"`
}

type BinaryConfig struct {
	ControlPlane string `yaml:"controlPlane"`
	CloudPlane   string `yaml:"cloudPlane"`
	NodeAgent    string `yaml:"nodeAgent"`
	WebDist      string `yaml:"webDist"`
}

type TokenConfig struct {
	ControlPlaneAdmin      string `yaml:"controlPlaneAdmin"`
	ControlPlaneSouthbound string `yaml:"controlPlaneSouthbound"`
	NodeAgent              string `yaml:"nodeAgent"`
}

type ProviderConfig struct {
	TencentCredentialFile string `yaml:"tencentCredentialFile"`
}

type Observability struct {
	WorkloadOTLPEndpoint string `yaml:"workloadOTLPEndpoint"`
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
	c.Install.Root = defaultString(c.Install.Root, "/opt/mini-cloud")
	c.Install.IngressBaseDomain = strings.Trim(strings.TrimSpace(c.Install.IngressBaseDomain), ".")
	c.ControlPlane.ListenHTTPAddr = defaultString(c.ControlPlane.ListenHTTPAddr, "0.0.0.0:18080")
	c.ControlPlane.URL = strings.TrimRight(strings.TrimSpace(c.ControlPlane.URL), "/")
	c.ControlPlane.SSH.applyDefaults(c.SSH)

	c.Binaries.ControlPlane = defaultString(c.Binaries.ControlPlane, "dist/release/linux-amd64/control-plane")
	c.Binaries.CloudPlane = defaultString(c.Binaries.CloudPlane, "dist/release/linux-amd64/cloud-plane")
	c.Binaries.NodeAgent = defaultString(c.Binaries.NodeAgent, "dist/release/linux-amd64/node-agent")
	c.Binaries.WebDist = defaultString(c.Binaries.WebDist, "web/dist")

	c.Provider.TencentCredentialFile = defaultString(c.Provider.TencentCredentialFile, "~/.tccli/default.credential")
	c.Provider.TencentCredentialFile = expandHome(c.Provider.TencentCredentialFile)

	c.Tokens.ControlPlaneAdmin = strings.TrimSpace(c.Tokens.ControlPlaneAdmin)
	c.Tokens.ControlPlaneSouthbound = strings.TrimSpace(c.Tokens.ControlPlaneSouthbound)
	c.Tokens.NodeAgent = strings.TrimSpace(c.Tokens.NodeAgent)

	for i := range c.Planes {
		plane := &c.Planes[i]
		plane.Name = strings.TrimSpace(plane.Name)
		plane.Provider = strings.ToLower(strings.TrimSpace(plane.Provider))
		plane.Region = strings.TrimSpace(plane.Region)
		plane.RegistryMirror = strings.TrimSpace(plane.RegistryMirror)
		plane.SSH.applyDefaults(c.SSH)
		plane.Terraform.Dir = defaultString(plane.Terraform.Dir, "deploy/terraform/lab")
		plane.Terraform.Workspace = defaultString(plane.Terraform.Workspace, plane.Name)
		plane.Terraform.VarFile = absolutePath(expandHome(plane.Terraform.VarFile))
	}
}

func expandHome(path string) string {
	path = strings.TrimSpace(path)
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	return path
}

func absolutePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return absolute
}

func (s *SSHConfig) applyDefaults(parent SSHConfig) {
	s.Host = defaultString(s.Host, parent.Host)
	s.KeyPath = defaultString(s.KeyPath, parent.KeyPath)
	if len(s.Options) == 0 {
		s.Options = append([]string(nil), parent.Options...)
	}
}

func (c Config) validateBase() error {
	if strings.TrimSpace(c.ControlPlane.SSH.Host) == "" {
		return fmt.Errorf("controlPlane.ssh.host is required")
	}
	if strings.TrimSpace(c.ControlPlane.URL) == "" {
		return fmt.Errorf("controlPlane.url is required")
	}
	if strings.TrimSpace(c.Install.Root) == "" {
		return fmt.Errorf("install.root is required")
	}
	if len(c.Planes) == 0 {
		return fmt.Errorf("planes is required")
	}
	seen := map[string]bool{}
	planeHosts := map[string]string{}
	for _, plane := range c.Planes {
		if strings.TrimSpace(plane.Name) == "" {
			return fmt.Errorf("planes.name is required")
		}
		if seen[plane.Name] {
			return fmt.Errorf("duplicate plane name %q", plane.Name)
		}
		seen[plane.Name] = true
		if plane.Provider != "aliyun" && plane.Provider != "tencent" {
			return fmt.Errorf("plane %q provider must be aliyun or tencent", plane.Name)
		}
		if strings.TrimSpace(plane.Region) == "" {
			return fmt.Errorf("plane %q region is required", plane.Name)
		}
		if strings.TrimSpace(plane.SSH.Host) == "" {
			return fmt.Errorf("plane %q ssh.host is required", plane.Name)
		}
		if owner := planeHosts[plane.SSH.Host]; owner != "" {
			return fmt.Errorf("planes %q and %q use the same ssh.host %q; a host can run only one cloud-plane", owner, plane.Name, plane.SSH.Host)
		}
		planeHosts[plane.SSH.Host] = plane.Name
		if strings.TrimSpace(plane.Terraform.Dir) == "" {
			return fmt.Errorf("plane %q terraform.dir is required", plane.Name)
		}
		if strings.TrimSpace(plane.Terraform.Workspace) == "" {
			return fmt.Errorf("plane %q terraform.workspace is required", plane.Name)
		}
		if strings.TrimSpace(plane.Terraform.VarFile) == "" {
			return fmt.Errorf("plane %q terraform.varFile is required", plane.Name)
		}
	}
	return nil
}

func (c Config) validateInstall() error {
	if strings.TrimSpace(c.Install.IngressBaseDomain) == "" {
		return fmt.Errorf("install.ingressBaseDomain is required")
	}
	if strings.TrimSpace(c.Tokens.ControlPlaneAdmin) == "" {
		return fmt.Errorf("tokens.controlPlaneAdmin is required")
	}
	if strings.TrimSpace(c.Tokens.ControlPlaneSouthbound) == "" {
		return fmt.Errorf("tokens.controlPlaneSouthbound is required")
	}
	if strings.TrimSpace(c.Tokens.NodeAgent) == "" {
		return fmt.Errorf("tokens.nodeAgent is required")
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
	if err := requireDir(c.Binaries.WebDist); err != nil {
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

func requireDir(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s does not exist; run npm --prefix web run build first or update deploy/lab/lab.yaml", path)
		}
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	return nil
}
