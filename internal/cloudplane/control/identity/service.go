// Package identity 编排 node-agent session token 的签发和解析。
package identity

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mini-cloud/internal/cloudplane/infra/store"
)

const (
	nodeAgentSessionTokenPrefix = "mcna_"
	tokenRandomBytes            = 24
)

// Service 提供 node-agent session token 相关的应用层操作。
type Service struct {
	// store 只负责持久化 token hash 和身份元数据。
	store *store.Store
}

// NewService 构造身份服务。
// 参数说明：stores 提供身份元数据和 token hash 持久化能力。
func NewService(stores *store.Store) *Service {
	return &Service{store: stores}
}

// IssueNodeAgentSessionToken 为 node-agent 签发 node-scoped session token。
// 参数说明：ctx 控制数据库请求生命周期；nodeID 是 node 唯一标识；ttl 是 token 有效期。
func (s *Service) IssueNodeAgentSessionToken(ctx context.Context, nodeID string, ttl time.Duration) (string, error) {
	secret, err := newSecretToken(nodeAgentSessionTokenPrefix, tokenRandomBytes)
	if err != nil {
		return "", err
	}
	var expiresAt *time.Time
	if ttl > 0 {
		value := time.Now().UTC().Add(ttl)
		expiresAt = &value
	}
	if err := s.store.UpsertNodeAgentSessionToken(ctx, store.NodeAgentSessionTokenRecord{
		NodeID:      nodeID,
		TokenPrefix: secret.Prefix,
		TokenHash:   secret.Hash,
		ExpiresAt:   expiresAt,
	}); err != nil {
		return "", fmt.Errorf("issue node agent session token: %w", err)
	}
	return secret.Value, nil
}

// ResolveNodeAgentSessionTokenBySecret 按明文 session token 解析 node ID。
// 参数说明：ctx 控制数据库请求生命周期；secret 是 node-agent 提交的明文 bearer secret。
func (s *Service) ResolveNodeAgentSessionTokenBySecret(ctx context.Context, secret string) (string, error) {
	trimmed := strings.TrimSpace(secret)
	if trimmed == "" {
		return "", store.ErrNodeAgentSessionTokenNotFound
	}
	return s.store.ResolveNodeAgentSessionTokenByHash(ctx, tokenHash(trimmed))
}
