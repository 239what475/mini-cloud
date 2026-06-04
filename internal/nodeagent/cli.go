package nodeagent

import (
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"strings"

	"mini-cloud/internal/common/util"
	agentconfig "mini-cloud/internal/nodeagent/config"
	"mini-cloud/internal/nodeagent/daemon"
)

// ErrConfigRequired 表示 node-agent CLI 没有收到配置文件路径。
var ErrConfigRequired = errors.New("config is required")

// RunCLI 解析 node-agent 命令行参数、加载配置，并启动 daemon 主循环。
func RunCLI(ctx context.Context, logger *slog.Logger, args []string, stderr io.Writer) error {
	configPath, err := parseCLIArgs(args, stderr)
	if err != nil {
		return err
	}

	cfg, err := agentconfig.Load(configPath)
	if err != nil {
		return err
	}

	logger = logger.With(
		"platform_name", cfg.PlatformName,
		"provider", cfg.RegisterInput.Provider,
		"region", cfg.RegisterInput.Region,
	)

	logger.Info("starting node agent loop",
		"server", cfg.ServerURL,
		"instance_id", cfg.RegisterInput.InstanceID,
		"heartbeat_interval", cfg.HeartbeatInterval,
		"work_interval", cfg.WorkInterval,
	)
	return daemon.Run(ctx, logger, cfg)
}

func parseCLIArgs(args []string, stderr io.Writer) (string, error) {
	fs := flag.NewFlagSet("node-agent", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		PrintUsage(stderr)
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
		return "", ErrConfigRequired
	}
	return strings.TrimSpace(*configPath), nil
}

// PrintUsage 向 stderr 写入 node-agent 命令行用法。
func PrintUsage(stderr io.Writer) {
	util.Fprintln(stderr, "usage:")
	util.Fprintln(stderr, "  node-agent --config ./node-agent.yaml")
}
