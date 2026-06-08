package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"mini-cloud/internal/controlplane"
	"mini-cloud/internal/controlplane/config"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cfg, err := loadConfig(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		logger.Error("failed to load control-plane config", "error", err)
		os.Exit(1)
	}

	plane, err := controlplane.Build(logger, cfg)
	if err != nil {
		logger.Error("failed to build control-plane", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := plane.Close(); err != nil {
			logger.Warn("close control-plane failed", "error", err)
		}
	}()

	logger.Info("starting mini-cloud control-plane",
		"config_path", plane.Config.Path,
		"http_addr", plane.Config.HTTPAddr,
		"ui_dir", plane.Config.UIDir,
	)

	if err := http.ListenAndServe(plane.Config.HTTPAddr, plane.Handler); err != nil {
		logger.Error("control-plane exited", "error", err)
		os.Exit(1)
	}
}

func loadConfig(args []string) (config.Config, error) {
	fs := flag.NewFlagSet("control-plane", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage:")
		fmt.Fprintln(os.Stderr, "  control-plane --config ./control-plane.yaml")
	}
	configPath := fs.String("config", "", "path to control-plane YAML config file")
	if err := fs.Parse(args); err != nil {
		return config.Config{}, err
	}
	if fs.NArg() > 0 {
		fs.Usage()
		return config.Config{}, flag.ErrHelp
	}
	if strings.TrimSpace(*configPath) == "" {
		fs.Usage()
		return config.Config{}, fmt.Errorf("config is required")
	}
	return config.Load(*configPath)
}
