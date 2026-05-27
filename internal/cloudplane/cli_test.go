package cloudplane

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"testing"
)

// TestRunCLIRequiresConfigFlag 验证 cloud-plane CLI 缺少 --config 时拒绝启动。
func TestRunCLIRequiresConfigFlag(t *testing.T) {
	t.Parallel()

	// 不传入任何参数时应返回 ErrConfigRequired。
	err := RunCLI(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)), nil, &bytes.Buffer{})
	if !errors.Is(err, ErrConfigRequired) {
		t.Fatalf("RunCLI error = %v, want ErrConfigRequired", err)
	}
}

// TestRunCLIHelp 验证 help 子命令返回 flag.ErrHelp。
func TestRunCLIHelp(t *testing.T) {
	t.Parallel()

	// help 只打印用法，不初始化配置、数据库或 gRPC server。
	err := RunCLI(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)), []string{"help"}, &bytes.Buffer{})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("RunCLI error = %v, want flag.ErrHelp", err)
	}
}

// TestRunCLIRejectsPositionalArgs 验证 cloud-plane CLI 不接受位置参数。
func TestRunCLIRejectsPositionalArgs(t *testing.T) {
	t.Parallel()

	// 非 flag 参数会被视为错误调用并返回 flag.ErrHelp。
	err := RunCLI(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)), []string{"unexpected"}, &bytes.Buffer{})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("RunCLI error = %v, want flag.ErrHelp", err)
	}
}
