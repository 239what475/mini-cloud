package controlplane

import (
	"context"
	"strings"

	"mini-cloud/internal/bearer"

	"google.golang.org/grpc/codes"
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
	secret, ok := bearer.Incoming(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "bearer token required")
	}
	if !bearer.Matches(secret, a.southboundToken) {
		return status.Error(codes.Unauthenticated, "invalid bearer token")
	}
	return nil
}
