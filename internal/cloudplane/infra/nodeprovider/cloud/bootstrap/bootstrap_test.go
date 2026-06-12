package bootstrap

import (
	"strings"
	"testing"
)

func TestShellQuoteEscapesExpansionSyntax(t *testing.T) {
	t.Parallel()

	got := shellQuote(`https://example.invalid/$(echo pwn)?token=$TOKEN&name=` + "`whoami`" + `\tail`)
	for i, ch := range got {
		if (ch == '$' || ch == '`') && (i == 0 || got[i-1] != '\\') {
			t.Fatalf("shellQuote left unescaped expansion character at %d: %s", i, got)
		}
	}
	if !strings.Contains(got, `\$(`) || !strings.Contains(got, `\$TOKEN`) || !strings.Contains(got, "\\`whoami\\`") {
		t.Fatalf("shellQuote missing expected escapes: %s", got)
	}
}

func TestBuildDockerDaemonJSON(t *testing.T) {
	t.Parallel()

	got, err := buildDockerDaemonJSON([]string{"", " https://mirror.example.com ", "\t"})
	if err != nil {
		t.Fatalf("buildDockerDaemonJSON returned error: %v", err)
	}
	if !strings.Contains(got, `"registry-mirrors"`) || !strings.Contains(got, `"https://mirror.example.com"`) {
		t.Fatalf("buildDockerDaemonJSON did not render registry mirror: %s", got)
	}

	empty, err := buildDockerDaemonJSON([]string{"", " "})
	if err != nil {
		t.Fatalf("buildDockerDaemonJSON empty returned error: %v", err)
	}
	if empty != "" {
		t.Fatalf("buildDockerDaemonJSON empty = %q, want empty string", empty)
	}
}
