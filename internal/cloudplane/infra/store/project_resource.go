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
	ErrConfigSetNameAlreadyExists          = errors.New("config set name already exists in this project")
	ErrSecretSetNotFound                   = errors.New("secret set not found")
	ErrSecretSetNameAlreadyExists          = errors.New("secret set name already exists in this project")
	ErrRegistryCredentialNotFound          = errors.New("registry credential not found")
	ErrRegistryCredentialNameAlreadyExists = errors.New("registry credential name already exists in this project")
)

// storedConfigSet 是 store 内部读取到的 config set 完整记录。
type storedConfigSet struct {
	// Spec 保存可写入 revision/env 的非敏感配置内容。
	cloudplaneapi.ProjectConfigSet
	// ProjectID 是资源所属 project 的唯一标识。
	ProjectID string
}

// storedSecretSet 是 store 内部读取到的 secret set 完整记录；不得作为 API 响应返回。
type storedSecretSet struct {
	// Spec 保存敏感配置明文，仅允许 runtime materialize 路径内部使用。
	cloudplaneapi.ProjectSecretSet
	// ProjectID 是资源所属 project 的唯一标识。
	ProjectID string
}

// storedRegistryCredential 是 store 内部读取到的 registry credential 完整记录；不得作为 API 响应返回。
type storedRegistryCredential struct {
	// Spec 保存仓库凭据明文，仅允许镜像拉取凭据解析路径内部使用。
	cloudplaneapi.ProjectRegistryCredential
	// ProjectID 是资源所属 project 的唯一标识。
	ProjectID string
}

// ApplyProjectResources 按 project 聚合期望状态同步项目资源，并返回是否产生写入。
// 参数说明：ctx 控制数据库请求生命周期；projectID 是 project 唯一标识；resources 是 project 下属资源期望状态。
func (s *Store) ApplyProjectResources(ctx context.Context, projectID string, project cloudplaneapi.Project) (bool, error) {
	// 逐个资源组应用；每组内部负责校验、冲突判断和 create/update。
	configChanged, err := s.applyProjectConfigSets(ctx, projectID, project.ConfigSets)
	if err != nil {
		return false, err
	}
	secretChanged, err := s.applyProjectSecretSets(ctx, projectID, project.SecretSets)
	if err != nil {
		return false, err
	}
	credentialChanged, err := s.applyProjectRegistryCredentials(ctx, projectID, project.RegistryCredentials)
	if err != nil {
		return false, err
	}
	return configChanged || secretChanged || credentialChanged, nil
}

// applyProjectConfigSets 同步 project config set 期望状态。
func (s *Store) applyProjectConfigSets(ctx context.Context, projectID string, specs []cloudplaneapi.ProjectConfigSet) (bool, error) {
	items, err := s.listStoredConfigSets(ctx, projectID)
	if err != nil {
		return false, err
	}
	changed := false
	for _, spec := range specs {
		if current, ok := findStoredConfigSetByID(items, spec.ID); ok {
			if current.Name == spec.Name && maps.Equal(current.Values, spec.Values) {
				continue
			}
			if err := s.updateProjectConfigSet(ctx, projectID, current.ID, spec); err != nil {
				return false, err
			}
			changed = true
			continue
		}
		if _, ok := findStoredConfigSetByName(items, spec.Name); ok {
			return false, ErrConfigSetNameAlreadyExists
		}
		if err := s.createProjectConfigSet(ctx, projectID, spec); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}

// applyProjectSecretSets 同步 project secret set 期望状态。
func (s *Store) applyProjectSecretSets(ctx context.Context, projectID string, specs []cloudplaneapi.ProjectSecretSet) (bool, error) {
	items, err := s.listStoredSecretSets(ctx, projectID)
	if err != nil {
		return false, err
	}
	changed := false
	for _, spec := range specs {
		if current, ok := findStoredSecretSetByID(items, spec.ID); ok {
			if current.Name == spec.Name && maps.Equal(current.Values, spec.Values) {
				continue
			}
			if err := s.updateProjectSecretSet(ctx, projectID, current.ID, spec); err != nil {
				return false, err
			}
			changed = true
			continue
		}
		if _, ok := findStoredSecretSetByName(items, spec.Name); ok {
			return false, ErrSecretSetNameAlreadyExists
		}
		if err := s.createProjectSecretSet(ctx, projectID, spec); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}

// applyProjectRegistryCredentials 同步 project registry credential 期望状态。
func (s *Store) applyProjectRegistryCredentials(ctx context.Context, projectID string, specs []cloudplaneapi.ProjectRegistryCredential) (bool, error) {
	items, err := s.listStoredRegistryCredentials(ctx, projectID)
	if err != nil {
		return false, err
	}
	changed := false
	for _, spec := range specs {
		if current, ok := findStoredRegistryCredentialByID(items, spec.ID); ok {
			if current.Name == spec.Name && current.Server == spec.Server && current.Username == spec.Username && current.Password == spec.Password {
				continue
			}
			if err := s.updateProjectRegistryCredential(ctx, projectID, current.ID, spec); err != nil {
				return false, err
			}
			changed = true
			continue
		}
		if _, ok := findStoredRegistryCredentialByName(items, spec.Name); ok {
			return false, ErrRegistryCredentialNameAlreadyExists
		}
		if err := s.createProjectRegistryCredential(ctx, projectID, spec); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}

// createProjectConfigSet 创建项目配置集。
func (s *Store) createProjectConfigSet(ctx context.Context, projectID string, spec cloudplaneapi.ProjectConfigSet) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	valuesJSON, err := marshalJSON(spec.Values, map[string]string{})
	if err != nil {
		return fmt.Errorf("marshal config set values: %w", err)
	}
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO project_config_sets (
			id,
			project_id,
			name,
			values_json
		)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, spec.ID, projectID, spec.Name, valuesJSON).Scan(new(string))
	if err != nil {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
			switch pgErr.Code {
			case "23503":
				return ErrProjectNotFound
			case "23505":
				return ErrConfigSetNameAlreadyExists
			}
		}
		return fmt.Errorf("insert project config set: %w", err)
	}
	return nil
}

