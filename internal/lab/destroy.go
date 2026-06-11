package lab

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

type aliyunInstancesResponse struct {
	Instances struct {
		Instance []struct {
			InstanceID string `json:"InstanceId"`
		} `json:"Instance"`
	} `json:"Instances"`
}

func (r *Runner) Destroy(ctx context.Context) error {
	out, hasOutput, err := r.tryTerraformOutput(ctx)
	if err != nil {
		return err
	}
	platformUninstalled := false
	if hasOutput {
		switch out.ProviderName() {
		case "aliyun":
			if out.PlatformModeName() == "existing_ecs" {
				host, err := r.platformHost(out)
				if err != nil {
					return err
				}
				if err := r.uninstallPlatform(ctx, host); err != nil {
					return err
				}
				platformUninstalled = true
			}
			if err := r.deleteServiceFrontDoors(ctx, out); err != nil {
				return err
			}
			if err := r.deleteAliyunRuntimeNodes(ctx, out); err != nil {
				return err
			}
		case "tencent":
			if out.PlatformModeName() == "existing_lighthouse" {
				host, err := r.platformHost(out)
				if err != nil {
					return err
				}
				if err := r.uninstallPlatform(ctx, host); err != nil {
					return err
				}
				platformUninstalled = true
			}
			if err := r.deleteServiceFrontDoors(ctx, out); err != nil {
				return err
			}
			if err := r.deleteTencentRuntimeNodes(ctx, out); err != nil {
				return err
			}
			if out.PlatformModeName() == "existing_lighthouse" {
				if err := r.deleteLighthouseFirewallRules(ctx, out); err != nil {
					return err
				}
				if err := r.detachLighthouseCCN(ctx, out); err != nil {
					return err
				}
			}
		}
	}
	if !platformUninstalled && strings.TrimSpace(r.cfg.SSH.Host) != "" {
		if err := r.uninstallPlatform(ctx, r.cfg.SSH.Host); err != nil {
			return err
		}
	}
	if !hasOutput {
		if err := r.deleteServiceFrontDoorsWithoutTerraform(ctx); err != nil {
			return err
		}
	}
	return r.terraform(ctx, append([]string{"destroy"}, r.cfg.Terraform.DestroyArgs...)...)
}

func (r *Runner) tryTerraformOutput(ctx context.Context) (TerraformOutput, bool, error) {
	out, err := r.terraformOutput(ctx)
	if err == nil {
		if out.ProviderName() == "" {
			return TerraformOutput{}, false, nil
		}
		return out, true, nil
	}
	return TerraformOutput{}, false, nil
}

func (r *Runner) deleteAliyunRuntimeNodes(ctx context.Context, out TerraformOutput) error {
	ids, err := r.aliyunRuntimeNodeIDs(ctx, out)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	args := []string{"ecs", "DeleteInstances", "--RegionId", out.RegionID(), "--Force", "true"}
	for index, id := range ids {
		args = append(args, "--InstanceId."+strconv.Itoa(index+1), id)
	}
	_, err = runOutput(ctx, "aliyun", args...)
	return err
}

func (r *Runner) aliyunRuntimeNodeIDs(ctx context.Context, out TerraformOutput) ([]string, error) {
	var response aliyunInstancesResponse
	err := runJSON(ctx, &response, "aliyun", "ecs", "DescribeInstances",
		"--RegionId", out.RegionID(),
		"--Tag.1.Key", "managed-by",
		"--Tag.1.Value", "mini-cloud",
		"--Tag.2.Key", "mini-cloud/platform",
		"--Tag.2.Value", out.Platform.Value.Name,
	)
	if err != nil {
		return nil, err
	}
	platformID := strings.TrimSpace(out.Platform.Value.InstanceID)
	var ids []string
	for _, item := range response.Instances.Instance {
		if item.InstanceID != "" && item.InstanceID != platformID {
			ids = append(ids, item.InstanceID)
		}
	}
	return ids, nil
}

func (r *Runner) uninstallPlatform(ctx context.Context, host string) error {
	script, err := renderTemplate("remote-uninstall.sh.tmpl", struct {
		InstallRoot string
	}{InstallRoot: r.cfg.Install.Root})
	if err != nil {
		return fmt.Errorf("render remote uninstall script: %w", err)
	}
	return r.ssh(ctx, host, script)
}
