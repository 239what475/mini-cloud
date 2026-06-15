package ops

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
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
	fmt.Println("[mini-cloud ops] e2e deploy")
	if err := r.Deploy(ctx); err != nil {
		return err
	}

	urls := r.controlPlaneWebURLs(ctx)
	if len(urls) == 0 {
		return fmt.Errorf("control-plane URL is missing")
	}
	fmt.Println("[mini-cloud ops] e2e run full web flow")
	if err := r.runWebE2E(ctx, urls, "full"); err != nil {
		return err
	}
	fmt.Println("[mini-cloud ops] e2e update control-plane")
	if err := r.Update(ctx); err != nil {
		return err
	}
	urls = r.controlPlaneWebURLs(ctx)
	if len(urls) == 0 {
		return fmt.Errorf("control-plane URL is missing after update")
	}
	if err := waitForHTTPStatus(ctx, urls[0]+"/api/healthz", http.StatusOK, 5*time.Minute); err != nil {
		return fmt.Errorf("control-plane update healthz: %w", err)
	}
	fmt.Println("[mini-cloud ops] e2e run post-update web smoke")
	if err := r.runWebE2E(ctx, urls, "smoke"); err != nil {
		return err
	}
	return nil
}

func (r *Runner) runWebE2E(ctx context.Context, controlPlaneURLs []string, mode string) error {
	planeIDs := make([]string, 0, len(r.cfg.Planes))
	for _, plane := range r.cfg.Planes {
		planeIDs = append(planeIDs, planeID(plane.Name))
	}
	urls := make([]string, 0, len(controlPlaneURLs))
	for _, url := range controlPlaneURLs {
		if url = strings.TrimRight(strings.TrimSpace(url), "/"); url != "" {
			urls = append(urls, url)
		}
	}
	if len(urls) == 0 {
		return fmt.Errorf("control-plane URL is missing")
	}

	cmd := exec.CommandContext(ctx, "npm", "--prefix", "web", "run", "test:ops-e2e")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = append(os.Environ(),
		"PLAYWRIGHT_BASE_URL="+urls[0],
		"MINI_CLOUD_CONTROL_PLANE_URLS="+strings.Join(urls, ","),
		"MINI_CLOUD_ADMIN_TOKEN="+r.cfg.Tokens.ControlPlaneAdmin,
		"MINI_CLOUD_E2E_PLANES="+strings.Join(planeIDs, ","),
		"MINI_CLOUD_E2E_BASE_DOMAIN="+strings.Trim(r.cfg.Install.IngressBaseDomain, "."),
		"MINI_CLOUD_E2E_MODE="+mode,
	)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("web ops e2e: %w", err)
	}
	return nil
}

func (r *Runner) controlPlaneWebURLs(ctx context.Context) []string {
	seen := map[string]bool{}
	var urls []string
	add := func(url string) {
		url = strings.TrimRight(strings.TrimSpace(url), "/")
		if url == "" || seen[url] {
			return
		}
		seen[url] = true
		urls = append(urls, url)
	}
	add(r.controlPlanePublicURL(ctx))
	if url, err := r.scfURL(ctx); err == nil {
		add(url)
	}
	return urls
}

func planeID(name string) string {
	return "pln_" + strings.ReplaceAll(strings.TrimSpace(name), "-", "_")
}
