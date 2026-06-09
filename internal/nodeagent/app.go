package nodeagent

import (
	"context"
	"log/slog"

	agentclient "mini-cloud/internal/nodeagent/client"
	agentconfig "mini-cloud/internal/nodeagent/config"
	"mini-cloud/internal/nodeagent/daemon"
	"mini-cloud/internal/nodeagent/runtime"
	"mini-cloud/internal/nodeagent/workloadlogs"
)

type App struct {
	Config agentconfig.Config

	runner *daemon.Runner
}

func Build(logger *slog.Logger, cfg agentconfig.Config) (App, error) {
	if logger == nil {
		logger = slog.Default()
	}
	client := agentclient.New(agentclient.Config{
		ServerURL:      cfg.ServerURL,
		BootstrapToken: cfg.BootstrapToken,
	})
	containerRuntime, err := runtime.New(logger, runtime.Config{
		Type: cfg.Runtime.Type,
	})
	if err != nil {
		_ = client.Close()
		return App{}, err
	}
	workloadLogs, err := workloadlogs.NewManager(logger, workloadlogs.Config{
		LokiURL:      cfg.WorkloadLogLokiURL,
		LokiTenantID: cfg.WorkloadLogLokiTenant,
		PlatformName: cfg.PlatformName,
		PushTimeout:  cfg.Logs.PushTimeout,
	}, containerRuntime)
	if err != nil {
		_ = client.Close()
		_ = containerRuntime.Close()
		return App{}, err
	}
	return App{
		Config: cfg,
		runner: daemon.NewRunner(logger, cfg, client, containerRuntime, workloadLogs),
	}, nil
}

func (a App) Run(ctx context.Context) error {
	return a.runner.Run(ctx)
}

func (a App) Close() error {
	if a.runner == nil {
		return nil
	}
	return a.runner.Close()
}
