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
	"mini-cloud/internal/controlplane/deploy"
	"mini-cloud/internal/controlplane/operatorapi"
	"mini-cloud/internal/controlplane/planeselector"
	"mini-cloud/internal/controlplane/planesync"
	controlplaneprocessconfig "mini-cloud/internal/controlplane/processconfig"
	servicecontroller "mini-cloud/internal/controlplane/servicecontroller"
	"mini-cloud/internal/controlplane/store"
	controlplanemigrations "mini-cloud/internal/controlplane/store/migrations"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

type App struct {
	ProcessConfig controlplaneprocessconfig.Config
	Handler       http.Handler

	db             *sql.DB
	stopBackground context.CancelFunc
}

func Build(logger *slog.Logger) (App, error) {
	processCfg, err := controlplaneprocessconfig.Load()
	if err != nil {
		return App{}, fmt.Errorf("load process config: %w", err)
	}

	db, err := store.Open(processCfg.DatabaseURL)
	if err != nil {
		return App{}, fmt.Errorf("open database: %w", err)
	}
	if err := controlplanemigrations.Up(db); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return App{}, errors.Join(fmt.Errorf("run migrations: %w", err), fmt.Errorf("close database after migration failure: %w", closeErr))
		}
		return App{}, fmt.Errorf("run migrations: %w", err)
	}

	stores := store.New(db)
	planeSyncService := planesync.NewService(logger, stores)
	deployService := deploy.NewService(logger, stores)
	planeSelector := planeselector.NewService(logger, stores, deployService)
	serviceController := servicecontroller.New(logger, stores, planeSelector, deployService)
	serviceController.SetReconcileTimeout(time.Duration(processCfg.ServiceReconcileTimeoutSeconds) * time.Second)
	backgroundCtx, backgroundCancel := context.WithCancel(context.Background())
	planesync.StartLoop(backgroundCtx, logger, planeSyncService, time.Duration(processCfg.PlaneSyncIntervalSeconds)*time.Second)
	go serviceController.Run(backgroundCtx)

	logQueryService := logquery.NewService(
		processCfg.LokiURL,
		processCfg.LokiTenantID,
		time.Duration(processCfg.LokiQueryTimeoutSeconds)*time.Second,
	)
	httpAPI := controlplaneapi.NewMux(controlplaneapi.Options{
		AdminToken:        processCfg.AdminToken,
		UIDir:             processCfg.UIDir,
		LogQueryService:   logQueryService,
		PlaneSyncService:  planeSyncService,
		DeployService:     deployService,
		PlaneSelector:     planeSelector,
		ServiceController: serviceController,
	}, logger, stores)
	operatorTransports, err := operatorapi.NewTransportSet(logger, stores, processCfg.AdminToken, planeSyncService, serviceController)
	if err != nil {
		backgroundCancel()
		if closeErr := db.Close(); closeErr != nil {
			return App{}, errors.Join(fmt.Errorf("initialize operator transport: %w", err), fmt.Errorf("close database after operator transport init failure: %w", closeErr))
		}
		return App{}, fmt.Errorf("initialize operator transport: %w", err)
	}

	publicMux := http.NewServeMux()
	publicMux.Handle("/api/operator/v1/", operatorTransports.GatewayHTTP)
	publicMux.Handle("/", httpAPI)

	handler := h2c.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if operatorapi.IsGRPCRequest(r) {
			operatorTransports.GRPC.ServeHTTP(w, r)
			return
		}
		publicMux.ServeHTTP(w, r)
	}), &http2.Server{})

	return App{
		ProcessConfig:  processCfg,
		Handler:        handler,
		db:             db,
		stopBackground: backgroundCancel,
	}, nil
}

func (a App) Close() error {
	if a.stopBackground != nil {
		a.stopBackground()
	}
	if a.db == nil {
		return nil
	}
	return a.db.Close()
}
