package controlplane

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"mini-cloud/internal/common/logquery"
	"mini-cloud/internal/controlplane/api"
	"mini-cloud/internal/controlplane/config"
	"mini-cloud/internal/controlplane/controller"
	"mini-cloud/internal/controlplane/deploy"
	"mini-cloud/internal/controlplane/planeselector"
	"mini-cloud/internal/controlplane/planesync"
	"mini-cloud/internal/controlplane/store"
	"mini-cloud/internal/controlplane/store/migrations"
)

type App struct {
	Config  config.Config
	Handler http.Handler

	db     *sql.DB
	cancel context.CancelFunc
}

func Build(logger *slog.Logger) (App, error) {
	cfg, err := config.Load()
	if err != nil {
		return App{}, fmt.Errorf("load process config: %w", err)
	}

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

	syncer := planesync.NewSyncer(logger, stores)
	planesync.StartLoop(backgroundCtx, logger, syncer, cfg.PlaneSyncIntervalSeconds)

	selector := planeselector.NewSelector(logger, stores)
	dispatcher := deploy.NewDispatcher(logger, stores)

	serviceController := controller.New(logger, stores, selector, dispatcher)
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
		PlaneSyncer:       syncer,
		Dispatcher:        dispatcher,
		PlaneSelector:     selector,
		ServiceController: serviceController,
	}, logger, stores)

	return App{
		Config:  cfg,
		Handler: handler,
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
