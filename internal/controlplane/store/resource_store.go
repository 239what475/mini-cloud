package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"mini-cloud/internal/controlplane/resource"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrConfigSetNotFound                   = errors.New("config set not found")
	ErrConfigSetNameAlreadyExists          = errors.New("config set name already exists")
	ErrSecretSetNotFound                   = errors.New("secret set not found")
	ErrSecretSetNameAlreadyExists          = errors.New("secret set name already exists")
	ErrRegistryCredentialNotFound          = errors.New("registry credential not found")
	ErrRegistryCredentialNameAlreadyExists = errors.New("registry credential name already exists")
)

func (s *Store) CreateConfigSet(ctx context.Context, input resource.CreateConfigSetInput) (resource.ConfigSet, error) {
	if err := input.Validate(); err != nil {
		return resource.ConfigSet{}, err
	}
	id, err := newID("cfg")
	if err != nil {
		return resource.ConfigSet{}, err
	}
	valuesJSON, err := marshalJSON(input.Values, map[string]string{})
	if err != nil {
		return resource.ConfigSet{}, fmt.Errorf("marshal config set values: %w", err)
	}
	item, err := scanConfigSet(s.db.QueryRowContext(ctx, `
		INSERT INTO config_sets (
			id,
			name,
			values_json
		)
		VALUES ($1, $2, $3)
		RETURNING
			id,
			name,
			values_json,
			created_at,
			updated_at
	`, id, input.Name, valuesJSON))
	if err != nil {
		return resource.ConfigSet{}, mapResourceWriteError(err, ErrConfigSetNameAlreadyExists)
	}
	return item, nil
}

func (s *Store) ListConfigSets(ctx context.Context) ([]resource.ConfigSet, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			name,
			values_json,
			created_at,
			updated_at
		FROM config_sets
		ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query config sets: %w", err)
	}
	defer closeRows(rows)

	items := make([]resource.ConfigSet, 0)
	for rows.Next() {
		item, err := scanConfigSet(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate config sets: %w", err)
	}
	return items, nil
}

func (s *Store) GetConfigSet(ctx context.Context, configSetID string) (resource.ConfigSet, error) {
	item, err := scanConfigSet(s.db.QueryRowContext(ctx, `
		SELECT
			id,
			name,
			values_json,
			created_at,
			updated_at
		FROM config_sets
		WHERE id = $1
	`, configSetID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return resource.ConfigSet{}, ErrConfigSetNotFound
		}
		return resource.ConfigSet{}, fmt.Errorf("query config set: %w", err)
	}
	return item, nil
}

func (s *Store) CreateSecretSet(ctx context.Context, input resource.CreateSecretSetInput) (resource.SecretSet, error) {
	if err := input.Validate(); err != nil {
		return resource.SecretSet{}, err
	}
	id, err := newID("sec")
	if err != nil {
		return resource.SecretSet{}, err
	}
	valuesJSON, err := marshalJSON(input.Values, map[string]string{})
	if err != nil {
		return resource.SecretSet{}, fmt.Errorf("marshal secret set values: %w", err)
	}
	item, err := scanSecretSet(s.db.QueryRowContext(ctx, `
		INSERT INTO secret_sets (
			id,
			name,
			values_json
		)
		VALUES ($1, $2, $3)
		RETURNING
			id,
			name,
			values_json,
			created_at,
			updated_at
	`, id, input.Name, valuesJSON))
	if err != nil {
		return resource.SecretSet{}, mapResourceWriteError(err, ErrSecretSetNameAlreadyExists)
	}
	return item, nil
}

func (s *Store) ListSecretSets(ctx context.Context) ([]resource.SecretSet, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			name,
			values_json,
			created_at,
			updated_at
		FROM secret_sets
		ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query secret sets: %w", err)
	}
	defer closeRows(rows)

	items := make([]resource.SecretSet, 0)
	for rows.Next() {
		item, err := scanSecretSet(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate secret sets: %w", err)
	}
	return items, nil
}

func (s *Store) GetSecretSet(ctx context.Context, secretSetID string) (resource.SecretSet, error) {
	item, err := scanSecretSet(s.db.QueryRowContext(ctx, `
		SELECT
			id,
			name,
			values_json,
			created_at,
			updated_at
		FROM secret_sets
		WHERE id = $1
	`, secretSetID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return resource.SecretSet{}, ErrSecretSetNotFound
		}
		return resource.SecretSet{}, fmt.Errorf("query secret set: %w", err)
	}
	return item, nil
}

func (s *Store) CreateRegistryCredential(ctx context.Context, input resource.CreateRegistryCredentialInput) (resource.RegistryCredential, error) {
	if err := input.Validate(); err != nil {
		return resource.RegistryCredential{}, err
	}
	id, err := newID("reg")
	if err != nil {
		return resource.RegistryCredential{}, err
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
		return resource.RegistryCredential{}, mapResourceWriteError(err, ErrRegistryCredentialNameAlreadyExists)
	}
	return item, nil
}

func (s *Store) ListRegistryCredentials(ctx context.Context) ([]resource.RegistryCredential, error) {
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

	items := make([]resource.RegistryCredential, 0)
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

func (s *Store) GetRegistryCredential(ctx context.Context, credentialID string) (resource.RegistryCredential, error) {
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
			return resource.RegistryCredential{}, ErrRegistryCredentialNotFound
		}
		return resource.RegistryCredential{}, fmt.Errorf("query registry credential: %w", err)
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

func scanConfigSet(scanner interface{ Scan(dest ...any) error }) (resource.ConfigSet, error) {
	var (
		item      resource.ConfigSet
		valuesRaw []byte
	)
	if err := scanner.Scan(
		&item.ID,
		&item.Name,
		&valuesRaw,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return resource.ConfigSet{}, err
	}
	if err := unmarshalJSON(valuesRaw, &item.Values, map[string]string{}); err != nil {
		return resource.ConfigSet{}, fmt.Errorf("decode config set values: %w", err)
	}
	return item, nil
}

func scanSecretSet(scanner interface{ Scan(dest ...any) error }) (resource.SecretSet, error) {
	var (
		item      resource.SecretSet
		valuesRaw []byte
	)
	if err := scanner.Scan(
		&item.ID,
		&item.Name,
		&valuesRaw,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return resource.SecretSet{}, err
	}
	if err := unmarshalJSON(valuesRaw, &item.Values, map[string]string{}); err != nil {
		return resource.SecretSet{}, fmt.Errorf("decode secret set values: %w", err)
	}
	item.Keys = resource.SecretKeys(item.Values)
	return item, nil
}

func scanRegistryCredential(scanner interface{ Scan(dest ...any) error }) (resource.RegistryCredential, error) {
	var item resource.RegistryCredential
	if err := scanner.Scan(
		&item.ID,
		&item.Name,
		&item.Server,
		&item.Username,
		&item.Password,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return resource.RegistryCredential{}, err
	}
	item.PasswordConfigured = item.Password != ""
	return item, nil
}
