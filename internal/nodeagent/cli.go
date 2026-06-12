package nodeagent

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"strings"

	agentconfig "mini-cloud/internal/nodeagent/config"
)

var errConfigRequired = errors.New("config is required")

func RunCLI(ctx context.Context, logger *slog.Logger, args []string, stderr io.Writer) error {
	configPath, err := parseConfigPath(args, stderr)
	if err != nil {
		return err
	}

	cfg, err := agentconfig.Load(configPath)
	if err != nil {
		return err
	}

	logger = logger.With(
		"platform_name", cfg.Platform.Name,
		"provider", cfg.Node.Provider,
		"region", cfg.Node.Region,
	)

	app, err := Build(logger, cfg)
	if err != nil {
		return err
	}
	defer func() {
		if err := app.Close(); err != nil && logger != nil {
			logger.Warn("close node-agent failed", "error", err)
		}
	}()

	logger.Info("starting mini-cloud node-agent",
		"server", cfg.Server.URL,
		"instance_id", cfg.Node.InstanceID,
		"heartbeat_interval", agentconfig.HeartbeatInterval,
		"work_interval", agentconfig.WorkInterval,
	)
	return app.Run(ctx)
}

func parseConfigPath(args []string, stderr io.Writer) (string, error) {
	fs := flag.NewFlagSet("node-agent", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		printUsage(stderr)
	}
	configPath := fs.String("config", "", "path to node-agent YAML config file")

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

func printUsage(stderr io.Writer) {
	_, _ = fmt.Fprintln(stderr, "usage:")
	_, _ = fmt.Fprintln(stderr, "  node-agent --config ./node-agent.yaml")
}
