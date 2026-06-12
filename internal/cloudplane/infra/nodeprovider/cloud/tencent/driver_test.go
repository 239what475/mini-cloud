package tencent

import (
	"encoding/base64"
	"os/exec"
	"strings"
	"testing"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
)

func testDriverConfig(provider cloudplaneconfig.TencentNodeConfig) cloudplaneconfig.Config {
	return cloudplaneconfig.Config{
		Plane:        cloudplaneconfig.PlaneConfig{Name: "mini-cloud-lab", GRPCEndpoint: "10.0.0.10:18081"},
		ControlPlane: cloudplaneconfig.ControlPlaneConfig{URL: "http://127.0.0.1:18080", BearerToken: "southbound-token"},
		Infrastructure: cloudplaneconfig.InfrastructureConfig{
			Provider: Name,
			RegionID: "ap-beijing",
			ZoneID:   "ap-beijing-6",
			TencentCredential: cloudplaneconfig.TencentCredentialConfig{
				SecretID:  "sid",
				SecretKey: "skey",
			},
		},
		NodeAgent: cloudplaneconfig.NodeAgentConfig{
			ConnectEndpoint: "10.0.0.10:18081",
			Token:           "node-agent-token",
			BinaryURL:       "https://artifacts.example.com/node-agent-linux-amd64",
		},
		NodeProvisioning: cloudplaneconfig.NodeProvisioningConfig{
			InstanceType:                "S5.MEDIUM4",
			WorkloadEgressProxyEndpoint: "http://10.0.0.10:3128",
			Tencent:                     provider,
		},
		Observability: cloudplaneconfig.ObservabilityConfig{
			OTLPEndpoint: "http://otel.example:4318",
		},
	}
}

func TestNewDriverConfig(t *testing.T) {
	provider := cloudplaneconfig.TencentNodeConfig{
		ImageID:          "img-123",
		VPCID:            "vpc-123",
		SubnetID:         "subnet-123",
		SecurityGroupIDs: []string{"sg-123"},
	}

	typed, err := newDriverConfig(testDriverConfig(provider))
	if err != nil {
		t.Fatalf("newDriverConfig returned error: %v", err)
	}
	if typed.Provider.SystemDiskType != "CLOUD_PREMIUM" {
		t.Fatalf("SystemDiskType = %q, want CLOUD_PREMIUM", typed.Provider.SystemDiskType)
	}
	if typed.Provider.SystemDiskSizeGiB != 50 {
		t.Fatalf("SystemDiskSizeGiB = %d, want 50", typed.Provider.SystemDiskSizeGiB)
	}
}

func TestNewDriverConfigRequiresTencentFields(t *testing.T) {
	provider := cloudplaneconfig.TencentNodeConfig{ImageID: "img-123"}

	if _, err := newDriverConfig(testDriverConfig(provider)); err == nil {
		t.Fatalf("expected newDriverConfig to reject missing provider-specific fields")
	}
}

func TestBuildNodeUserDataDoesNotTraceNodeAgentToken(t *testing.T) {
	t.Parallel()
	driver := providerDriver{
		config: driverConfig{
			CloudPlane: testDriverConfig(cloudplaneconfig.TencentNodeConfig{}),
		},
	}

	script, err := driver.buildNodeUserData("node-a", instanceTypeCapacity{
		instanceType: "S5.MEDIUM4",
		cpuMilli:     2000,
		memoryMi:     4096,
	})
	if err != nil {
		t.Fatalf("buildNodeUserData returned error: %v", err)
	}
	decodedScript, err := base64.StdEncoding.DecodeString(script)
	if err != nil {
		t.Fatalf("DecodeString returned error: %v", err)
	}
	script = string(decodedScript)
	if strings.Contains(script, "set -x") || strings.Contains(script, "set -eux") {
		t.Fatalf("node user-data enables xtrace")
	}
	if !strings.Contains(script, "chmod 0600 \"$INSTALL_ROOT/node-agent.yaml\"") {
		t.Fatalf("node user-data does not restrict node-agent.yaml permissions")
	}
	if !strings.Contains(script, "http://metadata.tencentyun.com/latest/meta-data") || !strings.Contains(script, "local-ipv4") {
		t.Fatalf("tencent node user-data does not contain tencent metadata flow")
	}
	if strings.Contains(script, "100.100.100.200") || strings.Contains(script, "X-aliyun-ecs-metadata-token") {
		t.Fatalf("tencent node user-data contains aliyun metadata flow")
	}
	if strings.Contains(script, "Acquire::http::Proxy") || strings.Contains(script, "EnvironmentFile=-/etc/mini-cloud/node-agent/proxy.env") {
		t.Fatalf("tencent node user-data applies workload proxy to apt or node-agent process")
	}
	if !strings.Contains(script, `Environment="HTTP_PROXY=$WORKLOAD_EGRESS_PROXY_ENDPOINT"`) || !strings.Contains(script, `Environment="HTTPS_PROXY=$WORKLOAD_EGRESS_PROXY_ENDPOINT"`) {
		t.Fatalf("tencent node user-data does not configure docker daemon proxy")
	}
	if !strings.Contains(script, "endpoint: \"http://10.0.0.10:3128\"") {
		t.Fatalf("tencent node user-data does not pass workload egress proxy to node-agent config")
	}
	if !strings.Contains(script, "workloadOTLPEndpoint: \"http://otel.example:4318\"") {
		t.Fatalf("tencent node user-data does not pass workload OTLP endpoint to node-agent config")
	}
	cmd := exec.Command("bash", "-n")
	cmd.Stdin = strings.NewReader(script)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bash -n failed: %v output=%s", err, output)
	}
}

func TestBuildNodeHostName(t *testing.T) {
	got := buildNodeHostName("mini-cloud-node-super-long-host-name-for-tencent-provider-test")
	if len(got) > 60 {
		t.Fatalf("host name length = %d, want <= 60", len(got))
	}
}
