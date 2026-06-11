package lab

import (
	"context"
	"fmt"
)

func (r *Runner) Bootstrap(ctx context.Context) error {
	if err := r.terraform(ctx, "init"); err != nil {
		return err
	}
	if err := r.terraform(ctx, append([]string{"apply"}, r.cfg.Terraform.ApplyArgs...)...); err != nil {
		return err
	}
	out, err := r.terraformOutput(ctx)
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
	fmt.Println("platform output:")
	return r.terraform(ctx, "output", "platform")
}
