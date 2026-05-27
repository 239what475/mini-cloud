package nodeagent

import (
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"

	"mini-cloud/internal/common/util"
	agentconfig "mini-cloud/internal/nodeagent/config"
	"mini-cloud/internal/nodeagent/daemon"
)

// ErrConfigRequired 表示 node-agent CLI 没有收到配置文件路径。
var ErrConfigRequired = errors.New("config is required")

// RunCLI 解析 node-agent 命令行参数、加载配置，并启动 daemon 主循环。
func RunCLI(ctx context.Context, logger *slog.Logger, args []string, stderr io.Writer) error {
	if len(args) == 0 {
		PrintUsage(stderr)
		return ErrConfigRequired
	}
	if len(args) == 1 && args[0] == "help" {
		PrintUsage(stderr)
		return flag.ErrHelp
	}

	fs := flag.NewFlagSet("node-agent", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		PrintUsage(stderr)
	}
	configPath := fs.String("config", "", "path to node-agent YAML config file")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configPath == "" {
		return ErrConfigRequired
	}

	cfg, err := agentconfig.Load(*configPath)
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
		"state_file", cfg.StateFile,
	)
	return daemon.Run(ctx, logger, cfg)
}

// PrintUsage 向 stderr 写入 node-agent 命令行用法。
func PrintUsage(stderr io.Writer) {
	util.Fprintln(stderr, "usage:")
	util.Fprintln(stderr, "  node-agent --config ./node-agent.yaml")
}
