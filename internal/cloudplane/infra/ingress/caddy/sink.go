// Package caddy 将 cloud-plane ingress 路由快照应用到外置 Caddy。
package caddy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	domainingress "mini-cloud/internal/cloudplane/domain/ingress"
)

// Config 描述外置 Caddy 的配置文件和 reload 命令。
type Config struct {
	// ListenHTTPAddr 是 Caddy HTTP 入口监听地址。
	ListenHTTPAddr string
	// ConfigPath 是 cloud-plane 写入的 Caddyfile 路径。
	ConfigPath string
	// ReloadCommand 是写入 Caddyfile 后执行的 reload 命令及参数。
	ReloadCommand []string
}

// Sink 把路由快照渲染成 Caddyfile，并在内容需要应用时 reload 外置 Caddy。
type Sink struct {
	// logger 记录 Caddyfile 应用结果。
	logger *slog.Logger
	// cfg 是已校验的 Caddy sink 配置。
	cfg Config
	// lastAppliedFingerprint 记录上一次成功 reload 的 Caddyfile 指纹；reload 失败时下一轮会继续重试。
	lastAppliedFingerprint string
}

// NewSink 构造外置 Caddy sink。
// 参数说明：logger 记录 Caddy 应用日志；cfg 提供 Caddyfile 路径和 reload 命令。
func NewSink(logger *slog.Logger, cfg Config) *Sink {
	if logger == nil {
		logger = slog.Default()
	}
	return &Sink{logger: logger, cfg: cfg}
}

// Apply 渲染当前 public service 路由快照，必要时写入 Caddyfile 并 reload 外置 Caddy。
// 参数说明：ctx 控制文件写入和 reload 命令生命周期；routes 是当前应发布的入口路由。
func (s *Sink) Apply(ctx context.Context, routes []domainingress.Route) error {
	// 将路由渲染成完整 Caddyfile；即使没有 route，也会生成一个受控的默认 404 配置。
	content, err := RenderCaddyfile(s.cfg.ListenHTTPAddr, routes)
	if err != nil {
		return err
	}
	// 只有配置内容变化时才写文件；若上轮写入但 reload 失败，下面的 fingerprint 判断会继续重试 reload。
	changed, err := writeFileIfChanged(s.cfg.ConfigPath, []byte(content))
	if err != nil {
		return err
	}
	fingerprint := contentFingerprint([]byte(content))
	if !changed && fingerprint == s.lastAppliedFingerprint {
		return nil
	}
	// Caddyfile 更新或尚未成功应用时，执行用户配置的外置 reload 命令。
	if err := runReloadCommand(ctx, s.cfg.ReloadCommand); err != nil {
		return err
	}
	s.lastAppliedFingerprint = fingerprint
	s.logger.Info("cloud-plane applied ingress caddy config", "routes", len(routes), "config_path", s.cfg.ConfigPath)
	return nil
}

// RenderCaddyfile 将 ingress 路由渲染成外置 Caddy 使用的 Caddyfile。
// 参数说明：listenHTTPAddr 是 Caddy HTTP 监听地址；routes 是 host 到 backend 的发布规则。
func RenderCaddyfile(listenHTTPAddr string, routes []domainingress.Route) (string, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(listenHTTPAddr))
	if err != nil {
		return "", fmt.Errorf("parse caddy listen HTTP addr: %w", err)
	}
	if strings.TrimSpace(port) == "" {
		return "", fmt.Errorf("caddy listen HTTP addr must include port")
	}
	bind := strings.TrimSpace(host)
	var out bytes.Buffer
	out.WriteString("{\n")
	out.WriteString("\tauto_https off\n")
	out.WriteString("\tadmin localhost:2019\n")
	out.WriteString("}\n\n")
	if len(routes) == 0 {
		fmt.Fprintf(&out, ":%s {\n", port)
		writeBindIfNeeded(&out, bind)
		out.WriteString("\trespond \"mini-cloud ingress has no public routes\" 404\n")
		out.WriteString("}\n")
		return out.String(), nil
	}
	for _, route := range routes {
		host := strings.TrimSpace(route.Host)
		if host == "" {
			continue
		}
		fmt.Fprintf(&out, "http://%s:%s {\n", host, port)
		writeBindIfNeeded(&out, bind)
		if len(route.Backends) == 0 {
			out.WriteString("\trespond \"service backend is not ready\" 503\n")
		} else {
			out.WriteString("\treverse_proxy")
			for _, backend := range route.Backends {
				fmt.Fprintf(&out, " %s", backend)
			}
			out.WriteString("\n")
		}
		out.WriteString("}\n\n")
	}
	return out.String(), nil
}

// writeBindIfNeeded 在监听地址不是通配地址时写入 Caddy bind 指令。
func writeBindIfNeeded(out *bytes.Buffer, bind string) {
	if bind == "" || bind == "0.0.0.0" || bind == "::" || bind == "[::]" {
		return
	}
	fmt.Fprintf(out, "\tbind %s\n", strings.Trim(bind, "[]"))
}

// writeFileIfChanged 原子写入配置文件，并返回内容是否发生变化。
func writeFileIfChanged(path string, content []byte) (bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return false, fmt.Errorf("caddy config path is required")
	}
	current, err := os.ReadFile(path)
	if err == nil && bytes.Equal(current, content) {
		return false, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("read caddy config %q: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("create caddy config dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".caddyfile-*")
	if err != nil {
		return false, fmt.Errorf("create temp caddy config: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return false, fmt.Errorf("write temp caddy config: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return false, fmt.Errorf("chmod temp caddy config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return false, fmt.Errorf("close temp caddy config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return false, fmt.Errorf("replace caddy config: %w", err)
	}
	return true, nil
}

// runReloadCommand 执行外置 Caddy reload 命令。
func runReloadCommand(ctx context.Context, command []string) error {
	command = trimCommand(command)
	if len(command) == 0 {
		return fmt.Errorf("caddy reload command is required")
	}
	reloadCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(reloadCtx, command[0], command[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(reloadCtx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("reload caddy timed out: %s", strings.TrimSpace(string(output)))
		}
		return fmt.Errorf("reload caddy: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// contentFingerprint 为已渲染 Caddyfile 生成指纹，用于判断该内容是否已经成功 reload。
func contentFingerprint(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// trimCommand 清理 reload 命令中的空白参数。
func trimCommand(command []string) []string {
	out := make([]string, 0, len(command))
	for _, item := range command {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}
