package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"mini-cloud/internal/cloudplane/domain/desired"
	"mini-cloud/internal/cloudplane/domain/workload"
	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/projectedfile"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrServiceDesiredNotFound = errors.New("service desired not found")
)

// UpsertServiceDesired 按 serviceID 创建或更新已接受的 service desired state。
// 参数说明：ctx 控制数据库请求生命周期；input 是已经过 API 层解析的 desired 输入。
func (s *Store) UpsertServiceDesired(ctx context.Context, input desired.AcceptInput) (desired.AcceptResult, error) {
	// serviceID 是 service_desired 的唯一身份，必须由 control-plane 明确传入。
	serviceID := strings.TrimSpace(input.ServiceID)
	if serviceID == "" {
		return desired.AcceptResult{}, desired.ErrServiceIDRequired
	}
	// 复制目标运行规格，避免归一化过程修改调用方持有的对象。
	spec := input.Spec
	// exposure 在入库前归一化，保证后续幂等 hash 使用稳定值。
	spec.Exposure = spec.NormalizedExposure()
	if err := spec.Validate(); err != nil {
		return desired.AcceptResult{}, err
	}
	name := strings.TrimSpace(input.Name)
	if err := workload.ValidateIdentity(name); err != nil {
		return desired.AcceptResult{}, err
	}
	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" {
		return desired.AcceptResult{}, workload.ErrServiceDisplayNameRequired
	}
	// 先检查引用的全局 config/secret/registry credential 是否存在。
	if err := s.ensureServiceReferencesResolved(ctx, spec.ConfigSetID, spec.SecretSetID, spec.RegistryCredentialID); err != nil {
		return desired.AcceptResult{}, err
	}
	// projected file 引用的 config/secret key 也必须能解析。
	if err := s.ensureServiceProjectedFilesResolved(ctx, spec.ProjectedFiles); err != nil {
		return desired.AcceptResult{}, err
	}

	// spec hash 用于判断本次 desired 与库中 accepted desired 是否等价。
	specHash, err := desired.HashSpec(spec)
	if err != nil {
		return desired.AcceptResult{}, err
	}
	// 以下字段以 JSON 存储，空值统一写成稳定的空数组或空对象。
	commandJSON, err := marshalJSON(spec.Command, []string{})
	if err != nil {
		return desired.AcceptResult{}, fmt.Errorf("marshal desired command: %w", err)
	}
	argsJSON, err := marshalJSON(spec.Args, []string{})
	if err != nil {
		return desired.AcceptResult{}, fmt.Errorf("marshal desired args: %w", err)
	}
	envJSON, err := marshalJSON(spec.Env, map[string]string{})
	if err != nil {
		return desired.AcceptResult{}, fmt.Errorf("marshal desired env: %w", err)
	}
	projectedFilesJSON, err := marshalJSON(projectedfile.CloneSpecs(spec.ProjectedFiles), []projectedfile.Spec{})
	if err != nil {
		return desired.AcceptResult{}, fmt.Errorf("marshal desired projected files: %w", err)
	}
	persistentDirsJSON, err := marshalJSON(persistentdir.CloneSpecs(spec.PersistentDirs), []persistentdir.Spec{})
	if err != nil {
		return desired.AcceptResult{}, fmt.Errorf("marshal desired persistent dirs: %w", err)
	}

	// accepted desired 的查找和创建/更新必须在同一事务内完成。
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return desired.AcceptResult{}, fmt.Errorf("begin accept service desired tx: %w", err)
	}
	// Commit 成功后 Rollback 会返回错误，这里忽略即可；失败路径则释放事务。
	defer func() { _ = tx.Rollback() }()

		// 按 serviceID 加锁读取现有 desired；serviceID 是唯一身份，name 是全局唯一的可读名称。
	current, err := s.getServiceDesiredByServiceIDTx(ctx, tx, serviceID, true)
	if err != nil && !errors.Is(err, ErrServiceDesiredNotFound) {
		return desired.AcceptResult{}, err
	}

	// 不存在现有 desired 时插入第一代记录。
	if errors.Is(err, ErrServiceDesiredNotFound) {
		// generation 从 1 开始，observed_generation 初始为 0，等待 reconciler 推进。
		row := tx.QueryRowContext(ctx, serviceDesiredReturningSQL(`
            INSERT INTO service_desired (
                service_id,
                name,
                generation,
                observed_generation,
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
                spec_env_json,
                spec_config_set_id,
                spec_secret_set_id,
                spec_registry_credential_id,
                spec_projected_files_json,
                spec_persistent_dirs_json,
                spec_hash,
                reconcile_phase,
                reconcile_message
			)
            VALUES ($1, $2, 1, 0, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, '')
        `),
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
			envJSON,
			nullableString(spec.ConfigSetID),
			nullableString(spec.SecretSetID),
			nullableString(spec.RegistryCredentialID),
			projectedFilesJSON,
			persistentDirsJSON,
			specHash,
			desired.PhaseAccepted,
		)
		// INSERT RETURNING 直接扫描出数据库最终值，包含时间戳等默认字段。
		created, err := scanServiceDesired(row)
		if err != nil {
			if isServiceDesiredNameConflict(err) {
				return desired.AcceptResult{}, ErrServiceNameAlreadyExists
			}
			return desired.AcceptResult{}, fmt.Errorf("insert service desired: %w", err)
		}
		// 提交事务后返回 created 动作。
		if err := tx.Commit(); err != nil {
			return desired.AcceptResult{}, fmt.Errorf("commit accept service desired tx: %w", err)
		}
		return desired.AcceptResult{Desired: created, Action: desired.ActionCreated}, nil
	}

	// serviceID 是 desired 的唯一身份；name 创建后不可变，避免后续 apply 静默改名同一个 serviceID。
	if current.Name != name {
		return desired.AcceptResult{}, workload.ErrServiceIdentityConflict
	}

	// spec 未变化时，只提交事务并返回 unchanged。
	if current.SpecHash == specHash {
		if err := tx.Commit(); err != nil {
			return desired.AcceptResult{}, fmt.Errorf("commit unchanged service desired tx: %w", err)
		}
		return desired.AcceptResult{Desired: current, Action: desired.ActionUnchanged}, nil
	}

	// spec 发生变化时推进 generation，供 reconciler 识别新 desired。
	nextGeneration := current.Generation + 1
	// 更新 desired 主体字段，并重置 reconcile phase/message。
	row := tx.QueryRowContext(ctx, serviceDesiredReturningSQL(`
        UPDATE service_desired
        SET
            generation = $2,
            display_name = $3,
            spec_region = $4,
            spec_replicas = $5,
            spec_instance_class = $6,
            spec_exposure = $7,
            spec_image = $8,
            spec_command_json = $9,
            spec_args_json = $10,
            spec_default_port = $11,
            spec_readiness_path = $12,
            spec_env_json = $13,
            spec_config_set_id = $14,
            spec_secret_set_id = $15,
            spec_registry_credential_id = $16,
            spec_projected_files_json = $17,
            spec_persistent_dirs_json = $18,
            spec_hash = $19,
            reconcile_phase = $20,
            reconcile_message = '',
            accepted_at = now(),
            updated_at = now()
        WHERE service_id = $1
    `),
		serviceID,
		nextGeneration,
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
		envJSON,
		nullableString(spec.ConfigSetID),
		nullableString(spec.SecretSetID),
		nullableString(spec.RegistryCredentialID),
		projectedFilesJSON,
		persistentDirsJSON,
		specHash,
		desired.PhaseAccepted,
	)
	// UPDATE RETURNING 直接扫描更新后的完整 desired。
	updated, err := scanServiceDesired(row)
	if err != nil {
		if isServiceDesiredNameConflict(err) {
			return desired.AcceptResult{}, ErrServiceNameAlreadyExists
		}
		return desired.AcceptResult{}, fmt.Errorf("update service desired: %w", err)
	}
	// 提交事务后返回 updated 动作。
	if err := tx.Commit(); err != nil {
		return desired.AcceptResult{}, fmt.Errorf("commit accept service desired tx: %w", err)
	}
	return desired.AcceptResult{Desired: updated, Action: desired.ActionUpdated}, nil
}

