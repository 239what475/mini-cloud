// Package control 承载 cloud-plane 的本地控制循环和控制用例。
package control

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	"mini-cloud/internal/cloudplane/control/lifecycle"
	"mini-cloud/internal/cloudplane/control/nodepool"
	"mini-cloud/internal/cloudplane/domain/desired"
	"mini-cloud/internal/cloudplane/domain/workload"
	"mini-cloud/internal/cloudplane/infra/runtimepool"
	"mini-cloud/internal/cloudplane/infra/store"
)

const (
	// serviceDesiredInterval 是扫描待处理 service desired state 的周期。
	serviceDesiredInterval = 2 * time.Second
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
	// lifecycle 复用 service lifecycle 控制器执行创建、更新和部署推进。
	lifecycle lifecycle.Controllers
	// ingress 负责把 public service 运行态发布到外置 ingress 数据面。
	ingress ingressReconciler
	// nodeScaleIn 负责 runtime node 自动缩容状态机。
	nodeScaleIn *nodepool.ScaleInService
	// staleAfter 是 node-agent 心跳超过该时间未刷新后被视为离线的阈值。
	staleAfter time.Duration

	// wg 跟踪已启动的后台 goroutine，供关闭流程等待退出。
	wg sync.WaitGroup
}

// NewManager 构造 cloud-plane 后台收敛管理器。
// 参数说明：logger 记录后台循环日志；stores 提供本地状态访问；cfg 是有效配置；driver 操作云厂商 runtime node；ingress 发布外置入口路由。
func NewManager(logger *slog.Logger, stores *store.Store, cfg cloudplaneconfig.Config, driver runtimepool.RuntimeDriver, ingress ingressReconciler) *Manager {
	// Manager 只保存聚合 store 和 lifecycle controller，不再额外持有单独 repository。
	return &Manager{
		logger: logger,
		store:  stores,
		// lifecycle controller 负责把 desired spec 转换为 service/revision/deployment 状态推进。
		lifecycle: lifecycle.NewControllers(logger, stores, lifecycle.Options{
			Config:        cfg,
			RuntimeDriver: driver,
		}),
		// ingress reconciler 不承载 HTTP 流量，只负责把路由快照发布到外置数据面。
		ingress: ingress,
		// nodeScaleIn 封装 runtime node 缩容候选判断和 provider 删除状态机。
		nodeScaleIn: nodepool.NewScaleInService(logger, stores, driver),
		// 心跳超过 3 分钟未刷新会在 node-health loop 中被标记为 offline。
		staleAfter: 3 * time.Minute,
	}
}

