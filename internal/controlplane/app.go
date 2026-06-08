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
	controlplaneapi "mini-cloud/internal/controlplane/api"
	"mini-cloud/internal/controlplane/config"
	"mini-cloud/internal/controlplane/deploy"
	"mini-cloud/internal/controlplane/planeselector"
	"mini-cloud/internal/controlplane/planesync"
	servicecontroller "mini-cloud/internal/controlplane/servicecontroller"
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

	stores := store.New(db)
	planeSyncer := planesync.NewSyncer(logger, stores)
	dispatcher := deploy.NewDispatcher(logger, stores)
	selector := planeselector.NewSelector(logger, stores)

	serviceController := servicecontroller.New(logger, stores, selector, dispatcher)
	serviceController.SetReconcileTimeout(time.Duration(cfg.ServiceReconcileTimeoutSeconds) * time.Second)

	backgroundCtx, backgroundCancel := context.WithCancel(context.Background())
	planesync.StartLoop(backgroundCtx, logger, planeSyncer, time.Duration(cfg.PlaneSyncIntervalSeconds)*time.Second)
	go serviceController.Run(backgroundCtx)

	logQueryService := logquery.NewService(
		cfg.LokiURL,
		cfg.LokiTenantID,
		time.Duration(cfg.LokiQueryTimeoutSeconds)*time.Second,
	)
	httpAPI := controlplaneapi.NewMux(controlplaneapi.Options{
		AdminToken:        cfg.AdminToken,
		UIDir:             cfg.UIDir,
		LogQueryService:   logQueryService,
		PlaneSyncer:       planeSyncer,
		Dispatcher:        dispatcher,
		PlaneSelector:     selector,
		ServiceController: serviceController,
	}, logger, stores)

	return App{
		Config:  cfg,
		Handler: httpAPI,
		db:      db,
		cancel:  backgroundCancel,
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
