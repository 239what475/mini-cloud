package controlplane

import (
	"database/sql"
	"log/slog"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	"mini-cloud/internal/cloudplane/infra/store"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"

	"google.golang.org/grpc"
)

type Options struct {
	Logger *slog.Logger
	DB     *sql.DB
	Store  *store.Store
	Config cloudplaneconfig.Config
}

func RegisterGRPC(grpcServer *grpc.Server, opts Options) {
	auth := NewAuthenticator(opts.Config.ControlPlane.Auth.BearerToken)
	cloudplanev1.RegisterControlPlaneSnapshotServiceServer(grpcServer, NewSnapshotServer(opts.Logger, opts.DB, opts.Store, opts.Config, auth))
	cloudplanev1.RegisterControlPlaneExecutionServiceServer(grpcServer, NewExecutionServer(opts.Logger, opts.Store, auth))
}
