package nodeagent

import (
	"context"
	"crypto/subtle"
	"errors"
	"strings"

	"mini-cloud/internal/cloudplane/infra/store"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// requireBootstrap 校验调用方是否满足当前 gRPC 操作的认证或授权要求。
// 参数说明：ctx 携带本次 gRPC 请求的 authorization metadata。
func (s *service) requireBootstrap(ctx context.Context) error {
	// bootstrap token 未配置时拒绝注册，避免 cloud-plane 以无认证方式接收新节点。
	if s.bootstrapToken == "" {
		return status.Error(codes.Unavailable, "node agent bootstrap token is not configured")
	}
	// 从 metadata authorization 中解析 Bearer token，并用 subtle 比较降低相同长度 token 的前缀匹配时序泄露风险。
	secret, ok := parseBearerSecret(authorizationFromIncomingMetadata(ctx))
	if !ok || subtle.ConstantTimeCompare([]byte(secret), []byte(s.bootstrapToken)) != 1 {
		return status.Error(codes.Unauthenticated, "invalid node agent bootstrap token")
	}
	return nil
}

// requireNodeAgentSession 校验调用方是否满足当前 gRPC 操作的认证或授权要求。
// 参数说明：ctx 携带本次 gRPC 请求的 authorization metadata；nodeID 是 node 唯一标识。
func (s *service) requireNodeAgentSession(ctx context.Context, nodeID string) error {
	// session token 同样通过 Authorization: Bearer <token> 传入。
	secret, ok := parseBearerSecret(authorizationFromIncomingMetadata(ctx))
	if !ok {
		return status.Error(codes.Unauthenticated, "invalid node agent session token")
	}
	// 根据 token secret 解析其绑定的 nodeID；找不到说明 token 无效或已过期。
	resolvedNodeID, err := s.identityService.ResolveNodeAgentSessionTokenBySecret(ctx, secret)
	if err != nil {
		if errors.Is(err, store.ErrNodeAgentSessionTokenNotFound) {
			return status.Error(codes.Unauthenticated, "invalid node agent session token")
		}
		return status.Error(codes.Internal, "internal server error")
	}
	// token 只能用于其绑定的 node，防止 node-agent 越权操作其他 node。
	if subtle.ConstantTimeCompare([]byte(resolvedNodeID), []byte(nodeID)) != 1 {
		return status.Error(codes.PermissionDenied, "node agent session token does not match nodeID")
	}
	return nil
}

// authorizationFromIncomingMetadata 从请求 metadata 中解析授权信息。
// 参数说明：ctx 携带本次 gRPC 请求 metadata。
func authorizationFromIncomingMetadata(ctx context.Context) string {
	// gRPC metadata 可能不存在；缺失时返回空字符串，由上层鉴权逻辑转为 Unauthenticated。
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	// authorization 可能有多个值；当前协议只读取第一个值。
	values := md.Get("authorization")
	if len(values) == 0 {
		return ""
	}
	// 这里只裁剪首尾空白，Bearer 格式解析由 parseBearerSecret 统一处理。
	return strings.TrimSpace(values[0])
}

// parseBearerSecret 从 Authorization header 原始值中解析 Bearer token。
// 参数说明：value 是 Authorization header 原始值，期望格式为 Bearer <token>，允许额外空白且 scheme 大小写不敏感。
func parseBearerSecret(value string) (string, bool) {
	// Fields 同时处理多余空白；合法格式必须恰好包含 scheme 和 token 两段。
	parts := strings.Fields(strings.TrimSpace(value))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		return "", false
	}
	// 返回 token 本体；第二个返回值表示解析是否成功。
	return strings.TrimSpace(parts[1]), true
}
