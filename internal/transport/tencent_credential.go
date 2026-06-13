package transport

import (
	"context"
	"net/http"
	"strings"
)

const (
	TencentSecretIDHeader     = "X-Scf-Secret-Id"
	TencentSecretKeyHeader    = "X-Scf-Secret-Key"
	TencentSessionTokenHeader = "X-Scf-Session-Token"
)

type TencentCredential struct {
	SecretID     string
	SecretKey    string
	SessionToken string
}

type tencentCredentialKey struct{}

func TencentCredentialFromHeaders(header http.Header) (TencentCredential, bool) {
	credential := TencentCredential{
		SecretID:     strings.TrimSpace(header.Get(TencentSecretIDHeader)),
		SecretKey:    strings.TrimSpace(header.Get(TencentSecretKeyHeader)),
		SessionToken: strings.TrimSpace(header.Get(TencentSessionTokenHeader)),
	}
	if credential.SecretID == "" || credential.SecretKey == "" {
		return TencentCredential{}, false
	}
	return credential, true
}

func ContextWithTencentCredential(ctx context.Context, credential TencentCredential) context.Context {
	credential.SecretID = strings.TrimSpace(credential.SecretID)
	credential.SecretKey = strings.TrimSpace(credential.SecretKey)
	credential.SessionToken = strings.TrimSpace(credential.SessionToken)
	if credential.SecretID == "" || credential.SecretKey == "" {
		return ctx
	}
	return context.WithValue(ctx, tencentCredentialKey{}, credential)
}

func TencentCredentialFromContext(ctx context.Context) (TencentCredential, bool) {
	if ctx == nil {
		return TencentCredential{}, false
	}
	credential, ok := ctx.Value(tencentCredentialKey{}).(TencentCredential)
	if !ok || strings.TrimSpace(credential.SecretID) == "" || strings.TrimSpace(credential.SecretKey) == "" {
		return TencentCredential{}, false
	}
	return credential, true
}
