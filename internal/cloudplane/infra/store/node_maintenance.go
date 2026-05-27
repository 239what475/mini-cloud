package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"mini-cloud/internal/cloudplane/domain/deployment"
	"mini-cloud/internal/cloudplane/domain/execution"
	"mini-cloud/internal/cloudplane/domain/node"
	"mini-cloud/internal/cloudplane/domain/workload"
)

// ErrHeartbeatStaleAfterInvalid 表示 stale heartbeat 判定窗口非法。
var ErrHeartbeatStaleAfterInvalid = errors.New("staleAfter must be greater than 0")

// UpdateStaleNodeHeartbeatState 将已有心跳但超时的非 offline/draining 节点标记为 offline，并失败化受影响 workload。
// 参数说明：ctx 控制数据库请求生命周期；staleAfter 是心跳超时窗口。
func (s *Store) UpdateStaleNodeHeartbeatState(ctx context.Context, staleAfter time.Duration) (node.HeartbeatReconcileResult, error) {
	// 复杂流程说明：周期性检查 heartbeat，把超时 node 推进到 offline。
	// 每个节点按最近心跳时间和当前状态判断，避免重复写入相同状态。
	if staleAfter <= 0 {
		return node.HeartbeatReconcileResult{}, ErrHeartbeatStaleAfterInvalid
	}

	// cutoffTime 之前最后心跳的节点会被视为 stale。
	cutoffTime := time.Now().UTC().Add(-staleAfter)
	result := node.HeartbeatReconcileResult{
		StaleAfterSeconds:   int(staleAfter / time.Second),
		CutoffTime:          cutoffTime,
		NodesMarkedOffline:  []node.Node{},
		ImpactedDeployments: []node.ReconcileImpact{},
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return node.HeartbeatReconcileResult{}, fmt.Errorf("begin reconcile stale heartbeats tx: %w", err)
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
		WHERE last_heartbeat_at IS NOT NULL
		  AND last_heartbeat_at < $1
		  AND status <> $2
		  AND status <> $3
		ORDER BY last_heartbeat_at ASC, id ASC
		FOR UPDATE
	`, cutoffTime, node.StatusOffline, node.StatusDraining)
	if err != nil {
		return node.HeartbeatReconcileResult{}, fmt.Errorf("query stale nodes: %w", err)
	}

	// 先读完 stale node 列表并关闭 rows，再对每个节点执行更新。
	var staleNodes []node.Node
	for rows.Next() {
		item, scanErr := scanNode(rows)
		if scanErr != nil {
			closeRows(rows)
			return node.HeartbeatReconcileResult{}, fmt.Errorf("scan stale node: %w", scanErr)
		}
		staleNodes = append(staleNodes, item)
	}
	// rows.Err 捕获迭代过程中延迟暴露的数据库错误。
	if err := rows.Err(); err != nil {
		closeRows(rows)
		return node.HeartbeatReconcileResult{}, fmt.Errorf("iterate stale nodes: %w", err)
	}
	// 后续会继续执行查询，先显式关闭当前 rows。
	if err := rows.Close(); err != nil {
		return node.HeartbeatReconcileResult{}, fmt.Errorf("close stale node rows: %w", err)
	}

	// 逐个处理 stale node，并收集其影响到的 deployment。
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
		`, staleNode.ID, node.StatusOffline)

		updatedNode, err := scanNode(updatedRow)
		if err != nil {
			return node.HeartbeatReconcileResult{}, fmt.Errorf("mark stale node offline: %w", err)
		}
		result.NodesMarkedOffline = append(result.NodesMarkedOffline, updatedNode)

		// 同一个 stale node 下受影响 deployment 使用统一原因。
		reason := fmt.Sprintf(
			"node %s marked offline because no heartbeat arrived after %s",
			staleNode.Name,
			cutoffTime.Format(time.RFC3339),
		)

		// 查找最近 selection 指向该节点且仍处于活跃状态的 deployment。
		deploymentRows, err := tx.QueryContext(ctx, `
			SELECT
				d.id,
				d.service_id,
				a.name,
				d.status
			FROM deployments d
			JOIN services a ON a.id = d.service_id
			JOIN LATERAL (
				SELECT node_id
				FROM placement_decisions
				WHERE deployment_id = d.id
				ORDER BY created_at DESC, id DESC
				LIMIT 1
			) pd ON TRUE
			WHERE pd.node_id = $1
			  AND d.status IN ($2, $3, $4)
			ORDER BY d.created_at ASC, d.id ASC
			FOR UPDATE OF d
		`, staleNode.ID, deployment.StatusAssigned, deployment.StatusDeploying, deployment.StatusRunning)
		if err != nil {
			return node.HeartbeatReconcileResult{}, fmt.Errorf("query impacted deployments: %w", err)
		}

		// impactedDeployment 是受当前离线节点影响、需要在同一事务内标记失败的 deployment。
		type impactedDeployment struct {
			// ID 是 deployment 唯一标识。
			ID string
			// ServiceID 是 deployment 所属 service 的唯一标识。
			ServiceID string
			// ServiceName 是 deployment 所属 service 名称，用于生成影响报告。
			ServiceName string
			// Status 是标记失败前的 deployment 状态。
			Status string
		}

		var impacted []impactedDeployment
		// 先读完 impacted deployment 列表并关闭 rows，再执行更新。
		for deploymentRows.Next() {
			var item impactedDeployment
			if err := deploymentRows.Scan(&item.ID, &item.ServiceID, &item.ServiceName, &item.Status); err != nil {
				closeRows(deploymentRows)
				return node.HeartbeatReconcileResult{}, fmt.Errorf("scan impacted deployment: %w", err)
			}
			impacted = append(impacted, item)
		}
		// rows.Err 捕获迭代过程中延迟暴露的数据库错误。
		if err := deploymentRows.Err(); err != nil {
			closeRows(deploymentRows)
			return node.HeartbeatReconcileResult{}, fmt.Errorf("iterate impacted deployments: %w", err)
		}
		// 后续会更新 deployment/execution，先关闭当前 rows。
		if err := deploymentRows.Close(); err != nil {
			return node.HeartbeatReconcileResult{}, fmt.Errorf("close impacted deployments rows: %w", err)
		}

		// 将受影响 deployment、service 和最新 execution 推进到失败状态。
		for _, item := range impacted {
			// deployment 状态变化先走领域状态机校验。
			if err := deployment.ValidateTransition(item.Status, deployment.StatusFailed, reason); err != nil {
				return node.HeartbeatReconcileResult{}, err
			}

			// deployment 失败后 ready/available 副本清零。
			if _, err := tx.ExecContext(ctx, `
				UPDATE deployments
				SET
					ready_replicas = 0,
					available_replicas = 0,
					status = $2,
					status_reason = $3,
					updated_at = now()
				WHERE id = $1
			`, item.ID, deployment.StatusFailed, reason); err != nil {
				return node.HeartbeatReconcileResult{}, fmt.Errorf("mark deployment failed during reconcile: %w", err)
			}

			// 记录 deployment transition 历史。
			fromStatus := item.Status
			if err := insertDeploymentTransition(ctx, tx, item.ID, &fromStatus, deployment.StatusFailed, reason); err != nil {
				return node.HeartbeatReconcileResult{}, err
			}

			// service 当前按失败处理；后续 reconcile/rollout 可再恢复。
			if _, err := tx.ExecContext(ctx, `
				UPDATE services
				SET
					status = $2,
					updated_at = now()
				WHERE id = $1
			`, item.ServiceID, workload.StatusFailed); err != nil {
				return node.HeartbeatReconcileResult{}, fmt.Errorf("mark service failed during reconcile: %w", err)
			}

			// 只标记该 deployment 最新 execution，避免重复更新历史 execution。
			if _, err := tx.ExecContext(ctx, `
				UPDATE deployment_executions
				SET
					status = $2,
					status_reason = $3,
					finished_at = COALESCE(finished_at, now()),
					updated_at = now()
				WHERE id = (
					SELECT id
					FROM deployment_executions
					WHERE deployment_id = $1
					ORDER BY created_at DESC, id DESC
					LIMIT 1
				)
				  AND status IN ($4, $5)
			`, item.ID, execution.StatusFailed, reason, execution.StatusDeploying, execution.StatusRunning); err != nil {
				return node.HeartbeatReconcileResult{}, fmt.Errorf("mark execution failed during reconcile: %w", err)
			}

			// 记录本轮 reconcile 对外可见的影响摘要。
			result.ImpactedDeployments = append(result.ImpactedDeployments, node.ReconcileImpact{
				NodeID:       staleNode.ID,
				NodeName:     staleNode.Name,
				DeploymentID: item.ID,
				ServiceID:    item.ServiceID,
				ServiceName:  item.ServiceName,
				Reason:       reason,
			})
		}
	}

	if err := tx.Commit(); err != nil {
		return node.HeartbeatReconcileResult{}, fmt.Errorf("commit reconcile stale heartbeats tx: %w", err)
	}

	// 返回本轮被标记 offline 的节点和被失败化的 deployment 摘要。
	return result, nil
}
