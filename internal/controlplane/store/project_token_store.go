package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"mini-cloud/internal/controlplane/projecttoken"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrProjectAPITokenNotFound          = errors.New("project api token not found")
	ErrProjectAPITokenNameAlreadyExists = errors.New("project api token name already exists in this project")
)

func (s *Store) CreateProjectAPIToken(ctx context.Context, projectID string, input projecttoken.IssueInput) (projecttoken.IssuedToken, error) {
	if err := input.Validate(); err != nil {
		return projecttoken.IssuedToken{}, err
	}

	id, err := newID("ptk")
	if err != nil {
		return projecttoken.IssuedToken{}, err
	}

	secret, err := newProjectTokenSecret()
	if err != nil {
		return projecttoken.IssuedToken{}, err
	}
	tokenHash := hashProjectTokenSecret(secret)
	tokenPrefix := tokenSecretPrefix(secret)

	var created projecttoken.Token
	var lastUsedAt sql.NullTime
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO project_api_tokens (
			id,
			project_id,
			name,
			token_prefix,
			token_hash
		)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, project_id, name, token_prefix, last_used_at, created_at, updated_at
	`, id, projectID, input.Name, tokenPrefix, tokenHash).Scan(
		&created.ID,
		&created.ProjectID,
		&created.Name,
		&created.TokenPrefix,
		&lastUsedAt,
		&created.CreatedAt,
		&created.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case "23503":
				return projecttoken.IssuedToken{}, ErrProjectNotFound
			case "23505":
				return projecttoken.IssuedToken{}, ErrProjectAPITokenNameAlreadyExists
			}
		}
		return projecttoken.IssuedToken{}, fmt.Errorf("insert project api token: %w", err)
	}
	if lastUsedAt.Valid {
		created.LastUsedAt = &lastUsedAt.Time
	}

	return projecttoken.IssuedToken{
		Token:  created,
		Secret: secret,
	}, nil
}

func (s *Store) ListProjectAPITokens(ctx context.Context, projectID string) ([]projecttoken.Token, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, project_id, name, token_prefix, last_used_at, created_at, updated_at
		FROM project_api_tokens
		WHERE project_id = $1
		ORDER BY created_at ASC, id ASC
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("query project api tokens: %w", err)
	}
	defer closeRows(rows)

	items := make([]projecttoken.Token, 0)
	for rows.Next() {
		item, err := scanProjectToken(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate project api tokens: %w", err)
	}
	return items, nil
}

func (s *Store) DeleteProjectAPIToken(ctx context.Context, projectID string, tokenID string) (projecttoken.Token, error) {
	var deleted projecttoken.Token
	var lastUsedAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		DELETE FROM project_api_tokens
		WHERE project_id = $1 AND id = $2
		RETURNING id, project_id, name, token_prefix, last_used_at, created_at, updated_at
	`, projectID, tokenID).Scan(
		&deleted.ID,
		&deleted.ProjectID,
		&deleted.Name,
		&deleted.TokenPrefix,
		&lastUsedAt,
		&deleted.CreatedAt,
		&deleted.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return projecttoken.Token{}, ErrProjectAPITokenNotFound
		}
		return projecttoken.Token{}, fmt.Errorf("delete project api token: %w", err)
	}
	if lastUsedAt.Valid {
		deleted.LastUsedAt = &lastUsedAt.Time
	}
	return deleted, nil
}

func (s *Store) ResolveProjectAPITokenBySecret(ctx context.Context, secret string) (projecttoken.Token, error) {
	trimmed := strings.TrimSpace(secret)
	if trimmed == "" {
		return projecttoken.Token{}, ErrProjectAPITokenNotFound
	}

	var item projecttoken.Token
	var lastUsedAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		UPDATE project_api_tokens
		SET
			last_used_at = now(),
			updated_at = now()
		WHERE token_hash = $1
		RETURNING id, project_id, name, token_prefix, last_used_at, created_at, updated_at
	`, hashProjectTokenSecret(trimmed)).Scan(
		&item.ID,
		&item.ProjectID,
		&item.Name,
		&item.TokenPrefix,
		&lastUsedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return projecttoken.Token{}, ErrProjectAPITokenNotFound
		}
		return projecttoken.Token{}, fmt.Errorf("resolve project api token by secret: %w", err)
	}
	if lastUsedAt.Valid {
		item.LastUsedAt = &lastUsedAt.Time
	}
	return item, nil
}

func scanProjectToken(scanner interface{ Scan(dest ...any) error }) (projecttoken.Token, error) {
	var item projecttoken.Token
	var lastUsedAt sql.NullTime
	if err := scanner.Scan(
		&item.ID,
		&item.ProjectID,
		&item.Name,
		&item.TokenPrefix,
		&lastUsedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return projecttoken.Token{}, err
	}
	if lastUsedAt.Valid {
		item.LastUsedAt = &lastUsedAt.Time
	}
	return item, nil
}

func newProjectTokenSecret() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("read random bytes for project api token: %w", err)
	}
	return "mcpt_" + hex.EncodeToString(raw), nil
}

func hashProjectTokenSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func tokenSecretPrefix(secret string) string {
	if len(secret) <= 12 {
		return secret
	}
	return secret[:12]
}
