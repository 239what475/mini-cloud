package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"mini-cloud/internal/controlplane/projectresource"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrProjectConfigSetNotFound                   = errors.New("project config set not found")
	ErrProjectConfigSetNameAlreadyExists          = errors.New("config set name already exists in this project")
	ErrProjectSecretSetNotFound                   = errors.New("project secret set not found")
	ErrProjectSecretSetNameAlreadyExists          = errors.New("secret set name already exists in this project")
	ErrProjectRegistryCredentialNotFound          = errors.New("project registry credential not found")
	ErrProjectRegistryCredentialNameAlreadyExists = errors.New("registry credential name already exists in this project")
)

func (s *Store) CreateProjectConfigSet(ctx context.Context, projectID string, input projectresource.CreateConfigSetInput) (projectresource.ConfigSet, error) {
	if err := input.Validate(); err != nil {
		return projectresource.ConfigSet{}, err
	}
	id, err := newID("cfg")
	if err != nil {
		return projectresource.ConfigSet{}, err
	}
	valuesJSON, err := marshalJSON(input.Values, map[string]string{})
	if err != nil {
		return projectresource.ConfigSet{}, fmt.Errorf("marshal project config set values: %w", err)
	}
	item, err := scanProjectConfigSet(s.db.QueryRowContext(ctx, `
		INSERT INTO project_config_sets (
			id,
			project_id,
			name,
			values_json
		)
		VALUES ($1, $2, $3, $4)
		RETURNING
			id,
			project_id,
			name,
			values_json,
			created_at,
			updated_at
	`, id, projectID, input.Name, valuesJSON))
	if err != nil {
		return projectresource.ConfigSet{}, mapProjectResourceWriteError(err, ErrProjectConfigSetNameAlreadyExists)
	}
	return item, nil
}

func (s *Store) ListProjectConfigSets(ctx context.Context, projectID string) ([]projectresource.ConfigSet, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			project_id,
			name,
			values_json,
			created_at,
			updated_at
		FROM project_config_sets
		WHERE project_id = $1
		ORDER BY created_at ASC, id ASC
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("query project config sets: %w", err)
	}
	defer closeRows(rows)

	items := make([]projectresource.ConfigSet, 0)
	for rows.Next() {
		item, err := scanProjectConfigSet(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate project config sets: %w", err)
	}
	return items, nil
}

func (s *Store) GetProjectConfigSet(ctx context.Context, projectID string, configSetID string) (projectresource.ConfigSet, error) {
	item, err := scanProjectConfigSet(s.db.QueryRowContext(ctx, `
		SELECT
			id,
			project_id,
			name,
			values_json,
			created_at,
			updated_at
		FROM project_config_sets
		WHERE project_id = $1
		  AND id = $2
	`, projectID, configSetID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return projectresource.ConfigSet{}, ErrProjectConfigSetNotFound
		}
		return projectresource.ConfigSet{}, fmt.Errorf("query project config set: %w", err)
	}
	return item, nil
}

func (s *Store) CreateProjectSecretSet(ctx context.Context, projectID string, input projectresource.CreateSecretSetInput) (projectresource.SecretSet, error) {
	if err := input.Validate(); err != nil {
		return projectresource.SecretSet{}, err
	}
	id, err := newID("sec")
	if err != nil {
		return projectresource.SecretSet{}, err
	}
	valuesJSON, err := marshalJSON(input.Values, map[string]string{})
	if err != nil {
		return projectresource.SecretSet{}, fmt.Errorf("marshal project secret set values: %w", err)
	}
	item, err := scanProjectSecretSet(s.db.QueryRowContext(ctx, `
		INSERT INTO project_secret_sets (
			id,
			project_id,
			name,
			values_json
		)
		VALUES ($1, $2, $3, $4)
		RETURNING
			id,
			project_id,
			name,
			values_json,
			created_at,
			updated_at
	`, id, projectID, input.Name, valuesJSON))
	if err != nil {
		return projectresource.SecretSet{}, mapProjectResourceWriteError(err, ErrProjectSecretSetNameAlreadyExists)
	}
	return item, nil
}

func (s *Store) ListProjectSecretSets(ctx context.Context, projectID string) ([]projectresource.SecretSet, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			project_id,
			name,
			values_json,
			created_at,
			updated_at
		FROM project_secret_sets
		WHERE project_id = $1
		ORDER BY created_at ASC, id ASC
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("query project secret sets: %w", err)
	}
	defer closeRows(rows)

	items := make([]projectresource.SecretSet, 0)
	for rows.Next() {
		item, err := scanProjectSecretSet(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate project secret sets: %w", err)
	}
	return items, nil
}

