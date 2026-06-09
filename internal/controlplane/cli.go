package controlplane

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"mini-cloud/internal/controlplane/config"
)

var ErrConfigRequired = errors.New("config is required")

func ParseConfigPath(args []string, stderr io.Writer) (string, error) {
	fs := flag.NewFlagSet("control-plane", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		PrintUsage(stderr)
	}
	configPath := fs.String("config", "", "path to control-plane YAML config file")

	if len(args) == 1 && args[0] == "help" {
		fs.Usage()
		return "", flag.ErrHelp
	}
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	if fs.NArg() > 0 {
		fs.Usage()
		return "", flag.ErrHelp
	}
	if strings.TrimSpace(*configPath) == "" {
		fs.Usage()
		return "", ErrConfigRequired
	}
	return strings.TrimSpace(*configPath), nil
}

func RunCLI(ctx context.Context, logger *slog.Logger, args []string, stderr io.Writer) error {
	configPath, err := ParseConfigPath(args, stderr)
	if err != nil {
		return err
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	app, err := Build(logger, cfg)
	if err != nil {
		return err
	}
	defer func() {
		if err := app.Close(); err != nil && logger != nil {
			logger.Warn("close control-plane failed", "error", err)
		}
	}()

	logger.Info("starting mini-cloud control-plane",
		"config_path", app.Config.Path,
		"http_addr", app.Config.HTTPAddr,
		"ui_dir", app.Config.UIDir,
	)
	return app.Run(ctx)
}

func PrintUsage(stderr io.Writer) {
	_, _ = fmt.Fprintln(stderr, "usage:")
	_, _ = fmt.Fprintln(stderr, "  control-plane --config ./control-plane.yaml")
}
