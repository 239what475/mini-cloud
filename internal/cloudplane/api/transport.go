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
	controlplane.RegisterGRPC(grpcServer, logger, db, stores, cfg)
	nodeagent.RegisterGRPC(grpcServer, logger, stores, cfg.NodeAgent.Token)
	return grpcServer
}
