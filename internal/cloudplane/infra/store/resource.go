package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"

	"mini-cloud/internal/contract/cloudplaneapi"

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

// storedConfigSet 是 store 内部读取到的 config set 完整记录。
type storedConfigSet struct {
	// Spec 保存可写入 revision/env 的非敏感配置内容。
	cloudplaneapi.ConfigSet
}

// storedSecretSet 是 store 内部读取到的 secret set 完整记录；不得作为 API 响应返回。
type storedSecretSet struct {
	// Spec 保存敏感配置明文，仅允许 runtime materialize 路径内部使用。
	cloudplaneapi.SecretSet
}

// storedRegistryCredential 是 store 内部读取到的 registry credential 完整记录；不得作为 API 响应返回。
type storedRegistryCredential struct {
	// Spec 保存仓库凭据明文，仅允许镜像拉取凭据解析路径内部使用。
	cloudplaneapi.RegistryCredential
}

// ApplyResources 同步 single-tenant 全局资源期望状态，并返回是否产生写入。
// 参数说明：ctx 控制数据库请求生命周期；resources 是全局资源期望状态。
func (s *Store) ApplyResources(ctx context.Context, resources cloudplaneapi.ResourceBundle) (bool, error) {
	// 逐个资源组应用；每组内部负责校验、冲突判断和 create/update。
	configChanged, err := s.applyConfigSets(ctx, resources.ConfigSets)
	if err != nil {
		return false, err
	}
	secretChanged, err := s.applySecretSets(ctx, resources.SecretSets)
	if err != nil {
		return false, err
	}
	credentialChanged, err := s.applyRegistryCredentials(ctx, resources.RegistryCredentials)
	if err != nil {
		return false, err
	}
	return configChanged || secretChanged || credentialChanged, nil
}

