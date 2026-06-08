package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type fileConfig struct {
	Server   serverConfig   `yaml:"server"`
	UI       uiConfig       `yaml:"ui"`
	Database databaseConfig `yaml:"database"`
	Auth     authConfig     `yaml:"auth"`
	Sync     syncConfig     `yaml:"sync"`
	Service  serviceConfig  `yaml:"service"`
	Logs     logsConfig     `yaml:"logs"`
}

type serverConfig struct {
	HTTPAddr string `yaml:"httpAddr"`
}

type uiConfig struct {
	Dir string `yaml:"dir"`
}

type databaseConfig struct {
	URL string `yaml:"url"`
}

type authConfig struct {
	AdminToken string `yaml:"adminToken"`
}

type syncConfig struct {
	PlaneIntervalSeconds int `yaml:"planeIntervalSeconds"`
}

type serviceConfig struct {
	ReconcileTimeoutSeconds int `yaml:"reconcileTimeoutSeconds"`
}

type logsConfig struct {
	Loki lokiConfig `yaml:"loki"`
}

type lokiConfig struct {
	URL                 string `yaml:"url"`
	TenantID            string `yaml:"tenantID"`
	QueryTimeoutSeconds int    `yaml:"queryTimeoutSeconds"`
}

type Config struct {
	Path                           string
	HTTPAddr                       string
	UIDir                          string
	DatabaseURL                    string
	AdminToken                     string
	PlaneSyncIntervalSeconds       int
	ServiceReconcileTimeoutSeconds int
	LokiURL                        string
	LokiTenantID                   string
	LokiQueryTimeoutSeconds        int
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

	var file fileConfig
	if err := yaml.Unmarshal(raw, &file); err != nil {
		return Config{}, fmt.Errorf("parse config file: %w", err)
	}

	cfg := Config{
		Path:                           path,
		HTTPAddr:                       defaultString(file.Server.HTTPAddr, ":8080"),
		UIDir:                          defaultString(file.UI.Dir, "web/dist"),
		DatabaseURL:                    strings.TrimSpace(file.Database.URL),
		AdminToken:                     strings.TrimSpace(file.Auth.AdminToken),
		PlaneSyncIntervalSeconds:       defaultPositiveInt(file.Sync.PlaneIntervalSeconds, 30),
		ServiceReconcileTimeoutSeconds: defaultPositiveInt(file.Service.ReconcileTimeoutSeconds, 1200),
		LokiURL:                        strings.TrimSpace(file.Logs.Loki.URL),
		LokiTenantID:                   strings.TrimSpace(file.Logs.Loki.TenantID),
		LokiQueryTimeoutSeconds:        defaultPositiveInt(file.Logs.Loki.QueryTimeoutSeconds, 5),
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.DatabaseURL) == "" {
		return fmt.Errorf("database.url is required")
	}
	if strings.TrimSpace(c.AdminToken) == "" {
		return fmt.Errorf("auth.adminToken is required")
	}
	if c.PlaneSyncIntervalSeconds <= 0 {
		return fmt.Errorf("sync.planeIntervalSeconds must be positive")
	}
	if c.ServiceReconcileTimeoutSeconds <= 0 {
		return fmt.Errorf("service.reconcileTimeoutSeconds must be positive")
	}
	if c.LokiQueryTimeoutSeconds <= 0 {
		return fmt.Errorf("logs.loki.queryTimeoutSeconds must be positive")
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

func defaultPositiveInt(value int, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}
