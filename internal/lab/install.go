package lab

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type installFiles struct {
	Config       string
	RemoteScript string
}

type controlPlaneTemplateData struct {
	HTTPAddr          string
	InstallRoot       string
	AdminToken        string
	SouthboundToken   string
	ServiceBaseDomain string
	DNSPodDomain      string
	DNSPodCredential  tencentCredential
	LokiURL           string
	LokiTenantID      string
}

type cloudPlaneTemplateData struct {
	ListenGRPCAddr              string
	PlaneName                   string
	PlaneGRPCEndpoint           string
	ControlPlaneURL             string
	SouthboundToken             string
	NodeAgentConnectEndpoint    string
	NodeAgentToken              string
	NodeAgentBinaryURL          string
	Provider                    string
	RegionID                    string
	ZoneID                      string
	TencentCredential           tencentCredential
	InstanceType                string
	RegistryMirrors             []string
	WorkloadEgressProxyEndpoint string
	NodeProvider                NodeProviderConfig
	IngressBaseDomain           string
	IngressPublicOrigin         string
	LokiURL                     string
	LokiTenantID                string
	OTLPEndpoint                string
}

type remoteControlPlaneInstallTemplateData struct {
	InstallRoot          string
	ControlPlaneHTTPPort string
}

type remoteCloudPlaneInstallTemplateData struct {
	InstallRoot        string
	IngressHTTPPort    int
	EgressProxyPort    int
	ArtifactHTTPPort   int
	SubnetCIDRBlock    string
	RegistryMirror     string
	CloudPlaneGRPCPort int
}

