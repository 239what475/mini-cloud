package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	cloudmodel "mini-cloud/internal/cloudplane/model"
)

// ErrNodeAgentSessionTokenNotFound 表示 node-agent session token 不存在、为空或已过期。
var ErrNodeAgentSessionTokenNotFound = errors.New("node agent session token not found")

// NodeAgentSessionTokenRecord 是写入 node-agent session token 时需要持久化的字段。
type NodeAgentSessionTokenRecord struct {
	// NodeID 是 token 绑定的 node 唯一标识。
	NodeID string
	// TokenPrefix 是可安全展示的令牌前缀。
	TokenPrefix string
	// TokenHash 是明文令牌的不可逆哈希。
	TokenHash string
	// ExpiresAt 是 token 过期时间；nil 表示不过期。
	ExpiresAt *time.Time
}

// UpsertNodeAgentSessionToken 写入或轮换 node-agent session token 元数据和 token hash。
// 参数说明：ctx 控制数据库请求生命周期；record 是由 node-agent control 层生成好的持久化字段。
func (s *Store) UpsertNodeAgentSessionToken(ctx context.Context, record NodeAgentSessionTokenRecord) error {
	if strings.TrimSpace(record.NodeID) == "" {
		return ErrNodeNotFound
	}
	if strings.TrimSpace(record.TokenHash) == "" {
		return errors.New("node agent session token hash is required")
	}
	if strings.TrimSpace(record.TokenPrefix) == "" {
		return errors.New("node agent session token prefix is required")
	}

	var expiresAt any
	if record.ExpiresAt != nil && !record.ExpiresAt.IsZero() {
		expiresAt = record.ExpiresAt.UTC()
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE nodes
		SET
			session_token_prefix = $2,
			session_token_hash = $3,
			session_expires_at = $4,
			session_last_used_at = NULL,
			updated_at = now()
		WHERE id = $1
	`, strings.TrimSpace(record.NodeID), strings.TrimSpace(record.TokenPrefix), strings.TrimSpace(record.TokenHash), expiresAt)
	if err != nil {
		return fmt.Errorf("upsert node agent session token: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("read node agent session token update result: %w", err)
	} else if affected == 0 {
		return ErrNodeNotFound
	}
	return nil
}

// ResolveNodeAgentSessionTokenByHash 按明文 session token hash 解析 node ID。
// 参数说明：ctx 控制数据库请求生命周期；tokenHash 是 node-agent 提交令牌的不可逆哈希。
func (s *Store) ResolveNodeAgentSessionTokenByHash(ctx context.Context, tokenHash string) (string, error) {
	// 空 hash 不查询数据库，直接按认证失败处理。
	trimmed := strings.TrimSpace(tokenHash)
	if trimmed == "" {
		return "", ErrNodeAgentSessionTokenNotFound
	}

	var nodeID string
	err := s.db.QueryRowContext(ctx, `
		UPDATE nodes
		SET
			session_last_used_at = now(),
			updated_at = now()
		WHERE session_token_hash = $1
		  AND (session_expires_at IS NULL OR session_expires_at > now())
		  AND status <> $2
		RETURNING id
	`, trimmed, cloudmodel.StatusDeleted).Scan(&nodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 未命中 hash 或 token 已过期时都返回同一个认证失败错误。
			return "", ErrNodeAgentSessionTokenNotFound
		}
		return "", fmt.Errorf("resolve node agent session token by hash: %w", err)
	}
	return nodeID, nil
}