// updateProjectConfigSet 更新项目配置集。
func (s *Store) updateProjectConfigSet(ctx context.Context, projectID string, configSetID string, spec cloudplaneapi.ProjectConfigSet) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	valuesJSON, err := marshalJSON(spec.Values, map[string]string{})
	if err != nil {
		return fmt.Errorf("marshal config set values: %w", err)
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE project_config_sets
		SET name = $3,
		    values_json = $4,
		    updated_at = now()
		WHERE project_id = $1
		  AND id = $2
	`, projectID, configSetID, spec.Name, valuesJSON)
	if err != nil {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" {
			return ErrConfigSetNameAlreadyExists
		}
		return fmt.Errorf("update project config set: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("read updated config set row count: %w", err)
	} else if affected == 0 {
		return ErrConfigSetNotFound
	}
	return nil
}

// listStoredConfigSets 读取项目内 config set 完整记录。
func (s *Store) listStoredConfigSets(ctx context.Context, projectID string) ([]storedConfigSet, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, project_id, name, values_json
		FROM project_config_sets
		WHERE project_id = $1
		ORDER BY created_at ASC, id ASC
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("query project config sets: %w", err)
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
		return nil, fmt.Errorf("iterate project config sets: %w", err)
	}
	return items, nil
}

// createProjectSecretSet 创建项目密钥集。
func (s *Store) createProjectSecretSet(ctx context.Context, projectID string, spec cloudplaneapi.ProjectSecretSet) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	valuesJSON, err := marshalJSON(spec.Values, map[string]string{})
	if err != nil {
		return fmt.Errorf("marshal secret set values: %w", err)
	}
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO project_secret_sets (
			id,
			project_id,
			name,
			values_json
		)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, spec.ID, projectID, spec.Name, valuesJSON).Scan(new(string))
	if err != nil {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
			switch pgErr.Code {
			case "23503":
				return ErrProjectNotFound
			case "23505":
				return ErrSecretSetNameAlreadyExists
			}
		}
		return fmt.Errorf("insert project secret set: %w", err)
	}
	return nil
}

// updateProjectSecretSet 更新项目密钥集。
func (s *Store) updateProjectSecretSet(ctx context.Context, projectID string, secretSetID string, spec cloudplaneapi.ProjectSecretSet) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	valuesJSON, err := marshalJSON(spec.Values, map[string]string{})
	if err != nil {
		return fmt.Errorf("marshal secret set values: %w", err)
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE project_secret_sets
		SET name = $3,
		    values_json = $4,
		    updated_at = now()
		WHERE project_id = $1
		  AND id = $2
	`, projectID, secretSetID, spec.Name, valuesJSON)
	if err != nil {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" {
			return ErrSecretSetNameAlreadyExists
		}
		return fmt.Errorf("update project secret set: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("read updated secret set row count: %w", err)
	} else if affected == 0 {
		return ErrSecretSetNotFound
	}
	return nil
}

// listStoredSecretSets 读取项目内 secret set 完整记录。
func (s *Store) listStoredSecretSets(ctx context.Context, projectID string) ([]storedSecretSet, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, project_id, name, values_json
		FROM project_secret_sets
		WHERE project_id = $1
		ORDER BY created_at ASC, id ASC
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("query project secret sets: %w", err)
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
		return nil, fmt.Errorf("iterate project secret sets: %w", err)
	}
	return items, nil
}

