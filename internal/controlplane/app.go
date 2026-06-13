package controlplane

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"mini-cloud/internal/controlplane/api"
	"mini-cloud/internal/controlplane/config"
	"mini-cloud/internal/controlplane/coordination"
)

type App struct {
	Config  config.Config
	Handler http.Handler

	logger *slog.Logger

	closeOnce sync.Once
	closeErr  error
}

func Build(logger *slog.Logger, cfg config.Config) (App, error) {
	if logger == nil {
		logger = slog.Default()
	}
	planes, err := coordination.NewPlaneCatalog(cfg.Planes)
	if err != nil {
		return App{}, err
	}
	dns, err := coordination.NewDNSClient(cfg.DNS.DNSPod)
	if err != nil {
		return App{}, err
	}
	planeSyncer := coordination.NewPlaneSyncer(logger, planes, cfg.Auth.SouthboundToken, dns)
	serviceOperations := coordination.NewServiceOperations(logger, planes, cfg.Auth.SouthboundToken, cfg.DNS.ServiceBaseDomain, planeSyncer)

	handler, err := api.NewMux(api.Options{
		AdminToken:        cfg.Auth.AdminToken,
		SouthboundToken:   cfg.Auth.SouthboundToken,
		UIDir:             cfg.UI.Dir,
		ServiceOperations: serviceOperations,
		PlaneSyncer:       planeSyncer,
	}, logger)
	if err != nil {
		return App{}, err
	}

	return App{
		Config:  cfg,
		Handler: handler,
		logger:  logger,
	}, nil
}

func (a *App) Close() error {
	a.closeOnce.Do(func() {})
	return a.closeErr
}

func (a *App) Run(ctx context.Context) error {
	server := &http.Server{
		Addr:    a.Config.Server.HTTPAddr,
		Handler: a.Handler,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			if a.logger != nil {
				a.logger.Warn("control-plane graceful shutdown failed", "error", err)
			}
			if closeErr := server.Close(); closeErr != nil {
				return errors.Join(err, closeErr)
			}
			return err
		}
		err := <-errCh
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
