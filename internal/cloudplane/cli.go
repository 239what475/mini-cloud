package cloudplane

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"strings"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
)

var errConfigRequired = errors.New("config is required")

func parseConfigPath(args []string, stderr io.Writer) (string, error) {
	fs := flag.NewFlagSet("cloud-plane", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		printUsage(stderr)
	}
	configPath := fs.String("config", "", "path to cloud-plane YAML config file")

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
		return "", errConfigRequired
	}
	return strings.TrimSpace(*configPath), nil
}

func RunCLI(ctx context.Context, logger *slog.Logger, args []string, stderr io.Writer) error {
	configPath, err := parseConfigPath(args, stderr)
	if err != nil {
		return err
	}
	cfg, err := cloudplaneconfig.Load(configPath)
	if err != nil {
		return err
	}
	app, err := Build(logger, cfg)
	if err != nil {
		return err
	}
	defer func() {
		if err := app.Close(); err != nil && logger != nil {
			logger.Warn("close cloud-plane failed", "error", err)
		}
	}()

	logger.Info("starting mini-cloud cloud-plane",
		"config_path", app.Config.Path,
		"grpc_addr", app.Config.Server.ListenGRPCAddr,
		"zone", app.Config.Infrastructure.ZoneID,
	)
	return app.Run(ctx)
}

func printUsage(stderr io.Writer) {
	_, _ = fmt.Fprintln(stderr, "usage:")
	_, _ = fmt.Fprintln(stderr, "  cloud-plane --config ./cloud-plane.yaml")
}
