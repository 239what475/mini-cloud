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

// Authenticator 校验 control-plane 调用 cloud-plane southbound gRPC 服务时携带的内部 token。
type Authenticator struct {
	// southboundToken 是 cloud-plane 配置中声明的 control-plane 内部调用 token。
	southboundToken string
}

// NewAuthenticator 构造 southbound token authenticator。
// 参数说明：southboundToken 是配置文件中的内部调用 token。
func NewAuthenticator(southboundToken string) Authenticator {
	return Authenticator{southboundToken: strings.TrimSpace(southboundToken)}
}

// Authorize 校验 southbound 请求的授权 token。
// 参数说明：ctx 携带本次 gRPC 请求的 authorization metadata。
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

// parseBearerSecret 解析 bearer secret。
// 参数说明：value 是 authorization header 的原始值。
func parseBearerSecret(value string) (string, error) {
	parts := strings.Fields(strings.TrimSpace(value))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		return "", errors.New("bearer token required")
	}
	return strings.TrimSpace(parts[1]), nil
}

// authorizationFromIncomingMetadata 从请求 metadata 中解析授权信息。
// 参数说明：ctx 携带本次 gRPC 请求 metadata。
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
