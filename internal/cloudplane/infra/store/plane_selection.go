package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"mini-cloud/internal/cloudplane/domain/node"
	"mini-cloud/internal/cloudplane/domain/scheduler"
)

// ListNodes 列出调度器可读取的全部节点记录。
// 参数说明：ctx 控制数据库请求生命周期。
func (s *Store) ListNodes(ctx context.Context) ([]node.Node, error) {
	// 当前调度器在内存中筛选节点，因此这里返回 nodes 表完整列表。
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			provider,
			region,
			name,
			role,
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
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query nodes: %w", err)
	}
	defer closeRows(rows)

	// 逐行扫描 node。
	items := make([]node.Node, 0)
	for rows.Next() {
		item, err := scanNode(rows)
		if err != nil {
			return nil, fmt.Errorf("scan node: %w", err)
		}
		items = append(items, item)
	}
	// rows.Err 捕获迭代过程中延迟暴露的数据库错误。
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate nodes: %w", err)
	}

	return items, nil
}

// CreatePlacementDecisions 创建 selection decisions。
// 参数说明：ctx 控制数据库请求生命周期；request 是调度请求；decisions 是预计算的调度放置决策。
func (s *Store) CreatePlacementDecisions(ctx context.Context, request scheduler.PlacementRequest, decisions []scheduler.PlacementDecision) ([]scheduler.StoredDecision, error) {
	// 空决策列表无需写库。
	if len(decisions) == 0 {
		return nil, nil
	}

	// 写入 decision 必须在同一事务内完成。
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin selection decision tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	// 逐条插入并收集带数据库 ID/时间的结果。
	items := make([]scheduler.StoredDecision, 0, len(decisions))
	for _, decision := range decisions {
		stored, err := s.insertPlacementDecisionTx(ctx, tx, request, decision)
		if err != nil {
			return nil, err
		}
		items = append(items, stored)
	}

	// 提交批量写入结果。
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit selection decision tx: %w", err)
	}
	return items, nil
}

// insertPlacementDecisionTx 在事务内插入单条 selection decision。
// 参数说明：ctx 控制数据库请求生命周期；tx 表示数据库事务；request 是调度请求；decision 是预计算的调度结果。
func (s *Store) insertPlacementDecisionTx(ctx context.Context, tx *sql.Tx, request scheduler.PlacementRequest, decision scheduler.PlacementDecision) (scheduler.StoredDecision, error) {
	// 为 selection decision 生成数据库主键。
	id, err := newID("pld")
	if err != nil {
		return scheduler.StoredDecision{}, err
	}

	// deployment_id 允许为空，因此使用 sql.NullString 写入。
	var deploymentID sql.NullString
	if request.DeploymentID != "" {
		deploymentID = sql.NullString{
			String: request.DeploymentID,
			Valid:  true,
		}
	}

	// 写入 selection 前锁定目标 node，并重新确认它仍然 ready 且可调度。
	// scheduler 使用的是内存节点快照；如果 scale-in 已经把 node 改为 draining，这里必须拒绝旧决策。
	var lockedNodeID string
	err = tx.QueryRowContext(ctx, `
		SELECT id
		FROM nodes
		WHERE id = $1
		  AND status = $2
		  AND schedulable = TRUE
		FOR UPDATE
	`, decision.NodeID, node.StatusReady).Scan(&lockedNodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return scheduler.StoredDecision{}, fmt.Errorf("placement target node %s is not ready and schedulable", decision.NodeID)
		}
		return scheduler.StoredDecision{}, fmt.Errorf("lock selection target node: %w", err)
	}

	// 插入调度决策的核心字段。
	var stored scheduler.StoredDecision
	var storedDeploymentID sql.NullString
	err = tx.QueryRowContext(ctx, `
		INSERT INTO placement_decisions (
			id,
			deployment_id,
			node_id,
			region,
			cpu_milli_request,
			memory_mi_request,
			score,
			reason
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, deployment_id, node_id, region, cpu_milli_request, memory_mi_request, score, reason, created_at
	`,
		id,
		deploymentID,
		decision.NodeID,
		decision.Region,
		request.CPUMilliRequest,
		request.MemoryMiRequest,
		decision.Score,
		decision.Reason,
	).Scan(
		&stored.ID,
		&storedDeploymentID,
		&stored.NodeID,
		&stored.Region,
		&stored.CPUMilliRequest,
		&stored.MemoryMiRequest,
		&stored.Score,
		&stored.Reason,
		&stored.CreatedAt,
	)
	if err != nil {
		return scheduler.StoredDecision{}, fmt.Errorf("insert selection decision: %w", err)
	}

	// deployment_id 有值时写回领域对象。
	if storedDeploymentID.Valid {
		stored.DeploymentID = storedDeploymentID.String
	}

	// 补充 node name、service id/name，供页面和审计展示。
	if err := tx.QueryRowContext(ctx, `
		SELECT
			n.name,
			COALESCE(a.id, ''),
			COALESCE(a.name, '')
		FROM nodes n
		LEFT JOIN deployments dep ON dep.id = $2
		LEFT JOIN services a ON a.id = dep.service_id
		WHERE n.id = $1
	`, stored.NodeID, stored.DeploymentID).Scan(&stored.NodeName, &stored.ServiceID, &stored.ServiceName); err != nil {
		return scheduler.StoredDecision{}, fmt.Errorf("load selection decision node name: %w", err)
	}

	return stored, nil
}

// ListPlacementDecisionsByDeployment 列出 deployment 下所有 selection decision。
// 参数说明：ctx 控制数据库请求生命周期；deploymentID 是 deployment 唯一标识。
func (s *Store) ListPlacementDecisionsByDeployment(ctx context.Context, deploymentID string) ([]scheduler.StoredDecision, error) {
	// 按创建时间稳定返回 deployment 对应的 decision。
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			d.id,
			d.deployment_id,
			COALESCE(a.id, ''),
			COALESCE(a.name, ''),
			d.node_id,
			n.name,
			d.region,
			d.cpu_milli_request,
			d.memory_mi_request,
			d.score,
			d.reason,
			d.created_at
		FROM placement_decisions d
		JOIN nodes n ON n.id = d.node_id
		LEFT JOIN deployments dep ON dep.id = d.deployment_id
		LEFT JOIN services a ON a.id = dep.service_id
		WHERE d.deployment_id = $1
		ORDER BY d.created_at DESC, d.id DESC
	`, deploymentID)
	if err != nil {
		return nil, fmt.Errorf("query selection decisions by deployment: %w", err)
	}
	defer closeRows(rows)

	// 逐行扫描 deployment 对应的 decision。
	items := make([]scheduler.StoredDecision, 0)
	for rows.Next() {
		var item scheduler.StoredDecision
		var deployment sql.NullString
		if err := rows.Scan(
			&item.ID,
			&deployment,
			&item.ServiceID,
			&item.ServiceName,
			&item.NodeID,
			&item.NodeName,
			&item.Region,
			&item.CPUMilliRequest,
			&item.MemoryMiRequest,
			&item.Score,
			&item.Reason,
			&item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan selection decision by deployment: %w", err)
		}
		if deployment.Valid {
			item.DeploymentID = deployment.String
		}
		items = append(items, item)
	}
	// rows.Err 捕获迭代过程中延迟暴露的数据库错误。
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate selection decisions by deployment: %w", err)
	}
	return items, nil
}
