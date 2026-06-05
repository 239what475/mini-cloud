package store

import (
	"context"
	"fmt"
	"time"

	"mini-cloud/internal/cloudplane/domain/observability"
)

// GetPlatformOverview 读取平台核心资源的状态计数概览。
// 参数说明：ctx 控制数据库请求生命周期。
func (s *Store) GetPlatformOverview(ctx context.Context) (observability.Overview, error) {
	// 复杂流程说明：overview 聚合 node 和 execution intent 状态计数。
	// 查询保持只读聚合，不在这里推导或修正业务状态。
	var out observability.Overview

	// service 计数来自每个 service 最新 execution plan，不再读取 cloud-plane 本地 service lifecycle 表。
	if err := s.db.QueryRowContext(ctx, `
		WITH plan_counts AS (
			SELECT
				plan_id,
				service_id,
				service_generation,
				COUNT(*)::int AS intent_count,
				COUNT(*) FILTER (WHERE status IN ('pending', 'deploying'))::int AS active_count,
				COUNT(*) FILTER (WHERE status = 'running')::int AS running_count,
				COUNT(*) FILTER (WHERE status = 'failed')::int AS failed_count,
				MAX(updated_at) AS observed_at
			FROM execution_intents
			GROUP BY plan_id, service_id, service_generation
		),
		latest_plan AS (
			SELECT DISTINCT ON (service_id)
				service_id,
				intent_count,
				active_count,
				running_count,
				failed_count
			FROM plan_counts
			ORDER BY service_id, service_generation DESC, observed_at DESC, plan_id DESC
		)
		SELECT
			COUNT(*),
			0,
			COUNT(*) FILTER (WHERE failed_count = 0 AND active_count > 0),
			COUNT(*) FILTER (WHERE failed_count = 0 AND intent_count > 0 AND running_count >= intent_count),
			COUNT(*) FILTER (WHERE failed_count > 0 AND running_count > 0),
			COUNT(*) FILTER (WHERE failed_count > 0 AND running_count = 0)
		FROM latest_plan
	`).Scan(
		&out.ServicesTotal,
		&out.ServicesIdle,
		&out.ServicesDeploying,
		&out.ServicesRunning,
		&out.ServicesDegraded,
		&out.ServicesFailed,
	); err != nil {
		return observability.Overview{}, fmt.Errorf("count services overview: %w", err)
	}

	// 统计 node 总数及各状态数量。
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE status = 'registering'),
			COUNT(*) FILTER (WHERE status = 'ready'),
			COUNT(*) FILTER (WHERE status = 'not_ready'),
			COUNT(*) FILTER (WHERE status = 'draining'),
			COUNT(*) FILTER (WHERE status = 'offline')
		FROM nodes
	`).Scan(
		&out.NodesTotal,
		&out.NodesRegistering,
		&out.NodesReady,
		&out.NodesNotReady,
		&out.NodesDraining,
		&out.NodesOffline,
	); err != nil {
		return observability.Overview{}, fmt.Errorf("count nodes overview: %w", err)
	}

	// execution plan 概览按当前 execution_intents 聚合，不再读取旧 lifecycle 表。
	if err := s.db.QueryRowContext(ctx, `
		WITH plan_counts AS (
			SELECT
				plan_id,
				COUNT(*)::int AS intent_count,
				COUNT(*) FILTER (WHERE status = 'pending')::int AS pending_count,
				COUNT(*) FILTER (WHERE status = 'deploying')::int AS deploying_count,
				COUNT(*) FILTER (WHERE status = 'running')::int AS running_count,
				COUNT(*) FILTER (WHERE status = 'failed')::int AS failed_count
			FROM execution_intents
			GROUP BY plan_id
		)
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE pending_count > 0 AND deploying_count = 0 AND running_count = 0 AND failed_count = 0),
			0,
			0,
			COUNT(*) FILTER (WHERE failed_count = 0 AND deploying_count > 0),
			COUNT(*) FILTER (WHERE failed_count = 0 AND intent_count > 0 AND running_count >= intent_count),
			COUNT(*) FILTER (WHERE failed_count > 0)
		FROM plan_counts
	`).Scan(
		&out.ExecutionPlansTotal,
		&out.ExecutionPlansPending,
		&out.ExecutionPlansScheduling,
		&out.ExecutionPlansAssigned,
		&out.ExecutionPlansDeploying,
		&out.ExecutionPlansRunning,
		&out.ExecutionPlansFailed,
	); err != nil {
		return observability.Overview{}, fmt.Errorf("count execution plans overview: %w", err)
	}

	// 统计 execution intent 总数及主要状态数量；pending 也属于尚未完成的 deploying 口径。
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE status IN ('pending', 'deploying')),
			COUNT(*) FILTER (WHERE status = 'running'),
			COUNT(*) FILTER (WHERE status = 'failed')
		FROM execution_intents
	`).Scan(
		&out.ExecutionsTotal,
		&out.ExecutionsDeploying,
		&out.ExecutionsRunning,
		&out.ExecutionsFailed,
	); err != nil {
		return observability.Overview{}, fmt.Errorf("count executions overview: %w", err)
	}

	// 返回只读聚合结果。
	return out, nil
}