// ListServiceDesiredForReconcile 列出 reconciler 可继续推进的 service desired state。
// 参数说明：ctx 控制数据库请求生命周期；limit 限制返回数量。
func (s *Store) ListServiceDesiredForReconcile(ctx context.Context, limit int) ([]desired.Service, error) {
	// 非正 limit 表示调用方没有明确批量大小，直接返回空集合。
	if limit <= 0 {
		return nil, nil
	}
	// 可收敛 phase 是 reconciler 的固定策略：
	// accepted 表示新 desired，reconciling 表示上次进程可能中断，retrying 表示上轮失败但应自动重试。
	rows, err := s.db.QueryContext(ctx, serviceDesiredSelectSQL()+`
        WHERE observed_generation < generation
          AND reconcile_phase = ANY($1::text[])
        ORDER BY updated_at ASC, service_id ASC
        LIMIT $2
    `, []string{
		desired.PhaseAccepted,
		desired.PhaseReconciling,
		desired.PhaseRetrying,
	}, limit)
	if err != nil {
		return nil, fmt.Errorf("list service desired for reconcile: %w", err)
	}
	defer closeRows(rows)

	// 逐行转换为领域对象。
	var items []desired.Service
	for rows.Next() {
		item, err := scanServiceDesired(rows)
		if err != nil {
			return nil, fmt.Errorf("scan service desired for reconcile: %w", err)
		}
		items = append(items, item)
	}
	// rows.Err 捕获迭代过程中延迟暴露的数据库错误。
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate service desired for reconcile: %w", err)
	}
	return items, nil
}

