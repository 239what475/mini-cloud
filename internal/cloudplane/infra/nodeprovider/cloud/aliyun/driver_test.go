package aliyun

import (
	"encoding/base64"
	"os/exec"
	"strings"
	"testing"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
)

func testDriverConfig(provider cloudplaneconfig.AliyunNodeConfig) cloudplaneconfig.Config {
	return cloudplaneconfig.Config{
		Plane:        cloudplaneconfig.PlaneConfig{Name: "mini-cloud-lab", GRPCEndpoint: "10.0.0.10:18081"},
		ControlPlane: cloudplaneconfig.ControlPlaneConfig{URL: "http://127.0.0.1:18080", BearerToken: "southbound-token"},
		Infrastructure: cloudplaneconfig.InfrastructureConfig{
			Provider: Name,
			RegionID: "cn-beijing",
			ZoneID:   "cn-beijing-f",
		},
		NodeAgent: cloudplaneconfig.NodeAgentConfig{
			ConnectEndpoint: "10.0.0.10:18081",
			Token:           "node-agent-token",
			BinaryURL:       "https://artifacts.example.com/node-agent-linux-amd64",
		},
		NodeProvisioning: cloudplaneconfig.NodeProvisioningConfig{
			InstanceType:                "ecs.u1-c1m1.large",
			WorkloadEgressProxyEndpoint: "http://10.0.0.10:3128",
			Aliyun:                      provider,
		},
		Observability: cloudplaneconfig.ObservabilityConfig{
			OTLPEndpoint: "http://otel.example:4318",
		},
	}
}

func TestNewDriverConfig(t *testing.T) {
	provider := cloudplaneconfig.AliyunNodeConfig{
		ImageID:         "m-123",
		VSwitchID:       "vsw-123",
		SecurityGroupID: "sg-123",
	}

	typed, err := newDriverConfig(testDriverConfig(provider))
	if err != nil {
		t.Fatalf("newDriverConfig returned error: %v", err)
	}
	if typed.Provider.SystemDiskCategory != "cloud_essd" {
		t.Fatalf("SystemDiskCategory = %q, want cloud_essd", typed.Provider.SystemDiskCategory)
	}
	if typed.Provider.SystemDiskSizeGiB != 40 {
		t.Fatalf("SystemDiskSizeGiB = %d, want 40", typed.Provider.SystemDiskSizeGiB)
	}
	if typed.CloudPlane.NodeProvisioning.InstanceType != "ecs.u1-c1m1.large" {
		t.Fatalf("InstanceType = %q, want ecs.u1-c1m1.large", typed.CloudPlane.NodeProvisioning.InstanceType)
	}
}

func TestNewDriverConfigRequiresAliyunFields(t *testing.T) {
	provider := cloudplaneconfig.AliyunNodeConfig{ImageID: "m-123"}

	if _, err := newDriverConfig(testDriverConfig(provider)); err == nil {
		t.Fatalf("expected newDriverConfig to reject missing provider-specific fields")
	}
}

func TestBuildNodeUserDataDoesNotTraceNodeAgentToken(t *testing.T) {
	t.Parallel()
	driver := providerDriver{
		config: driverConfig{
			CloudPlane: testDriverConfig(cloudplaneconfig.AliyunNodeConfig{}),
		},
	}

	script, err := driver.buildNodeUserData("node-a", instanceTypeCapacity{
		instanceType: "ecs.u1-c1m1.large",
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
	if !strings.HasPrefix(script, "#!/usr/bin/env bash\n") {
		t.Fatalf("node user-data is not a shell script")
	}
	if strings.Contains(script, "set -x") || strings.Contains(script, "set -eux") {
		t.Fatalf("node user-data enables xtrace")
	}
	if !strings.Contains(script, "chmod 0600 \"$INSTALL_ROOT/node-agent.yaml\"") {
		t.Fatalf("node user-data does not restrict node-agent.yaml permissions")
	}
	if !strings.Contains(script, "http://100.100.100.200/latest") || !strings.Contains(script, "X-aliyun-ecs-metadata-token") {
		t.Fatalf("aliyun node user-data does not contain aliyun metadata flow")
	}
	if strings.Contains(script, "metadata.tencentyun.com") || strings.Contains(script, "local-ipv4") {
		t.Fatalf("aliyun node user-data contains tencent metadata flow")
	}
	if strings.Contains(script, "Acquire::http::Proxy") || strings.Contains(script, "EnvironmentFile=-/etc/mini-cloud/node-agent/proxy.env") {
		t.Fatalf("aliyun node user-data applies workload proxy to apt or node-agent process")
	}
	if !strings.Contains(script, `Environment="HTTP_PROXY=$WORKLOAD_EGRESS_PROXY_ENDPOINT"`) || !strings.Contains(script, `Environment="HTTPS_PROXY=$WORKLOAD_EGRESS_PROXY_ENDPOINT"`) {
		t.Fatalf("aliyun node user-data does not configure docker daemon proxy")
	}
	if !strings.Contains(script, "endpoint: \"http://10.0.0.10:3128\"") {
		t.Fatalf("aliyun node user-data does not pass workload egress proxy to node-agent config")
	}
	if !strings.Contains(script, "workloadOTLPEndpoint: \"http://otel.example:4318\"") {
		t.Fatalf("aliyun node user-data does not pass workload OTLP endpoint to node-agent config")
	}
	cmd := exec.Command("bash", "-n")
	cmd.Stdin = strings.NewReader(script)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bash -n failed: %v output=%s", err, output)
	}
}
