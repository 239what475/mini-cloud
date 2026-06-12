package bearer

import (
	"context"
	"crypto/subtle"
	"strings"

	"google.golang.org/grpc/metadata"
)

const MetadataKey = "authorization"

func Header(token string) string {
	return "Bearer " + strings.TrimSpace(token)
}

func Parse(value string) (string, bool) {
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

func Incoming(ctx context.Context) (string, bool) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", false
	}
	values := md.Get(MetadataKey)
	if len(values) == 0 {
		return "", false
	}
	return Parse(values[0])
}

func Matches(secret string, expected string) bool {
	secret = strings.TrimSpace(secret)
	expected = strings.TrimSpace(expected)
	if secret == "" || expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(secret), []byte(expected)) == 1
}
