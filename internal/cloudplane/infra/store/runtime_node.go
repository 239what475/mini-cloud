package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"mini-cloud/internal/cloudplane/infra/runtimepool"
	cloudmodel "mini-cloud/internal/cloudplane/model"
)

// ErrRuntimeNodeNotFound 表示 runtime node 记录不存在。
var ErrRuntimeNodeNotFound = errors.New("runtime node not found")

// runtimeNodeSelectColumns 是扫描 runtime_nodes 完整记录时复用的字段列表。
const runtimeNodeSelectColumns = `
	id,
	provider,
	region,
	instance_id,
	instance_name,
	instance_type,
	node_id,
	status,
	status_reason,
	provisioned_at,
	ready_at,
	last_synced_at,
	created_at,
	updated_at
`

// CreateRuntimeNodeIntent 创建尚未绑定云实例 ID 的 runtime node 意图记录。
// 参数说明：ctx 控制数据库请求生命周期；input 是扩容意图信息。
func (s *Store) CreateRuntimeNodeIntent(ctx context.Context, input runtimepool.CreateIntentInput) (runtimepool.Record, error) {
	// intent 只记录即将创建的通用容量节点，不绑定任何 service/run。
	if err := input.Validate(); err != nil {
		return runtimepool.Record{}, err
	}
	// 未显式传入时间时使用控制面当前时间。
	if input.ProvisionedAt.IsZero() {
		input.ProvisionedAt = time.Now().UTC()
	}

	// intent 也使用 runtime node ID，后续 bind 时沿用同一条记录。
	id, err := newID("rtn")
	if err != nil {
		return runtimepool.Record{}, err
	}

	// intent 创建时 instance_id 为空，状态为 provisioning。
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO runtime_nodes (
			id,
			provider,
			region,
			instance_id,
			instance_name,
			instance_type,
			status,
			status_reason,
			provisioned_at
		)
		VALUES ($1, $2, $3, NULL, $4, $5, $6, $7, $8)
		RETURNING `+runtimeNodeSelectColumns+`
	`,
		id,
		input.Provider,
		input.Region,
		input.InstanceName,
		input.InstanceType,
		runtimepool.StatusProvisioning,
		input.StatusReason,
		input.ProvisionedAt.UTC(),
	)

	item, err := scanRuntimeNode(row)
	if err != nil {
		return runtimepool.Record{}, fmt.Errorf("create runtime node intent: %w", err)
	}
	return item, nil
}

// UpdateRuntimeNodeIntentInstanceName 更新 provisioning intent 的云实例名称。
// 参数说明：ctx 控制数据库请求生命周期；runtimeNodeID 是 runtime node 唯一标识；instanceName 是云厂商实例名称。
func (s *Store) UpdateRuntimeNodeIntentInstanceName(ctx context.Context, runtimeNodeID string, instanceName string) (runtimepool.Record, error) {
	// 只有 provisioning 状态的 intent 允许修改 instance_name。
	row := s.db.QueryRowContext(ctx, `
		UPDATE runtime_nodes
		SET
			instance_name = $2,
			updated_at = now()
		WHERE id = $1
		  AND status = $3
		RETURNING `+runtimeNodeSelectColumns+`
	`,
		runtimeNodeID,
		instanceName,
		runtimepool.StatusProvisioning,
	)

	item, err := scanRuntimeNode(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 找不到记录或状态已不再是 provisioning，都按不可更新处理。
			return runtimepool.Record{}, ErrRuntimeNodeNotFound
		}
		return runtimepool.Record{}, fmt.Errorf("update runtime node intent instance name: %w", err)
	}
	return item, nil
}

// BindRuntimeNodeProvisioned 将 provisioning intent 绑定到 provider 返回的云实例。
// 参数说明：ctx 控制数据库请求生命周期；runtimeNodeID 是 runtime node 唯一标识；instanceID/instanceName/instanceType 是云实例身份；reason 记录状态变化原因；observedAt 是观测时间。
func (s *Store) BindRuntimeNodeProvisioned(ctx context.Context, runtimeNodeID string, instanceID string, instanceName string, instanceType string, reason string, observedAt time.Time) (runtimepool.Record, error) {
	// 未传入观测时间时使用控制面当前时间。
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	// bind 需要读取当前记录并做 compare-and-set，必须在事务内完成。
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return runtimepool.Record{}, fmt.Errorf("begin runtime node bind tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	// 锁定 runtime node，防止并发 bind 交错。
	currentRow := tx.QueryRowContext(ctx, `
		SELECT `+runtimeNodeSelectColumns+`
		FROM runtime_nodes
		WHERE id = $1
		FOR UPDATE
	`, runtimeNodeID)

	current, err := scanRuntimeNode(currentRow)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return runtimepool.Record{}, ErrRuntimeNodeNotFound
		}
		return runtimepool.Record{}, fmt.Errorf("load runtime node for bind: %w", err)
	}

	// 已绑定同一实例时幂等返回；绑定不同实例或非 provisioning 状态则拒绝。
	switch {
	case current.InstanceID != "" && current.InstanceID == instanceID:
		return current, nil
	case current.InstanceID != "" && current.InstanceID != instanceID:
		return runtimepool.Record{}, fmt.Errorf("runtime node %s is already bound to instance %s", runtimeNodeID, current.InstanceID)
	case current.Status != runtimepool.StatusProvisioning:
		return runtimepool.Record{}, fmt.Errorf("runtime node %s is in status %s and cannot bind a provisioned instance", runtimeNodeID, current.Status)
	}

	// 通过 status 和 instance_id 条件做二次 CAS，避免锁外状态变化导致误绑定。
	row := tx.QueryRowContext(ctx, `
		UPDATE runtime_nodes
		SET
			instance_id = $2,
			instance_name = $3,
			instance_type = $4,
			status_reason = $5,
			provisioned_at = $6,
			last_synced_at = $6,
			updated_at = now()
		WHERE id = $1
		  AND status = $7
		  AND (instance_id IS NULL OR btrim(instance_id) = '')
		RETURNING `+runtimeNodeSelectColumns+`
	`,
		runtimeNodeID,
		instanceID,
		instanceName,
		instanceType,
		reason,
		observedAt.UTC(),
		runtimepool.StatusProvisioning,
	)

	item, err := scanRuntimeNode(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 理论上被前面的 FOR UPDATE 避免；保留错误便于定位异常并发。
			return runtimepool.Record{}, fmt.Errorf("runtime node %s bind lost compare-and-set race", runtimeNodeID)
		}
		return runtimepool.Record{}, fmt.Errorf("bind runtime node provisioned instance: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return runtimepool.Record{}, fmt.Errorf("commit runtime node bind tx: %w", err)
	}
	return item, nil
}

// ListRuntimeNodesByStatuses 按状态集合列出 runtime node。
// 参数说明：ctx 控制数据库请求生命周期；statuses 是要匹配的 runtime node 状态。
func (s *Store) ListRuntimeNodesByStatuses(ctx context.Context, statuses ...string) ([]runtimepool.Record, error) {
	// 空状态集合不访问数据库，避免拼出无意义查询。
	if len(statuses) == 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+runtimeNodeSelectColumns+`
		FROM runtime_nodes
		WHERE status = ANY($1::text[])
		ORDER BY created_at ASC, id ASC
	`,
		runtimeNodeStatuses(statuses),
	)
	if err != nil {
		return nil, fmt.Errorf("query runtime nodes by statuses: %w", err)
	}
	defer closeRows(rows)

	// 逐行扫描 runtime node。
	items := make([]runtimepool.Record, 0)
	for rows.Next() {
		item, err := scanRuntimeNode(rows)
		if err != nil {
			return nil, fmt.Errorf("scan runtime node by statuses: %w", err)
		}
		items = append(items, item)
	}
	// rows.Err 捕获迭代过程中延迟暴露的数据库错误。
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runtime nodes by statuses: %w", err)
	}
	return items, nil
}

// CountActiveExecutionsByNode 统计指定 node 上仍在运行或部署中的 execution 数量。
// 参数说明：ctx 控制数据库请求生命周期；nodeID 是 node 唯一标识。
func (s *Store) CountActiveExecutionsByNode(ctx context.Context, nodeID string) (int, error) {
	// active execution 是 scale-in 的硬保护条件；running/deploying 任一存在都禁止删除底层云主机。
	var count int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM execution_intents
		WHERE node_id = $1
		  AND status IN ($2, $3)
	`, nodeID, cloudmodel.StatusDeploying, cloudmodel.StatusRunning).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active executions by node: %w", err)
	}
	return count, nil
}

