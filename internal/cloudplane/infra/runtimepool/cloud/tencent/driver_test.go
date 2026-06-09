package tencent

import (
	"encoding/base64"
	"os/exec"
	"strings"
	"testing"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
)

// testRuntimeConfig 构造 tencent driver 单测用的 cloud-plane 配置。
func testRuntimeConfig(spec map[string]any) cloudplaneconfig.Config {
	// 组装 tencent driver 单测所需的公共配置。
	return cloudplaneconfig.Config{
		// plane/infrastructure/node-agent 字段参与 user-data 和实例归属信息生成。
		Plane: cloudplaneconfig.PlaneConfig{Name: "mini-cloud-lab"},
		Infrastructure: cloudplaneconfig.InfrastructureConfig{
			Provider: Name,
			RegionID: "ap-beijing",
			ZoneID:   "ap-beijing-6",
		},
		NodeAgent: cloudplaneconfig.NodeAgentConfig{
			ConnectEndpoint: "10.0.0.10:18081",
			BootstrapToken:  "bootstrap-token",
			BinaryURL:       "https://artifacts.example.com/node-agent-linux-amd64",
		},
		RuntimeProvisioning: cloudplaneconfig.RuntimeProvisioningConfig{ProviderSpec: spec},
	}
}

// TestParseRuntimeConfig 验证 tencent runtime config 解析和默认值填充。
func TestParseRuntimeConfig(t *testing.T) {
	// 只提供必填 providerSpec 字段，验证默认系统盘参数会按 driver 规则补齐。
	spec := map[string]any{
		"instanceType":     "S5.MEDIUM4",
		"imageId":          "img-123",
		"vpcId":            "vpc-123",
		"subnetId":         "subnet-123",
		"securityGroupIds": []string{"sg-123"},
	}

	// 解析完整 runtime config，得到 tencent provider 的强类型配置。
	typed, err := ParseRuntimeConfig(testRuntimeConfig(spec))
	if err != nil {
		t.Fatalf("ParseRuntimeConfig returned error: %v", err)
	}

	// 默认值应稳定，避免用户配置文件必须重复声明常规磁盘参数。
	if typed.ProviderSpec.SystemDiskType != "CLOUD_PREMIUM" {
		t.Fatalf("SystemDiskType = %q, want CLOUD_PREMIUM", typed.ProviderSpec.SystemDiskType)
	}
	if typed.ProviderSpec.SystemDiskSizeGiB != 50 {
		t.Fatalf("SystemDiskSizeGiB = %d, want 50", typed.ProviderSpec.SystemDiskSizeGiB)
	}
}

// TestParseRuntimeConfigRequiresTencentFields 验证 tencent provider 必填字段缺失时解析失败。
func TestParseRuntimeConfigRequiresTencentFields(t *testing.T) {
	spec := map[string]any{"instanceType": "S5.MEDIUM4"}

	if _, err := ParseRuntimeConfig(testRuntimeConfig(spec)); err == nil {
		t.Fatalf("expected ParseRuntimeConfig to reject missing provider-specific fields")
	}
}

// TestBuildRuntimeNodeUserDataDoesNotTraceBootstrapToken 验证 tencent user-data 不通过 xtrace 暴露 bootstrap token，并限制配置文件权限。
func TestBuildRuntimeNodeUserDataDoesNotTraceBootstrapToken(t *testing.T) {
	t.Parallel()

	// 使用最小 driver 配置构造 user-data，不触发真实腾讯云 SDK 调用。
	driver := runtimeDriver{
		config: RuntimeConfig{
			CloudPlane: testRuntimeConfig(nil),
		},
	}

	// 生成 user-data 后先解 base64，检查脚本内容安全属性。
	script, err := driver.buildRuntimeNodeUserData("runtime-node-a", instanceTypeCapacity{
		instanceType: "S5.MEDIUM4",
		cpuMilli:     2000,
		memoryMi:     4096,
	})
	if err != nil {
		t.Fatalf("buildRuntimeNodeUserData returned error: %v", err)
	}
	decodedScript, err := base64.StdEncoding.DecodeString(script)
	if err != nil {
		t.Fatalf("DecodeString returned error: %v", err)
	}
	script = string(decodedScript)
	// user-data 不能开启 xtrace，否则 bootstrap token 可能出现在 cloud-init 日志。
	if strings.Contains(script, "set -x") || strings.Contains(script, "set -eux") {
		t.Fatalf("runtime node user-data enables xtrace")
	}
	// node-agent 配置包含 bootstrap token，文件权限必须限制为 owner 可读写。
	if !strings.Contains(script, "chmod 0600 \"$INSTALL_ROOT/node-agent.yaml\"") {
		t.Fatalf("runtime node user-data does not restrict node-agent.yaml permissions")
	}
	if !strings.Contains(script, "http://metadata.tencentyun.com/latest/meta-data") || !strings.Contains(script, "local-ipv4") {
		t.Fatalf("tencent runtime node user-data does not contain tencent metadata flow")
	}
	if strings.Contains(script, "100.100.100.200") || strings.Contains(script, "X-aliyun-ecs-metadata-token") {
		t.Fatalf("tencent runtime node user-data contains aliyun metadata flow")
	}
	cmd := exec.Command("bash", "-n")
	cmd.Stdin = strings.NewReader(script)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bash -n failed: %v output=%s", err, output)
	}
}

// TestBuildRuntimeNodeHostName 验证 tencent runtime node hostname 长度限制。
func TestBuildRuntimeNodeHostName(t *testing.T) {
	got := buildRuntimeNodeHostName("mini-cloud-runtime-node-super-long-host-name-for-tencent-provider-test")
	if len(got) > 60 {
		t.Fatalf("host name length = %d, want <= 60", len(got))
	}
}
