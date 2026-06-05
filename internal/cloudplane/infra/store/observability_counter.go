package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"mini-cloud/internal/cloudplane/domain/observability"
)

const (
	runtimeNodeBootstrapCountersTable = "runtime_node_bootstrap_metric_counters"
)

// incrementMetricCounterTx 在事务内递增指定计数器表的 result 计数。
// 参数说明：ctx 控制数据库请求生命周期；tx 表示数据库事务；table 是受信任的计数器表名；result 是计数维度；delta 是增量。
func incrementMetricCounterTx(ctx context.Context, tx *sql.Tx, table string, result string, delta int64) error {
	// table 只来自本文件常量；这里用 Sprintf 拼表名，不接受外部输入。
	query := fmt.Sprintf(`
		INSERT INTO %s (result, value, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (result) DO UPDATE
		SET
			value = %s.value + EXCLUDED.value,
			updated_at = now()
	`, table, table)
	// result 是计数维度，delta 是本次增量；ON CONFLICT 分支会在原值上累加。
	if _, err := tx.ExecContext(ctx, query, result, delta); err != nil {
		return err
	}
	// 计数器写入成功后不返回当前值，避免调用方依赖中间统计值。
	return nil
}

// recordRuntimeNodeBootstrapStartTx 在事务内记录 runtime node bootstrap 开始事件。
// 参数说明：ctx 控制数据库请求生命周期；tx 表示数据库事务；provider/instanceID 定位一次 runtime node bootstrap；recordedAt 是记录时间。
func recordRuntimeNodeBootstrapStartTx(ctx context.Context, tx *sql.Tx, provider string, instanceID string, recordedAt time.Time) error {
	// mark 表按 provider/instance 去重，避免重试重复增加 started 计数。
	inserted, err := tx.ExecContext(ctx, `
		INSERT INTO runtime_node_bootstrap_marks (
			provider,
			instance_id,
			started_recorded_at
		)
		VALUES ($1, $2, $3)
		ON CONFLICT (provider, instance_id) DO NOTHING
	`, provider, instanceID, recordedAt)
	if err != nil {
		return fmt.Errorf("insert runtime node bootstrap start mark: %w", err)
	}

	// RowsAffected=0 表示 start 已记录过。
	rowsAffected, err := inserted.RowsAffected()
	if err != nil {
		return fmt.Errorf("runtime node bootstrap start rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return nil
	}

	// 首次记录 start 时递增 started 计数器。
	if err := incrementMetricCounterTx(ctx, tx, runtimeNodeBootstrapCountersTable, "started", 1); err != nil {
		return fmt.Errorf("increment runtime node bootstrap started counter: %w", err)
	}
	return nil
}

// recordRuntimeNodeBootstrapReadyTx 在事务内记录 runtime node bootstrap ready 事件。
// 参数说明：ctx 控制数据库请求生命周期；tx 表示数据库事务；provider/instanceID 定位一次 runtime node bootstrap；recordedAt 是记录时间。
func recordRuntimeNodeBootstrapReadyTx(ctx context.Context, tx *sql.Tx, provider string, instanceID string, recordedAt time.Time) (bool, error) {
	// 只有 start mark 存在且 ready_recorded_at 为空时才写入 ready。
	updated, err := tx.ExecContext(ctx, `
		UPDATE runtime_node_bootstrap_marks
		SET ready_recorded_at = $3
		WHERE provider = $1
		  AND instance_id = $2
		  AND ready_recorded_at IS NULL
	`, provider, instanceID, recordedAt)
	if err != nil {
		return false, fmt.Errorf("update runtime node bootstrap ready mark: %w", err)
	}

	// RowsAffected=0 表示没有对应 start 或 ready 已记录过。
	rowsAffected, err := updated.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("runtime node bootstrap ready rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return false, nil
	}

	// 首次记录 ready 时递增 ready 计数器。
	if err := incrementMetricCounterTx(ctx, tx, runtimeNodeBootstrapCountersTable, "ready", 1); err != nil {
		return false, fmt.Errorf("increment runtime node bootstrap ready counter: %w", err)
	}
	return true, nil
}

// GetExecutionPlanRolloutCounterSignal 读取 execution plan 终态计数器信号。
// 参数说明：ctx 控制数据库请求生命周期。
func (s *Store) GetExecutionPlanRolloutCounterSignal(ctx context.Context) (observability.ExecutionPlanRolloutCounterSignal, error) {
	// v8 不再维护 cloud-plane rollout 表；这里按 execution plan 的当前终态即时聚合。
	var out observability.ExecutionPlanRolloutCounterSignal
	if err := s.db.QueryRowContext(ctx, `
		WITH plan_counts AS (
			SELECT
				plan_id,
				COUNT(*)::int AS intent_count,
				COUNT(*) FILTER (WHERE status = 'running')::int AS running_count,
				COUNT(*) FILTER (WHERE status = 'failed')::int AS failed_count
			FROM execution_intents
			GROUP BY plan_id
		)
		SELECT
			COUNT(*) FILTER (
				WHERE failed_count > 0 OR (intent_count > 0 AND running_count >= intent_count)
			),
			COUNT(*) FILTER (
				WHERE failed_count = 0 AND intent_count > 0 AND running_count >= intent_count
			),
			COUNT(*) FILTER (WHERE failed_count > 0)
		FROM plan_counts
	`).Scan(
		&out.Total,
		&out.Success,
		&out.Failed,
	); err != nil {
		return observability.ExecutionPlanRolloutCounterSignal{}, fmt.Errorf("query execution rollout counters: %w", err)
	}
	return out, nil
}

// GetRuntimeNodeBootstrapCounterSignal 读取 runtime node bootstrap 计数器信号。
// 参数说明：ctx 控制数据库请求生命周期。
func (s *Store) GetRuntimeNodeBootstrapCounterSignal(ctx context.Context) (observability.RuntimeNodeBootstrapCounterSignal, error) {
	// 聚合 started/ready 两个 result；不存在的计数按 0 处理。
	var out observability.RuntimeNodeBootstrapCounterSignal
	// MAX(value) FILTER 按 result 取当前累计值；同一 result 在表中由唯一键保证最多一行。
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(MAX(value) FILTER (WHERE result = 'started'), 0),
			COALESCE(MAX(value) FILTER (WHERE result = 'ready'), 0)
		FROM runtime_node_bootstrap_metric_counters
	`).Scan(
		&out.Started,
		&out.Ready,
	); err != nil {
		// 包装查询错误，保留计数器类型上下文。
		return observability.RuntimeNodeBootstrapCounterSignal{}, fmt.Errorf("query runtime node bootstrap counters: %w", err)
	}
	// 返回的 signal 只代表本地 runtime node bootstrap marks 累计结果。
	return out, nil
}