func (s *Store) GetRuntimeNodeScaleOutCandidate(ctx context.Context, nodeNamePrefix string, instanceType string) (*runtimepool.ScaleOutCandidate, error) {
	var candidate runtimepool.ScaleOutCandidate
	err := s.db.QueryRowContext(ctx, `
		WITH pending AS (
			SELECT
				plan_id,
				service_id,
				cpu_milli_request,
				memory_mi_request,
				created_at
			FROM execution_intents
			WHERE work_action = $1
			  AND status = $2
			ORDER BY created_at ASC, id ASC
			LIMIT 1
		),
		capacity AS (
			SELECT EXISTS (
				SELECT 1
				FROM nodes, pending
				WHERE status = $3
				  AND schedulable
				  AND cpu_milli_allocatable - cpu_milli_allocated >= pending.cpu_milli_request
				  AND memory_mi_allocatable - memory_mi_allocated >= pending.memory_mi_request
			) AS has_capacity
		),
		provisioning AS (
			SELECT EXISTS (
				SELECT 1
				FROM runtime_nodes
				WHERE status = $4
			) AS has_provisioning
		)
		SELECT
			pending.plan_id,
			pending.service_id,
			pending.cpu_milli_request,
			pending.memory_mi_request,
			$5 || '-' || lower(substr(md5(pending.plan_id), 1, 10)),
			$6,
			pending.plan_id,
			capacity.has_capacity,
			provisioning.has_provisioning
		FROM pending
		CROSS JOIN capacity
		CROSS JOIN provisioning
	`, cloudmodel.WorkActionRun, cloudmodel.StatusPending, cloudmodel.StatusReady, runtimepool.StatusProvisioning, nodeNamePrefix, instanceType).Scan(
		&candidate.PlanID,
		&candidate.ServiceID,
		&candidate.CPUMilli,
		&candidate.MemoryMi,
		&candidate.InstanceName,
		&candidate.InstanceType,
		&candidate.ClientToken,
		&candidate.HasCapacity,
		&candidate.HasProvisioning,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query runtime node scale-out candidate: %w", err)
	}
	return &candidate, nil
}

