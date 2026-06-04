package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"mini-cloud/internal/cloudplane/domain/deployment"
	"mini-cloud/internal/cloudplane/domain/revision"
	"mini-cloud/internal/cloudplane/domain/scheduler"
	"mini-cloud/internal/cloudplane/domain/workload"
	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/projectedfile"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrServiceNotFound          = errors.New("service not found")
	ErrServiceNameAlreadyExists = errors.New("service name already exists")
	ErrRevisionNotFound         = errors.New("revision not found")
	ErrDeploymentNotFound       = errors.New("deployment not found")
)

// ServiceUpdateImpact 描述 store 模块中的 service update impact 数据。
type ServiceUpdateImpact struct {
	// RevisionChanged 表示更新是否改变了 revision 规格。
	RevisionChanged bool
	// ReplicasChanged 表示本次更新是否改变了副本数；是否为 replica-only 需结合 RevisionChanged 判断。
	ReplicasChanged bool
	// PreviousReplicas 是更新前的期望副本数。
	PreviousReplicas int
	// UpdatedReplicas 是更新后的期望副本数。
	UpdatedReplicas int
}

// InsertService 校验资源引用后创建 service 规格记录。
// 参数说明：ctx 控制数据库请求生命周期；serviceID/name 是 service 身份；spec 是 service 目标运行规格。
func (s *Store) InsertService(ctx context.Context, serviceID string, name string, displayName string, spec workload.Spec) (workload.Service, error) {
	// 阶段一：校验输入、全局资源引用和 projected file 来源。
	// 阶段二：持久化 service spec；后续 revision 会从这些字段生成不可变快照。
	// exposure 入库前归一化，保证后续比较和 revision 生成使用稳定值。
	serviceID = strings.TrimSpace(serviceID)
	name = strings.TrimSpace(name)
	if serviceID == "" {
		return workload.Service{}, workload.ErrServiceIDRequired
	}
	if err := workload.ValidateIdentity(name); err != nil {
		return workload.Service{}, err
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		return workload.Service{}, workload.ErrServiceDisplayNameRequired
	}
	spec.Exposure = spec.NormalizedExposure()
	if err := spec.Validate(); err != nil {
		return workload.Service{}, err
	}

	if err := s.ensureServiceReferencesResolved(ctx, spec.ConfigSetID, spec.SecretSetID, spec.RegistryCredentialID); err != nil {
		return workload.Service{}, err
	}
	// projected file 来源必须在创建时可解析。
	if err := s.ensureServiceProjectedFilesResolved(ctx, spec.ProjectedFiles); err != nil {
		return workload.Service{}, err
	}

	// service 运行输入和扩展字段以 JSONB 存储。
	envJSON, err := marshalJSON(spec.Env, map[string]string{})
	if err != nil {
		return workload.Service{}, fmt.Errorf("marshal service env: %w", err)
	}
	commandJSON, err := marshalJSON(spec.Command, []string{})
	if err != nil {
		return workload.Service{}, fmt.Errorf("marshal service command: %w", err)
	}
	argsJSON, err := marshalJSON(spec.Args, []string{})
	if err != nil {
		return workload.Service{}, fmt.Errorf("marshal service args: %w", err)
	}
	projectedFilesJSON, err := marshalJSON(projectedfile.CloneSpecs(spec.ProjectedFiles), []projectedfile.Spec{})
	if err != nil {
		return workload.Service{}, fmt.Errorf("marshal service projected files: %w", err)
	}
	persistentDirsJSON, err := marshalJSON(persistentdir.CloneSpecs(spec.PersistentDirs), []persistentdir.Spec{})
	if err != nil {
		return workload.Service{}, fmt.Errorf("marshal service persistent dirs: %w", err)
	}

	// nullable 外键和 JSON 字段用中间变量扫描。
	var created workload.Service
	var currentRevisionID sql.NullString
	var candidateRevisionID sql.NullString
	var configSetID sql.NullString
	var secretSetID sql.NullString
	var registryCredentialID sql.NullString
	// 插入 service，初始状态为 idle，rollout phase 为 idle。
	err = s.db.QueryRowContext(ctx, `
			INSERT INTO services (
				id,
				name,
				display_name,
			spec_region,
			spec_replicas,
			spec_instance_class,
			spec_exposure,
			spec_image,
			spec_command_json,
			spec_args_json,
			spec_default_port,
			spec_readiness_path,
			spec_config_set_id,
			spec_secret_set_id,
			spec_registry_credential_id,
			spec_projected_files_json,
			spec_persistent_dirs_json,
			spec_env_json,
			status_rollout_phase,
			status_rollout_message,
				status_phase
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)
			RETURNING
				id,
				name,
			display_name,
			spec_region,
			spec_replicas,
			spec_instance_class,
			spec_exposure,
			spec_image,
			spec_command_json,
			spec_args_json,
			spec_default_port,
			spec_readiness_path,
			spec_config_set_id,
			spec_secret_set_id,
			spec_registry_credential_id,
			spec_projected_files_json,
			spec_persistent_dirs_json,
			spec_env_json,
			status_current_revision_id,
			status_candidate_revision_id,
			status_rollout_phase,
			status_rollout_message,
			status_phase,
			created_at,
			updated_at
			`,
			serviceID,
			name,
			displayName,
		spec.Region,
		spec.Replicas,
		spec.InstanceClass,
		spec.Exposure,
		spec.Image,
		commandJSON,
		argsJSON,
		spec.DefaultPort,
		spec.ReadinessPath,
		nullableString(spec.ConfigSetID),
		nullableString(spec.SecretSetID),
		nullableString(spec.RegistryCredentialID),
		projectedFilesJSON,
		persistentDirsJSON,
		envJSON,
		workload.RolloutPhaseIdle,
		"",
		workload.RolloutPhaseIdle,
		).Scan(
			&created.Metadata.ID,
			&created.Metadata.Name,
		&created.Metadata.DisplayName,
		&created.Spec.Region,
		&created.Spec.Replicas,
		&created.Spec.InstanceClass,
		&created.Spec.Exposure,
		&created.Spec.Image,
		&commandJSON,
		&argsJSON,
		&created.Spec.DefaultPort,
		&created.Spec.ReadinessPath,
		&configSetID,
		&secretSetID,
		&registryCredentialID,
		&projectedFilesJSON,
		&persistentDirsJSON,
		&envJSON,
		&currentRevisionID,
		&candidateRevisionID,
		&created.Status.RolloutPhase,
		&created.Status.RolloutMessage,
		&created.Status.Phase,
		&created.CreatedAt,
		&created.UpdatedAt,
	)
	if err != nil {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
			switch pgErr.Code {
			case "23505":
				// serviceID 主键冲突表示调用方试图复用已存在 service 身份。
				if strings.Contains(pgErr.ConstraintName, "pkey") {
					return workload.Service{}, workload.ErrServiceIdentityConflict
				}
				// service name 是 single-tenant control plane 内全局唯一。
				if strings.Contains(pgErr.ConstraintName, "name") {
					return workload.Service{}, ErrServiceNameAlreadyExists
				}
			}
		}
		return workload.Service{}, fmt.Errorf("insert service: %w", err)
	}

	// 将数据库返回的 JSONB 字段恢复为领域对象字段。
	if err := unmarshalJSON(envJSON, &created.Spec.Env, map[string]string{}); err != nil {
		return workload.Service{}, fmt.Errorf("decode service env: %w", err)
	}
	if err := unmarshalJSON(commandJSON, &created.Spec.Command, []string{}); err != nil {
		return workload.Service{}, fmt.Errorf("decode service command: %w", err)
	}
	if err := unmarshalJSON(argsJSON, &created.Spec.Args, []string{}); err != nil {
		return workload.Service{}, fmt.Errorf("decode service args: %w", err)
	}
	if err := unmarshalJSON(projectedFilesJSON, &created.Spec.ProjectedFiles, []projectedfile.Spec{}); err != nil {
		return workload.Service{}, fmt.Errorf("decode service projected files: %w", err)
	}
	// clone 后返回，避免调用方复用时影响本函数组装出的中间切片。
	created.Spec.ProjectedFiles = projectedfile.CloneSpecs(created.Spec.ProjectedFiles)
	if err := unmarshalJSON(persistentDirsJSON, &created.Spec.PersistentDirs, []persistentdir.Spec{}); err != nil {
		return workload.Service{}, fmt.Errorf("decode service persistent dirs: %w", err)
	}
	// clone 后返回，避免调用方复用时影响本函数组装出的中间切片。
	created.Spec.PersistentDirs = persistentdir.CloneSpecs(created.Spec.PersistentDirs)
	// nullable revision/resource 外键有值时写回领域对象。
	if currentRevisionID.Valid {
		created.Status.CurrentRevisionID = currentRevisionID.String
	}
	if candidateRevisionID.Valid {
		created.Status.CandidateRevisionID = candidateRevisionID.String
	}
	if configSetID.Valid {
		created.Spec.ConfigSetID = configSetID.String
	}
	if secretSetID.Valid {
		created.Spec.SecretSetID = secretSetID.String
	}
	if registryCredentialID.Valid {
		created.Spec.RegistryCredentialID = registryCredentialID.String
	}

	return created, nil
}

