package ops

import (
	"context"
	"fmt"
)

func (r *Runner) Build(ctx context.Context) error {
	if err := r.check(ctx); err != nil {
		return err
	}
	return r.build(ctx)
}

func (r *Runner) build(ctx context.Context) error {
	fmt.Println("[mini-cloud ops] build release binaries and web assets")
	if err := runInteractive(ctx, "make", "build-release", "web-build"); err != nil {
		return err
	}
	return r.cfg.validateArtifacts()
}

func (r *Runner) Bootstrap(ctx context.Context) error {
	if err := r.check(ctx); err != nil {
		return err
	}
	return r.bootstrap(ctx)
}

func (r *Runner) bootstrap(ctx context.Context) error {
	for _, plane := range r.cfg.Planes {
		fmt.Printf("[mini-cloud ops] bootstrap infrastructure for plane %s (%s)\n", plane.Name, plane.Provider)
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

func (r *Runner) Install(ctx context.Context) error {
	if err := r.check(ctx); err != nil {
		return err
	}
	return r.install(ctx)
}

func (r *Runner) Deploy(ctx context.Context) error {
	if err := r.check(ctx); err != nil {
		return err
	}
	if err := r.build(ctx); err != nil {
		return err
	}
	if err := r.bootstrap(ctx); err != nil {
		return err
	}
	return r.install(ctx)
}
