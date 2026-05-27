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

	"mini-cloud/internal/controlplane/serviceaccount"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrPlatformServiceAccountNotFound        = errors.New("platform service account not found")
	ErrPlatformServiceAccountNameAlreadyUsed = errors.New("platform service account name already exists")
)

func (s *Store) CreatePlatformServiceAccount(ctx context.Context, input serviceaccount.IssueInput) (serviceaccount.IssuedAccount, error) {
	if err := input.Validate(); err != nil {
		return serviceaccount.IssuedAccount{}, err
	}

	id, err := newID("psa")
	if err != nil {
		return serviceaccount.IssuedAccount{}, err
	}

	secret, err := newPlatformServiceAccountSecret()
	if err != nil {
		return serviceaccount.IssuedAccount{}, err
	}
	tokenHash := hashPlatformServiceAccountSecret(secret)
	tokenPrefix := tokenSecretPrefix(secret)

	var created serviceaccount.Account
	var lastUsedAt sql.NullTime
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO platform_service_accounts (
			id,
			name,
			role,
			token_prefix,
			token_hash
		)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, name, role, token_prefix, last_used_at, created_at, updated_at
	`, id, input.Name, string(input.Role), tokenPrefix, tokenHash).Scan(
		&created.ID,
		&created.Name,
		&created.Role,
		&created.TokenPrefix,
		&lastUsedAt,
		&created.CreatedAt,
		&created.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return serviceaccount.IssuedAccount{}, ErrPlatformServiceAccountNameAlreadyUsed
		}
		return serviceaccount.IssuedAccount{}, fmt.Errorf("insert platform service account: %w", err)
	}
	if lastUsedAt.Valid {
		created.LastUsedAt = &lastUsedAt.Time
	}

	return serviceaccount.IssuedAccount{
		Account: created,
		Secret:  secret,
	}, nil
}

func (s *Store) ListPlatformServiceAccounts(ctx context.Context) ([]serviceaccount.Account, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, role, token_prefix, last_used_at, created_at, updated_at
		FROM platform_service_accounts
		ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query platform service accounts: %w", err)
	}
	defer closeRows(rows)

	items := make([]serviceaccount.Account, 0)
	for rows.Next() {
		item, err := scanPlatformServiceAccount(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate platform service accounts: %w", err)
	}
	return items, nil
}

func (s *Store) DeletePlatformServiceAccount(ctx context.Context, accountID string) (serviceaccount.Account, error) {
	var deleted serviceaccount.Account
	var lastUsedAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		DELETE FROM platform_service_accounts
		WHERE id = $1
		RETURNING id, name, role, token_prefix, last_used_at, created_at, updated_at
	`, accountID).Scan(
		&deleted.ID,
		&deleted.Name,
		&deleted.Role,
		&deleted.TokenPrefix,
		&lastUsedAt,
		&deleted.CreatedAt,
		&deleted.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return serviceaccount.Account{}, ErrPlatformServiceAccountNotFound
		}
		return serviceaccount.Account{}, fmt.Errorf("delete platform service account: %w", err)
	}
	if lastUsedAt.Valid {
		deleted.LastUsedAt = &lastUsedAt.Time
	}
	return deleted, nil
}

func (s *Store) ResolvePlatformServiceAccountBySecret(ctx context.Context, secret string) (serviceaccount.Account, error) {
	trimmed := strings.TrimSpace(secret)
	if trimmed == "" {
		return serviceaccount.Account{}, ErrPlatformServiceAccountNotFound
	}

	var item serviceaccount.Account
	var lastUsedAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		UPDATE platform_service_accounts
		SET
			last_used_at = now(),
			updated_at = now()
		WHERE token_hash = $1
		RETURNING id, name, role, token_prefix, last_used_at, created_at, updated_at
	`, hashPlatformServiceAccountSecret(trimmed)).Scan(
		&item.ID,
		&item.Name,
		&item.Role,
		&item.TokenPrefix,
		&lastUsedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return serviceaccount.Account{}, ErrPlatformServiceAccountNotFound
		}
		return serviceaccount.Account{}, fmt.Errorf("resolve platform service account by secret: %w", err)
	}
	if lastUsedAt.Valid {
		item.LastUsedAt = &lastUsedAt.Time
	}
	return item, nil
}

func scanPlatformServiceAccount(scanner interface{ Scan(dest ...any) error }) (serviceaccount.Account, error) {
	var item serviceaccount.Account
	var lastUsedAt sql.NullTime
	if err := scanner.Scan(
		&item.ID,
		&item.Name,
		&item.Role,
		&item.TokenPrefix,
		&lastUsedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return serviceaccount.Account{}, err
	}
	if lastUsedAt.Valid {
		item.LastUsedAt = &lastUsedAt.Time
	}
	return item, nil
}

func newPlatformServiceAccountSecret() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("read random bytes for platform service account: %w", err)
	}
	return "mccsa_" + hex.EncodeToString(raw), nil
}

func hashPlatformServiceAccountSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}
