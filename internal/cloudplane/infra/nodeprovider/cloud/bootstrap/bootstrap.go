package bootstrap

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
)

type Config struct {
	AgentBinaryURL         string
	CACert                 string
	Token                  string
	ConnectEndpoint        string
	DockerRegistryMirrors  []string
	InstanceName           string
	InstanceType           string
	MetadataBase           string
	MetadataGetFunction    string
	MetadataInit           string
	MetadataInstanceIDPath string
	MetadataPrivateIPPath  string
	NoProxyItems           []string
	PlatformName           string
	Provider               string
	Region                 string
	WorkloadProxyEndpoint  string
	WorkloadOTLPEndpoint   string
	CPUMilli               int
	MemoryMi               int
}

type templateData struct {
	InstallRoot            string
	AgentBinaryURL         string
	CACertBase64           string
	DockerDaemonJSONBase64 string
	DockerNoProxy          string
	WorkloadProxyEndpoint  string
	Token                  string
	BootstrapLog           string
	MetadataBase           string
	MetadataGetFunction    string
	MetadataInit           string
	MetadataInstanceIDPath string
	MetadataPrivateIPPath  string
	InstanceName           string
	ConnectEndpoint        string
	PlatformName           string
	Provider               string
	Region                 string
	InstanceType           string
	CPUMilli               int
	MemoryMi               int
	NoProxyItems           []string
	WorkloadOTLPEndpoint   string
	NodeAgentBinaryPath    string
	NodeAgentConfigPath    string
}

//go:embed node_bootstrap.sh.tmpl
var nodeBootstrapTemplate string

func RenderBase64(cfg Config) (string, error) {
	dockerDaemonJSON, err := buildDockerDaemonJSON(cfg.DockerRegistryMirrors)
	if err != nil {
		return "", err
	}

	workloadProxyEndpoint := strings.TrimSpace(cfg.WorkloadProxyEndpoint)
	data := templateData{
		InstallRoot:            shellQuote("/opt/mini-cloud"),
		AgentBinaryURL:         shellQuote(cfg.AgentBinaryURL),
		CACertBase64:           shellQuote(base64.StdEncoding.EncodeToString([]byte(strings.TrimSpace(cfg.CACert)))),
		DockerDaemonJSONBase64: shellQuote(base64.StdEncoding.EncodeToString([]byte(dockerDaemonJSON))),
		DockerNoProxy:          shellQuote(strings.Join(cleanNoProxyItems(cfg.NoProxyItems), ",")),
		WorkloadProxyEndpoint:  shellQuote(workloadProxyEndpoint),
		Token:                  shellQuote(strings.TrimSpace(cfg.Token)),
		BootstrapLog:           shellQuote("/var/log/mini-cloud-node-bootstrap.log"),
		MetadataBase:           shellQuote(cfg.MetadataBase),
		MetadataGetFunction:    strings.TrimSpace(cfg.MetadataGetFunction),
		MetadataInit:           strings.TrimSpace(cfg.MetadataInit),
		MetadataInstanceIDPath: shellQuote(cfg.MetadataInstanceIDPath),
		MetadataPrivateIPPath:  shellQuote(cfg.MetadataPrivateIPPath),
		InstanceName:           shellQuote(cfg.InstanceName),
		ConnectEndpoint:        shellQuote(strings.TrimRight(cfg.ConnectEndpoint, "/")),
		PlatformName:           shellQuote(cfg.PlatformName),
		Provider:               shellQuote(cfg.Provider),
		Region:                 shellQuote(cfg.Region),
		InstanceType:           shellQuote(cfg.InstanceType),
		CPUMilli:               cfg.CPUMilli,
		MemoryMi:               cfg.MemoryMi,
		NoProxyItems:           shellQuoteItems(cfg.NoProxyItems),
		WorkloadOTLPEndpoint:   shellQuote(strings.TrimSpace(cfg.WorkloadOTLPEndpoint)),
		NodeAgentBinaryPath:    shellQuote("/opt/mini-cloud/bin/node-agent"),
		NodeAgentConfigPath:    shellQuote("/opt/mini-cloud/node-agent.yaml"),
	}
	tmpl, err := template.New("node-bootstrap").Option("missingkey=error").Parse(nodeBootstrapTemplate)
	if err != nil {
		return "", fmt.Errorf("parse node bootstrap template: %w", err)
	}
	var script bytes.Buffer
	if err := tmpl.Execute(&script, data); err != nil {
		return "", fmt.Errorf("render node bootstrap template: %w", err)
	}
	return base64.StdEncoding.EncodeToString(script.Bytes()), nil
}

func cleanNoProxyItems(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func shellQuote(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, ch := range value {
		switch ch {
		case '\\', '"', '$', '`':
			b.WriteByte('\\')
			b.WriteRune(ch)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(ch)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func shellQuoteItems(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, shellQuote(item))
	}
	return out
}

func buildDockerDaemonJSON(mirrors []string) (string, error) {
	type dockerDaemonConfig struct {
		RegistryMirrors []string `json:"registry-mirrors,omitempty"`
	}

	cleaned := make([]string, 0, len(mirrors))
	for _, mirror := range mirrors {
		trimmed := strings.TrimSpace(mirror)
		if trimmed == "" {
			continue
		}
		cleaned = append(cleaned, trimmed)
	}
	if len(cleaned) == 0 {
		return "", nil
	}
	data, err := json.MarshalIndent(dockerDaemonConfig{RegistryMirrors: cleaned}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("render docker daemon config: %w", err)
	}
	return string(data) + "\n", nil
}
