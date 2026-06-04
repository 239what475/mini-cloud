package execution

import (
	"log/slog"

	controlplane "mini-cloud/internal/cloudplane/api/controlplane"
	"mini-cloud/internal/cloudplane/infra/store"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"
)

type Server struct {
	cloudplanev1.UnimplementedControlPlaneExecutionServiceServer

	logger *slog.Logger
	store  *store.Store
	auth   controlplane.Authenticator
}

func NewServer(logger *slog.Logger, stores *store.Store, auth controlplane.Authenticator) cloudplanev1.ControlPlaneExecutionServiceServer {
	return &Server{logger: logger, store: stores, auth: auth}
}
