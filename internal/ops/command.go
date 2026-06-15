package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Runner struct {
	cfg Config
}

func NewRunner(cfg Config) *Runner {
	return &Runner{cfg: cfg}
}

func runInteractive(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func runOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	var lastErr error
	attempts := 1
	timeout := time.Duration(0)
	if cloudCLI(name) {
		attempts = 3
		timeout = time.Minute
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		commandCtx := ctx
		cancel := func() {}
		if timeout > 0 {
			commandCtx, cancel = context.WithTimeout(ctx, timeout)
		}
		cmd := exec.CommandContext(commandCtx, name, args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		data, err := cmd.Output()
		ctxErr := commandCtx.Err()
		cancel()
		if err == nil {
			return data, nil
		}
		lastErr = fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
		if ctxErr != nil {
			lastErr = fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), ctxErr)
		}
		if attempt == attempts || !temporaryCommandFailure(lastErr) {
			return nil, lastErr
		}
		fmt.Printf("[mini-cloud ops] retry %s after temporary failure (%d/%d): %v\n", name, attempt+1, attempts, lastErr)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt*5) * time.Second):
		}
	}
	return nil, lastErr
}

func runInput(ctx context.Context, stdin io.Reader, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func runJSON(ctx context.Context, target any, name string, args ...string) error {
	data, err := runOutput(ctx, name, args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("parse %s %s output: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func cloudCLI(name string) bool {
	switch name {
	case "aliyun", "tccli":
		return true
	default:
		return false
	}
}

func temporaryCommandFailure(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "temporary failure in name resolution") ||
		strings.Contains(message, "nameresolutionerror") ||
		strings.Contains(message, "failed to resolve") ||
		strings.Contains(message, "context deadline exceeded") ||
		strings.Contains(message, "max retries exceeded") ||
		strings.Contains(message, "connection timed out") ||
		strings.Contains(message, "timeout")
}