func (r *Runner) Install(ctx context.Context) error {
	if err := r.cfg.validateInstall(); err != nil {
		return err
	}
	if err := r.installControlPlane(ctx); err != nil {
		return err
	}
	for _, plane := range r.cfg.Planes {
		if err := r.installCloudPlane(ctx, plane); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) installControlPlane(ctx context.Context) error {
	install, err := r.renderControlPlaneInstallFiles()
	if err != nil {
		return err
	}
	defer removeFiles([]string{install.Config, install.RemoteScript})

	host := strings.TrimSpace(r.cfg.ControlPlane.SSH.Host)
	remote := func(name string) string { return host + ":/tmp/" + name }
	if err := r.scp(ctx, r.cfg.ControlPlane.SSH, r.cfg.Binaries.ControlPlane, remote("mini-cloud-control-plane")); err != nil {
		return err
	}
	if err := r.scp(ctx, r.cfg.ControlPlane.SSH, install.Config, remote("mini-cloud-control-plane.yaml")); err != nil {
		return err
	}
	script, err := os.ReadFile(install.RemoteScript)
	if err != nil {
		return err
	}
	return r.ssh(ctx, r.cfg.ControlPlane.SSH, host, script)
}

func (r *Runner) installCloudPlane(ctx context.Context, plane Plane) error {
	if err := r.terraform(ctx, plane, "init"); err != nil {
		return err
	}
	if err := r.selectTerraformWorkspace(ctx, plane); err != nil {
		return err
	}
	out, err := r.terraformOutput(ctx, plane)
	if err != nil {
		return err
	}
	host, err := r.platformHost(plane, out)
	if err != nil {
		return err
	}
	platformPrivateIP := strings.TrimSpace(out.Platform.Value.PrivateIP)
	if platformPrivateIP == "" {
		platformPrivateIP = strings.TrimSpace(out.InstallEnv.Value.PlatformPrivateIP)
	}
	if platformPrivateIP == "" {
		platformPrivateIP, err = r.detectPrivateIP(ctx, plane.SSH, host)
		if err != nil {
			return err
		}
	}

	install, err := r.renderCloudPlaneInstallFiles(plane, out, platformPrivateIP)
	if err != nil {
		return err
	}
	defer removeFiles([]string{install.Config, install.RemoteScript})

	remote := func(name string) string { return host + ":/tmp/" + name }
	if err := r.scp(ctx, plane.SSH, r.cfg.Binaries.CloudPlane, remote("mini-cloud-cloud-plane")); err != nil {
		return err
	}
	if err := r.scp(ctx, plane.SSH, r.cfg.Binaries.NodeAgent, remote("mini-cloud-node-agent")); err != nil {
		return err
	}
	if err := r.scp(ctx, plane.SSH, install.Config, remote("mini-cloud-cloud-plane.yaml")); err != nil {
		return err
	}
	script, err := os.ReadFile(install.RemoteScript)
	if err != nil {
		return err
	}
	return r.ssh(ctx, plane.SSH, host, script)
}

func (r *Runner) renderControlPlaneInstallFiles() (installFiles, error) {
	dnspodCredential, err := readTencentCredentialFile(r.cfg.Provider.TencentCredentialFile)
	if err != nil {
		return installFiles{}, err
	}
	if strings.TrimSpace(dnspodCredential.SecretID) == "" || strings.TrimSpace(dnspodCredential.SecretKey) == "" {
		return installFiles{}, fmt.Errorf("provider.tencentCredentialFile must contain secretId and secretKey")
	}
	controlPlaneConfig, err := renderTemplate("control-plane.yaml.tmpl", controlPlaneTemplateData{
		HTTPAddr:          r.cfg.ControlPlane.ListenHTTPAddr,
		InstallRoot:       r.cfg.Install.Root,
		AdminToken:        r.cfg.Tokens.ControlPlaneAdmin,
		SouthboundToken:   r.cfg.Tokens.ControlPlaneSouthbound,
		ServiceBaseDomain: strings.Trim(r.cfg.Install.IngressBaseDomain, "."),
		DNSPodDomain:      rootDomain(r.cfg.Install.IngressBaseDomain),
		DNSPodCredential:  dnspodCredential,
		LokiURL:           r.cfg.Observability.WorkloadLogLokiURL,
		LokiTenantID:      r.cfg.Observability.WorkloadLogLokiTenantID,
	})
	if err != nil {
		return installFiles{}, err
	}
	remoteScript, err := renderTemplate("remote-control-plane-install.sh.tmpl", remoteControlPlaneInstallTemplateData{
		InstallRoot:          r.cfg.Install.Root,
		ControlPlaneHTTPPort: portFromAddr(r.cfg.ControlPlane.ListenHTTPAddr),
	})
	if err != nil {
		return installFiles{}, err
	}
	configPath, err := writeTempFile("mini-cloud-control-plane-*.yaml", controlPlaneConfig, 0600)
	if err != nil {
		return installFiles{}, err
	}
	scriptPath, err := writeTempFile("mini-cloud-control-plane-install-*.sh", remoteScript, 0700)
	if err != nil {
		_ = os.Remove(configPath)
		return installFiles{}, err
	}
	return installFiles{Config: configPath, RemoteScript: scriptPath}, nil
}

func (r *Runner) renderCloudPlaneInstallFiles(plane Plane, out TerraformOutput, platformPrivateIP string) (installFiles, error) {
	provider := out.ProviderName()
	if provider == "" {
		return installFiles{}, fmt.Errorf("terraform provider output is empty")
	}
	if plane.Provider != "" && provider != plane.Provider {
		return installFiles{}, fmt.Errorf("plane %s expected provider %s, terraform output is %s", plane.Name, plane.Provider, provider)
	}
	registryMirror := strings.TrimSpace(plane.RegistryMirror)
	if registryMirror == "" {
		registryMirror = strings.TrimSpace(r.cfg.Install.RegistryMirror)
	}
	if registryMirror == "" {
		if provider == "tencent" {
			registryMirror = "https://mirror.ccs.tencentyun.com"
		} else {
			registryMirror = "https://docker.m.daocloud.io"
		}
	}

	nodeProvider := out.NodeProviderConfig.Value
	if nodeProvider.Provider != "" && nodeProvider.Provider != provider {
		return installFiles{}, fmt.Errorf("node_provider_config provider %s does not match terraform provider %s", nodeProvider.Provider, provider)
	}
	instanceType := strings.TrimSpace(nodeProvider.InstanceType)
	if instanceType == "" {
		return installFiles{}, fmt.Errorf("node_provider_config.instanceType is required")
	}
	tencentProviderCredential := tencentCredential{}
	ingressEnabled := strings.TrimSpace(r.cfg.Install.IngressBaseDomain) != ""
	if provider == "tencent" {
		var err error
		tencentProviderCredential, err = readTencentCredentialFile(r.cfg.Provider.TencentCredentialFile)
		if err != nil {
			return installFiles{}, err
		}
		if strings.TrimSpace(tencentProviderCredential.SecretID) == "" || strings.TrimSpace(tencentProviderCredential.SecretKey) == "" {
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
	nodeAgentConnectEndpoint := fmt.Sprintf("%s:%d", platformPrivateIP, grpcPort)
	planeGRPCEndpoint := r.planeGRPCEndpoint(out, out.platformHost(), nodeAgentConnectEndpoint)
	platformPublicIP := strings.TrimSpace(out.Platform.Value.PublicIP)
	if platformPublicIP == "" {
		platformPublicIP = strings.TrimSpace(out.InstallEnv.Value.PlatformPublicIP)
	}
	if ingressEnabled && platformPublicIP == "" {
		return installFiles{}, fmt.Errorf("platform public IP is required when ingress frontDoor is configured")
	}

	cloudPlaneConfig, err := renderTemplate("cloud-plane.yaml.tmpl", cloudPlaneTemplateData{
		ListenGRPCAddr:              fmt.Sprintf("0.0.0.0:%d", grpcPort),
		PlaneName:                   out.Platform.Value.Name,
		PlaneGRPCEndpoint:           planeGRPCEndpoint,
		ControlPlaneURL:             r.cfg.ControlPlane.URL,
		SouthboundToken:             r.cfg.Tokens.ControlPlaneSouthbound,
		NodeAgentConnectEndpoint:    nodeAgentConnectEndpoint,
		NodeAgentToken:              r.cfg.Tokens.NodeAgent,
		NodeAgentBinaryURL:          nodeAgentURL,
		Provider:                    provider,
		RegionID:                    out.RegionID(),
		ZoneID:                      out.InstallEnv.Value.ZoneID,
		TencentCredential:           tencentProviderCredential,
		InstanceType:                instanceType,
		RegistryMirrors:             []string{registryMirror},
		WorkloadEgressProxyEndpoint: fmt.Sprintf("http://%s:%d", platformPrivateIP, proxyPort),
		NodeProvider:                nodeProvider,
		IngressBaseDomain:           r.cfg.Install.IngressBaseDomain,
		IngressPublicOrigin:         platformPublicIP,
		LokiURL:                     r.cfg.Observability.WorkloadLogLokiURL,
		LokiTenantID:                r.cfg.Observability.WorkloadLogLokiTenantID,
		OTLPEndpoint:                r.cfg.Observability.WorkloadOTLPEndpoint,
	})
	if err != nil {
		return installFiles{}, err
	}
	remoteScript, err := renderTemplate("remote-cloud-plane-install.sh.tmpl", remoteCloudPlaneInstallTemplateData{
		InstallRoot:        r.cfg.Install.Root,
		IngressHTTPPort:    ingressPort,
		EgressProxyPort:    proxyPort,
		ArtifactHTTPPort:   artifactPort,
		SubnetCIDRBlock:    out.Network.Value.SubnetCIDRBlock,
		RegistryMirror:     registryMirror,
		CloudPlaneGRPCPort: grpcPort,
	})
	if err != nil {
		return installFiles{}, err
	}

	cloudPath, err := writeTempFile("mini-cloud-cloud-plane-*.yaml", cloudPlaneConfig, 0600)
	if err != nil {
		return installFiles{}, err
	}
	scriptPath, err := writeTempFile("mini-cloud-install-*.sh", remoteScript, 0700)
	if err != nil {
		_ = os.Remove(cloudPath)
		return installFiles{}, err
	}

	return installFiles{Config: cloudPath, RemoteScript: scriptPath}, nil
}

func (r *Runner) planeGRPCEndpoint(out TerraformOutput, cloudPlaneHost string, privateEndpoint string) string {
	if strings.TrimSpace(r.controlPlaneHost()) == strings.TrimSpace(cloudPlaneHost) {
		return privateEndpoint
	}
	if endpoint := strings.TrimSpace(out.Platform.Value.GRPCEndpoint); endpoint != "" {
		return endpoint
	}
	if publicIP := strings.TrimSpace(out.Platform.Value.PublicIP); publicIP != "" && out.Network.Value.CloudPlaneGRPCPort != 0 {
		return fmt.Sprintf("%s:%d", publicIP, out.Network.Value.CloudPlaneGRPCPort)
	}
	return privateEndpoint
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

func portFromAddr(addr string) string {
	parts := strings.Split(addr, ":")
	return parts[len(parts)-1]
}

func rootDomain(baseDomain string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(baseDomain), "."), ".")
	if len(parts) <= 2 {
		return strings.Join(parts, ".")
	}
	return strings.Join(parts[len(parts)-2:], ".")
}