// SetServiceDesiredObserved 标记 service desired generation 已经被实际状态观察到。
func (s *Store) SetServiceDesiredObserved(ctx context.Context, serviceID string, generation int64, message string) error {
	// 只有 generation 匹配时才标记 observed，避免旧 reconciler 覆盖新 desired 状态。
	result, err := s.db.ExecContext(ctx, `
        UPDATE service_desired
        SET
            observed_generation = $2,
            reconcile_phase = $3,
            reconcile_message = $4,
            observed_at = now(),
            updated_at = now()
        WHERE service_id = $1
          AND generation = $2
    `, serviceID, generation, desired.PhaseObserved, message)
	if err != nil {
		return fmt.Errorf("mark service desired observed: %w", err)
	}
	// affected 为 0 通常表示该 desired 已被新 generation 覆盖；旧 reconciler 不应再写回 observed 状态。
	return ignoreStaleServiceDesiredWrite(result, "mark service desired observed")
}

// UpdateServiceDesiredPhase 更新指定 desired generation 的 reconcile phase。
// 参数说明：ctx 控制数据库请求生命周期；serviceID 和 generation 定位目标记录；phase/message 是要写入的状态。
func (s *Store) UpdateServiceDesiredPhase(ctx context.Context, serviceID string, generation int64, phase string, message string) error {
	// 只更新 generation 匹配的记录，避免旧 reconciler 覆盖新 desired 状态。
	result, err := s.db.ExecContext(ctx, `
        UPDATE service_desired
        SET reconcile_phase = $3,
            reconcile_message = $4,
            updated_at = now()
        WHERE service_id = $1
          AND generation = $2
    `, serviceID, generation, phase, message)
	if err != nil {
		return fmt.Errorf("update service desired phase: %w", err)
	}
	// affected 为 0 通常表示该 desired 已被新 generation 覆盖；旧 reconciler 不应覆盖新 phase/message。
	return ignoreStaleServiceDesiredWrite(result, "update service desired phase")
}

