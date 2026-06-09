// Package control 承载 cloud-plane 的本地控制循环和控制用例。
package control

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	"mini-cloud/internal/cloudplane/control/nodepool"
	"mini-cloud/internal/cloudplane/infra/runtimepool"
	"mini-cloud/internal/cloudplane/infra/store"
)

const (
	// fastReconcileInterval 是轻量后台循环的默认周期。
	fastReconcileInterval = 2 * time.Second
	// nodeHealthInterval 是扫描最近一次 node-agent 心跳过期并标记离线的周期。
	nodeHealthInterval = 10 * time.Second
)

// ingressReconciler 定义后台发布外置 ingress 路由快照的能力。
type ingressReconciler interface {
	// ReconcileOnce 执行一轮 ingress 路由收敛。
	ReconcileOnce(context.Context) error
}

// Manager 运行 cloud-plane 本地后台控制循环。
type Manager struct {
	// logger 记录后台收敛循环的结构化日志。
	logger *slog.Logger
	// store 提供 cloud-plane 本地持久化状态读写能力。
	store *store.Store
	// ingress 负责把 public service 运行态发布到外置 ingress 数据面。
	ingress  ingressReconciler
	nodePool *nodepool.Service
	// staleAfter 是 node-agent 心跳超过该时间未刷新后被视为离线的阈值。
	staleAfter time.Duration

	// wg 跟踪已启动的后台 goroutine，供关闭流程等待退出。
	wg sync.WaitGroup
}

// NewManager 构造 cloud-plane 后台收敛管理器。
// 参数说明：logger 记录后台循环日志；stores 提供本地状态访问；driver 操作云厂商 runtime node；ingress 发布外置入口路由。
func NewManager(logger *slog.Logger, stores *store.Store, driver runtimepool.RuntimeDriver, ingress ingressReconciler, cfg cloudplaneconfig.Config) *Manager {
	// Manager 只保存 execution/node/ingress 需要的控制器；service lifecycle truth 已迁回 control-plane。
	return &Manager{
		logger: logger,
		store:  stores,
		// ingress reconciler 不承载 HTTP 流量，只负责把路由快照发布到外置数据面。
		ingress:  ingress,
		nodePool: nodepool.NewService(logger, stores, driver, cfg),
		// 心跳超过 3 分钟未刷新会在 node-health loop 中被标记为 offline。
		staleAfter: 3 * time.Minute,
	}
}

// Start 启动所有后台收敛循环；调用方通过取消 ctx 停止循环。
// 参数说明：ctx 控制所有后台循环的生命周期。
func (m *Manager) Start(ctx context.Context) {
	// node-health 循环把心跳过期的 node 标记为 offline，并同步更新 plane-local runtime 状态。
	m.startLoop(ctx, "node-health", nodeHealthInterval, m.reconcileNodeHealthOnce)
	// ingress 循环把当前 running backends 发布到外置入口数据面；未启用 ingress 时该循环是 no-op。
	m.startLoop(ctx, "ingress", fastReconcileInterval, m.reconcileIngressOnce)
	// runtime-node-pool 循环把弹性 runtime node 池收敛到当前 execution 需求。
	m.startLoop(ctx, "runtime-node-pool", fastReconcileInterval, m.nodePool.ReconcileOnce)
}

// Wait 等待已启动的后台控制循环退出。
func (m *Manager) Wait() {
	m.wg.Wait()
}

// startLoop 启动一个固定间隔执行的后台收敛循环。
// 参数说明：ctx 控制循环退出；name 是日志中的循环名称；interval 是两次迭代间隔；fn 是单次收敛逻辑。
func (m *Manager) startLoop(ctx context.Context, name string, interval time.Duration, fn func(context.Context) error) {
	// 使用 WaitGroup.Go 启动循环，关闭流程通过 Wait 等待 goroutine 退出。
	m.wg.Go(func() {
		// 为该循环创建带 reconciler 名称字段的 logger，方便区分不同后台任务。
		logger := m.logger.With("reconciler", name)
		// ticker 控制两次迭代之间的间隔。
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			// 每轮先立即执行一次，避免启动后必须等待一个 tick 才开始推进状态。
			if err := fn(ctx); err != nil && !errors.Is(err, context.Canceled) {
				// 单轮失败只记录日志，不退出循环；下一轮会继续重试未完成的本地状态。
				logger.Warn("cloud-plane reconciler iteration failed", "error", err)
			}

			select {
			case <-ctx.Done():
				// 上层取消 ctx 时停止循环，并通过 WaitGroup 通知关闭流程。
				logger.Info("cloud-plane reconciler stopped", "reason", ctx.Err())
				return
			case <-ticker.C:
				// 到达下一次调度周期，进入下一轮 fn 执行。
			}
		}
	})
}

// reconcileNodeHealthOnce 标记心跳过期的 node offline，并同步更新匹配到的 plane-local execution 状态。
// 参数说明：ctx 控制本轮数据库访问。
func (m *Manager) reconcileNodeHealthOnce(ctx context.Context) error {
	// 心跳超时是 reconciler 的一个后台 loop，不单独抽薄 wrapper 包；store 只承载同事务状态更新 primitive。
	result, err := m.store.UpdateStaleNodeHeartbeatState(ctx, m.staleAfter)
	if err != nil {
		return err
	}
	// 只有本轮确实标记了 offline node 或影响 execution plan 时才输出告警日志。
	if len(result.NodesMarkedOffline) > 0 || len(result.ImpactedPlans) > 0 {
		// 只有实际产生状态变化时才输出 warn，避免正常空轮询污染日志。
		m.logger.Warn("cloud-plane reconciled stale node heartbeats", "nodes_offline", len(result.NodesMarkedOffline), "impacted_plans", len(result.ImpactedPlans))
	}
	// 没有过期心跳时本轮是 no-op。
	return nil
}

// reconcileIngressOnce 应用当前 public service 的外置入口路由。
// 参数说明：ctx 控制本轮 store 查询和下游 ingress 数据面应用。
func (m *Manager) reconcileIngressOnce(ctx context.Context) error {
	if m.ingress == nil {
		return nil
	}
	return m.ingress.ReconcileOnce(ctx)
}