// ListServices 列出所有 service。
// 参数说明：ctx 控制数据库请求生命周期。
func (s *Store) ListServices(ctx context.Context) ([]workload.Service, error) {
	// 当前不分页，按创建时间稳定返回。
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			name,
				display_name,
				spec_region,
				spec_replicas,
				spec_instance_class,
				spec_exposure,
				spec_image,
				spec_command_json,
				spec_args_json,
				spec_default_port,
				spec_readiness_path,
				spec_config_set_id,
				spec_secret_set_id,
				spec_registry_credential_id,
				spec_projected_files_json,
				spec_persistent_dirs_json,
				spec_env_json,
				status_current_revision_id,
				status_candidate_revision_id,
				status_rollout_phase,
				status_rollout_message,
				status_phase,
				created_at,
				updated_at
		FROM services
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query services: %w", err)
	}
	defer closeRows(rows)

	// 逐行扫描 service。
	items := make([]workload.Service, 0)
	for rows.Next() {
		item, err := scanService(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	// rows.Err 捕获迭代过程中延迟暴露的数据库错误。
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate services: %w", err)
	}

	return items, nil
}

// GetService 按 service ID 查询 service。
// 参数说明：ctx 控制数据库请求生命周期；serviceID 是 service 唯一标识。
func (s *Store) GetService(ctx context.Context, serviceID string) (workload.Service, error) {
	// 按主键读取 service 完整字段。
	row := s.db.QueryRowContext(ctx, `
		SELECT
			id,
			name,
				display_name,
				spec_region,
				spec_replicas,
				spec_instance_class,
				spec_exposure,
				spec_image,
				spec_command_json,
				spec_args_json,
				spec_default_port,
				spec_readiness_path,
				spec_config_set_id,
				spec_secret_set_id,
				spec_registry_credential_id,
				spec_projected_files_json,
				spec_persistent_dirs_json,
				spec_env_json,
				status_current_revision_id,
				status_candidate_revision_id,
				status_rollout_phase,
				status_rollout_message,
				status_phase,
				created_at,
				updated_at
		FROM services
		WHERE id = $1
	`, serviceID)

	item, err := scanService(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 数据库无行时转换为 service not found。
			return workload.Service{}, ErrServiceNotFound
		}
		return workload.Service{}, err
	}

	return item, nil
}