// HasExecutionIntentsWithStatuses 判断当前是否存在任一指定状态的 execution intent。
// 参数说明：ctx 控制数据库请求生命周期；statuses 是要匹配的 execution intent 状态。
func (s *Store) HasExecutionIntentsWithStatuses(ctx context.Context, statuses ...string) (bool, error) {
	// 空状态集合直接返回 false，调用方不需要额外判断。
	if len(statuses) == 0 {
		return false, nil
	}
	var exists bool
	if err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM execution_intents
			WHERE status = ANY($1::text[])
		)
	`, stringSliceValue(statuses)).Scan(&exists); err != nil {
		return false, fmt.Errorf("check execution intents by statuses: %w", err)
	}
	return exists, nil
}

// MarkRuntimeNodeTerminating 关闭 backing node 调度并把 runtime node 标记为等待 provider 删除。
func (s *Store) MarkRuntimeNodeTerminating(ctx context.Context, runtimeNodeID string, reason string, observedAt time.Time) (runtimepool.Record, bool, error) {
	// 未传入观测时间时使用控制面当前时间。
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return runtimepool.Record{}, false, fmt.Errorf("begin runtime node terminating tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	currentRow := tx.QueryRowContext(ctx, `
		SELECT `+runtimeNodeSelectColumns+`
		FROM runtime_nodes
		WHERE id = $1
		FOR UPDATE
	`, runtimeNodeID)
	current, err := scanRuntimeNode(currentRow)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return runtimepool.Record{}, false, ErrRuntimeNodeNotFound
		}
		return runtimepool.Record{}, false, fmt.Errorf("load runtime node for terminating: %w", err)
	}

	if current.Status == runtimepool.StatusTerminating {
		return current, true, nil
	}
	if current.Status != cloudmodel.StatusReady || strings.TrimSpace(current.InstanceID) == "" || strings.TrimSpace(current.NodeID) == "" {
		return current, false, nil
	}

	// 锁住 backing node，确保 node-agent claim work 与 scale-in 对同一 node 的判断串行化。
	var lockedNodeID string
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM nodes
		WHERE id = $1
		FOR UPDATE
	`, current.NodeID).Scan(&lockedNodeID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return runtimepool.Record{}, false, ErrNodeNotFound
		}
		return runtimepool.Record{}, false, fmt.Errorf("lock backing node for runtime node terminating: %w", err)
	}

	var activeCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM execution_intents
		WHERE node_id = $1
		  AND status IN ($2, $3)
	`, current.NodeID, cloudmodel.StatusDeploying, cloudmodel.StatusRunning).Scan(&activeCount); err != nil {
		return runtimepool.Record{}, false, fmt.Errorf("count active executions before runtime node terminating: %w", err)
	}
	if activeCount > 0 {
		return current, false, nil
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE nodes
		SET
			status = $2,
			schedulable = FALSE,
			updated_at = now()
		WHERE id = $1
	`, current.NodeID, cloudmodel.StatusDraining); err != nil {
		return runtimepool.Record{}, false, fmt.Errorf("mark backing node draining for runtime node terminating: %w", err)
	}

	row := tx.QueryRowContext(ctx, `
		UPDATE runtime_nodes
		SET
			status = $2,
			status_reason = $3,
			last_synced_at = $4,
			updated_at = now()
		WHERE id = $1
		RETURNING `+runtimeNodeSelectColumns+`
	`, runtimeNodeID, runtimepool.StatusTerminating, reason, observedAt.UTC())
	item, err := scanRuntimeNode(row)
	if err != nil {
		return runtimepool.Record{}, false, fmt.Errorf("mark runtime node terminating: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return runtimepool.Record{}, false, fmt.Errorf("commit runtime node terminating tx: %w", err)
	}
	return item, true, nil
}

