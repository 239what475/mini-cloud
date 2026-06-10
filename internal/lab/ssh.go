package lab

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func (r *Runner) sshArgs() []string {
	args := make([]string, 0, 2+len(r.cfg.SSH.Options))
	if key := strings.TrimSpace(r.cfg.SSH.KeyPath); key != "" {
		args = append(args, "-i", key)
	}
	args = append(args, r.cfg.SSH.Options...)
	return args
}

func (r *Runner) platformHost(out TerraformOutput) (string, error) {
	if host := strings.TrimSpace(r.cfg.SSH.Host); host != "" {
		return host, nil
	}
	host := out.platformHost()
	if host == "" {
		return "", fmt.Errorf("platform SSH host is missing; set ssh.host in deploy/lab/lab.yaml")
	}
	return host, nil
}

func (r *Runner) scp(ctx context.Context, local string, remote string) error {
	args := append(r.sshArgs(), local, remote)
	return runInteractive(ctx, "scp", args...)
}

func (r *Runner) ssh(ctx context.Context, host string, script []byte) error {
	args := append(r.sshArgs(), host)
	args = append(args, "if [ \"$(id -u)\" = 0 ]; then bash -s; else sudo -n bash -s; fi")
	return runInput(ctx, bytes.NewReader(script), "ssh", args...)
}

func (r *Runner) detectPrivateIP(ctx context.Context, host string) (string, error) {
	args := append(r.sshArgs(), host, "hostname -I")
	data, err := runOutput(ctx, "ssh", args...)
	if err != nil {
		return "", err
	}
	for _, field := range strings.Fields(string(data)) {
		if strings.HasPrefix(field, "10.") || strings.HasPrefix(field, "192.168.") || private172(field) {
			return field, nil
		}
	}
	return "", fmt.Errorf("could not detect a private IP on %s", host)
}

func private172(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) < 2 || parts[0] != "172" {
		return false
	}
	second, err := strconv.Atoi(parts[1])
	return err == nil && second >= 16 && second <= 31
}

func writeTempFile(pattern string, data []byte, mode os.FileMode) (string, error) {
	file, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	path := file.Name()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if err := os.Chmod(path, mode); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}
