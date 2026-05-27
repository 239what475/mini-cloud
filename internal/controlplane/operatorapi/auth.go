package operatorapi

import (
	"context"
	"crypto/subtle"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func authorizeAdmin(ctx context.Context, adminToken string) error {
	token := strings.TrimSpace(adminToken)
	if token == "" {
		return nil
	}
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "missing authorization metadata")
	}
	values := md.Get("authorization")
	if len(values) == 0 {
		return status.Error(codes.Unauthenticated, "missing authorization metadata")
	}
	secret, err := parseBearer(values[0])
	if err != nil {
		return status.Error(codes.Unauthenticated, err.Error())
	}
	if subtle.ConstantTimeCompare([]byte(secret), []byte(token)) != 1 {
		return status.Error(codes.PermissionDenied, "invalid bearer token")
	}
	return nil
}

func parseBearer(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", status.Error(codes.Unauthenticated, "missing authorization metadata")
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(value, prefix) {
		return "", status.Error(codes.Unauthenticated, "invalid Authorization header; use Bearer <token>")
	}
	secret := strings.TrimSpace(strings.TrimPrefix(value, prefix))
	if secret == "" {
		return "", status.Error(codes.Unauthenticated, "invalid Authorization header; use Bearer <token>")
	}
	return secret, nil
}
