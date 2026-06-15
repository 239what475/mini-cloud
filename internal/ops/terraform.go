package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type terraformValue[T any] struct {
	Value T `json:"value"`
}

type TerraformOutput struct {
	Provider           terraformValue[string]             `json:"provider"`
	PlatformMode       terraformValue[string]             `json:"platform_mode"`
	Platform           terraformValue[PlatformOutput]     `json:"platform"`
	Network            terraformValue[NetworkOutput]      `json:"network"`
	CCN                terraformValue[*CCNOutput]         `json:"ccn"`
	NodeProviderConfig terraformValue[NodeProviderConfig] `json:"node_provider_config"`
	InstallEnv         terraformValue[InstallEnv]         `json:"install_env"`
}

type NodeProviderConfig struct {
	Provider           string   `json:"provider"`
	InstanceType       string   `json:"instanceType"`
	ImageID            string   `json:"imageId"`
	KeyPairName        string   `json:"keyPairName"`
	VSwitchID          string   `json:"vSwitchId"`
	SecurityGroupID    string   `json:"securityGroupId"`
	SystemDiskCategory string   `json:"systemDiskCategory"`
	SystemDiskSizeGiB  int64    `json:"systemDiskSizeGiB"`
	KeyIDs             []string `json:"keyIds"`
	VPCID              string   `json:"vpcId"`
	SubnetID           string   `json:"subnetId"`
	SecurityGroupIDs   []string `json:"securityGroupIds"`
	SystemDiskType     string   `json:"systemDiskType"`
}

type PlatformOutput struct {
	Name         string `json:"name"`
	InstanceID   string `json:"instance_id"`
	PublicIP     string `json:"public_ip"`
	PrivateIP    string `json:"private_ip"`
	SSHHost      string `json:"ssh_host"`
	GRPCEndpoint string `json:"cloud_plane_endpoint"`
}

type NetworkOutput struct {
	SubnetCIDRBlock    string `json:"subnet_cidr_block"`
	CloudPlaneGRPCPort int    `json:"cloud_plane_grpc_port"`
	IngressHTTPPort    int    `json:"ingress_http_port"`
	WorkloadProxyPort  int    `json:"workload_proxy_port"`
	ArtifactHTTPPort   int    `json:"artifact_http_port"`
}

type CCNOutput struct {
	ID string `json:"id"`
}

type InstallEnv struct {
	PlatformPublicIP  string `json:"platform_public_ip"`
	PlatformPrivateIP string `json:"platform_private_ip"`
	PlatformSSHHost   string `json:"platform_ssh_host"`
	RegionID          string `json:"region_id"`
	ZoneID            string `json:"zone_id"`
}

func (r *Runner) terraform(ctx context.Context, plane Plane, args ...string) error {
	full := append([]string{"-chdir=" + plane.Terraform.Dir}, args...)
	return runInteractive(ctx, "terraform", full...)
}

func (r *Runner) selectTerraformWorkspace(ctx context.Context, plane Plane) error {
	args := []string{"-chdir=" + plane.Terraform.Dir, "workspace", "select", plane.Terraform.Workspace}
	if _, err := runOutput(ctx, "terraform", args...); err == nil {
		return nil
	}
	args = []string{"-chdir=" + plane.Terraform.Dir, "workspace", "new", plane.Terraform.Workspace}
	return runInteractive(ctx, "terraform", args...)
}

func (r *Runner) terraformApplyArgs(plane Plane) []string {
	args := append([]string{"apply"}, varFileArg(plane.Terraform.VarFile)...)
	return append(args, "-auto-approve")
}

func (r *Runner) terraformDestroyArgs(plane Plane) []string {
	args := append([]string{"destroy"}, varFileArg(plane.Terraform.VarFile)...)
	return append(args, "-auto-approve")
}

func varFileArg(path string) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	return []string{"-var-file=" + path}
}

func (r *Runner) terraformOutput(ctx context.Context, plane Plane) (TerraformOutput, error) {
	data, err := runOutput(ctx, "terraform", "-chdir="+plane.Terraform.Dir, "output", "-json")
	if err != nil {
		return TerraformOutput{}, err
	}
	var out TerraformOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return TerraformOutput{}, fmt.Errorf("parse terraform output: %w", err)
	}
	return out, nil
}

func (o TerraformOutput) ProviderName() string {
	return strings.ToLower(strings.TrimSpace(o.Provider.Value))
}

func (o TerraformOutput) PlatformModeName() string {
	return strings.ToLower(strings.TrimSpace(o.PlatformMode.Value))
}

func (o TerraformOutput) RegionID() string {
	return strings.TrimSpace(o.InstallEnv.Value.RegionID)
}

func (o TerraformOutput) platformHost() string {
	if host := strings.TrimSpace(o.Platform.Value.SSHHost); host != "" {
		return host
	}
	if host := strings.TrimSpace(o.InstallEnv.Value.PlatformSSHHost); host != "" {
		return host
	}
	if ip := strings.TrimSpace(o.Platform.Value.PublicIP); ip != "" {
		return "root@" + ip
	}
	return ""
}