// GetExecutionPlanStuckSignal 统计超过阈值仍处于未完成状态的 execution plan。
// 参数说明：ctx 控制数据库请求生命周期；threshold 表示卡住判定阈值。
func (s *Store) GetExecutionPlanStuckSignal(ctx context.Context, threshold time.Duration) (observability.ExecutionPlanStuckSignal, error) {
	// 未传入阈值时使用领域层默认阈值。
	if threshold <= 0 {
		threshold = time.Duration(observability.ExecutionPlanStuckThresholdSeconds) * time.Second
	}

	// 输出中保留阈值秒数，方便告警和页面说明口径。
	out := observability.ExecutionPlanStuckSignal{
		ThresholdSeconds: int64(threshold / time.Second),
	}
	// 分状态统计超时 execution plan，并计算最老未完成 plan 的年龄。
	if err := s.db.QueryRowContext(ctx, `
		WITH plan_counts AS (
			SELECT
				plan_id,
				COUNT(*) FILTER (WHERE status = 'pending')::int AS pending_count,
				COUNT(*) FILTER (WHERE status = 'deploying')::int AS deploying_count,
				COUNT(*) FILTER (WHERE status = 'running')::int AS running_count,
				COUNT(*) FILTER (WHERE status = 'failed')::int AS failed_count,
				MAX(updated_at) AS updated_at
			FROM execution_intents
			GROUP BY plan_id
		),
		stuck AS (
			SELECT *
			FROM plan_counts
			WHERE failed_count = 0
			  AND (pending_count > 0 OR deploying_count > 0)
			  AND updated_at <= now() - ($1 * interval '1 second')
		)
		SELECT
			COUNT(*) FILTER (WHERE pending_count > 0 AND deploying_count = 0),
			0,
			0,
			COUNT(*) FILTER (WHERE deploying_count > 0),
			COALESCE(MAX(EXTRACT(EPOCH FROM (now() - updated_at))) FILTER (
				WHERE pending_count > 0 OR deploying_count > 0
			), 0)::BIGINT
		FROM stuck
	`, out.ThresholdSeconds).Scan(
		&out.Pending,
		&out.Scheduling,
		&out.Assigned,
		&out.Deploying,
		&out.OldestAgeSeconds,
	); err != nil {
		return observability.ExecutionPlanStuckSignal{}, fmt.Errorf("count stuck execution plans: %w", err)
	}
	// total 是各非终态超时状态计数之和。
	out.Total = out.Pending + out.Scheduling + out.Assigned + out.Deploying
	return out, nil
}

