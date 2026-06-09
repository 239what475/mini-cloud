package aliyun

import (
	"encoding/base64"
	"os/exec"
	"strings"
	"testing"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
)

// testRuntimeConfig 构造 aliyun driver 单测用的 cloud-plane 配置。
func testRuntimeConfig(spec map[string]any) cloudplaneconfig.Config {
	// 组装 aliyun driver 单测所需的公共配置。
	return cloudplaneconfig.Config{
		// plane/infrastructure/node-agent 字段参与 user-data 和实例归属信息生成。
		Plane: cloudplaneconfig.PlaneConfig{Name: "mini-cloud-lab"},
		Infrastructure: cloudplaneconfig.InfrastructureConfig{
			Provider: Name,
			RegionID: "cn-beijing",
			ZoneID:   "cn-beijing-f",
		},
		NodeAgent: cloudplaneconfig.NodeAgentConfig{
			ConnectEndpoint: "10.0.0.10:18081",
			BootstrapToken:  "bootstrap-token",
			BinaryURL:       "https://artifacts.example.com/node-agent-linux-amd64",
		},
		RuntimeProvisioning: cloudplaneconfig.RuntimeProvisioningConfig{
			InstanceType: "ecs.u1-c1m1.large",
			ProviderSpec: spec,
		},
	}
}

// TestParseRuntimeConfig 验证 aliyun runtime config 解析和默认值填充。
func TestParseRuntimeConfig(t *testing.T) {
	// 只提供必填 providerSpec 字段，验证 ParseRuntimeConfig 会填入默认磁盘配置。
	spec := map[string]any{
		"imageId":         "m-123",
		"vSwitchId":       "vsw-123",
		"securityGroupId": "sg-123",
	}

	// 解析完整 runtime config，得到 aliyun provider 的强类型配置。
	typed, err := ParseRuntimeConfig(testRuntimeConfig(spec))
	if err != nil {
		t.Fatalf("ParseRuntimeConfig returned error: %v", err)
	}

	// 默认值应稳定，避免用户配置文件必须重复声明常规磁盘参数。
	if typed.ProviderSpec.SystemDiskCategory != "cloud_essd" {
		t.Fatalf("SystemDiskCategory = %q, want cloud_essd", typed.ProviderSpec.SystemDiskCategory)
	}
	if typed.ProviderSpec.SystemDiskSizeGiB != 40 {
		t.Fatalf("SystemDiskSizeGiB = %d, want 40", typed.ProviderSpec.SystemDiskSizeGiB)
	}
	if typed.CloudPlane.RuntimeProvisioning.InstanceType != "ecs.u1-c1m1.large" {
		t.Fatalf("InstanceType = %q, want ecs.u1-c1m1.large", typed.CloudPlane.RuntimeProvisioning.InstanceType)
	}
}

// TestParseRuntimeConfigRequiresAliyunFields 验证 aliyun provider 必填字段缺失时解析失败。
func TestParseRuntimeConfigRequiresAliyunFields(t *testing.T) {
	spec := map[string]any{"imageId": "m-123"}

	if _, err := ParseRuntimeConfig(testRuntimeConfig(spec)); err == nil {
		t.Fatalf("expected ParseRuntimeConfig to reject missing provider-specific fields")
	}
}

// TestBuildRuntimeNodeUserDataDoesNotTraceBootstrapToken 验证 aliyun user-data 不通过 xtrace 暴露 bootstrap token，并限制配置文件权限。
func TestBuildRuntimeNodeUserDataDoesNotTraceBootstrapToken(t *testing.T) {
	t.Parallel()

	// 使用最小 driver 配置构造 user-data，不触发真实 aliyun SDK 调用。
	driver := runtimeDriver{
		config: RuntimeConfig{
			CloudPlane: testRuntimeConfig(nil),
		},
	}

	// 生成 user-data 后先解 base64，检查脚本内容安全属性。
	script, err := driver.buildRuntimeNodeUserData("runtime-node-a", instanceTypeCapacity{
		instanceType: "ecs.u1-c1m1.large",
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
	if !strings.Contains(script, "http://100.100.100.200/latest") || !strings.Contains(script, "X-aliyun-ecs-metadata-token") {
		t.Fatalf("aliyun runtime node user-data does not contain aliyun metadata flow")
	}
	if strings.Contains(script, "metadata.tencentyun.com") || strings.Contains(script, "local-ipv4") {
		t.Fatalf("aliyun runtime node user-data contains tencent metadata flow")
	}
	cmd := exec.Command("bash", "-n")
	cmd.Stdin = strings.NewReader(script)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bash -n failed: %v output=%s", err, output)
	}
}
