package ops

import (
	"strings"
	"testing"
)

func TestRemoteInstallTemplatesRenderDockerFormats(t *testing.T) {
	cloud, err := renderTemplate("remote-cloud-plane-install.sh.tmpl", remoteCloudPlaneInstallTemplateData{
		InstallRoot:        "/opt/mini-cloud",
		IngressHTTPPort:    80,
		EgressProxyPort:    3128,
		ArtifactHTTPPort:   18082,
		SubnetCIDRBlock:    "10.1.0.0/24",
		RegistryMirror:     "https://mirror.example",
		CloudPlaneGRPCPort: 18081,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(cloud)
	for _, want := range []string{
		"docker ps -a --format '{{.Names}}'",
		"root * $INSTALL_ROOT/artifacts",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("rendered template does not contain %q", want)
		}
	}
}

func TestControlPlaneConfigTemplateRendersDNSPod(t *testing.T) {
	rendered, err := renderTemplate("control-plane.yaml.tmpl", controlPlaneTemplateData{
		HTTPAddr:          "127.0.0.1:18080",
		InstallRoot:       "/opt/mini-cloud",
		AdminToken:        "admin",
		SouthboundToken:   "southbound",
		ServiceBaseDomain: "apps.whatcloud.cn",
		DNSPodDomain:      "whatcloud.cn",
		Planes: []controlPlanePlaneTemplateData{
			{
				ID:           "pln_test",
				Name:         "test-plane",
				DisplayName:  "Test Plane",
				Provider:     "aliyun",
				Region:       "cn-beijing",
				GRPCEndpoint: "10.0.0.1:18081",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(rendered)
	for _, want := range []string{
		"  dnspod:",
		"  serviceBaseDomain: \"apps.whatcloud.cn\"",
		"    domain: \"whatcloud.cn\"",
		"planes:",
		"  - id: \"pln_test\"",
		"    grpcEndpoint: \"10.0.0.1:18081\"",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("control-plane config does not contain %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "secretId:") || strings.Contains(text, "secretKey:") || strings.Contains(text, "token:") {
		t.Fatalf("control-plane config should not render static DNSPod credentials:\n%s", text)
	}
}

func TestRemoteUninstallTemplateRendersDockerFormats(t *testing.T) {
	cloud, err := renderTemplate("remote-cloud-plane-uninstall.sh.tmpl", struct {
		InstallRoot    string
		RemovePostgres bool
	}{InstallRoot: "/opt/mini-cloud", RemovePostgres: true})
	if err != nil {
		t.Fatal(err)
	}
	text := string(cloud)
	for _, want := range []string{
		"docker ps -a --format '{{.Names}}'",
		"docker volume ls --format '{{.Name}}'",
		`rm -rf /etc/mini-cloud/cloud-plane`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("rendered template does not contain %q", want)
		}
	}
}

func TestCloudPlaneConfigTemplateRendersProviderDriverConfig(t *testing.T) {
	rendered, err := renderTemplate("cloud-plane.yaml.tmpl", cloudPlaneTemplateData{
		ListenGRPCAddr:           "0.0.0.0:18081",
		PlaneName:                "mini-cloud-ops",
		PlaneGRPCEndpoint:        "10.0.0.1:18081",
		SouthboundToken:          "southbound",
		NodeAgentConnectEndpoint: "10.0.0.1:18081",
		NodeAgentToken:           "node-agent-token",
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
		NodeProvider: NodeProviderConfig{
			ImageID:           "img-test",
			KeyIDs:            []string{"key-test"},
			VPCID:             "vpc-test",
			SubnetID:          "subnet-test",
			SecurityGroupIDs:  []string{"sg-test"},
			SystemDiskType:    "CLOUD_PREMIUM",
			SystemDiskSizeGiB: 50,
		},
		IngressBaseDomain:   "apps.example.com",
		IngressPublicOrigin: "203.0.113.10",
		OTLPEndpoint:        "http://otel.example:4318",
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(rendered)
	for _, want := range []string{
		"  tencent:",
		"    imageId: \"img-test\"",
		"      - \"key-test\"",
		"    vpcId: \"vpc-test\"",
		"    subnetId: \"subnet-test\"",
		"      - \"sg-test\"",
		"    systemDiskType: \"CLOUD_PREMIUM\"",
		"    systemDiskSizeGiB: 50",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("provider node config does not contain %q:\n%s", want, text)
		}
	}
	if !strings.Contains(text, "  tencentCredential:\n    secretId: \"sid\"\n    secretKey: \"skey\"\n    token: \"stok\"") {
		t.Fatalf("tencentCredential was not rendered:\n%s", text)
	}
	if !strings.Contains(text, "  otlpEndpoint: \"http://otel.example:4318\"") {
		t.Fatalf("otlpEndpoint was not rendered:\n%s", text)
	}
	if strings.Contains(text, "originHost") {
		t.Fatalf("cloud-plane config should not contain originHost:\n%s", text)
	}
	for _, want := range []string{
		"  publicOrigin: \"203.0.113.10\"",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("cloud-plane config does not contain %q:\n%s", want, text)
		}
	}
}

func TestCloudPlaneConfigTemplateDoesNotRenderDNSPodCredentials(t *testing.T) {
	rendered, err := renderTemplate("cloud-plane.yaml.tmpl", cloudPlaneTemplateData{
		ListenGRPCAddr:           "0.0.0.0:18081",
		PlaneName:                "mini-cloud-ops",
		PlaneGRPCEndpoint:        "10.0.0.1:18081",
		SouthboundToken:          "southbound",
		NodeAgentConnectEndpoint: "10.0.0.1:18081",
		NodeAgentToken:           "node-agent-token",
		NodeAgentBinaryURL:       "http://10.0.0.1:18082/node-agent-linux-amd64",
		Provider:                 "aliyun",
		RegionID:                 "cn-beijing",
		InstanceType:             "ecs.u1-c1m1.large",
		RegistryMirrors:          []string{"https://mirror.example"},
		NodeProvider: NodeProviderConfig{
			ImageID:            "m-test",
			VSwitchID:          "vsw-test",
			SecurityGroupID:    "sg-test",
			SystemDiskCategory: "cloud_essd",
			SystemDiskSizeGiB:  40,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rendered), "dnsPod") || strings.Contains(string(rendered), "frontDoor:") {
		t.Fatalf("cloud-plane config should not contain DNSPod frontdoor settings:\n%s", rendered)
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
