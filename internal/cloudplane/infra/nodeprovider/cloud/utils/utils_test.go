package utils

import (
	"strings"
	"testing"
)

// TestShellQuoteEscapesExpansionSyntax 验证写入模板的字符串不会在 shell 或 heredoc 中触发二次展开。
func TestShellQuoteEscapesExpansionSyntax(t *testing.T) {
	t.Parallel()

	got := ShellQuote(`https://example.invalid/$(echo pwn)?token=$TOKEN&name=` + "`whoami`" + `\tail`)
	for i, ch := range got {
		if (ch == '$' || ch == '`') && (i == 0 || got[i-1] != '\\') {
			t.Fatalf("ShellQuote left unescaped expansion character at %d: %s", i, got)
		}
	}
	if !strings.Contains(got, `\$(`) || !strings.Contains(got, `\$TOKEN`) || !strings.Contains(got, "\\`whoami\\`") {
		t.Fatalf("ShellQuote missing expected escapes: %s", got)
	}
}

// TestBuildDockerDaemonJSON 验证 Docker daemon registry mirror 配置渲染和空值清理。
func TestBuildDockerDaemonJSON(t *testing.T) {
	t.Parallel()

	got, err := BuildDockerDaemonJSON([]string{"", " https://mirror.example.com ", "\t"})
	if err != nil {
		t.Fatalf("BuildDockerDaemonJSON returned error: %v", err)
	}
	if !strings.Contains(got, `"registry-mirrors"`) || !strings.Contains(got, `"https://mirror.example.com"`) {
		t.Fatalf("BuildDockerDaemonJSON did not render registry mirror: %s", got)
	}

	empty, err := BuildDockerDaemonJSON([]string{"", " "})
	if err != nil {
		t.Fatalf("BuildDockerDaemonJSON empty returned error: %v", err)
	}
	if empty != "" {
		t.Fatalf("BuildDockerDaemonJSON empty = %q, want empty string", empty)
	}
}
