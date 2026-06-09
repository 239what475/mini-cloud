package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	cloudmodel "mini-cloud/internal/cloudplane/model"
)

// ErrHeartbeatStaleAfterInvalid 表示 stale heartbeat 判定窗口非法。
var ErrHeartbeatStaleAfterInvalid = errors.New("staleAfter must be greater than 0")

// UpdateStaleNodeHeartbeatState 将已有心跳但超时的非 offline/draining 节点标记为 offline，并失败化受影响 workload。
// 参数说明：ctx 控制数据库请求生命周期；staleAfter 是心跳超时窗口。
func (s *Store) UpdateStaleNodeHeartbeatState(ctx context.Context, staleAfter time.Duration) (cloudmodel.HeartbeatReconcileResult, error) {
	// 复杂流程说明：周期性检查 heartbeat，把超时 node 推进到 offline。
	// 每个节点按最近心跳时间和当前状态判断，避免重复写入相同状态。
	if staleAfter <= 0 {
		return cloudmodel.HeartbeatReconcileResult{}, ErrHeartbeatStaleAfterInvalid
	}

	// cutoffTime 之前最后心跳的节点会被视为 stale。
	cutoffTime := time.Now().UTC().Add(-staleAfter)
	result := cloudmodel.HeartbeatReconcileResult{
		StaleAfterSeconds:  int(staleAfter / time.Second),
		CutoffTime:         cutoffTime,
		NodesMarkedOffline: []cloudmodel.Node{},
		ImpactedPlans:      []cloudmodel.ReconcileImpact{},
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return cloudmodel.HeartbeatReconcileResult{}, fmt.Errorf("begin reconcile stale heartbeats tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	// 锁定所有心跳过期、尚未 offline/draining 的节点。
	rows, err := tx.QueryContext(ctx, `
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
		WHERE last_heartbeat_at IS NOT NULL
		  AND last_heartbeat_at < $1
		  AND status <> $2
		  AND status <> $3
		ORDER BY last_heartbeat_at ASC, id ASC
		FOR UPDATE
	`, cutoffTime, cloudmodel.StatusOffline, cloudmodel.StatusDraining)
	if err != nil {
		return cloudmodel.HeartbeatReconcileResult{}, fmt.Errorf("query stale nodes: %w", err)
	}

	// 先读完 stale node 列表并关闭 rows，再对每个节点执行更新。
	var staleNodes []cloudmodel.Node
	for rows.Next() {
		item, scanErr := scanNode(rows)
		if scanErr != nil {
			closeRows(rows)
			return cloudmodel.HeartbeatReconcileResult{}, fmt.Errorf("scan stale node: %w", scanErr)
		}
		staleNodes = append(staleNodes, item)
	}
	// rows.Err 捕获迭代过程中延迟暴露的数据库错误。
	if err := rows.Err(); err != nil {
		closeRows(rows)
		return cloudmodel.HeartbeatReconcileResult{}, fmt.Errorf("iterate stale nodes: %w", err)
	}
	// 后续会继续执行查询，先显式关闭当前 rows。
	if err := rows.Close(); err != nil {
		return cloudmodel.HeartbeatReconcileResult{}, fmt.Errorf("close stale node rows: %w", err)
	}

	// 逐个处理 stale node，并收集其影响到的 execution plan。
	for _, staleNode := range staleNodes {
		// 节点状态更新为 offline，同时关闭调度。
		updatedRow := tx.QueryRowContext(ctx, `
			UPDATE nodes
			SET
				status = $2,
				schedulable = FALSE,
				updated_at = now()
			WHERE id = $1
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
		`, staleNode.ID, cloudmodel.StatusOffline)

		updatedNode, err := scanNode(updatedRow)
		if err != nil {
			return cloudmodel.HeartbeatReconcileResult{}, fmt.Errorf("mark stale node offline: %w", err)
		}
		result.NodesMarkedOffline = append(result.NodesMarkedOffline, updatedNode)

		// 同一个 stale node 下受影响 execution plan 使用统一原因。
		reason := fmt.Sprintf(
			"node %s marked offline because no heartbeat arrived after %s",
			staleNode.Name,
			cutoffTime.Format(time.RFC3339),
		)

		impactedIntents, err := failExecutionIntentsForOfflineNode(ctx, tx, staleNode, reason)
		if err != nil {
			return cloudmodel.HeartbeatReconcileResult{}, err
		}
		for _, item := range impactedIntents {
			result.ImpactedPlans = append(result.ImpactedPlans, cloudmodel.ReconcileImpact{
				NodeID:      staleNode.ID,
				NodeName:    staleNode.Name,
				PlanID:      item.PlanID,
				ServiceID:   item.ServiceID,
				ServiceName: item.ServiceName,
				Reason:      reason,
			})
		}
	}

	if err := tx.Commit(); err != nil {
		return cloudmodel.HeartbeatReconcileResult{}, fmt.Errorf("commit reconcile stale heartbeats tx: %w", err)
	}

	// 返回本轮被标记 offline 的节点和被失败化的 execution plan 摘要。
	return result, nil
}

type impactedExecutionIntent struct {
	ID              string
	PlanID          string
	ServiceID       string
	ServiceName     string
	CPUMilliRequest int
	MemoryMiRequest int
}

func failExecutionIntentsForOfflineNode(ctx context.Context, tx *sql.Tx, staleNode cloudmodel.Node, reason string) ([]impactedExecutionIntent, error) {
	intentRows, err := tx.QueryContext(ctx, `
		SELECT
			id,
			plan_id,
			service_id,
			service_name,
			cpu_milli_request,
			memory_mi_request
		FROM execution_intents
		WHERE node_id = $1
		  AND status IN ($2, $3, $4)
		ORDER BY updated_at ASC, id ASC
		FOR UPDATE
	`, staleNode.ID, cloudmodel.StatusPending, cloudmodel.StatusDeploying, cloudmodel.StatusRunning)
	if err != nil {
		return nil, fmt.Errorf("query impacted execution intents: %w", err)
	}
	defer closeRows(intentRows)

	var impacted []impactedExecutionIntent
	for intentRows.Next() {
		var item impactedExecutionIntent
		if err := intentRows.Scan(&item.ID, &item.PlanID, &item.ServiceID, &item.ServiceName, &item.CPUMilliRequest, &item.MemoryMiRequest); err != nil {
			return nil, fmt.Errorf("scan impacted execution intent: %w", err)
		}
		impacted = append(impacted, item)
	}
	if err := intentRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate impacted execution intents: %w", err)
	}

	for _, item := range impacted {
		if _, err := tx.ExecContext(ctx, `
			UPDATE execution_intents
			SET
				status = $2,
				status_reason = $3,
				finished_at = COALESCE(finished_at, now()),
				updated_at = now()
			WHERE id = $1
		`, item.ID, cloudmodel.StatusFailed, reason); err != nil {
			return nil, fmt.Errorf("mark execution intent failed during node offline reconcile: %w", err)
		}
		if err := freeNodeAllocation(ctx, tx, staleNode.ID, item.CPUMilliRequest, item.MemoryMiRequest); err != nil {
			return nil, err
		}
	}
	return impacted, nil
}
