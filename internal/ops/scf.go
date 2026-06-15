package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type scfFunctionResponse struct {
	Status   string       `json:"Status"`
	Role     string       `json:"Role"`
	Triggers []scfTrigger `json:"Triggers"`
}

type scfTrigger struct {
	Type        string `json:"Type"`
	TriggerName string `json:"TriggerName"`
	TriggerDesc string `json:"TriggerDesc"`
	Enable      int    `json:"Enable"`
	BindStatus  string `json:"BindStatus"`
}

type scfTriggerDesc struct {
	NetConfig struct {
		ExtranetURL string `json:"ExtranetUrl"`
	} `json:"NetConfig"`
}

func (r *Runner) upsertSCFFunction(ctx context.Context, image string) error {
	cfg := r.cfg.ControlPlane.SCF
	exists, err := r.scfFunctionExists(ctx)
	if err != nil {
		return err
	}
	if !exists {
		if err := r.createSCFFunction(ctx, image); err != nil {
			return err
		}
	} else {
		if err := r.updateSCFFunction(ctx, image); err != nil {
			return err
		}
	}
	if err := r.waitForSCFActive(ctx); err != nil {
		return err
	}
	if err := r.ensureSCFHTTPTrigger(ctx); err != nil {
		return err
	}
	if err := r.waitForSCFActive(ctx); err != nil {
		return err
	}
	if err := r.ensureSCFCustomDomain(ctx); err != nil {
		return err
	}
	fmt.Printf("[mini-cloud ops] control-plane SCF %s/%s is ready with image %s\n", cfg.Namespace, cfg.FunctionName, image)
	return nil
}

func (r *Runner) scfFunctionExists(ctx context.Context) (bool, error) {
	var response scfFunctionResponse
	err := runJSON(ctx, &response, "tccli", "scf", "GetFunction",
		"--region", r.cfg.ControlPlane.SCF.Region,
		"--Namespace", r.cfg.ControlPlane.SCF.Namespace,
		"--FunctionName", r.cfg.ControlPlane.SCF.FunctionName,
	)
	if err == nil {
		return true, nil
	}
	if commandOutputIndicatesMissingResource(err) || commandOutputContains(err, "function not found") || commandOutputContains(err, "notfound") {
		return false, nil
	}
	return false, err
}

func (r *Runner) createSCFFunction(ctx context.Context, image string) error {
	cfg := r.cfg.ControlPlane.SCF
	input := map[string]any{
		"FunctionName": cfg.FunctionName,
		"Namespace":    cfg.Namespace,
		"Role":         cfg.Role,
		"Runtime":      "CustomImage",
		"Type":         "HTTP",
		"Description":  cfg.Description,
		"MemorySize":   cfg.MemoryMB,
		"Timeout":      cfg.TimeoutSec,
		"InitTimeout":  cfg.InitSec,
		"Code": map[string]any{
			"ImageConfig": map[string]any{
				"ImageType": "personal",
				"ImageUri":  image,
				"ImagePort": 9000,
			},
		},
	}
	path, err := writeJSONTempFile("mini-cloud-scf-create-*.json", input)
	if err != nil {
		return err
	}
	defer removeFiles([]string{path})
	return runInteractive(ctx, "tccli", "scf", "CreateFunction", "--region", cfg.Region, "--cli-input-json", "file://"+path)
}

func (r *Runner) updateSCFFunction(ctx context.Context, image string) error {
	cfg := r.cfg.ControlPlane.SCF
	codeInput := map[string]any{
		"FunctionName": cfg.FunctionName,
		"Namespace":    cfg.Namespace,
		"Code": map[string]any{
			"ImageConfig": map[string]any{
				"ImageType": "personal",
				"ImageUri":  image,
				"ImagePort": 9000,
			},
		},
	}
	codePath, err := writeJSONTempFile("mini-cloud-scf-code-*.json", codeInput)
	if err != nil {
		return err
	}
	defer removeFiles([]string{codePath})
	if err := runInteractive(ctx, "tccli", "scf", "UpdateFunctionCode", "--region", cfg.Region, "--cli-input-json", "file://"+codePath); err != nil {
		return err
	}
	if err := r.waitForSCFActive(ctx); err != nil {
		return err
	}
	configInput := map[string]any{
		"FunctionName": cfg.FunctionName,
		"Namespace":    cfg.Namespace,
		"Role":         cfg.Role,
		"Description":  cfg.Description,
		"MemorySize":   cfg.MemoryMB,
		"Timeout":      cfg.TimeoutSec,
		"InitTimeout":  cfg.InitSec,
	}
	configPath, err := writeJSONTempFile("mini-cloud-scf-config-*.json", configInput)
	if err != nil {
		return err
	}
	defer removeFiles([]string{configPath})
	return runInteractive(ctx, "tccli", "scf", "UpdateFunctionConfiguration", "--region", cfg.Region, "--cli-input-json", "file://"+configPath)
}

