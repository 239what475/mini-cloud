package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"mini-cloud/internal/cloudplane/domain/observability"
)

const (
	deploymentRolloutMetricCountersTable = "deployment_rollout_metric_counters"
	runtimeNodeBootstrapCountersTable    = "runtime_node_bootstrap_metric_counters"
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

// recordDeploymentRolloutOutcomeTx 在事务内记录 deployment rollout 结果并递增计数器。
// 参数说明：ctx 控制数据库请求生命周期；tx 表示数据库事务；deploymentID 是 deployment 唯一标识；result 是 rollout 结果。
func recordDeploymentRolloutOutcomeTx(ctx context.Context, tx *sql.Tx, deploymentID string, result string) error {
	// outcome mark 以 deployment_id 去重，避免同一 deployment 重复计数。
	inserted, err := tx.ExecContext(ctx, `
		INSERT INTO deployment_rollout_outcome_marks (
			deployment_id,
			result
		)
		VALUES ($1, $2)
		ON CONFLICT (deployment_id) DO NOTHING
	`, deploymentID, result)
	if err != nil {
		return fmt.Errorf("insert deployment rollout outcome mark: %w", err)
	}

	// RowsAffected=0 表示该 deployment 已经记录过 outcome。
	rowsAffected, err := inserted.RowsAffected()
	if err != nil {
		return fmt.Errorf("deployment rollout outcome rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return nil
	}

	// 首次记录 outcome 时同时递增 total 和具体 result 计数。
	if err := incrementMetricCounterTx(ctx, tx, deploymentRolloutMetricCountersTable, "total", 1); err != nil {
		return fmt.Errorf("increment deployment rollout total counter: %w", err)
	}
	if err := incrementMetricCounterTx(ctx, tx, deploymentRolloutMetricCountersTable, result, 1); err != nil {
		return fmt.Errorf("increment deployment rollout %s counter: %w", result, err)
	}
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

// GetDeploymentRolloutCounterSignal 读取 deployment rollout 计数器信号。
// 参数说明：ctx 控制数据库请求生命周期。
func (s *Store) GetDeploymentRolloutCounterSignal(ctx context.Context) (observability.DeploymentRolloutCounterSignal, error) {
	// 聚合 total/success/failed 三个 result；不存在的计数按 0 处理。
	var out observability.DeploymentRolloutCounterSignal
	// MAX(value) FILTER 按 result 取当前累计值；同一 result 在表中由唯一键保证最多一行。
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(MAX(value) FILTER (WHERE result = 'total'), 0),
			COALESCE(MAX(value) FILTER (WHERE result = 'success'), 0),
			COALESCE(MAX(value) FILTER (WHERE result = 'failed'), 0)
		FROM deployment_rollout_metric_counters
	`).Scan(
		&out.Total,
		&out.Success,
		&out.Failed,
	); err != nil {
		// 包装查询错误，保留计数器类型上下文。
		return observability.DeploymentRolloutCounterSignal{}, fmt.Errorf("query deployment rollout counters: %w", err)
	}
	// 返回的 signal 是当前数据库快照，不包含 Prometheus 或外部监控数据。
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
