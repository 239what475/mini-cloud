package controlplane

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"mini-cloud/internal/controlplane/api"
	"mini-cloud/internal/controlplane/config"
	"mini-cloud/internal/controlplane/coordination"
	"mini-cloud/internal/controlplane/logquery"
	"mini-cloud/internal/controlplane/store"
	"mini-cloud/internal/controlplane/store/migrations"
)

type App struct {
	Config  config.Config
	Handler http.Handler

	logger *slog.Logger
	db     *sql.DB
	cancel context.CancelFunc
}

func Build(logger *slog.Logger, cfg config.Config) (App, error) {
	db, err := store.Open(cfg.DatabaseURL)
	if err != nil {
		return App{}, fmt.Errorf("open database: %w", err)
	}
	if err := migrations.Up(db); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return App{}, errors.Join(fmt.Errorf("run migrations: %w", err), fmt.Errorf("close database after migration failure: %w", closeErr))
		}
		return App{}, fmt.Errorf("run migrations: %w", err)
	}

	backgroundCtx, cancel := context.WithCancel(context.Background())

	stores := store.New(db)

	planeSyncer := coordination.NewPlaneSyncer(logger, stores)
	coordination.StartPlaneSyncLoop(backgroundCtx, logger, planeSyncer, cfg.PlaneSyncIntervalSeconds)

	serviceController := coordination.NewServiceController(logger, stores)
	serviceController.SetReconcileTimeout(cfg.ServiceReconcileTimeoutSeconds)

	go serviceController.Run(backgroundCtx)

	logQueryService := logquery.NewService(
		cfg.LokiURL,
		cfg.LokiTenantID,
		time.Duration(cfg.LokiQueryTimeoutSeconds)*time.Second,
	)
	handler := api.NewMux(api.Options{
		AdminToken:        cfg.AdminToken,
		UIDir:             cfg.UIDir,
		LogQueryService:   logQueryService,
		ServiceController: serviceController,
	}, logger, stores)

	return App{
		Config:  cfg,
		Handler: handler,
		logger:  logger,
		db:      db,
		cancel:  cancel,
	}, nil
}

func (a App) Close() error {
	if a.cancel != nil {
		a.cancel()
	}
	if a.db == nil {
		return nil
	}
	return a.db.Close()
}

func (a App) Run(ctx context.Context) error {
	server := &http.Server{
		Addr:    a.Config.HTTPAddr,
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
		select {
		case err := <-errCh:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		default:
			return nil
		}
	}
}
