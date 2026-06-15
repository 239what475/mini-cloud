package ops

import (
	"context"
	"fmt"
)

func (r *Runner) build(ctx context.Context) error {
	fmt.Println("[mini-cloud ops] build release binaries and web assets")
	if err := runInteractive(ctx, "make", "release"); err != nil {
		return err
	}
	return r.cfg.validateArtifacts()
}

func (r *Runner) bootstrap(ctx context.Context) error {
	return runPlaneTasks(ctx, r.cfg.Planes, r.bootstrapPlane)
}

func (r *Runner) bootstrapPlane(ctx context.Context, plane Plane) error {
	fmt.Printf("[mini-cloud ops] bootstrap infrastructure for plane %s (%s)\n", plane.Name, plane.Provider)
	if err := r.terraformInit(ctx, plane); err != nil {
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
	return r.terraform(ctx, plane, "output", "platform")
}

func (r *Runner) Update(ctx context.Context) error {
	if err := r.check(ctx); err != nil {
		return err
	}
	return r.update(ctx)
}

func (r *Runner) Deploy(ctx context.Context) error {
	if err := r.build(ctx); err != nil {
		return err
	}
	if err := r.bootstrap(ctx); err != nil {
		return err
	}
	return r.install(ctx)
}
