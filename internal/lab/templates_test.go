package lab

import (
	"strings"
	"testing"
)

func TestRemoteInstallTemplateRendersDockerFormats(t *testing.T) {
	rendered, err := renderTemplate("remote-install.sh.tmpl", remoteInstallTemplateData{
		InstallRoot:          "/opt/mini-cloud",
		IngressHTTPPort:      80,
		EgressProxyPort:      3128,
		ArtifactHTTPPort:     18082,
		SubnetCIDRBlock:      "10.1.0.0/24",
		RegistryMirror:       "https://mirror.example",
		ControlPlaneHTTPPort: "18080",
		CloudPlaneGRPCPort:   18081,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(rendered)
	for _, want := range []string{
		"docker ps -a --format '{{.Names}}'",
		"root * $INSTALL_ROOT/artifacts",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("rendered template does not contain %q", want)
		}
	}
}

func TestRemoteUninstallTemplateRendersDockerFormats(t *testing.T) {
	rendered, err := renderTemplate("remote-uninstall.sh.tmpl", struct {
		InstallRoot string
	}{InstallRoot: "/opt/mini-cloud"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(rendered)
	for _, want := range []string{
		"docker ps -a --format '{{.Names}}'",
		"docker volume ls --format '{{.Name}}'",
		`rm -rf /etc/mini-cloud "$INSTALL_ROOT"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("rendered template does not contain %q", want)
		}
	}
}

func TestCloudPlaneConfigTemplateRendersProviderSpec(t *testing.T) {
	rendered, err := renderTemplate("cloud-plane.yaml.tmpl", cloudPlaneTemplateData{
		ListenGRPCAddr:           "0.0.0.0:18081",
		PlaneName:                "mini-cloud-lab",
		PlaneGRPCEndpoint:        "10.0.0.1:18081",
		ControlPlaneURL:          "http://127.0.0.1:18080",
		SouthboundToken:          "southbound",
		NodeAgentConnectEndpoint: "10.0.0.1:18081",
		NodeAgentBootstrapToken:  "bootstrap",
		NodeAgentBinaryURL:       "http://10.0.0.1:18082/node-agent-linux-amd64",
		Provider:                 "tencent",
		RegionID:                 "ap-guangzhou",
		ZoneID:                   "ap-guangzhou-6",
		TencentCredential: tencentCredential{
			SecretID:  "sid",
			SecretKey: "skey",
			Token:     "stok",
		},
		InstanceType:                "S5.MEDIUM2",
		RegistryMirrors:             []string{"https://mirror.example"},
		WorkloadEgressProxyEndpoint: "http://10.0.0.1:3128",
		ProviderSpecYAML:            "    imageId: img-test\n    subnetId: subnet-test",
		IngressBaseDomain:           "apps.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(rendered)
	if !strings.Contains(text, "  providerSpec:\n    imageId: img-test\n    subnetId: subnet-test") {
		t.Fatalf("providerSpec YAML was not rendered with expected indentation:\n%s", text)
	}
	if !strings.Contains(text, "  tencentCredential:\n    secretId: \"sid\"\n    secretKey: \"skey\"\n    token: \"stok\"") {
		t.Fatalf("tencentCredential was not rendered:\n%s", text)
	}
	if strings.Contains(text, "originHost") {
		t.Fatalf("cloud-plane config should not contain originHost:\n%s", text)
	}
}

func TestParseTencentCredentialData(t *testing.T) {
	credential, err := parseTencentCredentialData([]byte(`{"secretId":"sid","secretKey":"skey","token":"stok"}`))
	if err != nil {
		t.Fatalf("parseTencentCredentialData returned error: %v", err)
	}
	if credential.SecretID != "sid" || credential.SecretKey != "skey" || credential.Token != "stok" {
		t.Fatalf("unexpected credential: %+v", credential)
	}
}
