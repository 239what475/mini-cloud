package lab

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type installFiles struct {
	ControlPlaneConfig string
	CloudPlaneConfig   string
	RemoteScript       string
}

type controlPlaneTemplateData struct {
	HTTPAddr        string
	InstallRoot     string
	AdminToken      string
	SouthboundToken string
	LokiURL         string
	LokiTenantID    string
}

type cloudPlaneTemplateData struct {
	ListenGRPCAddr              string
	PlaneName                   string
	PlaneGRPCEndpoint           string
	ControlPlaneURL             string
	SouthboundToken             string
	NodeAgentConnectEndpoint    string
	NodeAgentBootstrapToken     string
	NodeAgentBinaryURL          string
	Provider                    string
	RegionID                    string
	ZoneID                      string
	TencentCredential           tencentCredential
	InstanceType                string
	RegistryMirrors             []string
	WorkloadEgressProxyEndpoint string
	ProviderSpecYAML            string
	IngressBaseDomain           string
	LokiURL                     string
	OTLPEndpoint                string
}

type remoteInstallTemplateData struct {
	InstallRoot          string
	IngressHTTPPort      int
	EgressProxyPort      int
	ArtifactHTTPPort     int
	SubnetCIDRBlock      string
	RegistryMirror       string
	ControlPlaneHTTPPort string
	CloudPlaneGRPCPort   int
}

func (r *Runner) Install(ctx context.Context) error {
	if err := r.cfg.validateInstall(); err != nil {
		return err
	}
	out, err := r.terraformOutput(ctx)
	if err != nil {
		return err
	}
	host, err := r.platformHost(out)
	if err != nil {
		return err
	}

	platformPrivateIP := strings.TrimSpace(out.Platform.Value.PrivateIP)
	if platformPrivateIP == "" {
		platformPrivateIP = strings.TrimSpace(out.InstallEnv.Value.PlatformPrivateIP)
	}
	if platformPrivateIP == "" {
		platformPrivateIP, err = r.detectPrivateIP(ctx, host)
		if err != nil {
			return err
		}
	}

	install, err := r.renderInstallFiles(out, platformPrivateIP)
	if err != nil {
		return err
	}
	paths := []string{install.ControlPlaneConfig, install.CloudPlaneConfig, install.RemoteScript}
	defer removeFiles(paths)

	remote := func(name string) string { return host + ":/tmp/" + name }
	if err := r.scp(ctx, r.cfg.Binaries.ControlPlane, remote("mini-cloud-control-plane")); err != nil {
		return err
	}
	if err := r.scp(ctx, r.cfg.Binaries.CloudPlane, remote("mini-cloud-cloud-plane")); err != nil {
		return err
	}
	if err := r.scp(ctx, r.cfg.Binaries.NodeAgent, remote("mini-cloud-node-agent")); err != nil {
		return err
	}
	if err := r.scp(ctx, install.ControlPlaneConfig, remote("mini-cloud-control-plane.yaml")); err != nil {
		return err
	}
	if err := r.scp(ctx, install.CloudPlaneConfig, remote("mini-cloud-cloud-plane.yaml")); err != nil {
		return err
	}
	script, err := os.ReadFile(install.RemoteScript)
	if err != nil {
		return err
	}
	return r.ssh(ctx, host, script)
}