// UpdateServiceSpec 校验目标规格，更新 service，并返回本次变更对发布流程的影响。
// 参数说明：ctx 控制数据库请求生命周期；serviceID 是 service 唯一标识；spec 是目标 service 运行规格。
func (s *Store) UpdateServiceSpec(ctx context.Context, serviceID string, displayName string, spec workload.Spec) (workload.Service, ServiceUpdateImpact, error) {
	// 阶段一：校验目标规格、资源引用和 persistent dir 变更限制。
	// 阶段二：计算当前 plan 与目标 plan 的 quota 差异。
	// 阶段三：区分 revision-changing、replica-only 和 metadata-only 更新，供 lifecycle 决定后续动作。
	// exposure 入库前归一化，保证比较逻辑使用稳定值。
	spec.Exposure = spec.NormalizedExposure()
	if err := spec.Validate(); err != nil {
		return workload.Service{}, ServiceUpdateImpact{}, err
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		return workload.Service{}, ServiceUpdateImpact{}, workload.ErrServiceDisplayNameRequired
	}

	current, err := s.GetService(ctx, serviceID)
	if err != nil {
		return workload.Service{}, ServiceUpdateImpact{}, err
	}
	// persistent dir 变更会影响已有数据挂载；有已发布 revision 时暂不允许触发新 rollout。
	if persistentDirRevisionChangeBlocked(current, spec) {
		return workload.Service{}, ServiceUpdateImpact{}, workload.ErrPersistentDirsRolloutUnsupported
	}
	// 校验全局资源引用仍可解析。
	if err := s.ensureServiceReferencesResolved(ctx, spec.ConfigSetID, spec.SecretSetID, spec.RegistryCredentialID); err != nil {
		return workload.Service{}, ServiceUpdateImpact{}, err
	}
	if err := s.ensureServiceProjectedFilesResolved(ctx, spec.ProjectedFiles); err != nil {
		return workload.Service{}, ServiceUpdateImpact{}, err
	}

	// 将目标 service spec 的 JSON 字段序列化为稳定存储格式。
	envJSON, err := marshalJSON(spec.Env, map[string]string{})
	if err != nil {
		return workload.Service{}, ServiceUpdateImpact{}, fmt.Errorf("marshal service env for update: %w", err)
	}
	commandJSON, err := marshalJSON(spec.Command, []string{})
	if err != nil {
		return workload.Service{}, ServiceUpdateImpact{}, fmt.Errorf("marshal service command for update: %w", err)
	}
	argsJSON, err := marshalJSON(spec.Args, []string{})
	if err != nil {
		return workload.Service{}, ServiceUpdateImpact{}, fmt.Errorf("marshal service args for update: %w", err)
	}
	projectedFilesJSON, err := marshalJSON(projectedfile.CloneSpecs(spec.ProjectedFiles), []projectedfile.Spec{})
	if err != nil {
		return workload.Service{}, ServiceUpdateImpact{}, fmt.Errorf("marshal service projected files for update: %w", err)
	}
	persistentDirsJSON, err := marshalJSON(persistentdir.CloneSpecs(spec.PersistentDirs), []persistentdir.Spec{})
	if err != nil {
		return workload.Service{}, ServiceUpdateImpact{}, fmt.Errorf("marshal service persistent dirs for update: %w", err)
	}

	// nullable 外键和 JSON 字段用中间变量扫描。
	var updated workload.Service
	var updatedConfigSetID sql.NullString
	var updatedSecretSetID sql.NullString
	var updatedRegistryCredentialID sql.NullString
	var updatedProjectedFilesJSON []byte
	var updatedPersistentDirsJSON []byte
	var currentRevisionID sql.NullString
	var candidateRevisionID sql.NullString
	// 更新 service spec 主体字段，不直接修改 current/candidate revision。
	err = s.db.QueryRowContext(ctx, `
		UPDATE services
		SET
			display_name = $2,
			spec_region = $3,
			spec_replicas = $4,
			spec_instance_class = $5,
			spec_exposure = $6,
			spec_image = $7,
			spec_command_json = $8,
			spec_args_json = $9,
			spec_default_port = $10,
			spec_readiness_path = $11,
			spec_config_set_id = $12,
			spec_secret_set_id = $13,
			spec_registry_credential_id = $14,
			spec_projected_files_json = $15,
			spec_persistent_dirs_json = $16,
			spec_env_json = $17,
			updated_at = now()
		WHERE id = $1
			RETURNING
				id,
				name,
			display_name,
			spec_region,
			spec_replicas,
			spec_instance_class,
			spec_exposure,
			spec_image,
			spec_command_json,
			spec_args_json,
			spec_default_port,
			spec_readiness_path,
			spec_config_set_id,
			spec_secret_set_id,
			spec_registry_credential_id,
			spec_projected_files_json,
			spec_persistent_dirs_json,
			spec_env_json,
			status_current_revision_id,
			status_candidate_revision_id,
			status_rollout_phase,
			status_rollout_message,
			status_phase,
			created_at,
			updated_at
	`, serviceID, displayName, spec.Region, spec.Replicas, spec.InstanceClass, spec.Exposure, spec.Image, commandJSON, argsJSON, spec.DefaultPort, spec.ReadinessPath, nullableString(spec.ConfigSetID), nullableString(spec.SecretSetID), nullableString(spec.RegistryCredentialID), projectedFilesJSON, persistentDirsJSON, envJSON).Scan(
		&updated.Metadata.ID,
		&updated.Metadata.Name,
		&updated.Metadata.DisplayName,
		&updated.Spec.Region,
		&updated.Spec.Replicas,
		&updated.Spec.InstanceClass,
		&updated.Spec.Exposure,
		&updated.Spec.Image,
		&commandJSON,
		&argsJSON,
		&updated.Spec.DefaultPort,
		&updated.Spec.ReadinessPath,
		&updatedConfigSetID,
		&updatedSecretSetID,
		&updatedRegistryCredentialID,
		&updatedProjectedFilesJSON,
		&updatedPersistentDirsJSON,
		&envJSON,
		&currentRevisionID,
		&candidateRevisionID,
		&updated.Status.RolloutPhase,
		&updated.Status.RolloutMessage,
		&updated.Status.Phase,
		&updated.CreatedAt,
		&updated.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// serviceID 不存在时返回 not found。
			return workload.Service{}, ServiceUpdateImpact{}, ErrServiceNotFound
		}
		return workload.Service{}, ServiceUpdateImpact{}, fmt.Errorf("update service: %w", err)
	}
	// 将数据库返回的 JSONB 字段恢复为领域对象字段。
	if err := unmarshalJSON(envJSON, &updated.Spec.Env, map[string]string{}); err != nil {
		return workload.Service{}, ServiceUpdateImpact{}, fmt.Errorf("decode updated service env: %w", err)
	}
	if err := unmarshalJSON(commandJSON, &updated.Spec.Command, []string{}); err != nil {
		return workload.Service{}, ServiceUpdateImpact{}, fmt.Errorf("decode updated service command: %w", err)
	}
	if err := unmarshalJSON(argsJSON, &updated.Spec.Args, []string{}); err != nil {
		return workload.Service{}, ServiceUpdateImpact{}, fmt.Errorf("decode updated service args: %w", err)
	}
	if err := unmarshalJSON(updatedProjectedFilesJSON, &updated.Spec.ProjectedFiles, []projectedfile.Spec{}); err != nil {
		return workload.Service{}, ServiceUpdateImpact{}, fmt.Errorf("decode updated service projected files: %w", err)
	}
	// clone 后返回，避免调用方复用时影响本函数组装出的中间切片。
	updated.Spec.ProjectedFiles = projectedfile.CloneSpecs(updated.Spec.ProjectedFiles)
	if err := unmarshalJSON(updatedPersistentDirsJSON, &updated.Spec.PersistentDirs, []persistentdir.Spec{}); err != nil {
		return workload.Service{}, ServiceUpdateImpact{}, fmt.Errorf("decode updated service persistent dirs: %w", err)
	}
	// clone 后返回，避免调用方复用时影响本函数组装出的中间切片。
	updated.Spec.PersistentDirs = persistentdir.CloneSpecs(updated.Spec.PersistentDirs)
	// nullable 外键有值时写回领域对象。
	if updatedConfigSetID.Valid {
		updated.Spec.ConfigSetID = updatedConfigSetID.String
	}
	if updatedSecretSetID.Valid {
		updated.Spec.SecretSetID = updatedSecretSetID.String
	}
	if updatedRegistryCredentialID.Valid {
		updated.Spec.RegistryCredentialID = updatedRegistryCredentialID.String
	}
	if currentRevisionID.Valid {
		updated.Status.CurrentRevisionID = currentRevisionID.String
	}
	if candidateRevisionID.Valid {
		updated.Status.CandidateRevisionID = candidateRevisionID.String
	}

	// impact 描述本次更新对后续 rollout 或 scale 的影响。
	return updated, ServiceUpdateImpact{
		RevisionChanged:  serviceNeedsNewRevision(current, updated),
		ReplicasChanged:  current.Spec.Replicas != updated.Spec.Replicas,
		PreviousReplicas: current.Spec.Replicas,
		UpdatedReplicas:  updated.Spec.Replicas,
	}, nil
}

