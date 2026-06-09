package controlplane

import (
	"context"
	"crypto/subtle"
	"errors"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type Authenticator struct {
	southboundToken string
}

func NewAuthenticator(southboundToken string) Authenticator {
	return Authenticator{southboundToken: strings.TrimSpace(southboundToken)}
}

func (a Authenticator) Authorize(ctx context.Context) error {
	if a.southboundToken == "" {
		return status.Error(codes.Unavailable, "plane southbound token is not configured")
	}
	secret, err := parseBearerSecret(authorizationFromIncomingMetadata(ctx))
	if err != nil {
		return status.Error(codes.Unauthenticated, "bearer token required")
	}
	if subtle.ConstantTimeCompare([]byte(secret), []byte(a.southboundToken)) != 1 {
		return status.Error(codes.Unauthenticated, "invalid bearer token")
	}
	return nil
}

func parseBearerSecret(value string) (string, error) {
	parts := strings.Fields(strings.TrimSpace(value))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		return "", errors.New("bearer token required")
	}
	return strings.TrimSpace(parts[1]), nil
}

func authorizationFromIncomingMetadata(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	values := md.Get("authorization")
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}