// applyConfigSets 同步全局 config set 期望状态。
func (s *Store) applyConfigSets(ctx context.Context, specs []cloudplaneapi.ConfigSet) (bool, error) {
	items, err := s.listStoredConfigSets(ctx)
	if err != nil {
		return false, err
	}
	changed := false
	for _, spec := range specs {
		if current, ok := findStoredConfigSetByID(items, spec.ID); ok {
			if current.Name == spec.Name && maps.Equal(current.Values, spec.Values) {
				continue
			}
			if err := s.updateConfigSet(ctx, current.ID, spec); err != nil {
				return false, err
			}
			changed = true
			continue
		}
		if _, ok := findStoredConfigSetByName(items, spec.Name); ok {
			return false, ErrConfigSetNameAlreadyExists
		}
		if err := s.createConfigSet(ctx, spec); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}

// applySecretSets 同步全局 secret set 期望状态。
func (s *Store) applySecretSets(ctx context.Context, specs []cloudplaneapi.SecretSet) (bool, error) {
	items, err := s.listStoredSecretSets(ctx)
	if err != nil {
		return false, err
	}
	changed := false
	for _, spec := range specs {
		if current, ok := findStoredSecretSetByID(items, spec.ID); ok {
			if current.Name == spec.Name && maps.Equal(current.Values, spec.Values) {
				continue
			}
			if err := s.updateSecretSet(ctx, current.ID, spec); err != nil {
				return false, err
			}
			changed = true
			continue
		}
		if _, ok := findStoredSecretSetByName(items, spec.Name); ok {
			return false, ErrSecretSetNameAlreadyExists
		}
		if err := s.createSecretSet(ctx, spec); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}

// applyRegistryCredentials 同步全局 registry credential 期望状态。
func (s *Store) applyRegistryCredentials(ctx context.Context, specs []cloudplaneapi.RegistryCredential) (bool, error) {
	items, err := s.listStoredRegistryCredentials(ctx)
	if err != nil {
		return false, err
	}
	changed := false
	for _, spec := range specs {
		if current, ok := findStoredRegistryCredentialByID(items, spec.ID); ok {
			if current.Name == spec.Name && current.Server == spec.Server && current.Username == spec.Username && current.Password == spec.Password {
				continue
			}
			if err := s.updateRegistryCredential(ctx, current.ID, spec); err != nil {
				return false, err
			}
			changed = true
			continue
		}
		if _, ok := findStoredRegistryCredentialByName(items, spec.Name); ok {
			return false, ErrRegistryCredentialNameAlreadyExists
		}
		if err := s.createRegistryCredential(ctx, spec); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}

// createConfigSet 创建全局配置集。
func (s *Store) createConfigSet(ctx context.Context, spec cloudplaneapi.ConfigSet) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	valuesJSON, err := marshalJSON(spec.Values, map[string]string{})
	if err != nil {
		return fmt.Errorf("marshal config set values: %w", err)
	}
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO config_sets (
			id,
			name,
			values_json
		)
		VALUES ($1, $2, $3)
		RETURNING id
	`, spec.ID, spec.Name, valuesJSON).Scan(new(string))
	if err != nil {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" {
			return ErrConfigSetNameAlreadyExists
		}
		return fmt.Errorf("insert config set: %w", err)
	}
	return nil
}

// updateConfigSet 更新全局配置集。
func (s *Store) updateConfigSet(ctx context.Context, configSetID string, spec cloudplaneapi.ConfigSet) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	valuesJSON, err := marshalJSON(spec.Values, map[string]string{})
	if err != nil {
		return fmt.Errorf("marshal config set values: %w", err)
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE config_sets
		SET name = $2,
		    values_json = $3,
		    updated_at = now()
		WHERE id = $1
	`, configSetID, spec.Name, valuesJSON)
	if err != nil {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" {
			return ErrConfigSetNameAlreadyExists
		}
		return fmt.Errorf("update config set: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("read updated config set row count: %w", err)
	} else if affected == 0 {
		return ErrConfigSetNotFound
	}
	return nil
}

// listStoredConfigSets 读取全局 config set 完整记录。
func (s *Store) listStoredConfigSets(ctx context.Context) ([]storedConfigSet, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, values_json
		FROM config_sets
		ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query config sets: %w", err)
	}
	defer closeRows(rows)

	items := make([]storedConfigSet, 0)
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

// createSecretSet 创建全局密钥集。
func (s *Store) createSecretSet(ctx context.Context, spec cloudplaneapi.SecretSet) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	valuesJSON, err := marshalJSON(spec.Values, map[string]string{})
	if err != nil {
		return fmt.Errorf("marshal secret set values: %w", err)
	}
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO secret_sets (
			id,
			name,
			values_json
		)
		VALUES ($1, $2, $3)
		RETURNING id
	`, spec.ID, spec.Name, valuesJSON).Scan(new(string))
	if err != nil {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" {
			return ErrSecretSetNameAlreadyExists
		}
		return fmt.Errorf("insert secret set: %w", err)
	}
	return nil
}

// updateSecretSet 更新全局密钥集。
func (s *Store) updateSecretSet(ctx context.Context, secretSetID string, spec cloudplaneapi.SecretSet) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	valuesJSON, err := marshalJSON(spec.Values, map[string]string{})
	if err != nil {
		return fmt.Errorf("marshal secret set values: %w", err)
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE secret_sets
		SET name = $2,
		    values_json = $3,
		    updated_at = now()
		WHERE id = $1
	`, secretSetID, spec.Name, valuesJSON)
	if err != nil {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" {
			return ErrSecretSetNameAlreadyExists
		}
		return fmt.Errorf("update secret set: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("read updated secret set row count: %w", err)
	} else if affected == 0 {
		return ErrSecretSetNotFound
	}
	return nil
}

// listStoredSecretSets 读取全局 secret set 完整记录。
func (s *Store) listStoredSecretSets(ctx context.Context) ([]storedSecretSet, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, values_json
		FROM secret_sets
		ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query secret sets: %w", err)
	}
	defer closeRows(rows)

	items := make([]storedSecretSet, 0)
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

// createRegistryCredential 创建全局镜像仓库凭据。
func (s *Store) createRegistryCredential(ctx context.Context, spec cloudplaneapi.RegistryCredential) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO registry_credentials (
			id,
			name,
			server,
			username,
			password
		)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, spec.ID, spec.Name, spec.Server, spec.Username, spec.Password).Scan(new(string))
	if err != nil {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" {
			return ErrRegistryCredentialNameAlreadyExists
		}
		return fmt.Errorf("insert registry credential: %w", err)
	}
	return nil
}

