package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type installFiles struct {
	Config       string
	RemoteScript string
}

type tlsTemplateData struct {
	CACert string
	Cert   string
	Key    string
}

type controlPlaneTemplateData struct {
	HTTPAddr          string
	InstallRoot       string
	AdminToken        string
	SouthboundToken   string
	SouthboundTLS     tlsTemplateData
	ServiceBaseDomain string
	DNSPodDomain      string
	Planes            []controlPlanePlaneTemplateData
}

type controlPlanePlaneTemplateData struct {
	ID           string
	Name         string
	DisplayName  string
	Provider     string
	Region       string
	GRPCEndpoint string
}

type cloudPlaneTemplateData struct {
	ListenGRPCAddr              string
	TLS                         tlsTemplateData
	PlaneName                   string
	PlaneGRPCEndpoint           string
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
	OTLPEndpoint                string
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

type cloudPlaneInstallPlan struct {
	Plane        Plane
	Output       TerraformOutput
	Host         string
	PrivateIP    string
	GRPCEndpoint string
}

func (r *Runner) install(ctx context.Context) error {
	if err := r.cfg.validateDeploy(); err != nil {
		return err
	}
	if err := r.cfg.validateArtifacts(); err != nil {
		return err
	}
	plans, err := r.prepareCloudPlaneInstallPlans(ctx)
	if err != nil {
		return err
	}
	certs, err := buildCertificateBundle(plans)
	if err != nil {
		return err
	}
	if err := r.installControlPlane(ctx, plans, certs.ControlPlane); err != nil {
		return err
	}
	for _, plan := range plans {
		cloudTLS, ok := certs.CloudPlanes[plan.Plane.Name]
		if !ok {
			return fmt.Errorf("missing cloud-plane certificate for %s", plan.Plane.Name)
		}
		if err := r.installCloudPlane(ctx, plan, cloudTLS); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) installControlPlane(ctx context.Context, plans []cloudPlaneInstallPlan, tlsData tlsTemplateData) error {
	install, err := r.renderControlPlaneInstallFiles(plans, tlsData)
	if err != nil {
		return err
	}
	defer removeFiles([]string{install.Config, install.RemoteScript})
	return r.deployControlPlaneSCF(ctx, install.Config)
}

func (r *Runner) prepareCloudPlaneInstallPlans(ctx context.Context) ([]cloudPlaneInstallPlan, error) {
	plans := make([]cloudPlaneInstallPlan, 0, len(r.cfg.Planes))
	for _, plane := range r.cfg.Planes {
		plan, err := r.prepareCloudPlaneInstallPlan(ctx, plane)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

func (r *Runner) prepareCloudPlaneInstallPlan(ctx context.Context, plane Plane) (cloudPlaneInstallPlan, error) {
	if err := r.terraform(ctx, plane, "init"); err != nil {
		return cloudPlaneInstallPlan{}, err
	}
	if err := r.selectTerraformWorkspace(ctx, plane); err != nil {
		return cloudPlaneInstallPlan{}, err
	}
	out, err := r.terraformOutput(ctx, plane)
	if err != nil {
		return cloudPlaneInstallPlan{}, err
	}
	host, err := r.platformHost(plane, out)
	if err != nil {
		return cloudPlaneInstallPlan{}, err
	}
	platformPrivateIP := strings.TrimSpace(out.Platform.Value.PrivateIP)
	if platformPrivateIP == "" {
		platformPrivateIP = strings.TrimSpace(out.InstallEnv.Value.PlatformPrivateIP)
	}
	if platformPrivateIP == "" {
		platformPrivateIP, err = r.detectPrivateIP(ctx, plane.SSH, host)
		if err != nil {
			return cloudPlaneInstallPlan{}, err
		}
	}

	nodeAgentConnectEndpoint := fmt.Sprintf("grpcs://%s:%d", platformPrivateIP, out.Network.Value.CloudPlaneGRPCPort)
	return cloudPlaneInstallPlan{
		Plane:        plane,
		Output:       out,
		Host:         host,
		PrivateIP:    platformPrivateIP,
		GRPCEndpoint: r.planeGRPCEndpoint(out, out.platformHost(), nodeAgentConnectEndpoint),
	}, nil
}

func (r *Runner) installCloudPlane(ctx context.Context, plan cloudPlaneInstallPlan, tlsData tlsTemplateData) error {
	install, err := r.renderCloudPlaneInstallFiles(plan.Plane, plan.Output, plan.PrivateIP, tlsData)
	if err != nil {
		return err
	}
	defer removeFiles([]string{install.Config, install.RemoteScript})
	if err := r.uploadFile(ctx, plan.Plane.SSH, plan.Host, r.cfg.Binaries.CloudPlane, "/tmp/mini-cloud-cloud-plane", 0755); err != nil {
		return err
	}
	if err := r.uploadFile(ctx, plan.Plane.SSH, plan.Host, r.cfg.Binaries.NodeAgent, "/tmp/mini-cloud-node-agent", 0755); err != nil {
		return err
	}
	if err := r.uploadFile(ctx, plan.Plane.SSH, plan.Host, install.Config, "/tmp/mini-cloud-cloud-plane.yaml", 0600); err != nil {
		return err
	}
	script, err := os.ReadFile(install.RemoteScript)
	if err != nil {
		return err
	}
	return r.ssh(ctx, plan.Plane.SSH, plan.Host, script)
}

func (r *Runner) renderControlPlaneInstallFiles(plans []cloudPlaneInstallPlan, tlsData tlsTemplateData) (installFiles, error) {
	planes := make([]controlPlanePlaneTemplateData, 0, len(plans))
	for _, plan := range plans {
		provider := plan.Output.ProviderName()
		if provider == "" {
			provider = plan.Plane.Provider
		}
		region := plan.Output.RegionID()
		if region == "" {
			region = plan.Plane.Region
		}
		planes = append(planes, controlPlanePlaneTemplateData{
			ID:           planeID(plan.Plane.Name),
			Name:         plan.Plane.Name,
			DisplayName:  plan.Plane.Name,
			Provider:     provider,
			Region:       region,
			GRPCEndpoint: plan.GRPCEndpoint,
		})
	}
	controlPlaneConfig, err := renderTemplate("control-plane.yaml.tmpl", controlPlaneTemplateData{
		HTTPAddr:          "0.0.0.0:9000",
		InstallRoot:       r.cfg.Install.Root,
		AdminToken:        r.cfg.Tokens.ControlPlaneAdmin,
		SouthboundToken:   r.cfg.Tokens.ControlPlaneSouthbound,
		SouthboundTLS:     tlsData,
		ServiceBaseDomain: strings.Trim(r.cfg.Install.IngressBaseDomain, "."),
		DNSPodDomain:      rootDomain(r.cfg.Install.IngressBaseDomain),
		Planes:            planes,
	})
	if err != nil {
		return installFiles{}, err
	}
	configPath, err := writeTempFile("mini-cloud-control-plane-*.yaml", controlPlaneConfig, 0600)
	if err != nil {
		return installFiles{}, err
	}
	return installFiles{Config: configPath}, nil
}

func (r *Runner) renderCloudPlaneInstallFiles(plane Plane, out TerraformOutput, platformPrivateIP string, tlsData tlsTemplateData) (installFiles, error) {
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
	nodeAgentConnectEndpoint := fmt.Sprintf("grpcs://%s:%d", platformPrivateIP, grpcPort)
	planeGRPCEndpoint := r.planeGRPCEndpoint(out, out.platformHost(), nodeAgentConnectEndpoint)
	platformPublicIP := strings.TrimSpace(out.Platform.Value.PublicIP)
	if platformPublicIP == "" {
		platformPublicIP = strings.TrimSpace(out.InstallEnv.Value.PlatformPublicIP)
	}
	if ingressEnabled && platformPublicIP == "" {
		return installFiles{}, fmt.Errorf("platform public IP is required when ingress is enabled")
	}

	cloudPlaneConfig, err := renderTemplate("cloud-plane.yaml.tmpl", cloudPlaneTemplateData{
		ListenGRPCAddr:              fmt.Sprintf("0.0.0.0:%d", grpcPort),
		TLS:                         tlsData,
		PlaneName:                   out.Platform.Value.Name,
		PlaneGRPCEndpoint:           planeGRPCEndpoint,
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
	if endpoint := strings.TrimSpace(out.Platform.Value.GRPCEndpoint); endpoint != "" {
		return endpoint
	}
	if publicIP := strings.TrimSpace(out.Platform.Value.PublicIP); publicIP != "" && out.Network.Value.CloudPlaneGRPCPort != 0 {
		return fmt.Sprintf("grpcs://%s:%d", publicIP, out.Network.Value.CloudPlaneGRPCPort)
	}
	return privateEndpoint
}

func (r *Runner) deployControlPlaneSCF(ctx context.Context, configPath string) error {
	image, err := r.buildAndPushControlPlaneImage(ctx, configPath)
	if err != nil {
		return err
	}
	if err := r.upsertSCFFunction(ctx, image); err != nil {
		return err
	}
	return r.waitForControlPlaneSCF(ctx)
}

func (r *Runner) buildAndPushControlPlaneImage(ctx context.Context, configPath string) (string, error) {
	repo := strings.TrimRight(strings.TrimSpace(r.cfg.ControlPlane.SCF.Image), ":")
	if repo == "" {
		return "", fmt.Errorf("controlPlane.scf.image is required")
	}
	contextConfig, err := r.copyControlPlaneConfigForDocker(configPath)
	if err != nil {
		return "", err
	}
	defer removeFiles([]string{contextConfig})
	tag := "e2e-" + time.Now().UTC().Format("20060102150405")
	image := repo + ":" + tag
	localImage := "mini-cloud/control-plane:e2e"
	if err := runInteractive(ctx, "docker", "build",
		"--platform", "linux/amd64",
		"--provenance=false",
		"-f", "deploy/container/control-plane.Dockerfile",
		"--build-arg", "CONTROL_PLANE_CONFIG="+filepath.ToSlash(contextConfig),
		"-t", localImage,
		".",
	); err != nil {
		return "", err
	}
	if err := runInteractive(ctx, "docker", "tag", localImage, image); err != nil {
		return "", err
	}
	if err := runInteractive(ctx, "docker", "push", image); err != nil {
		return "", err
	}
	return image, nil
}

func (r *Runner) copyControlPlaneConfigForDocker(configPath string) (string, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", err
	}
	dir := filepath.Join("dist", "ops")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	target := filepath.Join(dir, "control-plane.yaml")
	if err := os.WriteFile(target, data, 0600); err != nil {
		return "", err
	}
	return target, nil
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
