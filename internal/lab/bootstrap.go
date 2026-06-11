package lab

import (
	"context"
	"fmt"
)

func (r *Runner) Bootstrap(ctx context.Context) error {
	for _, plane := range r.cfg.Planes {
		fmt.Printf("[mini-cloud lab] bootstrap plane %s (%s)\n", plane.Name, plane.Provider)
		if err := r.terraform(ctx, plane, "init"); err != nil {
			return err
		}
		if err := r.selectTerraformWorkspace(ctx, plane); err != nil {
			return err
		}
		if err := r.terraform(ctx, plane, r.terraformApplyArgs(plane)...); err != nil {
			return err
		}
		out, err := r.terraformOutput(ctx, plane)
		if err != nil {
			return err
		}
		if out.ProviderName() == "tencent" && out.PlatformModeName() == "existing_lighthouse" {
			if err := r.attachLighthouseCCN(ctx, out); err != nil {
				return err
			}
			if err := r.ensureLighthouseFirewallRules(ctx, out); err != nil {
				return err
			}
		}
		fmt.Println()
		fmt.Printf("platform output for %s:\n", plane.Name)
		if err := r.terraform(ctx, plane, "output", "platform"); err != nil {
			return err
		}
	}
	return nil
}
