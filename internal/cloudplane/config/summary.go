package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

// Summary is the small redacted runtime config view synced to the control-plane.
type Summary struct {
	ObservedAt          time.Time        `json:"observedAt"`
	Fingerprint         string           `json:"fingerprint"`
	Plane               PlaneSummary     `json:"plane"`
	Provider            ProviderSummary  `json:"provider"`
	NodeAgent           NodeAgentSummary `json:"nodeAgent"`
	RuntimeProvisioning ProvisionSummary `json:"runtimeProvisioning"`
	Ingress             IngressSummary   `json:"ingress"`
	Observability       TelemetrySummary `json:"observability"`
}

type PlaneSummary struct {
	Name string `json:"name"`
}

type ProviderSummary struct {
	Name     string `json:"name"`
	RegionID string `json:"regionId"`
	ZoneID   string `json:"zoneId,omitempty"`
}

type NodeAgentSummary struct {
	ConnectEndpoint          string `json:"connectEndpoint"`
	BootstrapTokenConfigured bool   `json:"bootstrapTokenConfigured"`
	BinaryURL                string `json:"binaryUrl"`
	HostPortRangeMin         int    `json:"hostPortRangeMin"`
	HostPortRangeMax         int    `json:"hostPortRangeMax"`
}

type ProvisionSummary struct {
	ProviderSpecConfigured bool `json:"providerSpecConfigured"`
	RegistryMirrorsCount   int  `json:"registryMirrorsCount"`
	EgressProxyEnabled     bool `json:"egressProxyEnabled"`
}

type IngressSummary struct {
	Enabled    bool   `json:"enabled"`
	BaseDomain string `json:"baseDomain,omitempty"`
}

type TelemetrySummary struct {
	LogsConfigured   bool `json:"logsConfigured"`
	TracesConfigured bool `json:"tracesConfigured"`
}

func BuildSummary(cfg Config, observedAt time.Time) Summary {
	summary := Summary{
		ObservedAt: observedAt.UTC(),
		Plane: PlaneSummary{
			Name: strings.TrimSpace(cfg.Plane.Identity.Name),
		},
		Provider: ProviderSummary{
			Name:     strings.TrimSpace(cfg.Infrastructure.Provider),
			RegionID: strings.TrimSpace(cfg.Infrastructure.Location.RegionID),
			ZoneID:   strings.TrimSpace(cfg.Infrastructure.Location.ZoneID),
		},
		NodeAgent: NodeAgentSummary{
			ConnectEndpoint:          strings.TrimSpace(cfg.NodeAgent.ConnectEndpoint),
			BootstrapTokenConfigured: strings.TrimSpace(cfg.NodeAgent.Auth.BootstrapToken) != "",
			BinaryURL:                strings.TrimSpace(cfg.NodeAgent.Artifact.BinaryURL),
			HostPortRangeMin:         cfg.NodeAgent.Defaults.HostPortRange.Min,
			HostPortRangeMax:         cfg.NodeAgent.Defaults.HostPortRange.Max,
		},
		RuntimeProvisioning: ProvisionSummary{
			ProviderSpecConfigured: len(cfg.RuntimeProvisioning.ProviderSpec) > 0,
			RegistryMirrorsCount:   len(cfg.RuntimeProvisioning.ImagePull.RegistryMirrors),
			EgressProxyEnabled:     cfg.RuntimeProvisioning.Egress.Proxy.Enabled,
		},
		Ingress: IngressSummary{
			Enabled:    cfg.Ingress.Enabled,
			BaseDomain: strings.TrimSpace(cfg.Ingress.BaseDomain),
		},
		Observability: TelemetrySummary{
			LogsConfigured:   strings.TrimSpace(cfg.Observability.Logs.LokiURL) != "",
			TracesConfigured: strings.TrimSpace(cfg.Observability.Traces.OTLPEndpoint) != "",
		},
	}
	summary.Fingerprint = fingerprint(summary)
	return summary
}

func SummaryMap(summary Summary) (map[string]any, error) {
	out := map[string]any{}
	data, err := json.Marshal(summary)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func fingerprint(summary Summary) string {
	payload := summary
	payload.ObservedAt = time.Time{}
	payload.Fingerprint = ""
	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
