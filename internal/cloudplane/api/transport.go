package cloudplaneapi

import (
	"database/sql"
	"log/slog"

	"mini-cloud/internal/cloudplane/api/controlplane"
	"mini-cloud/internal/cloudplane/api/nodeagent"
	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	"mini-cloud/internal/cloudplane/infra/store"

	"google.golang.org/grpc"
)

func NewGRPCServer(cfg cloudplaneconfig.Config, logger *slog.Logger, db *sql.DB, stores *store.Store) *grpc.Server {
	grpcServer := grpc.NewServer()
	controlplane.RegisterGRPC(grpcServer, controlplane.Options{
		Logger: logger,
		DB:     db,
		Store:  stores,
		Config: cfg,
	})
	nodeagent.RegisterGRPC(grpcServer, nodeagent.Options{
		Logger:         logger,
		Store:          stores,
		BootstrapToken: cfg.NodeAgent.Auth.BootstrapToken,
		SessionTTL:     cfg.NodeAgent.Auth.SessionTTL,
	})
	return grpcServer
}
