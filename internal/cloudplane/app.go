package cloudplane

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net"

	cloudplaneapi "mini-cloud/internal/cloudplane/api"
	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	cloudplanecontrol "mini-cloud/internal/cloudplane/control"
	frontdoor "mini-cloud/internal/cloudplane/infra/frontdoor"
	caddyingress "mini-cloud/internal/cloudplane/infra/ingress/caddy"
	nodeprovidercloud "mini-cloud/internal/cloudplane/infra/nodeprovider/cloud"
	"mini-cloud/internal/cloudplane/infra/store"
	cloudplanemigrations "mini-cloud/internal/cloudplane/infra/store/migrations"

	"google.golang.org/grpc"
)

type App struct {
	Config cloudplaneconfig.Config

	logger     *slog.Logger
	db         *sql.DB
	reconciler *cloudplanecontrol.Reconciler
	server     *grpc.Server
}

func Build(logger *slog.Logger, cfg cloudplaneconfig.Config) (App, error) {
	if logger == nil {
		logger = slog.Default()
	}
	driver, err := nodeprovidercloud.NewDriver(cfg)
	if err != nil {
		return App{}, err
	}

	db, err := store.Open(cfg.Database.URL)
	if err != nil {
		return App{}, err
	}
	if err := cloudplanemigrations.Up(db); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return App{}, errors.Join(err, closeErr)
		}
		return App{}, err
	}

	logger = logger.With(
		"plane_name", cfg.Plane.Name,
		"provider", cfg.Infrastructure.Provider,
		"region", cfg.Infrastructure.RegionID,
	)
	stores := store.New(db)
	frontDoorService, err := frontdoor.NewService(logger, cfg, stores)
	if err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return App{}, errors.Join(err, closeErr)
		}
		return App{}, err
	}
	var localIngress cloudplanecontrol.RouteSyncer
	if cfg.Ingress.CaddyAdminURL != "" {
		localIngress = caddyingress.NewSink(logger, caddyingress.Config{
			ListenHTTPAddr:       cloudplaneconfig.CaddyListenHTTPAddr,
			ArtifactListenAddr:   cloudplaneconfig.CaddyArtifactListenAddr,
			ArtifactDocumentRoot: cloudplaneconfig.CaddyArtifactRoot,
			AdminURL:             cfg.Ingress.CaddyAdminURL,
		})
	}

	server, err := cloudplaneapi.NewGRPCServer(cfg, logger, stores)
	if err != nil {
		_ = db.Close()
		return App{}, err
	}

	return App{
		Config:     cfg,
		logger:     logger,
		db:         db,
		reconciler: cloudplanecontrol.NewReconciler(logger, stores, driver, localIngress, frontDoorService, cfg),
		server:     server,
	}, nil
}

func (a App) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", a.Config.Server.ListenGRPCAddr)
	if err != nil {
		return err
	}
	defer listener.Close()

	runCtx, cancel := context.WithCancel(ctx)
	a.reconciler.Start(runCtx)
	defer a.reconciler.Wait()
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- a.server.Serve(listener)
	}()

	select {
	case err := <-errCh:
		cancel()
		if errors.Is(err, grpc.ErrServerStopped) || errors.Is(err, net.ErrClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		cancel()
		a.server.GracefulStop()
		err := <-errCh
		if errors.Is(err, grpc.ErrServerStopped) || errors.Is(err, net.ErrClosed) {
			return nil
		}
		return err
	}
}

func (a App) Close() error {
	if a.server != nil {
		a.server.Stop()
	}
	if a.db == nil {
		return nil
	}
	return a.db.Close()
}