// Start 启动所有后台收敛循环；调用方通过取消 ctx 停止循环。
// 参数说明：ctx 控制所有后台循环的生命周期。
func (m *Manager) Start(ctx context.Context) {
	// service-desired 循环把 control-plane 已接受的 desired state 异步转换为本地 service/deployment 变更；
	// runtime node 选择完全由 cloud-plane 本地 lifecycle scheduler 完成。
	m.startLoop(ctx, "service-desired", serviceDesiredInterval, m.reconcileServiceDesiredOnce)
	// node-health 循环把心跳过期的 node 标记为 offline，并同步更新匹配到的 deployment、service 和 latest execution 状态。
	m.startLoop(ctx, "node-health", nodeHealthInterval, m.reconcileNodeHealthOnce)
	// ingress 循环把 public service 当前 running backends 发布到外置入口数据面；未启用 ingress 时该循环是 no-op。
	m.startLoop(ctx, "ingress", serviceDesiredInterval, m.reconcileIngressOnce)
	// runtime-node-scale-in 循环回收没有 active execution 的弹性 runtime node；允许缩到 0 台。
	m.startLoop(ctx, "runtime-node-scale-in", serviceDesiredInterval, m.nodeScaleIn.ReconcileOnce)
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

// reconcileServiceDesiredOnce 扫描一批可继续推进的 service desired state，并逐个推进收敛。
// 可推进集合由 store 固定为 accepted、reconciling 和 retrying；调用方不再传入任意 phase，避免把 reconciler 策略伪装成通用查询。
// 参数说明：ctx 控制本轮数据库访问和 lifecycle 操作。
func (m *Manager) reconcileServiceDesiredOnce(ctx context.Context) error {
	// 每轮限制批量大小，避免单次循环长期占用后台 worker。
	desiredStates, err := m.store.ListServiceDesiredForReconcile(ctx, 20)
	if err != nil {
		return err
	}
	// 逐条处理 desired；收敛失败会先标记为 retrying，保留错误信息，并在下一轮继续重试。
	for _, desiredState := range desiredStates {
		if err := m.reconcileServiceDesired(ctx, desiredState); err != nil {
			// 单个 desired 失败时标记为 retrying，而不是 blocked；这是自动重试状态，不表示需要人工解除阻塞。
			if markErr := m.store.UpdateServiceDesiredPhase(ctx, desiredState.ServiceID, desiredState.Generation, desired.PhaseRetrying, err.Error()); markErr != nil {
				return errors.Join(err, markErr)
			}
			// 记录失败上下文，下一轮仍可能重新尝试该 desired。
			m.logger.Warn("service desired reconcile failed", "service_id", desiredState.ServiceID, "service_name", desiredState.Name, "generation", desiredState.Generation, "error", err)
		}
	}
	// 本轮所有 pending desired 都已经处理或记录失败。
	return nil
}

// reconcileServiceDesired 将一个已接受的 service desired state 应用到本地 service/deployment 控制状态。
// 参数说明：ctx 控制本次收敛；item 是 control-plane 下发、cloud-plane 接受并写入本地库的期望状态。
func (m *Manager) reconcileServiceDesired(ctx context.Context, item desired.Service) error {
	// 先标记为 reconciling，使查询侧能看到该 generation 正在被后台处理。
	if err := m.store.UpdateServiceDesiredPhase(ctx, item.ServiceID, item.Generation, desired.PhaseReconciling, "cloud-plane reconciler is applying accepted desired state"); err != nil {
		return err
	}

	// serviceID 是 service desired 的唯一身份；不存在同名兜底或延迟绑定逻辑。
	serviceItem, err := m.store.GetService(ctx, item.ServiceID)
	if errors.Is(err, store.ErrServiceNotFound) {
		// 本地还没有对应 service 时，按 desired spec 创建 service 并启动首个 deployment。
		_, err := m.createServiceFromDesired(ctx, item)
		if err != nil {
			return err
		}
		// observed 表示 desired 已被本地 lifecycle 消费，不表示 workload 已经运行健康。
		return m.store.SetServiceDesiredObserved(ctx, item.ServiceID, item.Generation, "desired state created service and launched deployment")
	}
	if err != nil {
		return err
	}
	// serviceID 命中的 service 必须仍属于 desired 声明的 project/name；serviceID 不允许被复用或改绑。
	if serviceItem.Metadata.ProjectID != item.ProjectID || serviceItem.Metadata.Name != item.Name {
		return workload.ErrServiceIdentityConflict
	}

	// 本地已有 service 时，将 desired spec 作为更新输入，并按需要触发新的 deployment。
	_, err = m.updateServiceFromDesired(ctx, item, serviceItem)
	if err != nil {
		return err
	}
	// observed 表示 desired 已被本地 lifecycle 消费，不表示 workload 已经运行健康。
	return m.store.SetServiceDesiredObserved(ctx, item.ServiceID, item.Generation, "desired state updated service and reconciled deployment intent")
}

// createServiceFromDesired 按 desired spec 创建本地 service，并由 cloud-plane 本地 scheduler 选择 runtime node。
// 参数说明：ctx 控制创建流程；item 是 desired state。
func (m *Manager) createServiceFromDesired(ctx context.Context, item desired.Service) (lifecycle.CreateResult, error) {
	return m.lifecycle.Mutations.Create(ctx, item.ServiceID, item.ProjectID, item.Name, item.DisplayName, item.Spec)
}

// updateServiceFromDesired 按 desired spec 更新已有 service，并由 cloud-plane 本地 scheduler 处理副本放置。
// 参数说明：ctx 控制更新流程；item 是 desired state；serviceItem 是已有 service。
func (m *Manager) updateServiceFromDesired(ctx context.Context, item desired.Service, serviceItem workload.Service) (lifecycle.UpdateResult, error) {
	return m.lifecycle.Mutations.Update(ctx, serviceItem, item.DisplayName, item.Spec)
}

// reconcileNodeHealthOnce 标记心跳过期的 node offline，并同步更新匹配到的 deployment、service 和 latest execution 状态。
// 参数说明：ctx 控制本轮数据库访问。
func (m *Manager) reconcileNodeHealthOnce(ctx context.Context) error {
	// 心跳超时是 reconciler 的一个后台 loop，不单独抽薄 wrapper 包；store 只承载同事务状态更新 primitive。
	result, err := m.store.UpdateStaleNodeHeartbeatState(ctx, m.staleAfter)
	if err != nil {
		return err
	}
	// 只有本轮确实标记了 offline node 或影响 deployment 时才输出告警日志。
	if len(result.NodesMarkedOffline) > 0 || len(result.ImpactedDeployments) > 0 {
		// 只有实际产生状态变化时才输出 warn，避免正常空轮询污染日志。
		m.logger.Warn("cloud-plane reconciled stale node heartbeats", "nodes_offline", len(result.NodesMarkedOffline), "impacted_deployments", len(result.ImpactedDeployments))
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
