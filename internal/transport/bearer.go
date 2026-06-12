package transport

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"strings"

	"google.golang.org/grpc/metadata"
)

const BearerMetadataKey = "authorization"

func BearerHeader(token string) string {
	return "Bearer " + strings.TrimSpace(token)
}

func ParseBearer(value string) (string, bool) {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) != 2 {
		return "", false
	}
	if !strings.EqualFold(fields[0], "Bearer") {
		return "", false
	}
	secret := strings.TrimSpace(fields[1])
	if secret == "" {
		return "", false
	}
	return secret, true
}

func BearerFromIncomingContext(ctx context.Context) (string, bool) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", false
	}
	values := md.Get(BearerMetadataKey)
	if len(values) == 0 {
		return "", false
	}
	return ParseBearer(values[0])
}

func BearerMatches(secret string, expected string) bool {
	secret = strings.TrimSpace(secret)
	expected = strings.TrimSpace(expected)
	if secret == "" || expected == "" {
		return false
	}
	secretSum := sha256.Sum256([]byte(secret))
	expectedSum := sha256.Sum256([]byte(expected))
	return subtle.ConstantTimeCompare(secretSum[:], expectedSum[:]) == 1
}
