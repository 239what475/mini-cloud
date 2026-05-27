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
	// 复杂流程说明：overview 聚合 project、service、node、deployment 多张表的状态计数。
	// 查询保持只读聚合，不在这里推导或修正业务状态。
	var out observability.Overview

	// 统计项目总数。
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM projects
	`).Scan(&out.ProjectsTotal); err != nil {
		return observability.Overview{}, fmt.Errorf("count projects: %w", err)
	}

	// 统计 service 总数及各状态数量。
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE status = 'idle'),
			COUNT(*) FILTER (WHERE status = 'deploying'),
			COUNT(*) FILTER (WHERE status = 'running'),
			COUNT(*) FILTER (WHERE status = 'degraded'),
			COUNT(*) FILTER (WHERE status = 'failed')
		FROM services
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

	// 统计 deployment 总数及各状态数量。
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE status = 'pending'),
			COUNT(*) FILTER (WHERE status = 'scheduling'),
			COUNT(*) FILTER (WHERE status = 'assigned'),
			COUNT(*) FILTER (WHERE status = 'deploying'),
			COUNT(*) FILTER (WHERE status = 'running'),
			COUNT(*) FILTER (WHERE status = 'failed')
		FROM deployments
	`).Scan(
		&out.DeploymentsTotal,
		&out.DeploymentsPending,
		&out.DeploymentsScheduling,
		&out.DeploymentsAssigned,
		&out.DeploymentsDeploying,
		&out.DeploymentsRunning,
		&out.DeploymentsFailed,
	); err != nil {
		return observability.Overview{}, fmt.Errorf("count deployments overview: %w", err)
	}

	// 统计 execution 总数及主要状态数量。
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE status = 'deploying'),
			COUNT(*) FILTER (WHERE status = 'running'),
			COUNT(*) FILTER (WHERE status = 'failed')
		FROM deployment_executions
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

// GetDeploymentStuckSignal 统计超过阈值仍处于非终态的 deployment。
// 参数说明：ctx 控制数据库请求生命周期；threshold 表示卡住判定阈值。
func (s *Store) GetDeploymentStuckSignal(ctx context.Context, threshold time.Duration) (observability.DeploymentStuckSignal, error) {
	// 未传入阈值时使用领域层默认阈值。
	if threshold <= 0 {
		threshold = time.Duration(observability.DeploymentStuckThresholdSeconds) * time.Second
	}

	// 输出中保留阈值秒数，方便告警和页面说明口径。
	out := observability.DeploymentStuckSignal{
		ThresholdSeconds: int64(threshold / time.Second),
	}
	// 分状态统计超时 deployment，并计算最老非终态 deployment 的年龄。
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (
				WHERE status = 'pending'
				  AND updated_at <= now() - ($1 * interval '1 second')
			),
			COUNT(*) FILTER (
				WHERE status = 'scheduling'
				  AND updated_at <= now() - ($1 * interval '1 second')
			),
			COUNT(*) FILTER (
				WHERE status = 'assigned'
				  AND updated_at <= now() - ($1 * interval '1 second')
			),
			COUNT(*) FILTER (
				WHERE status = 'deploying'
				  AND updated_at <= now() - ($1 * interval '1 second')
			),
			COALESCE(MAX(EXTRACT(EPOCH FROM (now() - updated_at))) FILTER (
				WHERE status IN ('pending', 'scheduling', 'assigned', 'deploying')
			), 0)::BIGINT
		FROM deployments
	`, out.ThresholdSeconds).Scan(
		&out.Pending,
		&out.Scheduling,
		&out.Assigned,
		&out.Deploying,
		&out.OldestAgeSeconds,
	); err != nil {
		return observability.DeploymentStuckSignal{}, fmt.Errorf("count stuck deployments: %w", err)
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
	// 复杂流程说明：可靠性输入需要提取卡住的 deployment 和 runtime node 状态。
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

	// deployment SLO 只看最近 24h 创建且当前为 running/failed 的终态 deployment。
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status IN ('running', 'failed') AND created_at >= now() - interval '24 hours'),
			COUNT(*) FILTER (WHERE status = 'running' AND created_at >= now() - interval '24 hours')
		FROM deployments
	`).Scan(
		&input.TerminalDeploymentsLast24h,
		&input.SuccessfulDeploymentsLast24h,
	); err != nil {
		return observability.ReliabilityInputs{}, fmt.Errorf("count deployment slo window: %w", err)
	}

	// 复用卡住 deployment 信号。
	input.DeploymentStuck, err = s.GetDeploymentStuckSignal(ctx, time.Duration(observability.DeploymentStuckThresholdSeconds)*time.Second)
	if err != nil {
		return observability.ReliabilityInputs{}, err
	}

	// 复用 runtime node 注册超时信号。
	input.RuntimeNodeRegistration, err = s.GetRuntimeNodeRegistrationSignal(ctx, time.Duration(observability.RuntimeNodeRegistrationTimeoutSeconds)*time.Second)
	if err != nil {
		return observability.ReliabilityInputs{}, err
	}

	// 读取 rollout 结果累计计数器。
	input.DeploymentRolloutCounters, err = s.GetDeploymentRolloutCounterSignal(ctx)
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
