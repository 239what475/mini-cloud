package ops

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
	for _, plane := range r.cfg.Planes {
		fmt.Printf("[mini-cloud ops] destroy plane %s (%s)\n", plane.Name, plane.Provider)
		if err := r.terraformInit(ctx, plane); err != nil {
			return err
		}
		if err := r.selectTerraformWorkspace(ctx, plane); err != nil {
			return err
		}
		out, hasOutput, err := r.tryTerraformOutput(ctx, plane)
		if err != nil {
			return err
		}
		if hasOutput {
			if err := r.uninstallCloudPlane(ctx, plane, out); err != nil {
				return err
			}
		} else if strings.TrimSpace(plane.SSH.Host) != "" {
			if err := r.uninstallCloudPlaneAtHost(ctx, plane.SSH, plane.SSH.Host); err != nil {
				return err
			}
		}
		if hasOutput {
			if err := r.destroyPlaneCloudResources(ctx, plane, out); err != nil {
				return err
			}
		} else if err := r.deleteServiceFrontDoorsWithoutTerraform(ctx, plane); err != nil {
			return err
		}
		if err := r.terraform(ctx, plane, r.terraformDestroyArgs(plane)...); err != nil {
			return err
		}
	}
	return r.uninstallControlPlane(ctx)
}

func (r *Runner) destroyPlaneCloudResources(ctx context.Context, plane Plane, out TerraformOutput) error {
	switch out.ProviderName() {
	case "aliyun":
		if err := r.deleteServiceFrontDoors(ctx, out); err != nil {
			return err
		}
		if err := r.deleteAliyunWorkerNodes(ctx, out); err != nil {
			return err
		}
	case "tencent":
		if err := r.deleteServiceFrontDoors(ctx, out); err != nil {
			return err
		}
		if err := r.deleteTencentWorkerNodes(ctx, out); err != nil {
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
	return nil
}

func (r *Runner) tryTerraformOutput(ctx context.Context, plane Plane) (TerraformOutput, bool, error) {
	out, err := r.terraformOutput(ctx, plane)
	if err == nil {
		if out.ProviderName() == "" {
			return TerraformOutput{}, false, nil
		}
		return out, true, nil
	}
	if !terraformOutputUnavailable(err) {
		return TerraformOutput{}, false, err
	}
	return TerraformOutput{}, false, nil
}

func terraformOutputUnavailable(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no state file was found") ||
		strings.Contains(message, "state file either has no outputs") ||
		strings.Contains(message, "has no outputs")
}

func (r *Runner) uninstallCloudPlane(ctx context.Context, plane Plane, out TerraformOutput) error {
	host, err := r.platformHost(plane, out)
	if err != nil {
		return err
	}
	return r.uninstallCloudPlaneAtHost(ctx, plane.SSH, host)
}

func (r *Runner) uninstallCloudPlaneAtHost(ctx context.Context, sshConfig SSHConfig, host string) error {
	script, err := renderTemplate("remote-cloud-plane-uninstall.sh.tmpl", struct {
		InstallRoot    string
		RemovePostgres bool
	}{InstallRoot: r.cfg.Install.Root, RemovePostgres: true})
	if err != nil {
		return fmt.Errorf("render remote cloud-plane uninstall script: %w", err)
	}
	return r.ssh(ctx, sshConfig, host, script)
}

func (r *Runner) uninstallControlPlane(ctx context.Context) error {
	cfg := r.cfg.ControlPlane.SCF
	if strings.TrimSpace(cfg.FunctionName) == "" {
		return nil
	}
	if strings.TrimSpace(cfg.PublicDomain) != "" {
		_, err := runOutput(ctx, "tccli", "scf", "DeleteCustomDomain",
			"--region", cfg.Region,
			"--Domain", cfg.PublicDomain,
		)
		if err != nil && !commandOutputIndicatesMissingResource(err) && !commandOutputContains(err, "notfound") && !commandOutputContains(err, "not found") {
			return err
		}
		if err := r.deleteDNSPodRecordByRemark(ctx, cfg.PublicDomain, "CNAME", "mini-cloud control-plane"); err != nil {
			return err
		}
	}
	_, err := runOutput(ctx, "tccli", "scf", "DeleteFunction",
		"--region", cfg.Region,
		"--Namespace", cfg.Namespace,
		"--FunctionName", cfg.FunctionName,
	)
	if err != nil && !commandOutputIndicatesMissingResource(err) && !commandOutputContains(err, "notfound") && !commandOutputContains(err, "not found") {
		return err
	}
	return nil
}

func (r *Runner) deleteAliyunWorkerNodes(ctx context.Context, out TerraformOutput) error {
	ids, err := r.aliyunWorkerNodeIDs(ctx, out)
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

func (r *Runner) aliyunWorkerNodeIDs(ctx context.Context, out TerraformOutput) ([]string, error) {
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
