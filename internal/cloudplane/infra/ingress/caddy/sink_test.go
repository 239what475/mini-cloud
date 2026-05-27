package caddy

import (
	"context"
	"os"
	"strings"
	"testing"

	domainingress "mini-cloud/internal/cloudplane/domain/ingress"
)

// TestRenderCaddyfileBuildsReverseProxyRoute 验证外置 Caddy 配置包含 host 站点和私网 backend。
func TestRenderCaddyfileBuildsReverseProxyRoute(t *testing.T) {
	content, err := RenderCaddyfile("0.0.0.0:8080", []domainingress.Route{{
		Host:     "api.team.apps.example.test",
		Backends: []string{"10.0.1.20:32768", "10.0.1.21:32769"},
	}})
	if err != nil {
		t.Fatalf("RenderCaddyfile returned error: %v", err)
	}
	for _, want := range []string{
		"http://api.team.apps.example.test:8080",
		"reverse_proxy 10.0.1.20:32768 10.0.1.21:32769",
		"auto_https off",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("Caddyfile missing %q:\n%s", want, content)
		}
	}
}

// TestRenderCaddyfileUses503ForRouteWithoutBackends 验证 public service 没有 ready backend 时返回受控 503。
func TestRenderCaddyfileUses503ForRouteWithoutBackends(t *testing.T) {
	content, err := RenderCaddyfile("0.0.0.0:80", []domainingress.Route{{Host: "api.team.apps.example.test"}})
	if err != nil {
		t.Fatalf("RenderCaddyfile returned error: %v", err)
	}
	if !strings.Contains(content, "respond \"service backend is not ready\" 503") {
		t.Fatalf("Caddyfile does not contain 503 backend fallback:\n%s", content)
	}
}

// TestSinkSkipsReloadAfterSuccessfulUnchangedApply 验证同一份已成功应用的 Caddyfile 不会反复 reload。
func TestSinkSkipsReloadAfterSuccessfulUnchangedApply(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := dir + "/Caddyfile"
	reloadLog := dir + "/reload.log"
	sink := NewSink(nil, Config{
		ListenHTTPAddr: "0.0.0.0:80",
		ConfigPath:     configPath,
		ReloadCommand:  []string{"bash", "-c", "echo reload >> \"$1\"", "_", reloadLog},
	})
	routes := []domainingress.Route{{Host: "api.team.apps.example.test", Backends: []string{"10.0.1.20:30080"}}}

	if err := sink.Apply(context.Background(), routes); err != nil {
		t.Fatalf("first Apply returned error: %v", err)
	}
	if err := sink.Apply(context.Background(), routes); err != nil {
		t.Fatalf("second Apply returned error: %v", err)
	}

	data, err := os.ReadFile(reloadLog)
	if err != nil {
		t.Fatalf("ReadFile(reloadLog) returned error: %v", err)
	}
	if got := strings.Count(string(data), "reload"); got != 1 {
		t.Fatalf("reload count = %d, want 1; log=%q", got, string(data))
	}
}

// TestSinkRetriesReloadAfterFailure 验证 Caddyfile 写入成功但 reload 失败后，下一轮会继续尝试 reload。
func TestSinkRetriesReloadAfterFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := dir + "/Caddyfile"
	reloadLog := dir + "/reload.log"
	sink := NewSink(nil, Config{
		ListenHTTPAddr: "0.0.0.0:80",
		ConfigPath:     configPath,
		ReloadCommand:  []string{"bash", "-c", "echo fail >> \"$1\"; exit 1", "_", reloadLog},
	})
	routes := []domainingress.Route{{Host: "api.team.apps.example.test", Backends: []string{"10.0.1.20:30080"}}}

	if err := sink.Apply(context.Background(), routes); err == nil {
		t.Fatal("first Apply returned nil error, want reload failure")
	}
	sink.cfg.ReloadCommand = []string{"bash", "-c", "echo ok >> \"$1\"", "_", reloadLog}
	if err := sink.Apply(context.Background(), routes); err != nil {
		t.Fatalf("second Apply returned error: %v", err)
	}

	data, err := os.ReadFile(reloadLog)
	if err != nil {
		t.Fatalf("ReadFile(reloadLog) returned error: %v", err)
	}
	logText := string(data)
	if !strings.Contains(logText, "fail") || !strings.Contains(logText, "ok") {
		t.Fatalf("reload log = %q, want both fail and ok attempts", logText)
	}
}