// createProjectRegistryCredential 创建项目镜像仓库凭据。
func (s *Store) createProjectRegistryCredential(ctx context.Context, projectID string, spec cloudplaneapi.ProjectRegistryCredential) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO project_registry_credentials (
			id,
			project_id,
			name,
			server,
			username,
			password
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`, spec.ID, projectID, spec.Name, spec.Server, spec.Username, spec.Password).Scan(new(string))
	if err != nil {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
			switch pgErr.Code {
			case "23503":
				return ErrProjectNotFound
			case "23505":
				return ErrRegistryCredentialNameAlreadyExists
			}
		}
		return fmt.Errorf("insert project registry credential: %w", err)
	}
	return nil
}

// updateProjectRegistryCredential 更新项目镜像仓库凭据。
func (s *Store) updateProjectRegistryCredential(ctx context.Context, projectID string, credentialID string, spec cloudplaneapi.ProjectRegistryCredential) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE project_registry_credentials
		SET name = $3,
		    server = $4,
		    username = $5,
		    password = $6,
		    updated_at = now()
		WHERE project_id = $1
		  AND id = $2
	`, projectID, credentialID, spec.Name, spec.Server, spec.Username, spec.Password)
	if err != nil {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" {
			return ErrRegistryCredentialNameAlreadyExists
		}
		return fmt.Errorf("update project registry credential: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("read updated registry credential row count: %w", err)
	} else if affected == 0 {
		return ErrRegistryCredentialNotFound
	}
	return nil
}

// listStoredRegistryCredentials 读取项目内 registry credential 完整记录。
func (s *Store) listStoredRegistryCredentials(ctx context.Context, projectID string) ([]storedRegistryCredential, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, project_id, name, server, username, password
		FROM project_registry_credentials
		WHERE project_id = $1
		ORDER BY created_at ASC, id ASC
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("query project registry credentials: %w", err)
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
		return nil, fmt.Errorf("iterate project registry credentials: %w", err)
	}
	return items, nil
}

// resolveProjectSecretSet 读取项目内指定密钥集及其明文值。
// 参数说明：ctx 控制数据库请求生命周期；projectID 是 project 唯一标识；secretSetID 表示 secret set 的唯一标识。
func (s *Store) resolveProjectSecretSet(ctx context.Context, projectID string, secretSetID string) (storedSecretSet, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, project_id, name, values_json
		FROM project_secret_sets
		WHERE id = $1 AND project_id = $2
	`, secretSetID, projectID)

	item, err := scanSecretSet(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storedSecretSet{}, ErrSecretSetNotFound
		}
		return storedSecretSet{}, fmt.Errorf("query project secret set: %w", err)
	}
	return item, nil
}

// resolveProjectConfigSet 读取项目内指定配置集及其值。
// 参数说明：ctx 控制数据库请求生命周期；projectID 是 project 唯一标识；configSetID 表示 config set 的唯一标识。
func (s *Store) resolveProjectConfigSet(ctx context.Context, projectID string, configSetID string) (storedConfigSet, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, project_id, name, values_json
		FROM project_config_sets
		WHERE id = $1 AND project_id = $2
	`, configSetID, projectID)

	item, err := scanConfigSet(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storedConfigSet{}, ErrConfigSetNotFound
		}
		return storedConfigSet{}, fmt.Errorf("query project config set: %w", err)
	}
	return item, nil
}

// resolveProjectRegistryCredential 读取项目内指定仓库凭据及其 password。
// 参数说明：ctx 控制数据库请求生命周期；projectID 是 project 唯一标识；credentialID 表示 credential 的唯一标识。
func (s *Store) resolveProjectRegistryCredential(ctx context.Context, projectID string, credentialID string) (storedRegistryCredential, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, project_id, name, server, username, password
		FROM project_registry_credentials
		WHERE id = $1 AND project_id = $2
	`, credentialID, projectID)

	item, err := scanStoredRegistryCredential(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storedRegistryCredential{}, ErrRegistryCredentialNotFound
		}
		return storedRegistryCredential{}, fmt.Errorf("query project registry credential: %w", err)
	}
	return item, nil
}

// scanConfigSet 从 SQL 扫描器读取一行 config set 完整记录。
func scanConfigSet(scanner interface{ Scan(dest ...any) error }) (storedConfigSet, error) {
	var item storedConfigSet
	var valuesJSON []byte
	if err := scanner.Scan(&item.ID, &item.ProjectID, &item.Name, &valuesJSON); err != nil {
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
	if err := scanner.Scan(&item.ID, &item.ProjectID, &item.Name, &valuesJSON); err != nil {
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
	if err := scanner.Scan(&item.ID, &item.ProjectID, &item.Name, &item.Server, &item.Username, &item.Password); err != nil {
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