// DeleteService 删除 service 记录。
// 参数说明：ctx 控制数据库请求生命周期；serviceID 是 service 唯一标识。
func (s *Store) DeleteService(ctx context.Context, serviceID string) error {
	// 这里只删除 services 表记录；关联数据由数据库约束决定是否允许删除。
	result, err := s.db.ExecContext(ctx, `DELETE FROM services WHERE id = $1`, serviceID)
	if err != nil {
		return fmt.Errorf("delete service: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		// 没有删除任何行时返回 not found。
		return ErrServiceNotFound
	}
	return nil
}

// UpdateServiceRevisionState 更新 service revision state。
// 参数说明：ctx 控制数据库请求生命周期；serviceID 是 service 唯一标识；currentRevisionIDValue/candidateRevisionIDValue 是要写入的 revision 状态；status/rolloutPhase/rolloutMessage 是要写入的 rollout 状态。
func (s *Store) UpdateServiceRevisionState(
	ctx context.Context,
	serviceID string,
	currentRevisionIDValue string,
	candidateRevisionIDValue string,
	status string,
	rolloutPhase string,
	rolloutMessage string,
) (workload.Service, error) {
	// 复杂流程说明：该方法统一写入 current/candidate revision、service status 和 rollout message。
	// 通过集中入口维护 revision 状态，避免多个调用方各自拼 SQL 造成状态组合不一致。
	// nullable 外键和 JSON 字段用中间变量扫描。
	var updated workload.Service
	var envJSON []byte
	var commandJSON []byte
	var argsJSON []byte
	var projectedFilesJSON []byte
	var persistentDirsJSON []byte
	var currentRevisionID sql.NullString
	var candidateRevisionID sql.NullString
	var configSetID sql.NullString
	var secretSetID sql.NullString
	var registryCredentialID sql.NullString

	// 更新 revision 状态字段，并返回 service 完整快照。
	err := s.db.QueryRowContext(ctx, `
		UPDATE services
		SET
			status_current_revision_id = $2,
			status_candidate_revision_id = $3,
			status_phase = $4,
			status_rollout_phase = $5,
			status_rollout_message = $6,
			updated_at = now()
		WHERE id = $1
			RETURNING
				id,
				name,
				display_name,
				spec_region,
				spec_replicas,
				spec_instance_class,
				spec_exposure,
				spec_image,
				spec_command_json,
				spec_args_json,
				spec_default_port,
				spec_readiness_path,
				spec_config_set_id,
				spec_secret_set_id,
				spec_registry_credential_id,
				spec_projected_files_json,
				spec_persistent_dirs_json,
				spec_env_json,
				status_current_revision_id,
				status_candidate_revision_id,
				status_rollout_phase,
				status_rollout_message,
				status_phase,
				created_at,
				updated_at
		`, serviceID, nullableString(currentRevisionIDValue), nullableString(candidateRevisionIDValue), status, workload.NormalizeRolloutPhase(rolloutPhase), rolloutMessage).Scan(
		&updated.Metadata.ID,
		&updated.Metadata.Name,
		&updated.Metadata.DisplayName,
		&updated.Spec.Region,
		&updated.Spec.Replicas,
		&updated.Spec.InstanceClass,
		&updated.Spec.Exposure,
		&updated.Spec.Image,
		&commandJSON,
		&argsJSON,
		&updated.Spec.DefaultPort,
		&updated.Spec.ReadinessPath,
		&configSetID,
		&secretSetID,
		&registryCredentialID,
		&projectedFilesJSON,
		&persistentDirsJSON,
		&envJSON,
		&currentRevisionID,
		&candidateRevisionID,
		&updated.Status.RolloutPhase,
		&updated.Status.RolloutMessage,
		&updated.Status.Phase,
		&updated.CreatedAt,
		&updated.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// serviceID 不存在时返回 not found。
			return workload.Service{}, ErrServiceNotFound
		}
		return workload.Service{}, fmt.Errorf("update service current revision: %w", err)
	}

	// 将数据库返回的 JSONB 字段恢复为领域对象字段。
	if err := unmarshalJSON(envJSON, &updated.Spec.Env, map[string]string{}); err != nil {
		return workload.Service{}, fmt.Errorf("decode service env: %w", err)
	}
	if err := unmarshalJSON(commandJSON, &updated.Spec.Command, []string{}); err != nil {
		return workload.Service{}, fmt.Errorf("decode service command: %w", err)
	}
	if err := unmarshalJSON(argsJSON, &updated.Spec.Args, []string{}); err != nil {
		return workload.Service{}, fmt.Errorf("decode service args: %w", err)
	}
	if err := unmarshalJSON(projectedFilesJSON, &updated.Spec.ProjectedFiles, []projectedfile.Spec{}); err != nil {
		return workload.Service{}, fmt.Errorf("decode service projected files: %w", err)
	}
	// clone 后返回，避免调用方复用时影响本函数组装出的中间切片。
	updated.Spec.ProjectedFiles = projectedfile.CloneSpecs(updated.Spec.ProjectedFiles)
	if err := unmarshalJSON(persistentDirsJSON, &updated.Spec.PersistentDirs, []persistentdir.Spec{}); err != nil {
		return workload.Service{}, fmt.Errorf("decode service persistent dirs: %w", err)
	}
	// clone 后返回，避免调用方复用时影响本函数组装出的中间切片。
	updated.Spec.PersistentDirs = persistentdir.CloneSpecs(updated.Spec.PersistentDirs)
	// nullable revision/resource 外键有值时写回领域对象。
	if currentRevisionID.Valid {
		updated.Status.CurrentRevisionID = currentRevisionID.String
	}
	if candidateRevisionID.Valid {
		updated.Status.CandidateRevisionID = candidateRevisionID.String
	}
	if configSetID.Valid {
		updated.Spec.ConfigSetID = configSetID.String
	}
	if secretSetID.Valid {
		updated.Spec.SecretSetID = secretSetID.String
	}
	if registryCredentialID.Valid {
		updated.Spec.RegistryCredentialID = registryCredentialID.String
	}

	return updated, nil
}

