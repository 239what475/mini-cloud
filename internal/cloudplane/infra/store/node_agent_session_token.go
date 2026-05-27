package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
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
	// store 只持久化 hash/prefix，不生成明文 session token。
	if strings.TrimSpace(record.NodeID) == "" {
		return ErrNodeNotFound
	}
	if strings.TrimSpace(record.TokenHash) == "" {
		return errors.New("node agent session token hash is required")
	}
	if strings.TrimSpace(record.TokenPrefix) == "" {
		return errors.New("node agent session token prefix is required")
	}

	// INSERT 路径需要新 ID；ON CONFLICT UPDATE 路径不会使用该 ID。
	id, err := newID("nas")
	if err != nil {
		return err
	}

	// 每个 node 只保留一个 session token；重复签发会覆盖旧 token 并清空 last_used_at。
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO node_agent_session_tokens (
			id,
			node_id,
			token_prefix,
			token_hash,
			expires_at
		)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (node_id) DO UPDATE
		SET
			token_prefix = EXCLUDED.token_prefix,
			token_hash = EXCLUDED.token_hash,
			expires_at = EXCLUDED.expires_at,
			last_used_at = NULL,
			updated_at = now()
	`, id, strings.TrimSpace(record.NodeID), strings.TrimSpace(record.TokenPrefix), strings.TrimSpace(record.TokenHash), nullableTime(record.ExpiresAt)); err != nil {
		return fmt.Errorf("upsert node agent session token: %w", err)
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

	// 命中未过期 token 后刷新 last_used_at，并返回绑定的 node_id。
	var nodeID string
	err := s.db.QueryRowContext(ctx, `
		UPDATE node_agent_session_tokens
		SET
			last_used_at = now(),
			updated_at = now()
		WHERE token_hash = $1
		  AND (expires_at IS NULL OR expires_at > now())
		RETURNING node_id
	`, trimmed).Scan(&nodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 未命中 hash 或 token 已过期时都返回同一个认证失败错误。
			return "", ErrNodeAgentSessionTokenNotFound
		}
		return "", fmt.Errorf("resolve node agent session token by hash: %w", err)
	}
	return nodeID, nil
}
