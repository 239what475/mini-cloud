package controlplane

import (
	"context"
	"strings"

	"mini-cloud/internal/transport"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type authenticator struct {
	southboundToken string
}

func newAuthenticator(southboundToken string) authenticator {
	return authenticator{southboundToken: strings.TrimSpace(southboundToken)}
}

func (a authenticator) authorize(ctx context.Context) error {
	if a.southboundToken == "" {
		return status.Error(codes.Unavailable, "plane southbound token is not configured")
	}
	if p, ok := peer.FromContext(ctx); ok && p.AuthInfo != nil && !transport.HasVerifiedClientCertificate(p) {
		return status.Error(codes.Unauthenticated, "client certificate required")
	}
	secret, ok := transport.BearerFromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "bearer token required")
	}
	if !transport.BearerMatches(secret, a.southboundToken) {
		return status.Error(codes.Unauthenticated, "invalid bearer token")
	}
	return nil
}
