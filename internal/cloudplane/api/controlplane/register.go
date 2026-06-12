package controlplane

import (
	"log/slog"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	"mini-cloud/internal/cloudplane/infra/store"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"

	"google.golang.org/grpc"
)

func RegisterGRPC(grpcServer *grpc.Server, logger *slog.Logger, stores *store.Store, cfg cloudplaneconfig.Config) {
	if logger == nil {
		logger = slog.Default()
	}
	auth := newAuthenticator(cfg.ControlPlane.BearerToken)
	cloudplanev1.RegisterCloudPlaneSnapshotServiceServer(grpcServer, newSnapshotServer(logger, stores, cfg, auth))
	cloudplanev1.RegisterCloudPlaneServiceServer(grpcServer, newServiceServer(logger, stores, auth))
}
