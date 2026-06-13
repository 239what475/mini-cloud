package transport

import (
	"context"
	"net/http"
	"testing"
)

func TestTencentCredentialFromHeaders(t *testing.T) {
	t.Parallel()

	header := http.Header{}
	header.Set(TencentSecretIDHeader, " sid ")
	header.Set(TencentSecretKeyHeader, " skey ")
	header.Set(TencentSessionTokenHeader, " token ")

	credential, ok := TencentCredentialFromHeaders(header)
	if !ok {
		t.Fatal("TencentCredentialFromHeaders returned false")
	}
	if credential.SecretID != "sid" || credential.SecretKey != "skey" || credential.SessionToken != "token" {
		t.Fatalf("credential = %+v, want trimmed SCF credential", credential)
	}
}

func TestTencentCredentialFromHeadersRejectsIncompleteCredential(t *testing.T) {
	t.Parallel()

	header := http.Header{}
	header.Set(TencentSecretIDHeader, "sid")

	if credential, ok := TencentCredentialFromHeaders(header); ok || credential.SecretID != "" {
		t.Fatalf("TencentCredentialFromHeaders returned %+v/%v, want empty/false", credential, ok)
	}
}

func TestTencentCredentialContext(t *testing.T) {
	t.Parallel()

	ctx := ContextWithTencentCredential(context.Background(), TencentCredential{
		SecretID:     " sid ",
		SecretKey:    " skey ",
		SessionToken: " token ",
	})
	credential, ok := TencentCredentialFromContext(ctx)
	if !ok {
		t.Fatal("TencentCredentialFromContext returned false")
	}
	if credential.SecretID != "sid" || credential.SecretKey != "skey" || credential.SessionToken != "token" {
		t.Fatalf("credential = %+v, want trimmed context credential", credential)
	}
}
