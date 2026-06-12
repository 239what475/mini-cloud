package bearer

import (
	"context"
	"testing"

	"google.golang.org/grpc/metadata"
)

func TestParse(t *testing.T) {
	t.Parallel()

	secret, ok := Parse("bearer test-token")
	if !ok || secret != "test-token" {
		t.Fatalf("Parse returned %q/%v, want test-token/true", secret, ok)
	}

	for _, value := range []string{"", "Basic test-token", "Bearer", "Bearer "} {
		if secret, ok := Parse(value); ok || secret != "" {
			t.Fatalf("Parse(%q) = %q/%v, want empty/false", value, secret, ok)
		}
	}
}

func TestIncoming(t *testing.T) {
	t.Parallel()

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(MetadataKey, Header("test-token")))
	secret, ok := Incoming(ctx)
	if !ok || secret != "test-token" {
		t.Fatalf("Incoming returned %q/%v, want test-token/true", secret, ok)
	}
}

func TestMatches(t *testing.T) {
	t.Parallel()

	if !Matches(" test-token ", "test-token") {
		t.Fatal("Matches returned false for matching secrets")
	}
	if Matches("", "test-token") || Matches("test-token", "") || Matches("wrong", "test-token") {
		t.Fatal("Matches returned true for non-matching secrets")
	}
}