func (r *Runner) renderInstallFiles(out TerraformOutput, platformPrivateIP string) (installFiles, error) {
	provider := out.ProviderName()
	if provider == "" {
		return installFiles{}, fmt.Errorf("terraform provider output is empty")
	}
	registryMirror := strings.TrimSpace(r.cfg.Install.RegistryMirror)
	if registryMirror == "" {
		if provider == "tencent" {
			registryMirror = "https://mirror.ccs.tencentyun.com"
		} else {
			registryMirror = "https://docker.m.daocloud.io"
		}
	}

	spec := cloudPlaneProviderSpec(out.RuntimeProviderSpec.Value)
	instanceType := stringFromMap(out.RuntimeProviderSpec.Value, "instanceType")
	if instanceType == "" {
		return installFiles{}, fmt.Errorf("runtime_provider_spec.instanceType is required")
	}
	specYAML, err := yamlBlock(spec, 4)
	if err != nil {
		return installFiles{}, err
	}
	if strings.TrimSpace(specYAML) == "" {
		specYAML = "    {}"
	}
	tencentCredential := tencentCredential{}
	if provider == "tencent" {
		var err error
		tencentCredential, err = readTencentCredentialFile(r.cfg.Provider.TencentCredentialFile)
		if err != nil {
			return installFiles{}, err
		}
		if strings.TrimSpace(tencentCredential.SecretID) == "" || strings.TrimSpace(tencentCredential.SecretKey) == "" {
			return installFiles{}, fmt.Errorf("provider.tencentCredentialFile must contain secretId and secretKey")
		}
	}

	artifactPort := out.Network.Value.ArtifactHTTPPort
	grpcPort := out.Network.Value.CloudPlaneGRPCPort
	proxyPort := out.Network.Value.EgressProxyPort
	ingressPort := out.Network.Value.IngressHTTPPort
	if artifactPort == 0 || grpcPort == 0 || proxyPort == 0 || ingressPort == 0 {
		return installFiles{}, fmt.Errorf("terraform network outputs are incomplete")
	}
	nodeAgentURL := fmt.Sprintf("http://%s:%d/node-agent-linux-amd64", platformPrivateIP, artifactPort)
	connectEndpoint := fmt.Sprintf("%s:%d", platformPrivateIP, grpcPort)

	controlPlaneConfig, err := renderTemplate("control-plane.yaml.tmpl", controlPlaneTemplateData{
		HTTPAddr:        r.cfg.Install.ControlPlaneHTTPAddr,
		InstallRoot:     r.cfg.Install.Root,
		AdminToken:      r.cfg.Tokens.ControlPlaneAdmin,
		SouthboundToken: r.cfg.Tokens.ControlPlaneSouthbound,
		LokiURL:         r.cfg.Observability.WorkloadLogLokiURL,
		LokiTenantID:    r.cfg.Observability.WorkloadLogLokiTenantID,
	})
	if err != nil {
		return installFiles{}, err
	}
	cloudPlaneConfig, err := renderTemplate("cloud-plane.yaml.tmpl", cloudPlaneTemplateData{
		ListenGRPCAddr:              fmt.Sprintf("0.0.0.0:%d", grpcPort),
		PlaneName:                   out.Platform.Value.Name,
		PlaneGRPCEndpoint:           connectEndpoint,
		ControlPlaneURL:             fmt.Sprintf("http://%s", r.cfg.Install.ControlPlaneHTTPAddr),
		SouthboundToken:             r.cfg.Tokens.ControlPlaneSouthbound,
		NodeAgentConnectEndpoint:    connectEndpoint,
		NodeAgentBootstrapToken:     r.cfg.Tokens.NodeAgentBootstrap,
		NodeAgentBinaryURL:          nodeAgentURL,
		Provider:                    provider,
		RegionID:                    out.RegionID(),
		ZoneID:                      out.InstallEnv.Value.ZoneID,
		TencentCredential:           tencentCredential,
		InstanceType:                instanceType,
		RegistryMirrors:             []string{registryMirror},
		WorkloadEgressProxyEndpoint: fmt.Sprintf("http://%s:%d", platformPrivateIP, proxyPort),
		ProviderSpecYAML:            specYAML,
		IngressBaseDomain:           r.cfg.Install.IngressBaseDomain,
		LokiURL:                     r.cfg.Observability.WorkloadLogLokiURL,
		OTLPEndpoint:                r.cfg.Observability.WorkloadOTLPEndpoint,
	})
	if err != nil {
		return installFiles{}, err
	}
	remoteScript, err := renderTemplate("remote-install.sh.tmpl", remoteInstallTemplateData{
		InstallRoot:          r.cfg.Install.Root,
		IngressHTTPPort:      ingressPort,
		EgressProxyPort:      proxyPort,
		ArtifactHTTPPort:     artifactPort,
		SubnetCIDRBlock:      out.Network.Value.SubnetCIDRBlock,
		RegistryMirror:       registryMirror,
		ControlPlaneHTTPPort: portFromAddr(r.cfg.Install.ControlPlaneHTTPAddr),
		CloudPlaneGRPCPort:   grpcPort,
	})
	if err != nil {
		return installFiles{}, err
	}

	controlPath, err := writeTempFile("mini-cloud-control-plane-*.yaml", controlPlaneConfig, 0600)
	if err != nil {
		return installFiles{}, err
	}
	cloudPath, err := writeTempFile("mini-cloud-cloud-plane-*.yaml", cloudPlaneConfig, 0600)
	if err != nil {
		_ = os.Remove(controlPath)
		return installFiles{}, err
	}
	scriptPath, err := writeTempFile("mini-cloud-install-*.sh", remoteScript, 0700)
	if err != nil {
		_ = os.Remove(controlPath)
		_ = os.Remove(cloudPath)
		return installFiles{}, err
	}

	return installFiles{ControlPlaneConfig: controlPath, CloudPlaneConfig: cloudPath, RemoteScript: scriptPath}, nil
}

type tencentCredential struct {
	SecretID  string
	SecretKey string
	Token     string
}

func readTencentCredentialFile(path string) (tencentCredential, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return tencentCredential{}, fmt.Errorf("read Tencent credential file %q: %w", path, err)
	}
	credential, err := parseTencentCredentialData(data)
	if err != nil {
		return tencentCredential{}, fmt.Errorf("parse Tencent credential file %q: %w", path, err)
	}
	return credential, nil
}

func parseTencentCredentialData(data []byte) (tencentCredential, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return tencentCredential{}, fmt.Errorf("credential file is empty")
	}
	var values struct {
		SecretID     string `json:"secretId"`
		SecretKey    string `json:"secretKey"`
		SessionToken string `json:"sessionToken"`
		Token        string `json:"token"`
	}
	if err := json.Unmarshal([]byte(trimmed), &values); err != nil {
		return tencentCredential{}, err
	}
	return tencentCredential{
		SecretID:  strings.TrimSpace(values.SecretID),
		SecretKey: strings.TrimSpace(values.SecretKey),
		Token:     defaultString(values.SessionToken, values.Token),
	}, nil
}

func removeFiles(paths []string) {
	for _, path := range paths {
		if path != "" {
			_ = os.Remove(path)
		}
	}
}

func stringFromMap(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func portFromAddr(addr string) string {
	parts := strings.Split(addr, ":")
	return parts[len(parts)-1]
}