// updateRegistryCredential 更新全局镜像仓库凭据。
func (s *Store) updateRegistryCredential(ctx context.Context, credentialID string, spec cloudplaneapi.RegistryCredential) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE registry_credentials
		SET name = $2,
		    server = $3,
		    username = $4,
		    password = $5,
		    updated_at = now()
		WHERE id = $1
	`, credentialID, spec.Name, spec.Server, spec.Username, spec.Password)
	if err != nil {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" {
			return ErrRegistryCredentialNameAlreadyExists
		}
		return fmt.Errorf("update registry credential: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("read updated registry credential row count: %w", err)
	} else if affected == 0 {
		return ErrRegistryCredentialNotFound
	}
	return nil
}

// listStoredRegistryCredentials 读取全局 registry credential 完整记录。
func (s *Store) listStoredRegistryCredentials(ctx context.Context) ([]storedRegistryCredential, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, server, username, password
		FROM registry_credentials
		ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query registry credentials: %w", err)
	}
	defer closeRows(rows)

	items := make([]storedRegistryCredential, 0)
	for rows.Next() {
		item, err := scanStoredRegistryCredential(rows)
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

// resolveSecretSet 读取指定密钥集及其明文值。
func (s *Store) resolveSecretSet(ctx context.Context, secretSetID string) (storedSecretSet, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, values_json
		FROM secret_sets
		WHERE id = $1
	`, secretSetID)

	item, err := scanSecretSet(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storedSecretSet{}, ErrSecretSetNotFound
		}
		return storedSecretSet{}, fmt.Errorf("query secret set: %w", err)
	}
	return item, nil
}

// resolveConfigSet 读取指定配置集及其值。
func (s *Store) resolveConfigSet(ctx context.Context, configSetID string) (storedConfigSet, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, values_json
		FROM config_sets
		WHERE id = $1
	`, configSetID)

	item, err := scanConfigSet(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storedConfigSet{}, ErrConfigSetNotFound
		}
		return storedConfigSet{}, fmt.Errorf("query config set: %w", err)
	}
	return item, nil
}

// resolveRegistryCredential 读取指定仓库凭据及其 password。
func (s *Store) resolveRegistryCredential(ctx context.Context, credentialID string) (storedRegistryCredential, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, server, username, password
		FROM registry_credentials
		WHERE id = $1
	`, credentialID)

	item, err := scanStoredRegistryCredential(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storedRegistryCredential{}, ErrRegistryCredentialNotFound
		}
		return storedRegistryCredential{}, fmt.Errorf("query registry credential: %w", err)
	}
	return item, nil
}

// scanConfigSet 从 SQL 扫描器读取一行 config set 完整记录。
func scanConfigSet(scanner interface{ Scan(dest ...any) error }) (storedConfigSet, error) {
	var item storedConfigSet
	var valuesJSON []byte
	if err := scanner.Scan(&item.ID, &item.Name, &valuesJSON); err != nil {
		return storedConfigSet{}, err
	}
	if err := unmarshalJSON(valuesJSON, &item.Values, map[string]string{}); err != nil {
		return storedConfigSet{}, fmt.Errorf("decode config set values: %w", err)
	}
	return item, nil
}

// scanSecretSet 从 SQL 扫描器读取一行 secret set 完整记录。
func scanSecretSet(scanner interface{ Scan(dest ...any) error }) (storedSecretSet, error) {
	var item storedSecretSet
	var valuesJSON []byte
	if err := scanner.Scan(&item.ID, &item.Name, &valuesJSON); err != nil {
		return storedSecretSet{}, err
	}
	if err := unmarshalJSON(valuesJSON, &item.Values, map[string]string{}); err != nil {
		return storedSecretSet{}, fmt.Errorf("decode secret set values: %w", err)
	}
	return item, nil
}

// scanStoredRegistryCredential 从 SQL 扫描器读取一行 registry credential 完整记录。
func scanStoredRegistryCredential(scanner interface{ Scan(dest ...any) error }) (storedRegistryCredential, error) {
	var item storedRegistryCredential
	if err := scanner.Scan(&item.ID, &item.Name, &item.Server, &item.Username, &item.Password); err != nil {
		return storedRegistryCredential{}, err
	}
	return item, nil
}

func findStoredConfigSetByID(items []storedConfigSet, id string) (storedConfigSet, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return storedConfigSet{}, false
}

func findStoredConfigSetByName(items []storedConfigSet, name string) (storedConfigSet, bool) {
	for _, item := range items {
		if item.Name == name {
			return item, true
		}
	}
	return storedConfigSet{}, false
}

func findStoredSecretSetByID(items []storedSecretSet, id string) (storedSecretSet, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return storedSecretSet{}, false
}

func findStoredSecretSetByName(items []storedSecretSet, name string) (storedSecretSet, bool) {
	for _, item := range items {
		if item.Name == name {
			return item, true
		}
	}
	return storedSecretSet{}, false
}

func findStoredRegistryCredentialByID(items []storedRegistryCredential, id string) (storedRegistryCredential, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return storedRegistryCredential{}, false
}

func findStoredRegistryCredentialByName(items []storedRegistryCredential, name string) (storedRegistryCredential, bool) {
	for _, item := range items {
		if item.Name == name {
			return item, true
		}
	}
	return storedRegistryCredential{}, false
}
