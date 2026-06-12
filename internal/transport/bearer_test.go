package transport

import (
	"context"
	"testing"

	"google.golang.org/grpc/metadata"
)

func TestParse(t *testing.T) {
	t.Parallel()

	secret, ok := ParseBearer("bearer test-token")
	if !ok || secret != "test-token" {
		t.Fatalf("ParseBearer returned %q/%v, want test-token/true", secret, ok)
	}

	for _, value := range []string{"", "Basic test-token", "Bearer", "Bearer "} {
		if secret, ok := ParseBearer(value); ok || secret != "" {
			t.Fatalf("ParseBearer(%q) = %q/%v, want empty/false", value, secret, ok)
		}
	}
}

func TestIncoming(t *testing.T) {
	t.Parallel()

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(BearerMetadataKey, BearerHeader("test-token")))
	secret, ok := BearerFromIncomingContext(ctx)
	if !ok || secret != "test-token" {
		t.Fatalf("BearerFromIncomingContext returned %q/%v, want test-token/true", secret, ok)
	}
}

func TestMatches(t *testing.T) {
	t.Parallel()

	if !BearerMatches(" test-token ", "test-token") {
		t.Fatal("BearerMatches returned false for matching secrets")
	}
	if BearerMatches("", "test-token") || BearerMatches("test-token", "") || BearerMatches("wrong", "test-token") {
		t.Fatal("BearerMatches returned true for non-matching secrets")
	}
	if BearerMatches("short", "much-longer-token") {
		t.Fatal("BearerMatches returned true for different length secrets")
	}
}