// InsertRevisionFromService 把当前 service 规格复制为不可变 revision 快照。
// 参数说明：ctx 控制数据库请求生命周期；serviceID 是 service 唯一标识。
func (s *Store) InsertRevisionFromService(ctx context.Context, serviceID string) (revision.Revision, error) {
	// 阶段一：重新校验 service 引用，避免创建引用已删除资源的 revision。
	// 阶段二：把 command/env/projected files/persistent dirs 序列化进 revision 快照。
	// 阶段三：生成 service 内递增 revision number 和 label。
	// 先读取 service 当前 spec。
	serviceItem, err := s.GetService(ctx, serviceID)
	if err != nil {
		return revision.Revision{}, err
	}
	if err := s.ensureServiceReferencesResolved(ctx, serviceItem.Spec.ConfigSetID, serviceItem.Spec.SecretSetID, serviceItem.Spec.RegistryCredentialID); err != nil {
		return revision.Revision{}, err
	}
	// projected file 来源必须仍可解析。
	if err := s.ensureServiceProjectedFilesResolved(ctx, serviceItem.Spec.ProjectedFiles); err != nil {
		return revision.Revision{}, err
	}

	// 生成 revision ID。
	id, err := newID("rev")
	if err != nil {
		return revision.Revision{}, err
	}

	// 将 service 当前运行输入序列化为 revision 快照。
	commandJSON, err := marshalJSON(serviceItem.Spec.Command, []string{})
	if err != nil {
		return revision.Revision{}, fmt.Errorf("marshal revision command: %w", err)
	}
	argsJSON, err := marshalJSON(serviceItem.Spec.Args, []string{})
	if err != nil {
		return revision.Revision{}, fmt.Errorf("marshal revision args: %w", err)
	}
	envJSON, err := marshalJSON(serviceItem.Spec.Env, map[string]string{})
	if err != nil {
		return revision.Revision{}, fmt.Errorf("marshal revision env: %w", err)
	}
	if serviceItem.Spec.ConfigSetID != "" {
		// config set 值会合并进 revision env，形成不可变环境快照。
		configSet, err := s.resolveConfigSet(ctx, serviceItem.Spec.ConfigSetID)
		if err != nil {
			return revision.Revision{}, err
		}
		envJSON, err = marshalJSON(mergeStringMaps(serviceItem.Spec.Env, configSet.Values), map[string]string{})
		if err != nil {
			return revision.Revision{}, fmt.Errorf("marshal revision env with config set: %w", err)
		}
	}
	projectedFilesJSON, err := marshalJSON(projectedfile.CloneSpecs(serviceItem.Spec.ProjectedFiles), []projectedfile.Spec{})
	if err != nil {
		return revision.Revision{}, fmt.Errorf("marshal revision projected files: %w", err)
	}
	persistentDirsJSON, err := marshalJSON(persistentdir.CloneSpecs(serviceItem.Spec.PersistentDirs), []persistentdir.Spec{})
	if err != nil {
		return revision.Revision{}, fmt.Errorf("marshal revision persistent dirs: %w", err)
	}

	// revision number 生成和 revision 插入必须在同一事务内完成。
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return revision.Revision{}, fmt.Errorf("begin create revision tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	// 在 service 内递增 revision_number。
	var nextNumber int
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(revision_number), 0) + 1
		FROM revisions
		WHERE service_id = $1
	`, serviceID).Scan(&nextNumber); err != nil {
		return revision.Revision{}, fmt.Errorf("query next revision number: %w", err)
	}

	// label 由 revision number 派生。
	label := revision.LabelForNumber(nextNumber)
	// nullable 外键用中间变量扫描。
	var created revision.Revision
	var configSetID sql.NullString
	var secretSetID sql.NullString
	var registryCredentialID sql.NullString
	// 插入 revision 快照。
	err = tx.QueryRowContext(ctx, `
		INSERT INTO revisions (
			id,
			service_id,
			label,
			revision_number,
			spec_image,
			spec_command_json,
			spec_args_json,
			spec_env_json,
			spec_config_set_id,
			spec_secret_set_id,
			spec_registry_credential_id,
			spec_projected_files_json,
			spec_persistent_dirs_json,
			port,
			readiness_path
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING id, service_id, revision_number, label, spec_image, spec_command_json, spec_args_json, spec_env_json, spec_config_set_id, spec_secret_set_id, spec_registry_credential_id, spec_projected_files_json, spec_persistent_dirs_json, port, spec_readiness_path, created_at
	`,
		id,
		serviceID,
		label,
		nextNumber,
		serviceItem.Spec.Image,
		commandJSON,
		argsJSON,
		envJSON,
		nullableString(serviceItem.Spec.ConfigSetID),
		nullableString(serviceItem.Spec.SecretSetID),
		nullableString(serviceItem.Spec.RegistryCredentialID),
		projectedFilesJSON,
		persistentDirsJSON,
		serviceItem.Spec.DefaultPort,
		serviceItem.Spec.ReadinessPath,
	).Scan(
		&created.ID,
		&created.ServiceID,
		&created.Number,
		&created.Label,
		&created.Image,
		&commandJSON,
		&argsJSON,
		&envJSON,
		&configSetID,
		&secretSetID,
		&registryCredentialID,
		&projectedFilesJSON,
		&persistentDirsJSON,
		&created.Port,
		&created.ReadinessPath,
		&created.CreatedAt,
	)
	if err != nil {
		return revision.Revision{}, fmt.Errorf("insert revision: %w", err)
	}
	// 提交 revision number 和 revision 记录。
	if err := tx.Commit(); err != nil {
		return revision.Revision{}, fmt.Errorf("commit create revision tx: %w", err)
	}

	// 将数据库返回的 JSONB 字段恢复为 revision 领域对象。
	if err := unmarshalJSON(commandJSON, &created.Command, []string{}); err != nil {
		return revision.Revision{}, fmt.Errorf("decode revision command: %w", err)
	}
	if err := unmarshalJSON(argsJSON, &created.Args, []string{}); err != nil {
		return revision.Revision{}, fmt.Errorf("decode revision args: %w", err)
	}
	if err := unmarshalJSON(envJSON, &created.Env, map[string]string{}); err != nil {
		return revision.Revision{}, fmt.Errorf("decode revision env: %w", err)
	}
	if err := unmarshalJSON(projectedFilesJSON, &created.ProjectedFiles, []projectedfile.Spec{}); err != nil {
		return revision.Revision{}, fmt.Errorf("decode revision projected files: %w", err)
	}
	// clone 后返回，避免调用方复用时影响本函数组装出的中间切片。
	created.ProjectedFiles = projectedfile.CloneSpecs(created.ProjectedFiles)
	if err := unmarshalJSON(persistentDirsJSON, &created.PersistentDirs, []persistentdir.Spec{}); err != nil {
		return revision.Revision{}, fmt.Errorf("decode revision persistent dirs: %w", err)
	}
	// clone 后返回，避免调用方复用时影响本函数组装出的中间切片。
	created.PersistentDirs = persistentdir.CloneSpecs(created.PersistentDirs)
	// nullable resource 外键有值时写回领域对象。
	if configSetID.Valid {
		created.ConfigSetID = configSetID.String
	}
	if secretSetID.Valid {
		created.SecretSetID = secretSetID.String
	}
	if registryCredentialID.Valid {
		created.RegistryCredentialID = registryCredentialID.String
	}

	return created, nil
}

// ListRevisionsByService 按 revision number 倒序列出 service 的 revision。
// 参数说明：ctx 控制数据库请求生命周期；serviceID 是 service 唯一标识。
func (s *Store) ListRevisionsByService(ctx context.Context, serviceID string) ([]revision.Revision, error) {
	// 每条 revision 都是 service spec 的不可变快照。
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			service_id,
			revision_number,
			label,
			spec_image,
			spec_command_json,
			spec_args_json,
			spec_env_json,
			spec_config_set_id,
			spec_secret_set_id,
			spec_registry_credential_id,
			spec_projected_files_json,
			spec_persistent_dirs_json,
			port,
			spec_readiness_path,
			created_at
		FROM revisions
		WHERE service_id = $1
		ORDER BY revision_number DESC, id DESC
	`, serviceID)
	if err != nil {
		return nil, fmt.Errorf("query revisions: %w", err)
	}
	defer closeRows(rows)

	// 逐行扫描 revision。
	items := make([]revision.Revision, 0)
	for rows.Next() {
		item, err := scanRevision(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	// rows.Err 捕获迭代过程中延迟暴露的数据库错误。
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate revisions: %w", err)
	}

	return items, nil
}

// GetRevisionByService 在指定 service 下查询 revision。
// 参数说明：ctx 控制数据库请求生命周期；serviceID 是 service 唯一标识；revisionID 是 revision 唯一标识。
func (s *Store) GetRevisionByService(ctx context.Context, serviceID string, revisionID string) (revision.Revision, error) {
	// 同时匹配 revision id 和 service id，防止跨 service 读取。
	row := s.db.QueryRowContext(ctx, `
		SELECT
			id,
			service_id,
			revision_number,
			label,
			spec_image,
			spec_command_json,
			spec_args_json,
			spec_env_json,
			spec_config_set_id,
			spec_secret_set_id,
			spec_registry_credential_id,
			spec_projected_files_json,
			spec_persistent_dirs_json,
			port,
			spec_readiness_path,
			created_at
		FROM revisions
		WHERE id = $1 AND service_id = $2
	`, revisionID, serviceID)

	item, err := scanRevision(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 未找到或不属于该 service 都返回 revision not found。
			return revision.Revision{}, ErrRevisionNotFound
		}
		return revision.Revision{}, fmt.Errorf("query revision by service: %w", err)
	}
	return item, nil
}

// InsertDeployment 创建 deployment 并写入初始 transition。
// 参数说明：ctx 控制数据库请求生命周期；input 是 deployment 创建参数；reason 记录初始状态原因。
func (s *Store) InsertDeployment(ctx context.Context, input deployment.CreateInput, reason string) (deployment.Deployment, error) {
	// 校验 service/revision/replica 输入。
	if err := input.Validate(); err != nil {
		return deployment.Deployment{}, err
	}

	id, err := newID("dep")
	if err != nil {
		return deployment.Deployment{}, err
	}

	// deployment 创建和 transition 记录必须原子提交。
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return deployment.Deployment{}, fmt.Errorf("begin deployment tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var created deployment.Deployment
	// 新 deployment 初始为 pending，ready/available 副本数为 0。
	err = tx.QueryRowContext(ctx, `
			INSERT INTO deployments (
				id,
				service_id,
				revision_id,
				desired_replicas,
				ready_replicas,
				available_replicas,
				status,
				status_reason
			)
			VALUES ($1, $2, $3, $4, 0, 0, $5, $6)
			RETURNING id, service_id, revision_id, desired_replicas, ready_replicas, available_replicas, status, status_reason, created_at, updated_at
		`,
		id,
		input.ServiceID,
		input.RevisionID,
		input.DesiredReplicas,
		deployment.StatusPending,
		reason,
	).Scan(
		&created.ID,
		&created.ServiceID,
		&created.RevisionID,
		&created.DesiredReplicas,
		&created.ReadyReplicas,
		&created.AvailableReplicas,
		&created.Status,
		&created.StatusReason,
		&created.CreatedAt,
		&created.UpdatedAt,
	)
	if err != nil {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23503" {
			// service_id 或 revision_id 外键失败时统一映射为 service not found；调用方通常应保证 revision 来源。
			return deployment.Deployment{}, ErrServiceNotFound
		}
		return deployment.Deployment{}, fmt.Errorf("insert deployment: %w", err)
	}

	// 记录从无状态进入 pending 的 transition。
	if err := insertDeploymentTransition(ctx, tx, created.ID, nil, deployment.StatusPending, reason); err != nil {
		return deployment.Deployment{}, err
	}

	if err := tx.Commit(); err != nil {
		return deployment.Deployment{}, fmt.Errorf("commit deployment tx: %w", err)
	}

	return created, nil
}

