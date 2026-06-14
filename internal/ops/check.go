package ops

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func (r *Runner) Check(ctx context.Context) error {
	if err := r.check(ctx); err != nil {
		return err
	}
	fmt.Println("[mini-cloud ops] check passed")
	return nil
}

func (r *Runner) check(ctx context.Context) error {
	if err := r.cfg.validateDeploy(); err != nil {
		return err
	}
	for _, tool := range []string{"go", "make", "npm", "terraform", "ssh", "tccli", "aliyun"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("required tool %q is not available in PATH", tool)
		}
	}
	if err := r.checkTencentCredentialFile(); err != nil {
		return err
	}
	for _, plane := range r.cfg.Planes {
		if err := r.checkPlane(ctx, plane); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) checkTencentCredentialFile() error {
	path := strings.TrimSpace(r.cfg.Provider.TencentCredentialFile)
	if path == "" {
		return fmt.Errorf("provider.tencentCredentialFile is required")
	}
	credential, err := readTencentCredentialFile(path)
	if err != nil {
		return err
	}
	if strings.TrimSpace(credential.SecretID) == "" || strings.TrimSpace(credential.SecretKey) == "" {
		return fmt.Errorf("provider.tencentCredentialFile must contain secretId and secretKey")
	}
	return nil
}

func (r *Runner) checkPlane(ctx context.Context, plane Plane) error {
	if strings.TrimSpace(plane.Terraform.VarFile) == "" {
		return fmt.Errorf("plane %s terraform.varFile is required", plane.Name)
	}
	if _, err := os.Stat(plane.Terraform.VarFile); err != nil {
		return fmt.Errorf("plane %s terraform.varFile %q is not readable: %w", plane.Name, plane.Terraform.VarFile, err)
	}
	if strings.TrimSpace(plane.Terraform.Dir) == "" {
		return fmt.Errorf("plane %s terraform.dir is required", plane.Name)
	}
	if _, err := os.Stat(plane.Terraform.Dir); err != nil {
		return fmt.Errorf("plane %s terraform.dir %q is not readable: %w", plane.Name, plane.Terraform.Dir, err)
	}
	if strings.TrimSpace(plane.SSH.Host) == "" {
		return fmt.Errorf("plane %s ssh.host is required", plane.Name)
	}
	if err := r.ssh(ctx, plane.SSH, plane.SSH.Host, []byte("true\n")); err != nil {
		return fmt.Errorf("plane %s ssh check failed: %w", plane.Name, err)
	}
	return nil
}
