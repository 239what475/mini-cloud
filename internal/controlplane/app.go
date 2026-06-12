package controlplane

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"mini-cloud/internal/controlplane/api"
	"mini-cloud/internal/controlplane/config"
	"mini-cloud/internal/controlplane/coordination"
	"mini-cloud/internal/controlplane/store"
	"mini-cloud/internal/controlplane/store/migrations"
)

type App struct {
	Config  config.Config
	Handler http.Handler

	logger *slog.Logger
	db     *sql.DB

	closeOnce sync.Once
	closeErr  error
}

func Build(logger *slog.Logger, cfg config.Config) (App, error) {
	if logger == nil {
		logger = slog.Default()
	}
	db, err := store.Open(cfg.Database.URL)
	if err != nil {
		return App{}, fmt.Errorf("open database: %w", err)
	}
	if err := migrations.Up(db); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return App{}, errors.Join(fmt.Errorf("run migrations: %w", err), fmt.Errorf("close database after migration failure: %w", closeErr))
		}
		return App{}, fmt.Errorf("run migrations: %w", err)
	}

	stores := store.New(db)

	dns, err := coordination.NewDNSClient(cfg.DNS.DNSPod)
	if err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return App{}, errors.Join(err, closeErr)
		}
		return App{}, err
	}
	planeSyncer := coordination.NewPlaneSyncer(logger, stores, cfg.Auth.SouthboundToken, dns)
	serviceOperations := coordination.NewServiceOperations(logger, stores, cfg.Auth.SouthboundToken, cfg.DNS.ServiceBaseDomain, planeSyncer)

	handler := api.NewMux(api.Options{
		AdminToken:        cfg.Auth.AdminToken,
		SouthboundToken:   cfg.Auth.SouthboundToken,
		UIDir:             cfg.UI.Dir,
		ServiceOperations: serviceOperations,
		PlaneSyncer:       planeSyncer,
	}, logger, stores)

	return App{
		Config:  cfg,
		Handler: handler,
		logger:  logger,
		db:      db,
	}, nil
}

func (a *App) Close() error {
	a.closeOnce.Do(func() {
		if a.db != nil {
			a.closeErr = a.db.Close()
		}
	})
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