// UpdateDeploymentStatus 在校验当前状态后推进 deployment 状态并记录 transition。
// 参数说明：ctx 控制数据库请求生命周期；deploymentID 是 deployment 唯一标识；toStatus 是目标 deployment 状态；reason 记录状态变化原因。
func (s *Store) UpdateDeploymentStatus(ctx context.Context, deploymentID string, toStatus string, reason string) (deployment.Deployment, error) {
	// 状态读取、校验、更新和 transition 记录必须原子提交。
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return deployment.Deployment{}, fmt.Errorf("begin deployment transition tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	// 锁定 deployment 当前状态，避免并发 transition 交错。
	var current deployment.Deployment
	err = tx.QueryRowContext(ctx, `
		SELECT id, service_id, revision_id, desired_replicas, ready_replicas, available_replicas, status, status_reason, created_at, updated_at
		FROM deployments
		WHERE id = $1
		FOR UPDATE
	`, deploymentID).Scan(
		&current.ID,
		&current.ServiceID,
		&current.RevisionID,
		&current.DesiredReplicas,
		&current.ReadyReplicas,
		&current.AvailableReplicas,
		&current.Status,
		&current.StatusReason,
		&current.CreatedAt,
		&current.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// deployment 不存在时返回 not found。
			return deployment.Deployment{}, ErrDeploymentNotFound
		}
		return deployment.Deployment{}, fmt.Errorf("load deployment for transition: %w", err)
	}

	// 先通过领域状态机校验迁移是否合法。
	if err := deployment.ValidateTransition(current.Status, toStatus, reason); err != nil {
		return deployment.Deployment{}, err
	}

	// 阶段二：更新 deployment 状态并写入 transition 记录。
	// 更新当前状态和原因。
	var updated deployment.Deployment
	err = tx.QueryRowContext(ctx, `
		UPDATE deployments
		SET
			status = $2,
			status_reason = $3,
			updated_at = now()
		WHERE id = $1
		RETURNING id, service_id, revision_id, desired_replicas, ready_replicas, available_replicas, status, status_reason, created_at, updated_at
	`, deploymentID, toStatus, reason).Scan(
		&updated.ID,
		&updated.ServiceID,
		&updated.RevisionID,
		&updated.DesiredReplicas,
		&updated.ReadyReplicas,
		&updated.AvailableReplicas,
		&updated.Status,
		&updated.StatusReason,
		&updated.CreatedAt,
		&updated.UpdatedAt,
	)
	if err != nil {
		return deployment.Deployment{}, fmt.Errorf("update deployment status: %w", err)
	}

	// 记录 transition 历史。
	from := current.Status
	if err := insertDeploymentTransition(ctx, tx, deploymentID, &from, toStatus, reason); err != nil {
		return deployment.Deployment{}, err
	}

	if err := tx.Commit(); err != nil {
		return deployment.Deployment{}, fmt.Errorf("commit deployment transition tx: %w", err)
	}

	return updated, nil
}

// AddDeploymentPlacements 在同一事务内更新扩容副本数并追加新增副本的 selection decisions。
// 参数说明：ctx 控制数据库请求生命周期；deploymentID 是 deployment 唯一标识；desiredReplicas 是目标副本数；reason 记录状态变化原因；request 是调度请求；decisions 是新增副本的调度决策。
func (s *Store) AddDeploymentPlacements(
	ctx context.Context,
	deploymentID string,
	desiredReplicas int,
	reason string,
	request scheduler.PlacementRequest,
	decisions []scheduler.PlacementDecision,
) (deployment.Deployment, []scheduler.StoredDecision, error) {
	// 扩容后的期望副本数必须为正数。
	if desiredReplicas <= 0 {
		return deployment.Deployment{}, nil, deployment.ErrDesiredReplicasInvalid
	}
	if len(decisions) == 0 {
		return deployment.Deployment{}, nil, fmt.Errorf("scale-up requires at least one selection decision")
	}

	// 更新 deployment 和追加 selection decisions 必须原子提交。
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return deployment.Deployment{}, nil, fmt.Errorf("begin deployment scale-up tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	// 更新 deployment 期望副本数。
	var updated deployment.Deployment
	if err := tx.QueryRowContext(ctx, `
		UPDATE deployments
		SET
			desired_replicas = $2,
			status_reason = $3,
			updated_at = now()
		WHERE id = $1
		RETURNING id, service_id, revision_id, desired_replicas, ready_replicas, available_replicas, status, status_reason, created_at, updated_at
	`, deploymentID, desiredReplicas, reason).Scan(
		&updated.ID,
		&updated.ServiceID,
		&updated.RevisionID,
		&updated.DesiredReplicas,
		&updated.ReadyReplicas,
		&updated.AvailableReplicas,
		&updated.Status,
		&updated.StatusReason,
		&updated.CreatedAt,
		&updated.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// deployment 不存在时返回 not found。
			return deployment.Deployment{}, nil, ErrDeploymentNotFound
		}
		return deployment.Deployment{}, nil, fmt.Errorf("update deployment desired replicas for scale-up: %w", err)
	}

	// 只插入调用方传入的新增副本 selection decisions。
	items := make([]scheduler.StoredDecision, 0, len(decisions))
	for _, decision := range decisions {
		stored, err := s.insertPlacementDecisionTx(ctx, tx, request, decision)
		if err != nil {
			return deployment.Deployment{}, nil, err
		}
		items = append(items, stored)
	}

	if err := tx.Commit(); err != nil {
		return deployment.Deployment{}, nil, fmt.Errorf("commit deployment scale-up tx: %w", err)
	}
	return updated, items, nil
}