func (s *Store) GetProjectSecretSet(ctx context.Context, projectID string, secretSetID string) (projectresource.SecretSet, error) {
	item, err := scanProjectSecretSet(s.db.QueryRowContext(ctx, `
		SELECT
			id,
			project_id,
			name,
			values_json,
			created_at,
			updated_at
		FROM project_secret_sets
		WHERE project_id = $1
		  AND id = $2
	`, projectID, secretSetID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return projectresource.SecretSet{}, ErrProjectSecretSetNotFound
		}
		return projectresource.SecretSet{}, fmt.Errorf("query project secret set: %w", err)
	}
	return item, nil
}

func (s *Store) CreateProjectRegistryCredential(ctx context.Context, projectID string, input projectresource.CreateRegistryCredentialInput) (projectresource.RegistryCredential, error) {
	if err := input.Validate(); err != nil {
		return projectresource.RegistryCredential{}, err
	}
	id, err := newID("reg")
	if err != nil {
		return projectresource.RegistryCredential{}, err
	}
	item, err := scanProjectRegistryCredential(s.db.QueryRowContext(ctx, `
		INSERT INTO project_registry_credentials (
			id,
			project_id,
			name,
			server,
			username,
			password
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING
			id,
			project_id,
			name,
			server,
			username,
			password,
			created_at,
			updated_at
	`, id, projectID, input.Name, input.Server, input.Username, input.Password))
	if err != nil {
		return projectresource.RegistryCredential{}, mapProjectResourceWriteError(err, ErrProjectRegistryCredentialNameAlreadyExists)
	}
	return item, nil
}

func (s *Store) ListProjectRegistryCredentials(ctx context.Context, projectID string) ([]projectresource.RegistryCredential, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			project_id,
			name,
			server,
			username,
			password,
			created_at,
			updated_at
		FROM project_registry_credentials
		WHERE project_id = $1
		ORDER BY created_at ASC, id ASC
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("query project registry credentials: %w", err)
	}
	defer closeRows(rows)

	items := make([]projectresource.RegistryCredential, 0)
	for rows.Next() {
		item, err := scanProjectRegistryCredential(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate project registry credentials: %w", err)
	}
	return items, nil
}

func (s *Store) GetProjectRegistryCredential(ctx context.Context, projectID string, credentialID string) (projectresource.RegistryCredential, error) {
	item, err := scanProjectRegistryCredential(s.db.QueryRowContext(ctx, `
		SELECT
			id,
			project_id,
			name,
			server,
			username,
			password,
			created_at,
			updated_at
		FROM project_registry_credentials
		WHERE project_id = $1
		  AND id = $2
	`, projectID, credentialID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return projectresource.RegistryCredential{}, ErrProjectRegistryCredentialNotFound
		}
		return projectresource.RegistryCredential{}, fmt.Errorf("query project registry credential: %w", err)
	}
	return item, nil
}

func mapProjectResourceWriteError(err error, duplicateErr error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23503":
			return ErrProjectNotFound
		case "23505":
			return duplicateErr
		}
	}
	return fmt.Errorf("write project resource: %w", err)
}

func scanProjectConfigSet(scanner interface{ Scan(dest ...any) error }) (projectresource.ConfigSet, error) {
	var (
		item      projectresource.ConfigSet
		valuesRaw []byte
	)
	if err := scanner.Scan(
		&item.ID,
		&item.ProjectID,
		&item.Name,
		&valuesRaw,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return projectresource.ConfigSet{}, err
	}
	if err := unmarshalJSON(valuesRaw, &item.Values, map[string]string{}); err != nil {
		return projectresource.ConfigSet{}, fmt.Errorf("decode project config set values: %w", err)
	}
	return item, nil
}

func scanProjectSecretSet(scanner interface{ Scan(dest ...any) error }) (projectresource.SecretSet, error) {
	var (
		item      projectresource.SecretSet
		valuesRaw []byte
	)
	if err := scanner.Scan(
		&item.ID,
		&item.ProjectID,
		&item.Name,
		&valuesRaw,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return projectresource.SecretSet{}, err
	}
	if err := unmarshalJSON(valuesRaw, &item.Values, map[string]string{}); err != nil {
		return projectresource.SecretSet{}, fmt.Errorf("decode project secret set values: %w", err)
	}
	item.Keys = projectresource.SecretKeys(item.Values)
	return item, nil
}

func scanProjectRegistryCredential(scanner interface{ Scan(dest ...any) error }) (projectresource.RegistryCredential, error) {
	var item projectresource.RegistryCredential
	if err := scanner.Scan(
		&item.ID,
		&item.ProjectID,
		&item.Name,
		&item.Server,
		&item.Username,
		&item.Password,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return projectresource.RegistryCredential{}, err
	}
	item.PasswordConfigured = item.Password != ""
	return item, nil
}
