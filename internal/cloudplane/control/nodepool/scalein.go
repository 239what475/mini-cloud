// Package nodepool 承载 runtime node 池的调度期扩容和后台缩容控制用例。
package nodepool

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"mini-cloud/internal/cloudplane/domain/execution"
	infraruntimepool "mini-cloud/internal/cloudplane/infra/runtimepool"
	"mini-cloud/internal/cloudplane/infra/store"
)

// ScaleInService 负责回收没有 active execution 的弹性 runtime node。
type ScaleInService struct {
	// logger 记录 runtime node 缩容过程中的单节点失败。
	logger *slog.Logger
	// store 提供 runtime node 和 execution intent 的持久化访问。
	store *store.Store
	// driver 调用云厂商 API 删除 runtime node 对应的云实例。
	driver infraruntimepool.RuntimeDriver
}

// NewScaleInService 构造 runtime node 缩容控制服务。
// 参数说明：logger 记录后台日志；stores 提供本地状态访问；driver 负责云厂商 runtime node 生命周期操作。
func NewScaleInService(logger *slog.Logger, stores *store.Store, driver infraruntimepool.RuntimeDriver) *ScaleInService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ScaleInService{logger: logger, store: stores, driver: driver}
}

// ReconcileOnce 执行一轮 runtime node 自动缩容。
// 参数说明：ctx 控制本轮数据库访问、状态更新和 provider 删除调用。
func (s *ScaleInService) ReconcileOnce(ctx context.Context) error {
	// 如果仍有 execution intent 处在调度或启动过程中，说明系统容量正在被消费或即将被消费；
	// 本轮不缩容，避免删除刚为调度缺口创建、但还没来得及产生 execution 的 runtime node。
	unsettled, err := s.store.HasExecutionIntentsWithStatuses(
		ctx,
		execution.StatusPending,
		execution.StatusDeploying,
	)
	if err != nil {
		return err
	}
	if unsettled {
		return nil
	}

	// 查询 ready 节点和上轮已进入 drain/delete 的节点；具体删除候选条件由本层策略判断。
	items, err := s.store.ListRuntimeNodesByStatuses(ctx, infraruntimepool.StatusReady, infraruntimepool.StatusDraining, infraruntimepool.StatusDeleting)
	if err != nil {
		return err
	}

	var joinedErr error
	for _, item := range items {
		// 只有已经绑定云实例和 backing node 的 runtime node 才能走 provider 删除。
		if strings.TrimSpace(item.InstanceID) == "" || strings.TrimSpace(item.NodeID) == "" {
			continue
		}
		// active execution 是缩容硬保护条件；没有 active execution 的节点才进入删除状态机。
		activeCount, err := s.store.CountActiveExecutionsByNode(ctx, item.NodeID)
		if err != nil {
			return err
		}
		if activeCount > 0 {
			continue
		}
		// 单个节点失败只记录并继续处理其它候选；失败节点会通过 deleting 状态在后续轮次重试。
		if err := s.reconcileRuntimeNodeDeletion(ctx, item); err != nil {
			joinedErr = errors.Join(joinedErr, err)
			s.logger.Warn("runtime node scale-in failed",
				"runtime_node_id", item.ID,
				"instance_id", item.InstanceID,
				"node_id", item.NodeID,
				"error", err,
			)
		}
	}
	return joinedErr
}

// reconcileRuntimeNodeDeletion 推进单个 runtime node 的 drain/delete 状态机。
// 参数说明：ctx 控制本次操作；item 是本轮扫描得到的 runtime node 快照。
func (s *ScaleInService) reconcileRuntimeNodeDeletion(ctx context.Context, item infraruntimepool.Record) error {
	// 已经 deleted 的记录不会被扫描到；如果并发状态变化导致输入不再可删除，直接跳过。
	if item.Status != infraruntimepool.StatusReady && item.Status != infraruntimepool.StatusDraining && item.Status != infraruntimepool.StatusDeleting {
		return nil
	}
	if strings.TrimSpace(item.InstanceID) == "" || strings.TrimSpace(item.NodeID) == "" {
		return nil
	}

	current := item
	if item.Status == infraruntimepool.StatusReady {
		// 先进入 draining 并关闭 nodes.schedulable，阻断新调度和 node-agent 新 work 领取。
		reason := "runtime node has no active execution, so cloud-plane is draining it before provider deletion"
		drained, changed, err := s.store.MarkRuntimeNodeDraining(ctx, item.ID, reason, time.Now().UTC())
		if err != nil {
			return err
		}
		if !changed {
			return nil
		}
		current = drained

		// drain 后立即二次检查 active execution，处理和 node-agent 领取 work 并发的情况。
		activeCount, err := s.store.CountActiveExecutionsByNode(ctx, current.NodeID)
		if err != nil {
			return err
		}
		if activeCount > 0 {
			// 如果 drain 后发现仍有 active execution，恢复 ready，让该节点继续承载现有 workload。
			_, err := s.store.MarkRuntimeNodeReady(
				ctx,
				current.ID,
				fmt.Sprintf("runtime node scale-in was aborted because %d active execution(s) exist on the node", activeCount),
				time.Now().UTC(),
			)
			return err
		}
	}
	if item.Status == infraruntimepool.StatusDraining {
		// 进程可能在上一轮 drain 后、mark deleting 前重启；恢复后仍要先确认没有 active execution。
		activeCount, err := s.store.CountActiveExecutionsByNode(ctx, current.NodeID)
		if err != nil {
			return err
		}
		if activeCount > 0 {
			_, err := s.store.MarkRuntimeNodeReady(
				ctx,
				current.ID,
				fmt.Sprintf("runtime node scale-in was recovered from draining because %d active execution(s) exist on the node", activeCount),
				time.Now().UTC(),
			)
			return err
		}
	}

	// 进入 deleting 后，即使 provider 调用失败也保持 deleting，下一轮继续重试。
	deleting, err := s.store.MarkRuntimeNodeDeleting(
		ctx,
		current.ID,
		"runtime node has no active execution and cloud-plane is deleting the provider instance",
		time.Now().UTC(),
	)
	if err != nil {
		return err
	}

	if err := s.driver.Delete(ctx, infraruntimepool.DeleteRequest{InstanceID: deleting.InstanceID}); err != nil {
		// 删除失败不引入 delete_failed 状态；记录原因并依赖下一轮重试。
		_, markErr := s.store.MarkRuntimeNodeDeleting(
			ctx,
			deleting.ID,
			fmt.Sprintf("provider delete failed and will be retried: %v", err),
			time.Now().UTC(),
		)
		return errors.Join(err, markErr)
	}

	// provider 删除请求成功返回或返回 not found 后，本地状态收敛为 deleted，backing node 保留为 offline inventory。
	_, err = s.store.MarkRuntimeNodeDeleted(
		ctx,
		deleting.ID,
		"provider instance was deleted or was already absent",
		time.Now().UTC(),
	)
	return err
}