// GetCurrentDeploymentByService 查询 service 最新创建的 deployment。
// 参数说明：ctx 控制数据库请求生命周期；serviceID 是 service 唯一标识。
func (s *Store) GetCurrentDeploymentByService(ctx context.Context, serviceID string) (*deployment.Deployment, error) {
	// 当前语义取最新 deployment，不要求其 revision 等于 service.status_current_revision_id。
	row := s.db.QueryRowContext(ctx, `
		SELECT id, service_id, revision_id, desired_replicas, ready_replicas, available_replicas, status, status_reason, created_at, updated_at
		FROM deployments
		WHERE service_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, serviceID)

	var item deployment.Deployment
	err := row.Scan(
		&item.ID,
		&item.ServiceID,
		&item.RevisionID,
		&item.DesiredReplicas,
		&item.ReadyReplicas,
		&item.AvailableReplicas,
		&item.Status,
		&item.StatusReason,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 没有 deployment 时返回 nil。
			return nil, nil
		}
		return nil, fmt.Errorf("query current deployment: %w", err)
	}

	return &item, nil
}

// GetPromotedDeploymentByService 查询 service current revision 对应的 deployment。
// 参数说明：ctx 控制数据库请求生命周期；serviceID 是 service 唯一标识。
func (s *Store) GetPromotedDeploymentByService(ctx context.Context, serviceID string) (*deployment.Deployment, error) {
	// 优先返回 running，其次 deploying，最后返回其他 current revision deployment。
	row := s.db.QueryRowContext(ctx, `
			SELECT
				d.id,
				d.service_id,
				d.revision_id,
			d.desired_replicas,
			d.ready_replicas,
			d.available_replicas,
			d.status,
			d.status_reason,
			d.created_at,
			d.updated_at
			FROM services a
			JOIN deployments d
				ON d.service_id = a.id
				AND d.revision_id = a.status_current_revision_id
		WHERE a.id = $1
		ORDER BY
			CASE
				WHEN d.status = 'running' THEN 0
				WHEN d.status = 'deploying' THEN 1
				ELSE 2
			END,
			d.created_at DESC,
			d.id DESC
		LIMIT 1
	`, serviceID)

	var item deployment.Deployment
	err := row.Scan(
		&item.ID,
		&item.ServiceID,
		&item.RevisionID,
		&item.DesiredReplicas,
		&item.ReadyReplicas,
		&item.AvailableReplicas,
		&item.Status,
		&item.StatusReason,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 没有 current revision deployment 时返回 nil。
			return nil, nil
		}
		return nil, fmt.Errorf("query promoted deployment: %w", err)
	}

	return &item, nil
}

// scanService 从 SQL 扫描器读取一行数据并组装领域对象。
// 参数说明：scanner 是当前数据库查询结果行扫描器。
func scanService(scanner interface{ Scan(dest ...any) error }) (workload.Service, error) {
	// 复杂流程说明：service 行包含基础 spec、运行输入、rollout 字段和 JSON 扩展字段。
	// JSON 字段解析失败必须返回错误，避免后续用空配置覆盖真实配置。
	var item workload.Service
	var commandJSON []byte
	var argsJSON []byte
	var envJSON []byte
	var projectedFilesJSON []byte
	var persistentDirsJSON []byte
	var currentRevisionID sql.NullString
	var candidateRevisionID sql.NullString
	var configSetID sql.NullString
	var secretSetID sql.NullString
	var registryCredentialID sql.NullString

	// 扫描顺序必须和 service SELECT/RETURNING 字段顺序一致。
	err := scanner.Scan(
		&item.Metadata.ID,
		&item.Metadata.Name,
		&item.Metadata.DisplayName,
		&item.Spec.Region,
		&item.Spec.Replicas,
		&item.Spec.InstanceClass,
		&item.Spec.Exposure,
		&item.Spec.Image,
		&commandJSON,
		&argsJSON,
		&item.Spec.DefaultPort,
		&item.Spec.ReadinessPath,
		&configSetID,
		&secretSetID,
		&registryCredentialID,
		&projectedFilesJSON,
		&persistentDirsJSON,
		&envJSON,
		&currentRevisionID,
		&candidateRevisionID,
		&item.Status.RolloutPhase,
		&item.Status.RolloutMessage,
		&item.Status.Phase,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return workload.Service{}, err
	}

	// 将数据库 JSONB 字段恢复为领域对象字段。
	if err := unmarshalJSON(commandJSON, &item.Spec.Command, []string{}); err != nil {
		return workload.Service{}, fmt.Errorf("decode service command: %w", err)
	}
	if err := unmarshalJSON(argsJSON, &item.Spec.Args, []string{}); err != nil {
		return workload.Service{}, fmt.Errorf("decode service args: %w", err)
	}
	if err := unmarshalJSON(envJSON, &item.Spec.Env, map[string]string{}); err != nil {
		return workload.Service{}, fmt.Errorf("decode service env: %w", err)
	}
	if err := unmarshalJSON(projectedFilesJSON, &item.Spec.ProjectedFiles, []projectedfile.Spec{}); err != nil {
		return workload.Service{}, fmt.Errorf("decode service projected files: %w", err)
	}
	// clone 后返回，避免调用方复用时影响本函数组装出的中间切片。
	item.Spec.ProjectedFiles = projectedfile.CloneSpecs(item.Spec.ProjectedFiles)
	if err := unmarshalJSON(persistentDirsJSON, &item.Spec.PersistentDirs, []persistentdir.Spec{}); err != nil {
		return workload.Service{}, fmt.Errorf("decode service persistent dirs: %w", err)
	}
	// clone 后返回，避免调用方复用时影响本函数组装出的中间切片。
	item.Spec.PersistentDirs = persistentdir.CloneSpecs(item.Spec.PersistentDirs)
	// nullable revision/resource 外键有值时写回领域对象。
	if currentRevisionID.Valid {
		item.Status.CurrentRevisionID = currentRevisionID.String
	}
	if candidateRevisionID.Valid {
		item.Status.CandidateRevisionID = candidateRevisionID.String
	}
	if configSetID.Valid {
		item.Spec.ConfigSetID = configSetID.String
	}
	if secretSetID.Valid {
		item.Spec.SecretSetID = secretSetID.String
	}
	if registryCredentialID.Valid {
		item.Spec.RegistryCredentialID = registryCredentialID.String
	}

	return item, nil
}

// scanRevision 从 SQL 扫描器读取一行数据并组装领域对象。
// 参数说明：scanner 是当前数据库查询结果行扫描器。
func scanRevision(scanner interface{ Scan(dest ...any) error }) (revision.Revision, error) {
	// revision JSON 和 nullable 外键用中间变量扫描。
	var item revision.Revision
	var commandJSON []byte
	var argsJSON []byte
	var envJSON []byte
	var projectedFilesJSON []byte
	var persistentDirsJSON []byte
	var configSetID sql.NullString
	var secretSetID sql.NullString
	var registryCredentialID sql.NullString

	// 扫描顺序必须和 revision SELECT/RETURNING 字段顺序一致。
	err := scanner.Scan(
		&item.ID,
		&item.ServiceID,
		&item.Number,
		&item.Label,
		&item.Image,
		&commandJSON,
		&argsJSON,
		&envJSON,
		&configSetID,
		&secretSetID,
		&registryCredentialID,
		&projectedFilesJSON,
		&persistentDirsJSON,
		&item.Port,
		&item.ReadinessPath,
		&item.CreatedAt,
	)
	if err != nil {
		return revision.Revision{}, err
	}

	// 将数据库 JSONB 字段恢复为领域对象字段。
	if err := unmarshalJSON(commandJSON, &item.Command, []string{}); err != nil {
		return revision.Revision{}, fmt.Errorf("decode revision command: %w", err)
	}
	if err := unmarshalJSON(argsJSON, &item.Args, []string{}); err != nil {
		return revision.Revision{}, fmt.Errorf("decode revision args: %w", err)
	}
	if err := unmarshalJSON(envJSON, &item.Env, map[string]string{}); err != nil {
		return revision.Revision{}, fmt.Errorf("decode revision env: %w", err)
	}
	if err := unmarshalJSON(projectedFilesJSON, &item.ProjectedFiles, []projectedfile.Spec{}); err != nil {
		return revision.Revision{}, fmt.Errorf("decode revision projected files: %w", err)
	}
	// clone 后返回，避免调用方复用时影响本函数组装出的中间切片。
	item.ProjectedFiles = projectedfile.CloneSpecs(item.ProjectedFiles)
	if err := unmarshalJSON(persistentDirsJSON, &item.PersistentDirs, []persistentdir.Spec{}); err != nil {
		return revision.Revision{}, fmt.Errorf("decode revision persistent dirs: %w", err)
	}
	// clone 后返回，避免调用方复用时影响本函数组装出的中间切片。
	item.PersistentDirs = persistentdir.CloneSpecs(item.PersistentDirs)
	// nullable resource 外键有值时写回领域对象。
	if configSetID.Valid {
		item.ConfigSetID = configSetID.String
	}
	if secretSetID.Valid {
		item.SecretSetID = secretSetID.String
	}
	if registryCredentialID.Valid {
		item.RegistryCredentialID = registryCredentialID.String
	}

	return item, nil
}

// ensureServiceReferencesResolved 校验 service 引用的全局资源都存在。
func (s *Store) ensureServiceReferencesResolved(ctx context.Context, configSetID string, secretSetID string, registryCredentialID string) error {
	// 配置集引用为空时跳过。
	if configSetID != "" {
		if _, err := s.resolveConfigSet(ctx, configSetID); err != nil {
			return err
		}
	}
	// 密钥集引用为空时跳过。
	if secretSetID != "" {
		if _, err := s.resolveSecretSet(ctx, secretSetID); err != nil {
			return err
		}
	}
	// 仓库凭据引用为空时跳过。
	if registryCredentialID != "" {
		if _, err := s.resolveRegistryCredential(ctx, registryCredentialID); err != nil {
			return err
		}
	}
	return nil
}

// ensureServiceProjectedFilesResolved 校验 projected file 来源资源和 key 都可解析。
func (s *Store) ensureServiceProjectedFilesResolved(ctx context.Context, projectedFiles []projectedfile.Spec) error {
	// clone 后遍历，避免校验过程影响调用方切片。
	for _, item := range projectedfile.CloneSpecs(projectedFiles) {
		switch item.SourceKind {
		case projectedfile.SourceKindConfigSet:
			// config set 来源必须存在且包含指定 key。
			configSet, err := s.resolveConfigSet(ctx, item.SourceID)
			if err != nil {
				return err
			}
			if _, ok := configSet.Values[item.SourceKey]; !ok {
				return fmt.Errorf("config set %s does not contain key %s", item.SourceID, item.SourceKey)
			}
		case projectedfile.SourceKindSecretSet:
			// secret set 来源必须存在且包含指定 key。
			secretSet, err := s.resolveSecretSet(ctx, item.SourceID)
			if err != nil {
				return err
			}
			if _, ok := secretSet.Values[item.SourceKey]; !ok {
				return fmt.Errorf("secret set %s does not contain key %s", item.SourceID, item.SourceKey)
			}
		default:
			// 未知来源类型直接拒绝。
			return projectedfile.ErrSourceKindInvalid
		}
	}
	return nil
}

// serviceNeedsNewRevision 判断 service 规格变更是否需要创建新 revision。
// 参数说明：before 是变更前状态；after 是变更后状态。
func serviceNeedsNewRevision(before workload.Service, after workload.Service) bool {
	// 只有会改变运行时 revision 快照的字段才触发新 revision。
	return before.Spec.Region != after.Spec.Region ||
		// 调度地域或实例档位变化会改变运行环境，需要新 revision。
		before.Spec.InstanceClass != after.Spec.InstanceClass ||
		// 镜像、启动命令、参数和环境变量变化会改变容器运行内容。
		before.Spec.Image != after.Spec.Image ||
		!reflect.DeepEqual(before.Spec.Command, after.Spec.Command) ||
		!reflect.DeepEqual(before.Spec.Args, after.Spec.Args) ||
		!reflect.DeepEqual(before.Spec.Env, after.Spec.Env) ||
		// 端口、探针和引用资源会影响 node-agent 下发的 work item。
		before.Spec.DefaultPort != after.Spec.DefaultPort ||
		before.Spec.ReadinessPath != after.Spec.ReadinessPath ||
		before.Spec.ConfigSetID != after.Spec.ConfigSetID ||
		before.Spec.SecretSetID != after.Spec.SecretSetID ||
		before.Spec.RegistryCredentialID != after.Spec.RegistryCredentialID ||
		// projected files 和 persistent dirs 先规范化再比较，避免顺序差异造成误判。
		!reflect.DeepEqual(projectedfile.CloneSpecs(before.Spec.ProjectedFiles), projectedfile.CloneSpecs(after.Spec.ProjectedFiles)) ||
		!reflect.DeepEqual(persistentdir.CloneSpecs(before.Spec.PersistentDirs), persistentdir.CloneSpecs(after.Spec.PersistentDirs))
}

// persistentDirRevisionChangeBlocked 判断 persistent dir 变更是否会触发当前不支持的 revision rollout。
// 参数说明：current 是当前 service 记录；spec 是本次更新的目标运行规格。
func persistentDirRevisionChangeBlocked(current workload.Service, spec workload.Spec) bool {
	// 当前和目标都没有 persistent dir 时，不存在受限变更。
	if len(current.Spec.PersistentDirs) == 0 && len(spec.PersistentDirs) == 0 {
		return false
	}
	// 尚未发布过 revision 时，允许首次设置 persistent dir。
	if current.Status.CurrentRevisionID == "" && current.Status.CandidateRevisionID == "" {
		return false
	}
	// 构造拟更新后的 service，用统一 revision diff 逻辑判断是否会触发 rollout。
	proposed := current
	proposed.Spec.Region = spec.Region
	proposed.Spec.Replicas = spec.Replicas
	proposed.Spec.InstanceClass = spec.InstanceClass
	proposed.Spec.Exposure = spec.NormalizedExposure()
	proposed.Spec.Image = spec.Image
	proposed.Spec.Command = append([]string(nil), spec.Command...)
	proposed.Spec.Args = append([]string(nil), spec.Args...)
	proposed.Spec.DefaultPort = spec.DefaultPort
	proposed.Spec.ReadinessPath = spec.ReadinessPath
	proposed.Spec.Env = mergeStringMaps(spec.Env)
	proposed.Spec.ConfigSetID = spec.ConfigSetID
	proposed.Spec.SecretSetID = spec.SecretSetID
	proposed.Spec.RegistryCredentialID = spec.RegistryCredentialID
	proposed.Spec.ProjectedFiles = projectedfile.CloneSpecs(spec.ProjectedFiles)
	proposed.Spec.PersistentDirs = persistentdir.CloneSpecs(spec.PersistentDirs)
	return serviceNeedsNewRevision(current, proposed)
}

// insertDeploymentTransition 插入 deployment transition 记录。
// 参数说明：ctx 控制数据库请求生命周期；tx 表示数据库事务；deploymentID 是 deployment 唯一标识；fromStatus 是原状态；toStatus 是目标状态；reason 记录状态变化原因。
func insertDeploymentTransition(ctx context.Context, tx *sql.Tx, deploymentID string, fromStatus *string, toStatus string, reason string) error {
	// 生成 transition ID。
	id, err := newID("dtr")
	if err != nil {
		return err
	}

	// 初始 transition 没有 from_status，使用 SQL NULL。
	var rawFrom sql.NullString
	if fromStatus != nil {
		rawFrom = sql.NullString{
			String: string(*fromStatus),
			Valid:  true,
		}
	}

	// 写入 transition 记录。
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO deployment_transitions (
			id,
			deployment_id,
			from_status,
			to_status,
			reason
		)
		VALUES ($1, $2, $3, $4, $5)
	`, id, deploymentID, rawFrom, toStatus, reason); err != nil {
		return fmt.Errorf("insert deployment transition: %w", err)
	}

	// 进入 running/failed 终态时记录 rollout outcome 计数。
	switch toStatus {
	case deployment.StatusRunning:
		if err := recordDeploymentRolloutOutcomeTx(ctx, tx, deploymentID, "success"); err != nil {
			return err
		}
	case deployment.StatusFailed:
		if err := recordDeploymentRolloutOutcomeTx(ctx, tx, deploymentID, "failed"); err != nil {
			return err
		}
	}

	return nil
}