// GetRuntimeNodeRegistrationSignal 统计 runtime node 注册和超时信号。
// 参数说明：ctx 控制数据库请求生命周期；threshold 表示 provisioning 超时阈值。
func (s *Store) GetRuntimeNodeRegistrationSignal(ctx context.Context, threshold time.Duration) (observability.RuntimeNodeRegistrationSignal, error) {
	// 未传入阈值时使用领域层默认阈值。
	if threshold <= 0 {
		threshold = time.Duration(observability.RuntimeNodeRegistrationTimeoutSeconds) * time.Second
	}

	// 输出中保留阈值秒数，方便告警和页面说明口径。
	out := observability.RuntimeNodeRegistrationSignal{
		ThresholdSeconds: int64(threshold / time.Second),
	}
	// 统计 runtime node 生命周期状态，并统计 provisioning 超时数量。
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE status = 'provisioning'),
			COUNT(*) FILTER (WHERE status = 'ready'),
			COUNT(*) FILTER (
				WHERE status = 'provisioning'
				  AND provisioned_at <= now() - ($1 * interval '1 second')
			),
			COALESCE(MAX(EXTRACT(EPOCH FROM (now() - provisioned_at))) FILTER (
				WHERE status = 'provisioning'
			), 0)::BIGINT
		FROM runtime_nodes
	`, out.ThresholdSeconds).Scan(
		&out.Total,
		&out.Provisioning,
		&out.Ready,
		&out.ProvisioningTimedOut,
		&out.OldestProvisioningAgeSeconds,
	); err != nil {
		return observability.RuntimeNodeRegistrationSignal{}, fmt.Errorf("count runtime node registration state: %w", err)
	}
	return out, nil
}

// GetPlatformReliabilityInputs 组装计算平台可靠性快照所需的输入信号。
// 参数说明：ctx 控制数据库请求生命周期。
func (s *Store) GetPlatformReliabilityInputs(ctx context.Context) (observability.ReliabilityInputs, error) {
	// 复杂流程说明：可靠性输入需要提取卡住的 execution plan 和 runtime node 状态。
	// 这里只组装信号，具体健康判断交给 observability 领域层。
	overview, err := s.GetPlatformOverview(ctx)
	if err != nil {
		return observability.ReliabilityInputs{}, err
	}

	// reliability input 先包含平台 overview。
	input := observability.ReliabilityInputs{
		Overview: overview,
	}

	// 统计 runtime 角色节点的基础健康分布。
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE role = 'runtime'),
			COUNT(*) FILTER (WHERE role = 'runtime' AND status = 'ready'),
			COUNT(*) FILTER (WHERE role = 'runtime' AND status = 'offline'),
			COUNT(*) FILTER (WHERE role = 'runtime' AND status = 'not_ready'),
			COUNT(*) FILTER (WHERE role = 'runtime' AND status = 'draining')
		FROM nodes
	`).Scan(
		&input.RuntimeNodesTotal,
		&input.RuntimeNodesReady,
		&input.RuntimeNodesOffline,
		&input.RuntimeNodesNotReady,
		&input.RuntimeNodesDraining,
	); err != nil {
		return observability.ReliabilityInputs{}, fmt.Errorf("count runtime node reliability overview: %w", err)
	}

	// 发布 SLO 按最近 24h 创建且当前为 running/failed 的 execution plan 聚合。
	if err := s.db.QueryRowContext(ctx, `
		WITH plan_counts AS (
			SELECT
				plan_id,
				COUNT(*)::int AS intent_count,
				COUNT(*) FILTER (WHERE status = 'running')::int AS running_count,
				COUNT(*) FILTER (WHERE status = 'failed')::int AS failed_count,
				MIN(created_at) AS created_at
			FROM execution_intents
			GROUP BY plan_id
		)
		SELECT
			COUNT(*) FILTER (
				WHERE created_at >= now() - interval '24 hours'
				  AND (failed_count > 0 OR (intent_count > 0 AND running_count >= intent_count))
			),
			COUNT(*) FILTER (
				WHERE created_at >= now() - interval '24 hours'
				  AND failed_count = 0
				  AND intent_count > 0
				  AND running_count >= intent_count
			)
		FROM plan_counts
	`).Scan(
		&input.TerminalExecutionPlansLast24h,
		&input.SuccessfulExecutionPlansLast24h,
	); err != nil {
		return observability.ReliabilityInputs{}, fmt.Errorf("count execution plan slo window: %w", err)
	}

	// 复用卡住 execution plan 信号。
	input.ExecutionPlanStuck, err = s.GetExecutionPlanStuckSignal(ctx, time.Duration(observability.ExecutionPlanStuckThresholdSeconds)*time.Second)
	if err != nil {
		return observability.ReliabilityInputs{}, err
	}

	// 复用 runtime node 注册超时信号。
	input.RuntimeNodeRegistration, err = s.GetRuntimeNodeRegistrationSignal(ctx, time.Duration(observability.RuntimeNodeRegistrationTimeoutSeconds)*time.Second)
	if err != nil {
		return observability.ReliabilityInputs{}, err
	}

	// 读取 rollout 结果累计计数器。
	input.ExecutionPlanRolloutCounters, err = s.GetExecutionPlanRolloutCounterSignal(ctx)
	if err != nil {
		return observability.ReliabilityInputs{}, err
	}

	// 读取 runtime node bootstrap 累计计数器。
	input.RuntimeNodeBootstrapCounts, err = s.GetRuntimeNodeBootstrapCounterSignal(ctx)
	if err != nil {
		return observability.ReliabilityInputs{}, err
	}

	// 返回完整可靠性输入，具体评分由领域层计算。
	return input, nil
}

// GetPlatformReliabilitySnapshot 读取输入信号并构造平台可靠性快照。
// 参数说明：ctx 控制数据库请求生命周期。
func (s *Store) GetPlatformReliabilitySnapshot(ctx context.Context) (observability.ReliabilitySnapshot, error) {
	// 先从数据库聚合可靠性输入。
	input, err := s.GetPlatformReliabilityInputs(ctx)
	if err != nil {
		return observability.ReliabilitySnapshot{}, err
	}

	// 快照时间使用控制面当前 UTC 时间。
	return observability.BuildReliabilitySnapshot(time.Now().UTC(), input), nil
}
