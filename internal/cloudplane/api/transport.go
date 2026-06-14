package cloudplaneapi

import (
	"log/slog"

	"mini-cloud/internal/cloudplane/api/controlplane"
	"mini-cloud/internal/cloudplane/api/nodeagent"
	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	"mini-cloud/internal/cloudplane/infra/store"
	"mini-cloud/internal/transport"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func NewGRPCServer(cfg cloudplaneconfig.Config, logger *slog.Logger, stores *store.Store) (*grpc.Server, error) {
	serverCredentials, err := transport.ServerTLSConfig(transport.TLSMaterial{
		CACert: cfg.TLS.CACert,
		Cert:   cfg.TLS.Cert,
		Key:    cfg.TLS.Key,
	}, false)
	if err != nil {
		return nil, err
	}
	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverCredentials)))
	controlplane.RegisterGRPC(grpcServer, logger, stores, cfg)
	nodeagent.RegisterGRPC(grpcServer, logger, stores, cfg.NodeAgent.Token)
	return grpcServer, nil
}
