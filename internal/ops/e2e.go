package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func (r *Runner) E2E(ctx context.Context) (runErr error) {
	needsDestroy := false
	defer func() {
		if !needsDestroy {
			return
		}
		fmt.Println("[mini-cloud ops] destroy ops resources")
		if err := r.Destroy(context.WithoutCancel(ctx)); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("destroy ops resources: %w", err))
		}
	}()

	needsDestroy = true
	if err := r.Deploy(ctx); err != nil {
		return err
	}

	url := r.controlPlanePublicURL(ctx)
	if url == "" {
		return fmt.Errorf("control-plane URL is missing")
	}
	if err := r.runWebE2E(ctx, url); err != nil {
		return err
	}
	return nil
}

func (r *Runner) runWebE2E(ctx context.Context, controlPlaneURL string) error {
	planeIDs := make([]string, 0, len(r.cfg.Planes))
	for _, plane := range r.cfg.Planes {
		planeIDs = append(planeIDs, planeID(plane.Name))
	}

	cmd := exec.CommandContext(ctx, "npm", "--prefix", "web", "run", "test:ops-e2e")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = append(os.Environ(),
		"PLAYWRIGHT_BASE_URL="+strings.TrimRight(controlPlaneURL, "/"),
		"MINI_CLOUD_ADMIN_TOKEN="+r.cfg.Tokens.ControlPlaneAdmin,
		"MINI_CLOUD_E2E_PLANES="+strings.Join(planeIDs, ","),
		"MINI_CLOUD_E2E_BASE_DOMAIN="+strings.Trim(r.cfg.Install.IngressBaseDomain, "."),
	)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("web ops e2e: %w", err)
	}
	return nil
}

func planeID(name string) string {
	return "pln_" + strings.ReplaceAll(strings.TrimSpace(name), "-", "_")
}