// ignoreStaleServiceDesiredWrite 显式检查 generation CAS 更新结果，并把旧 generation 写回视为成功跳过。
// 参数说明：result 是 ExecContext 返回的更新结果；operation 是错误上下文中的操作名称。
func ignoreStaleServiceDesiredWrite(result sql.Result, operation string) error {
	// RowsAffected 表示 UPDATE 实际改动的行数；当前 SQL 用 service_id + generation 作为 CAS 条件。
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s rows affected: %w", operation, err)
	}
	// affected == 0 表示记录不存在或 generation 已变；对旧 reconciler 来说这两种情况都不应再写回状态。
	if affected == 0 {
		return nil
	}
	// service_desired.service_id 是主键，正常最多只能更新一行；超过一行说明 SQL 条件或 schema 已经异常。
	if affected > 1 {
		return fmt.Errorf("%s affected %d service desired rows", operation, affected)
	}
	return nil
}

// isServiceDesiredNameConflict 判断写入 service_desired 是否撞上全局 service name 唯一约束。
// 参数说明：err 是 INSERT/UPDATE RETURNING 扫描时暴露的数据库错误。
func isServiceDesiredNameConflict(err error) bool {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok || pgErr.Code != "23505" {
		return false
	}
	return strings.Contains(pgErr.ConstraintName, "name")
}

// getServiceDesiredByServiceIDTx 按 serviceID 读取 desired，可选择在事务内加锁。
// 参数说明：ctx 控制数据库请求生命周期；tx 为 nil 时使用 Store 连接池；serviceID 是 desired 主键；forUpdate 表示是否附加 FOR UPDATE。
func (s *Store) getServiceDesiredByServiceIDTx(ctx context.Context, tx *sql.Tx, serviceID string, forUpdate bool) (desired.Service, error) {
	// 基础查询固定按 service_id 定位一条 desired。
	query := serviceDesiredSelectSQL() + ` WHERE service_id = $1`
	// accept 流程需要 FOR UPDATE 防止并发更新同一 desired。
	if forUpdate {
		query += ` FOR UPDATE`
	}
	// QueryRowContext 在事务和非事务路径上分别选择执行入口。
	var row interface{ Scan(dest ...any) error }
	if tx != nil {
		row = tx.QueryRowContext(ctx, query, serviceID)
	} else {
		row = s.db.QueryRowContext(ctx, query, serviceID)
	}
	// 统一使用 scanServiceDesired 做行到领域对象的转换。
	item, err := scanServiceDesired(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return desired.Service{}, ErrServiceDesiredNotFound
		}
		return desired.Service{}, fmt.Errorf("get service desired: %w", err)
	}
	return item, nil
}

// serviceDesiredSelectSQL 返回读取 service_desired 完整领域字段的 SELECT 片段。
func serviceDesiredSelectSQL() string {
	// 返回片段以 SELECT 开头，调用方会继续拼接 WHERE 或 FOR UPDATE。
	// 字段顺序必须和 scanServiceDesired 保持一致；前半段是 desired 身份、版本和 spec 字段。
	// 后半段是 reconcile phase、观测时间和审计时间字段。
	return `
        SELECT
            service_id,
            name,
            generation,
            observed_generation,
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
            spec_env_json,
            spec_config_set_id,
            spec_secret_set_id,
            spec_registry_credential_id,
            spec_projected_files_json,
            spec_persistent_dirs_json,
            spec_hash,
            reconcile_phase,
            reconcile_message,
            accepted_at,
            observed_at,
            created_at,
            updated_at
        FROM service_desired
    `
}