// marshalJSON 将结构化字段序列化为 JSON，空值时使用 fallback。
// 参数说明：value 是待序列化的结构化值；fallback 表示 value 为空时写入的默认值。
func marshalJSON(value any, fallback any) ([]byte, error) {
	// nil map/slice/interface 按 fallback 序列化，避免数据库中出现 JSON null。
	if isNilJSONValue(value) {
		return json.Marshal(fallback)
	}
	return json.Marshal(value)
}

// unmarshalJSON 将数据库中的 JSON 字节反序列化到目标对象，空值时写入 fallback。
// 参数说明：raw 是数据库中读取的 JSON 字节；target 是反序列化写入目标；fallback 是 raw 为空时使用的默认值。
func unmarshalJSON[T any](raw []byte, target *T, fallback T) error {
	// 空 bytes 按 fallback 写入目标。
	if len(raw) == 0 {
		*target = fallback
		return nil
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return err
	}
	// JSON null 反序列化后仍按 fallback 处理。
	if isNilJSONValue(*target) {
		*target = fallback
	}
	return nil
}

// isNilJSONValue 判断值是否为 nil 或 nil-able 类型的 nil。
// 参数说明：value 是待检查的任意值。
func isNilJSONValue(value any) bool {
	// interface 本身为 nil 时直接返回 true。
	if value == nil {
		return true
	}

	// 只有 nil-able kind 才能调用 IsNil。
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

// nullableString 把可选字符串转换为 SQL nullable string。
// 参数说明：value 是待写入数据库的可选字符串。
func nullableString(value string) sql.NullString {
	// 空字符串按 SQL NULL 处理。
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{
		String: value,
		Valid:  true,
	}
}
