package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"mini-cloud/internal/cloudplane/domain/node"
	"mini-cloud/internal/cloudplane/infra/runtimepool"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrNodeNotFound                         = errors.New("node not found")
	ErrHeartbeatCPUMilliAllocatableTooLarge = errors.New("heartbeat cpuMilliAllocatable exceeds node cpuMilliTotal")
	ErrHeartbeatMemoryMiAllocatableTooLarge = errors.New("heartbeat memoryMiAllocatable exceeds node memoryMiTotal")
	ErrNodeProviderInstanceAlreadyExists    = errors.New("node with this provider and instanceID already exists")
)

// RegisterNode 创建或刷新 node-agent 注册的节点记录。
// 参数说明：ctx 控制数据库请求生命周期；input 是 node-agent 上报的注册信息。
func (s *Store) RegisterNode(ctx context.Context, input node.RegisterInput) (node.Node, error) {
	// 复杂流程说明：node 注册既可能创建新 node，也可能刷新已有 node-agent 记录。
	// 注册只创建或刷新 node 静态记录；runtime node ready 同步由后续 ready heartbeat 推进。
	if err := input.Validate(); err != nil {
		return node.Node{}, err
	}

	// INSERT 路径需要新 node ID；冲突更新路径会保留原 ID。
	id, err := newID("node")
	if err != nil {
		return node.Node{}, err
	}

	// 同一台底层实例重复注册时，不新建第二个 node，而是刷新静态信息。
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO nodes (
			id,
			provider,
			region,
			name,
			private_ip,
			public_ip,
			instance_id,
			instance_type,
			cpu_milli_total,
			memory_mi_total,
			cpu_milli_allocatable,
			memory_mi_allocatable,
			cpu_milli_allocated,
			memory_mi_allocated,
			status,
			schedulable
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 0, 0, 0, 0, $11, TRUE)
		ON CONFLICT (provider, instance_id) DO UPDATE
		SET
			region = EXCLUDED.region,
			name = EXCLUDED.name,
			private_ip = EXCLUDED.private_ip,
			public_ip = EXCLUDED.public_ip,
			instance_type = EXCLUDED.instance_type,
			cpu_milli_total = EXCLUDED.cpu_milli_total,
			memory_mi_total = EXCLUDED.memory_mi_total,
			updated_at = now()
		RETURNING
			id,
			provider,
			region,
			name,
			private_ip,
			public_ip,
			instance_id,
			instance_type,
			cpu_milli_total,
			memory_mi_total,
			cpu_milli_allocatable,
			memory_mi_allocatable,
			cpu_milli_allocated,
			memory_mi_allocated,
			status,
			schedulable,
			last_heartbeat_at,
			created_at,
			updated_at
	`,
		id,
		input.Provider,
		input.Region,
		input.Name,
		input.PrivateIP,
		input.PublicIP,
		input.InstanceID,
		input.InstanceType,
		input.CPUMilliTotal,
		input.MemoryMiTotal,
		node.StatusRegistering,
	)

	registered, err := scanNode(row)
	if err != nil {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" {
			// provider/instanceID 唯一冲突转为领域错误，避免泄漏数据库错误码。
			return node.Node{}, ErrNodeProviderInstanceAlreadyExists
		}
		return node.Node{}, fmt.Errorf("register node: %w", err)
	}

	return registered, nil
}

// ListNodes 列出 cloud-plane 当前记录的所有 node。
// 参数说明：ctx 控制数据库请求生命周期。
func (s *Store) ListNodes(ctx context.Context) ([]node.Node, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			provider,
			region,
			name,
			private_ip,
			public_ip,
			instance_id,
			instance_type,
			cpu_milli_total,
			memory_mi_total,
			cpu_milli_allocatable,
			memory_mi_allocatable,
			cpu_milli_allocated,
			memory_mi_allocated,
			status,
			schedulable,
			last_heartbeat_at,
			created_at,
			updated_at
		FROM nodes
		ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query nodes: %w", err)
	}
	defer closeRows(rows)

	items := make([]node.Node, 0)
	for rows.Next() {
		item, err := scanNode(rows)
		if err != nil {
			return nil, fmt.Errorf("scan node: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate nodes: %w", err)
	}
	return items, nil
}

// RecordNodeHeartbeat 记录 node heartbeat。
// 参数说明：ctx 控制数据库请求生命周期；nodeID 是 node 唯一标识；input 是 node-agent 上报的心跳摘要。
func (s *Store) RecordNodeHeartbeat(ctx context.Context, nodeID string, input node.HeartbeatInput) (node.HeartbeatSummary, time.Time, error) {
	// 复杂流程说明：heartbeat 同时更新容量、状态、版本和可调度性。
	// 写入后会刷新 nodes 表摘要，并在 ready 且可调度时同步 runtime node ready 状态。
	if err := input.Validate(); err != nil {
		return node.HeartbeatSummary{}, time.Time{}, err
	}

	// 一次心跳刷新 nodes 表里的最新摘要，并在 ready 时推进 runtime node 状态。
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return node.HeartbeatSummary{}, time.Time{}, fmt.Errorf("begin heartbeat tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var cpuTotal int
	var memoryTotal int
	var cpuAllocated int
	var memoryAllocated int
	var currentStatus string
	var schedulable bool
	var provider string
	var instanceID string
	var name string
	// 读取节点当前容量、分配量、状态和调度开关，用于校验和状态合成。
	err = tx.QueryRowContext(ctx, `
		SELECT provider, instance_id, name, cpu_milli_total, memory_mi_total, cpu_milli_allocated, memory_mi_allocated, status, schedulable
		FROM nodes
		WHERE id = $1
	`, nodeID).Scan(&provider, &instanceID, &name, &cpuTotal, &memoryTotal, &cpuAllocated, &memoryAllocated, &currentStatus, &schedulable)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 心跳引用未知 node 时返回 not found。
			return node.HeartbeatSummary{}, time.Time{}, ErrNodeNotFound
		}
		return node.HeartbeatSummary{}, time.Time{}, fmt.Errorf("load node capacity: %w", err)
	}

	// 如果 backing runtime node 已经进入回收状态，后续心跳只能刷新摘要，不能把 node 重新打开调度。
	var runtimeNodeStatus sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT status
		FROM runtime_nodes
		WHERE provider = $1
		  AND instance_id = $2
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, provider, instanceID).Scan(&runtimeNodeStatus)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return node.HeartbeatSummary{}, time.Time{}, fmt.Errorf("load runtime node lifecycle for heartbeat: %w", err)
	}

	if input.CPUMilliAllocatable > cpuTotal {
		return node.HeartbeatSummary{}, time.Time{}, ErrHeartbeatCPUMilliAllocatableTooLarge
	}
	if input.MemoryMiAllocatable > memoryTotal {
		return node.HeartbeatSummary{}, time.Time{}, ErrHeartbeatMemoryMiAllocatableTooLarge
	}

	reportedAt := input.ReportedAt.UTC()
	// lastHeartbeatAt 更适合记录“控制面什么时候真正收到这次心跳”。
	receivedAt := time.Now().UTC()
	// node-agent 上报的是静态 allocatable 预算，不是实时 free。
	// 因此这里只刷新节点可调度上限，不再用 total-allocatable 反推 allocated；
	// allocated 只能来自调度/执行占用，systemReserved、agentReserved、evictionReserved
	// 都属于不可调度预算，不应该被混入业务负载已分配量。
	nextStatus := input.Status
	nextSchedulable := schedulable

	// draining 是平台管理员显式打开的维护状态，
	// 后续 heartbeat 只能继续刷新资源摘要和时间戳，不能把它覆盖掉。
	switch currentStatus {
	case node.StatusDraining:
		nextStatus = node.StatusDraining
		nextSchedulable = false
	case node.StatusOffline:
		// offline 是平台按“心跳过期”推导出来的临时状态。
		// 一旦新 heartbeat 真到了，就允许节点自动恢复成 agent 当前上报的状态。
		nextStatus = input.Status
		nextSchedulable = true
	}
	// runtime node scale-in 状态优先级高于 node-agent 心跳；
	// 否则 provider 删除前最后几次心跳可能把 terminating/deleted 节点重新变成可调度。
	if runtimeNodeStatus.Valid {
		switch runtimeNodeStatus.String {
		case runtimepool.StatusTerminating:
			nextStatus = node.StatusDraining
			nextSchedulable = false
		case runtimepool.StatusDeleted:
			nextStatus = node.StatusOffline
			nextSchedulable = false
		}
	}

	// nodes 表保留的是当前最新摘要，方便平台页和后续调度直接读取。
	if _, err := tx.ExecContext(ctx, `
		UPDATE nodes
		SET
			cpu_milli_allocatable = $2,
			memory_mi_allocatable = $3,
			status = $4,
			schedulable = $5,
			last_heartbeat_at = $6,
			updated_at = CASE
				WHEN cpu_milli_allocatable IS DISTINCT FROM $2
					OR memory_mi_allocatable IS DISTINCT FROM $3
					OR status IS DISTINCT FROM $4
					OR schedulable IS DISTINCT FROM $5
				THEN now()
				ELSE updated_at
			END
		WHERE id = $1
	`,
		nodeID,
		input.CPUMilliAllocatable,
		input.MemoryMiAllocatable,
		nextStatus,
		nextSchedulable,
		receivedAt,
	); err != nil {
		return node.HeartbeatSummary{}, time.Time{}, fmt.Errorf("update node summary from heartbeat: %w", err)
	}

	if nextStatus == node.StatusReady && nextSchedulable {
		// ready 且可调度时同步 runtime node ready 状态，唤醒 provider 扩容链路。
		if _, err := syncRuntimeNodeReadyTx(ctx, tx, provider, instanceID, name, nodeID, receivedAt); err != nil {
			return node.HeartbeatSummary{}, time.Time{}, fmt.Errorf("sync runtime node from heartbeat: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return node.HeartbeatSummary{}, time.Time{}, fmt.Errorf("commit heartbeat tx: %w", err)
	}

	// 返回本次心跳摘要和控制面接收时间。
	return node.HeartbeatSummary{
		ReportedAt:          reportedAt,
		AgentVersion:        input.AgentVersion,
		CPUMilliAllocatable: input.CPUMilliAllocatable,
		MemoryMiAllocatable: input.MemoryMiAllocatable,
		RunningContainers:   input.RunningContainers,
		Status:              input.Status,
	}, receivedAt, nil
}

// GetNode 按 node ID 查询节点。
// 参数说明：ctx 控制数据库请求生命周期；nodeID 是 node 唯一标识。
func (s *Store) GetNode(ctx context.Context, nodeID string) (node.Node, error) {
	// 按主键读取 node 完整字段。
	row := s.db.QueryRowContext(ctx, `
		SELECT
			id,
			provider,
			region,
			name,
			private_ip,
			public_ip,
			instance_id,
			instance_type,
			cpu_milli_total,
			memory_mi_total,
			cpu_milli_allocatable,
			memory_mi_allocatable,
			cpu_milli_allocated,
			memory_mi_allocated,
			status,
			schedulable,
			last_heartbeat_at,
			created_at,
			updated_at
		FROM nodes
		WHERE id = $1
	`, nodeID)

	item, err := scanNode(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 数据库无行时转换为 store 领域错误。
			return node.Node{}, ErrNodeNotFound
		}
		return node.Node{}, fmt.Errorf("query node: %w", err)
	}

	return item, nil
}

// GetNodeByProviderInstance 按 provider 和云实例 ID 查询节点。
// 参数说明：ctx 控制数据库请求生命周期；provider 是云厂商名称；instanceID 是云厂商实例 ID。
func (s *Store) GetNodeByProviderInstance(ctx context.Context, provider string, instanceID string) (node.Node, error) {
	// provider + instance_id 是云实例到 node 的唯一定位键。
	row := s.db.QueryRowContext(ctx, `
		SELECT
			id,
			provider,
			region,
			name,
			private_ip,
			public_ip,
			instance_id,
			instance_type,
			cpu_milli_total,
			memory_mi_total,
			cpu_milli_allocatable,
			memory_mi_allocatable,
			cpu_milli_allocated,
			memory_mi_allocated,
			status,
			schedulable,
			last_heartbeat_at,
			created_at,
			updated_at
		FROM nodes
		WHERE provider = $1 AND instance_id = $2
	`, provider, instanceID)

	item, err := scanNode(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 数据库无行时转换为 store 领域错误。
			return node.Node{}, ErrNodeNotFound
		}
		return node.Node{}, fmt.Errorf("query node by provider and instance: %w", err)
	}

	return item, nil
}

// scanNode 从 SQL 扫描器读取一行数据并组装领域对象。
// 参数说明：scanner 是当前数据库查询结果行扫描器。
func scanNode(scanner interface{ Scan(dest ...any) error }) (node.Node, error) {
	// last_heartbeat_at 允许为空，使用 NullTime 扫描。
	var item node.Node
	var lastHeartbeatAt sql.NullTime

	// 扫描顺序必须和 node SELECT/RETURNING 字段顺序一致。
	err := scanner.Scan(
		&item.ID,
		&item.Provider,
		&item.Region,
		&item.Name,
		&item.PrivateIP,
		&item.PublicIP,
		&item.InstanceID,
		&item.InstanceType,
		&item.CPUMilliTotal,
		&item.MemoryMiTotal,
		&item.CPUMilliAllocatable,
		&item.MemoryMiAllocatable,
		&item.CPUMilliAllocated,
		&item.MemoryMiAllocated,
		&item.Status,
		&item.Schedulable,
		&lastHeartbeatAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return node.Node{}, err
	}

	// 有心跳时间时转换为指针字段。
	if lastHeartbeatAt.Valid {
		item.LastHeartbeatAt = &lastHeartbeatAt.Time
	}

	return item, nil
}