func (r *Runner) ensureSCFHTTPTrigger(ctx context.Context) error {
	url, err := r.scfURL(ctx)
	if err != nil {
		return err
	}
	if url != "" {
		return nil
	}
	cfg := r.cfg.ControlPlane.SCF
	triggerName := "mini-cloud-http"
	err = runInteractive(ctx, "tccli", "scf", "CreateTrigger",
		"--region", cfg.Region,
		"--Namespace", cfg.Namespace,
		"--FunctionName", cfg.FunctionName,
		"--Type", "http",
		"--TriggerName", triggerName,
		"--TriggerDesc", `{"AuthType":"NONE","NetConfig":{"EnableExtranet":true},"ApiGwCompatible":false}`,
	)
	if err != nil && !commandOutputContains(err, "already") && !commandOutputContains(err, "exist") {
		return err
	}
	return nil
}

func (r *Runner) ensureSCFCustomDomain(ctx context.Context) error {
	cfg := r.cfg.ControlPlane.SCF
	if strings.TrimSpace(cfg.PublicDomain) == "" {
		return nil
	}
	url, err := r.scfURL(ctx)
	if err != nil {
		return err
	}
	target := scfCustomDomainCNAMETarget(url)
	if target == "" {
		return fmt.Errorf("SCF HTTP trigger URL is missing")
	}
	if err := r.ensureDNSPodCNAMERecord(ctx, cfg.PublicDomain, target, "mini-cloud control-plane"); err != nil {
		return err
	}
	fmt.Printf("[mini-cloud ops] wait for control-plane CNAME %s -> %s\n", cfg.PublicDomain, target)
	if err := r.waitForDNSPodCNAMERecord(ctx, cfg.PublicDomain, target); err != nil {
		return err
	}
	if err := r.ensureSCFCustomDomainBinding(ctx); err != nil {
		return err
	}
	return nil
}

