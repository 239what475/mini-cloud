package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"mini-cloud/internal/controlplane/domain"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrRegistryCredentialNotFound          = errors.New("registry credential not found")
	ErrRegistryCredentialNameAlreadyExists = errors.New("registry credential name already exists")
)

func (s *Store) CreateRegistryCredential(ctx context.Context, input domain.CreateRegistryCredentialInput) (domain.RegistryCredential, error) {
	if err := input.Validate(); err != nil {
		return domain.RegistryCredential{}, err
	}
	id, err := newID("reg")
	if err != nil {
		return domain.RegistryCredential{}, err
	}
	item, err := scanRegistryCredential(s.db.QueryRowContext(ctx, `
		INSERT INTO registry_credentials (
			id,
			name,
			server,
			username,
			password
		)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING
			id,
			name,
			server,
			username,
			password,
			created_at,
			updated_at
	`, id, input.Name, input.Server, input.Username, input.Password))
	if err != nil {
		return domain.RegistryCredential{}, mapResourceWriteError(err, ErrRegistryCredentialNameAlreadyExists)
	}
	return item, nil
}

func (s *Store) ListRegistryCredentials(ctx context.Context) ([]domain.RegistryCredential, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			name,
			server,
			username,
			password,
			created_at,
			updated_at
		FROM registry_credentials
		ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query registry credentials: %w", err)
	}
	defer closeRows(rows)

	items := make([]domain.RegistryCredential, 0)
	for rows.Next() {
		item, err := scanRegistryCredential(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate registry credentials: %w", err)
	}
	return items, nil
}

func (s *Store) GetRegistryCredential(ctx context.Context, credentialID string) (domain.RegistryCredential, error) {
	item, err := scanRegistryCredential(s.db.QueryRowContext(ctx, `
		SELECT
			id,
			name,
			server,
			username,
			password,
			created_at,
			updated_at
		FROM registry_credentials
		WHERE id = $1
	`, credentialID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.RegistryCredential{}, ErrRegistryCredentialNotFound
		}
		return domain.RegistryCredential{}, fmt.Errorf("query registry credential: %w", err)
	}
	return item, nil
}

func mapResourceWriteError(err error, duplicateErr error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return duplicateErr
		}
	}
	return fmt.Errorf("write resource: %w", err)
}

func scanRegistryCredential(scanner interface{ Scan(dest ...any) error }) (domain.RegistryCredential, error) {
	var item domain.RegistryCredential
	if err := scanner.Scan(
		&item.ID,
		&item.Name,
		&item.Server,
		&item.Username,
		&item.Password,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return domain.RegistryCredential{}, err
	}
	item.PasswordConfigured = item.Password != ""
	return item, nil
}