// serviceDesiredReturningSQL 为 INSERT/UPDATE SQL 拼接 service_desired 的 RETURNING 字段。
// 参数说明：prefix 是已经包含 INSERT 或 UPDATE 主体的 SQL 片段。
func serviceDesiredReturningSQL(prefix string) string {
	// prefix 通常是 INSERT ... 或 UPDATE ...，这里统一追加完整 RETURNING 字段。
	// RETURNING 字段顺序复用 scanServiceDesired，避免写入路径和读取路径维护两套扫描顺序。
	// 该 helper 不做 SQL 参数拼接，只追加固定字段列表。
	return prefix + `
        RETURNING
            service_id,
            name,
            generation,
            observed_generation,
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
            spec_env_json,
            spec_config_set_id,
            spec_secret_set_id,
            spec_registry_credential_id,
            spec_projected_files_json,
            spec_persistent_dirs_json,
            spec_hash,
            reconcile_phase,
            reconcile_message,
            accepted_at,
            observed_at,
            created_at,
            updated_at
    `
}

// scanServiceDesired 将数据库行转换为 desired.Service。
// 参数说明：scanner 是 *sql.Row 或 *sql.Rows 等提供 Scan 方法的对象。
func scanServiceDesired(scanner interface{ Scan(dest ...any) error }) (desired.Service, error) {
	// 先声明数据库 nullable/JSON 中间变量，避免把存储形态泄漏到领域对象。
	var item desired.Service
	var configSetID sql.NullString
	var secretSetID sql.NullString
	var registryCredentialID sql.NullString
	var commandJSON []byte
	var argsJSON []byte
	var envJSON []byte
	var projectedFilesJSON []byte
	var persistentDirsJSON []byte
	var observedAt sql.NullTime
	// 扫描顺序必须和 serviceDesiredSelectSQL/serviceDesiredReturningSQL 的字段顺序一致。
	if err := scanner.Scan(
		&item.ServiceID,
		&item.Name,
		&item.Generation,
		&item.ObservedGeneration,
		&item.DisplayName,
		&item.Spec.Region,
		&item.Spec.Replicas,
		&item.Spec.InstanceClass,
		&item.Spec.Exposure,
		&item.Spec.Image,
		&commandJSON,
		&argsJSON,
		&item.Spec.DefaultPort,
		&item.Spec.ReadinessPath,
		&envJSON,
		&configSetID,
		&secretSetID,
		&registryCredentialID,
		&projectedFilesJSON,
		&persistentDirsJSON,
		&item.SpecHash,
		&item.Phase,
		&item.Message,
		&item.AcceptedAt,
		&observedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return desired.Service{}, err
	}
	// 将 nullable 字段还原为领域对象使用的普通字符串或指针。
	item.Spec.ConfigSetID = nullStringValue(configSetID)
	item.Spec.SecretSetID = nullStringValue(secretSetID)
	item.Spec.RegistryCredentialID = nullStringValue(registryCredentialID)
	if observedAt.Valid {
		item.ObservedAt = &observedAt.Time
	}
	// JSON 字段逐个反序列化；空数据库值按对应空集合处理。
	if err := unmarshalJSON(commandJSON, &item.Spec.Command, []string{}); err != nil {
		return desired.Service{}, fmt.Errorf("unmarshal desired command: %w", err)
	}
	if err := unmarshalJSON(argsJSON, &item.Spec.Args, []string{}); err != nil {
		return desired.Service{}, fmt.Errorf("unmarshal desired args: %w", err)
	}
	if err := unmarshalJSON(envJSON, &item.Spec.Env, map[string]string{}); err != nil {
		return desired.Service{}, fmt.Errorf("unmarshal desired env: %w", err)
	}
	if err := unmarshalJSON(projectedFilesJSON, &item.Spec.ProjectedFiles, []projectedfile.Spec{}); err != nil {
		return desired.Service{}, fmt.Errorf("unmarshal desired projected files: %w", err)
	}
	if err := unmarshalJSON(persistentDirsJSON, &item.Spec.PersistentDirs, []persistentdir.Spec{}); err != nil {
		return desired.Service{}, fmt.Errorf("unmarshal desired persistent dirs: %w", err)
	}
	return item, nil
}

// nullStringValue 将 sql.NullString 转换为普通字符串。
// 参数说明：value 是数据库 nullable string 扫描结果。
func nullStringValue(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}