func (r *Runner) ensureSCFCustomDomainBinding(ctx context.Context) error {
	cfg := r.cfg.ControlPlane.SCF
	input := map[string]any{
		"Domain":   cfg.PublicDomain,
		"Protocol": "HTTP",
		"EndpointsConfig": []map[string]any{
			scfCustomDomainEndpoint(cfg, "/"),
			scfCustomDomainEndpoint(cfg, "/api/*"),
			scfCustomDomainEndpoint(cfg, "/assets/*"),
		},
	}
	path, err := writeJSONTempFile("mini-cloud-scf-domain-*.json", input)
	if err != nil {
		return err
	}
	defer removeFiles([]string{path})

	deadline := time.Now().Add(10 * time.Minute)
	for {
		exists, err := r.scfCustomDomainExists(ctx, cfg.PublicDomain)
		if err != nil {
			return err
		}
		action := "CreateCustomDomain"
		if exists {
			action = "UpdateCustomDomain"
		}
		_, err = runOutput(ctx, "tccli", "scf", action, "--region", cfg.Region, "--cli-input-json", "file://"+path)
		if err == nil || commandOutputContains(err, "already") || commandOutputContains(err, "exist") {
			return nil
		}
		if !scfCustomDomainCNAMEPending(err) {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for SCF to accept CNAME for %s: %w", cfg.PublicDomain, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func scfCustomDomainCNAMEPending(err error) bool {
	return commandOutputContains(err, "FailedOperation.CNAME") ||
		commandOutputContains(err, "CNAME记录") ||
		commandOutputContains(err, "CNAME record") ||
		commandOutputContains(err, "must add CNAME")
}

func scfCustomDomainEndpoint(cfg SCFControlPlane, path string) map[string]any {
	return map[string]any{
		"Namespace":    cfg.Namespace,
		"FunctionName": cfg.FunctionName,
		"Qualifier":    "$LATEST",
		"PathMatch":    path,
	}
}

func (r *Runner) scfCustomDomainExists(ctx context.Context, domain string) (bool, error) {
	err := runJSON(ctx, &struct{}{}, "tccli", "scf", "GetCustomDomain",
		"--region", r.cfg.ControlPlane.SCF.Region,
		"--Domain", domain,
	)
	if err == nil {
		return true, nil
	}
	if commandOutputIndicatesMissingResource(err) || commandOutputContains(err, "not found") || commandOutputContains(err, "not exist") {
		return false, nil
	}
	return false, err
}

func (r *Runner) waitForSCFActive(ctx context.Context) error {
	deadline := time.Now().Add(10 * time.Minute)
	for {
		status, err := r.scfStatus(ctx)
		if err != nil {
			return err
		}
		if strings.EqualFold(status, "Active") || strings.EqualFold(status, "Available") {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for SCF function %s to become active; current status: %s", r.cfg.ControlPlane.SCF.FunctionName, status)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func (r *Runner) scfStatus(ctx context.Context) (string, error) {
	var response scfFunctionResponse
	if err := runJSON(ctx, &response, "tccli", "scf", "GetFunction",
		"--region", r.cfg.ControlPlane.SCF.Region,
		"--Namespace", r.cfg.ControlPlane.SCF.Namespace,
		"--FunctionName", r.cfg.ControlPlane.SCF.FunctionName,
	); err != nil {
		return "", err
	}
	return strings.TrimSpace(response.Status), nil
}

func (r *Runner) scfURL(ctx context.Context) (string, error) {
	var response scfFunctionResponse
	if err := runJSON(ctx, &response, "tccli", "scf", "GetFunction",
		"--region", r.cfg.ControlPlane.SCF.Region,
		"--Namespace", r.cfg.ControlPlane.SCF.Namespace,
		"--FunctionName", r.cfg.ControlPlane.SCF.FunctionName,
	); err != nil {
		return "", err
	}
	for _, trigger := range response.Triggers {
		if strings.ToLower(strings.TrimSpace(trigger.Type)) != "http" {
			continue
		}
		var desc scfTriggerDesc
		if err := json.Unmarshal([]byte(trigger.TriggerDesc), &desc); err != nil {
			return "", fmt.Errorf("parse SCF trigger %s: %w", trigger.TriggerName, err)
		}
		if url := strings.TrimSpace(desc.NetConfig.ExtranetURL); url != "" {
			return strings.TrimRight(url, "/"), nil
		}
	}
	return "", nil
}

func (r *Runner) waitForControlPlaneSCF(ctx context.Context) error {
	url := r.controlPlanePublicURL(ctx)
	if url == "" {
		return fmt.Errorf("SCF HTTP trigger URL is missing")
	}
	deadline := time.Now().Add(10 * time.Minute)
	publicURL := r.controlPlanePublicURL(ctx)
	healthURL := publicURL + "/api/healthz"
	var lastStatus int
	var lastErr error
	for {
		status, err := getHTTPStatus(ctx, healthURL, 20*time.Second)
		if err != nil {
			lastErr = err
		} else {
			lastStatus = status
		}
		if status == http.StatusOK {
			fmt.Printf("[mini-cloud ops] control-plane URL: %s\n", publicURL)
			return nil
		}
		if time.Now().After(deadline) {
			if lastErr != nil {
				return fmt.Errorf("timed out waiting for control-plane healthz: %w", lastErr)
			}
			return fmt.Errorf("timed out waiting for control-plane healthz; last status: %d", lastStatus)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func (r *Runner) controlPlanePublicURL(ctx context.Context) string {
	if domain := strings.TrimSpace(r.cfg.ControlPlane.SCF.PublicDomain); domain != "" {
		return "http://" + strings.TrimRight(domain, "/")
	}
	url, err := r.scfURL(ctx)
	if err != nil {
		return ""
	}
	return strings.TrimRight(url, "/")
}

func scfCustomDomainCNAMETarget(url string) string {
	host := strings.TrimPrefix(strings.TrimPrefix(strings.TrimRight(strings.TrimSpace(url), "/"), "https://"), "http://")
	parts := strings.SplitN(host, "-", 2)
	if len(parts) != 2 {
		return cleanDomain(host)
	}
	suffix := parts[1]
	if dot := strings.Index(suffix, "."); dot >= 0 {
		return cleanDomain(parts[0] + suffix[dot:])
	}
	return cleanDomain(host)
}

func writeJSONTempFile(pattern string, value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return writeTempFile(pattern, data, 0600)
}