// MarkRuntimeNodeDeleted 将 runtime node 标记为已删除，并保持对应 node 不可调度。
// 参数说明：ctx 控制数据库请求生命周期；runtimeNodeID 是 runtime node 唯一标识；reason 记录状态变化原因；observedAt 是观测时间。
func (s *Store) MarkRuntimeNodeDeleted(ctx context.Context, runtimeNodeID string, reason string, observedAt time.Time) (runtimepool.Record, error) {
	// 未传入观测时间时使用控制面当前时间。
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}

	// deleted 状态和 backing node offline 必须原子提交，避免已删除实例仍被看作可调度节点。
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return runtimepool.Record{}, fmt.Errorf("begin runtime node deleted tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	// 锁定 runtime node，读取 node_id 后再更新 node 状态。
	currentRow := tx.QueryRowContext(ctx, `
		SELECT `+runtimeNodeSelectColumns+`
		FROM runtime_nodes
		WHERE id = $1
		FOR UPDATE
	`, runtimeNodeID)
	current, err := scanRuntimeNode(currentRow)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return runtimepool.Record{}, ErrRuntimeNodeNotFound
		}
		return runtimepool.Record{}, fmt.Errorf("load runtime node for deleted: %w", err)
	}

	// backing node 保留为 inventory 历史，但状态必须 offline 且不可调度。
	if strings.TrimSpace(current.NodeID) != "" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE nodes
			SET
				status = $2,
				schedulable = FALSE,
				updated_at = now()
			WHERE id = $1
		`, current.NodeID, cloudmodel.StatusOffline); err != nil {
			return runtimepool.Record{}, fmt.Errorf("mark runtime node backing node offline: %w", err)
		}
	}

	row := tx.QueryRowContext(ctx, `
		UPDATE runtime_nodes
		SET
			status = $2,
			status_reason = $3,
			last_synced_at = $4,
			updated_at = now()
		WHERE id = $1
		RETURNING `+runtimeNodeSelectColumns+`
	`, runtimeNodeID, runtimepool.StatusDeleted, reason, observedAt.UTC())
	item, err := scanRuntimeNode(row)
	if err != nil {
		return runtimepool.Record{}, fmt.Errorf("mark runtime node deleted: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return runtimepool.Record{}, fmt.Errorf("commit runtime node deleted tx: %w", err)
	}
	return item, nil
}

// syncRuntimeNodeReadyTx 在事务内把 runtime node 标记为 ready。
// 参数说明：ctx 控制数据库请求生命周期；tx 表示数据库事务；provider/instanceID/instanceName 定位云实例；nodeID 是已注册 node；observedAt 是观测时间。
func syncRuntimeNodeReadyTx(ctx context.Context, tx *sql.Tx, provider string, instanceID string, instanceName string, nodeID string, observedAt time.Time) (bool, error) {
	// 未传入观测时间时使用控制面当前时间。
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}

	// matchedID 用于判断本次 ready 上报是否匹配到 runtime node intent。
	var matchedID sql.NullString
	// 优先按 instance_id 匹配；如果 intent 尚未绑定 instance_id，则用 provisioning + instance_name 匹配。
	// 已进入 draining/deleting/deleted 的 runtime node 不能被后续心跳重新改回 ready。
	if err := tx.QueryRowContext(ctx, `
		WITH matched AS (
			SELECT id
			FROM runtime_nodes
			WHERE provider = $1
			  AND (
				(instance_id = $2 AND status IN ($8, $5))
				OR (
					status = $8
					AND
					(instance_id IS NULL OR btrim(instance_id) = '')
					AND instance_name = $3
				)
			  )
			ORDER BY CASE WHEN instance_id = $2 THEN 0 ELSE 1 END ASC, created_at DESC, id DESC
			LIMIT 1
			FOR UPDATE
		),
		updated AS (
			UPDATE runtime_nodes
			SET
				instance_id = COALESCE(NULLIF(runtime_nodes.instance_id, ''), NULLIF($2, '')),
				instance_name = CASE
					WHEN COALESCE(NULLIF(runtime_nodes.instance_name, ''), '') = '' THEN $3
					ELSE runtime_nodes.instance_name
				END,
				node_id = $4,
				status = $5,
				status_reason = $6,
				ready_at = COALESCE(ready_at, $7),
				last_synced_at = $7,
				updated_at = now()
			WHERE id IN (SELECT id FROM matched)
			RETURNING id
		)
		SELECT COALESCE(MAX(id), '')
		FROM matched
	`, provider,
		instanceID,
		instanceName,
		nodeID,
		cloudmodel.StatusReady,
		"runtime node registered back and reported a ready heartbeat",
		observedAt.UTC(),
		runtimepool.StatusProvisioning,
	).Scan(&matchedID); err != nil {
		return false, err
	}

	if !matchedID.Valid || matchedID.String == "" {
		return false, nil
	}
	return true, nil
}

// scanRuntimeNode 从 SQL 扫描器读取一行数据并组装领域对象。
// 参数说明：scanner 是当前数据库查询结果行扫描器。
func scanRuntimeNode(scanner interface{ Scan(dest ...any) error }) (runtimepool.Record, error) {
	// runtime node 的若干生命周期字段允许为空，使用 sql.Null* 中间变量扫描。
	var item runtimepool.Record
	var instanceID sql.NullString
	var nodeID sql.NullString
	var readyAt sql.NullTime
	var lastSyncedAt sql.NullTime

	// 扫描顺序必须和 runtimeNodeSelectColumns 字段顺序一致。
	err := scanner.Scan(
		&item.ID,
		&item.Provider,
		&item.Region,
		&instanceID,
		&item.InstanceName,
		&item.InstanceType,
		&nodeID,
		&item.Status,
		&item.StatusReason,
		&item.ProvisionedAt,
		&readyAt,
		&lastSyncedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return runtimepool.Record{}, err
	}
	// nullable 字符串字段有值时写回领域对象。
	if instanceID.Valid {
		item.InstanceID = instanceID.String
	}
	if nodeID.Valid {
		item.NodeID = nodeID.String
	}
	// nullable 时间字段有值时转换成指针。
	if readyAt.Valid {
		value := readyAt.Time
		item.ReadyAt = &value
	}
	if lastSyncedAt.Valid {
		value := lastSyncedAt.Time
		item.LastSyncedAt = &value
	}
	return item, nil
}

// runtimeNodeStatuses 将 string 列表转换为数据库文本数组。
// 参数说明：statuses 是调用方要匹配的 runtime node 状态集合。
func runtimeNodeStatuses(statuses []string) []string {
	out := make([]string, 0, len(statuses))
	for _, status := range statuses {
		out = append(out, string(status))
	}
	return out
}

// stringSliceValue 将 string 列表转换为数据库文本数组。
func stringSliceValue(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, string(value))
	}
	return out
}

// nullableTime 把可选值转换为 SQL nullable time。
// 参数说明：value 是待写入数据库的可选时间。
func nullableTime(value *time.Time) any {
	// nil 或零值都按 SQL NULL 处理。
	if value == nil || value.IsZero() {
		return nil
	}
	// 入库前统一转换为 UTC。
	return value.UTC()
}
