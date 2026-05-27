package nodepool

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	"mini-cloud/internal/cloudplane/domain/node"
	"mini-cloud/internal/cloudplane/domain/scheduler"
	"mini-cloud/internal/cloudplane/domain/workload"
	infraruntimepool "mini-cloud/internal/cloudplane/infra/runtimepool"
	"mini-cloud/internal/cloudplane/infra/store"
	"mini-cloud/internal/common/logctx"
)

const provisionedRuntimeNodeReadyTimeout = 10 * time.Minute

// ScaleOutService 负责在调度容量不足时创建新的 runtime node。
type ScaleOutService struct {
	// logger 记录 runtime node 扩容过程。
	logger *slog.Logger
	// store 提供 runtime node intent、node inventory 和调度状态持久化访问。
	store *store.Store
	// config 提供当前 cloud-plane 的 provider、region 和 node 命名前缀。
	config cloudplaneconfig.Config
	// driver 调用云厂商 API 创建 runtime node 对应的云实例。
	driver infraruntimepool.RuntimeDriver
}

// ScaleOutRequest 描述一次因调度失败触发的 runtime node 扩容请求。
type ScaleOutRequest struct {
	// Service 是正在调度的 service 持久化记录。
	Service workload.Service
	// Placement 是本次调度请求。
	Placement scheduler.PlacementRequest
	// PinnedToActiveNode 表示本次调度是否必须固定到已有活跃节点。
	PinnedToActiveNode bool
	// InitialFailure 是触发扩容前 scheduler 返回的失败原因。
	InitialFailure string
}

// ScaleOutResult 描述 runtime node 扩容后的重新调度输入和诊断信息。
type ScaleOutResult struct {
	// Nodes 是扩容后重新读取的最新 node 列表；为空表示没有可用于重新调度的新列表。
	Nodes []node.Node
	// Summary 是扩容成功或跳过时的用户可读摘要。
	Summary string
	// Failure 是扩容未能提供可用节点时的用户可读失败原因。
	Failure string
}

// NewScaleOutService 构造 runtime node 扩容控制服务。
// 参数说明：logger 记录后台日志；stores 提供本地状态访问；cfg 是有效配置；driver 负责云厂商 runtime node 生命周期操作。
func NewScaleOutService(logger *slog.Logger, stores *store.Store, cfg cloudplaneconfig.Config, driver infraruntimepool.RuntimeDriver) *ScaleOutService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ScaleOutService{logger: logger, store: stores, config: cfg, driver: driver}
}

// TryForPlacement 在现有节点无法放置时创建 runtime node，并返回重新调度用的最新节点列表。
// 参数说明：ctx 控制本次请求或后台操作生命周期；request 描述调度失败上下文和扩容输入。
func (s *ScaleOutService) TryForPlacement(ctx context.Context, request ScaleOutRequest) (ScaleOutResult, error) {
	serviceItem := request.Service
	placementRequest := request.Placement
	pinnedToActiveNode := request.PinnedToActiveNode
	initialFailure := request.InitialFailure
	// scale-out 只在 provider、region 匹配且本轮未被 pinnedToActiveNode 固定时执行。
	// 阶段一：再次读取节点并重新 Plan，避免在调用 runtime driver 前忽略刚释放出的容量。
	// 阶段二：创建 runtime node intent 后再调用 runtime driver，使失败也能在本地留下可观测状态。
	// 阶段三：runtime driver 完成云侧创建后等待 node-agent 注册 ready，再返回最新节点列表给 scheduler 重新 Plan。
	// 没有 runtime driver 时说明当前进程不能自动创建云侧 runtime node，直接跳过 scale-out。
	if s.driver == nil {
		return ScaleOutResult{}, nil
	}
	// 被固定到活跃节点时不能通过新增节点解决调度问题。
	if pinnedToActiveNode {
		return ScaleOutResult{}, nil
	}
	// 只允许为当前 plane 配置的 provider 做自动扩容。
	if !strings.EqualFold(strings.TrimSpace(s.config.Infrastructure.Provider), strings.TrimSpace(placementRequest.Provider)) {
		return ScaleOutResult{}, nil
	}
	// 只允许在当前 plane 配置的 region 内扩容，避免跨 region 创建不受控节点。
	if !strings.EqualFold(strings.TrimSpace(s.config.Infrastructure.Location.RegionID), strings.TrimSpace(serviceItem.Spec.Region)) {
		return ScaleOutResult{}, nil
	}

	// scale-out 的结果通过外部变量返回，便于统一返回给调度流程。
	var (
		scaledNodes     []node.Node
		scaleOutSummary string
		scaleOutFailure string
	)

	// runScaleOut 封装真正的扩容流程。
	runScaleOut := func() error {
		// 从 ctx 中取带业务字段的 logger，记录触发 scale-out 的原始调度失败原因。
		logger := logctx.Logger(ctx, s.logger)
		logger.Info("scheduler found no ready runtime node capacity, trying runtime scale-out",
			"provider", placementRequest.Provider,
			"region", serviceItem.Spec.Region,
			"failure_reason", initialFailure,
		)

		// 调用 provider 前重新读取节点并调度一次，避免容量刚释放却仍创建新节点。
		latestNodes, err := s.store.ListNodes(ctx)
		if err != nil {
			return err
		}
		latestPlan, err := scheduler.Plan(latestNodes, placementRequest)
		if err != nil {
			return err
		}
		// 如果最新节点已经能放下 workload，跳过云侧扩容并返回最新节点列表。
		if len(latestPlan.Decisions) > 0 {
			scaledNodes = latestNodes
			scaleOutSummary = "runtime scale-out was skipped because capacity became available before provider provisioning started"
			logger.Info("runtime scale-out skipped because fresh capacity check already found placement",
				"provider", placementRequest.Provider,
				"region", serviceItem.Spec.Region,
			)
			return nil
		}

		// 先创建本地 runtime node intent，让后续 provider 调用失败也有可观测记录。
		intent, err := s.store.CreateRuntimeNodeIntent(ctx, infraruntimepool.CreateIntentInput{
			Provider:      placementRequest.Provider,
			Region:        serviceItem.Spec.Region,
			InstanceName:  buildRuntimeNodeInstanceName(s.config.NodeAgent.Defaults.NodeNamePrefix, "pending"),
			InstanceType:  "",
			StatusReason:  "runtime node provisioning intent was recorded before calling the provider API",
			ProvisionedAt: time.Now().UTC(),
		})
		if err != nil {
			return err
		}

		// 后续日志和 store 写入都带上 runtime node id。
		mutationCtx := logctx.WithFields(ctx, logctx.Fields{RuntimeNodeID: intent.ID})
		logger = logctx.Logger(mutationCtx, s.logger)

		// 用 runtime node intent ID 生成最终实例名；runtime node 属于通用容量池，不绑定 service/deployment。
		instanceName := buildRuntimeNodeInstanceName(
			s.config.NodeAgent.Defaults.NodeNamePrefix,
			intent.ID,
		)
		// intent 创建时还没有最终 ID 衍生名称，这里回写最终实例名。
		if _, err := s.store.UpdateRuntimeNodeIntentInstanceName(mutationCtx, intent.ID, instanceName); err != nil {
			return err
		}

		// runtime driver 只接收创建云主机所需的名称、幂等 token 和资源需求；云资源 ownership 标签由 driver 统一生成。
		request := infraruntimepool.CreateRequest{
			Name:        instanceName,
			ClientToken: buildRuntimeNodeClientToken(intent.ID),
			CPUMilli:    placementRequest.CPUMilliRequest,
			MemoryMi:    placementRequest.MemoryMiRequest,
		}

		// 调用云厂商 driver 创建 runtime node；driver 内部负责 provider 专属 API。
		createdResult, err := s.driver.Create(mutationCtx, request)
		if err != nil {
			// provider 调用失败时，云侧最终状态可能未知，保持 provisioning 状态并更新原因供后续排查。
			if _, markErr := s.store.MarkRuntimeNodeProvisioningPending(mutationCtx, intent.ID, fmt.Sprintf("provider create call returned an error and the final instance state is unknown: %v", err), time.Now().UTC()); markErr != nil {
				logger.Warn("mark runtime node provisioning pending after provision error failed", "runtime_node_id", intent.ID, "error", markErr)
			}
			logger.Warn("runtime scale-out provisioning failed",
				"provider", placementRequest.Provider,
				"region", serviceItem.Spec.Region,
				"error", err,
			)
			scaleOutFailure = fmt.Sprintf("auto scale-out failed before scheduling could continue: %v", err)
			return nil
		}

		// provider 返回实例信息后，把本地 intent 绑定到真实云实例。
		createReason := fmt.Sprintf(
			"scheduler had no ready runtime node capacity in provider %s region %s, so cloud-plane created a new runtime node",
			placementRequest.Provider,
			serviceItem.Spec.Region,
		)
		_, err = s.store.BindRuntimeNodeProvisioned(
			mutationCtx,
			intent.ID,
			createdResult.InstanceID,
			createdResult.InstanceName,
			createdResult.InstanceType,
			createReason,
			time.Now().UTC(),
		)
		if err != nil {
			return err
		}
		// 等待新实例上的 node-agent 注册并进入 ready，否则本轮调度不能使用它。
		provisionedNode, err := s.waitForProvisionedNodeReady(mutationCtx, placementRequest.Provider, createdResult.InstanceID)
		if err != nil {
			logger.Warn("runtime scale-out runtime node did not become ready in time",
				"instance_id", createdResult.InstanceID,
				"error", err,
			)
			scaleOutFailure = fmt.Sprintf("auto scale-out created runtime node %s, but it did not register back as a ready node in time: %v", createdResult.InstanceID, err)
			return nil
		}

		// 节点 ready 后重新读取完整节点列表，让上层 scheduler 基于最新容量重新 Plan。
		scaledNodes, err = s.store.ListNodes(mutationCtx)
		if err != nil {
			return err
		}

		// 记录用户可读摘要，供调度仍失败时组合成诊断信息。
		scaleOutSummary = fmt.Sprintf(
			"auto scale-out created runtime node %s (%s) and it registered back as ready node %s",
			createdResult.InstanceName,
			createdResult.InstanceID,
			provisionedNode.Name,
		)
		logger.With("node_id", provisionedNode.ID).Info("runtime scale-out runtime node is ready",
			"instance_id", createdResult.InstanceID,
		)
		return nil
	}

	if err := runScaleOut(); err != nil {
		return ScaleOutResult{}, err
	}
	// 返回最新节点列表、扩容摘要和扩容失败原因；三者由调用方组合成调度结果。
	return ScaleOutResult{Nodes: scaledNodes, Summary: scaleOutSummary, Failure: scaleOutFailure}, nil
}

// waitForProvisionedNodeReady 等待新创建的 runtime node 完成 node-agent 注册并进入 ready。
// 参数说明：ctx 控制本次请求或后台操作生命周期；provider 是云厂商名称；instanceID 是云厂商实例 ID。
func (s *ScaleOutService) waitForProvisionedNodeReady(ctx context.Context, provider string, instanceID string) (node.Node, error) {
	// 为等待新节点 ready 设置总超时，避免 provider 创建成功但 agent 永不注册时阻塞调度循环。
	waitCtx, cancel := context.WithTimeout(ctx, provisionedRuntimeNodeReadyTimeout)
	defer cancel()

	// 使用固定轮询间隔查询本地 node 表；node-agent 注册成功后会写入 provider/instanceID。
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		// 按 provider + instanceID 查找 node-agent 上报的节点记录。
		item, err := s.store.GetNodeByProviderInstance(waitCtx, provider, instanceID)
		switch {
		case err == nil:
			// 只有 ready 且 schedulable 的节点才能返回给 scheduler 重新放置 workload。
			if item.Status == node.StatusReady && item.Schedulable {
				return item, nil
			}
		case errors.Is(err, store.ErrNodeNotFound):
			// node-agent 还没注册时继续等待下一轮轮询。
		default:
			// 其它 store 错误通常不是等待能解决的，直接返回给调用方。
			return node.Node{}, err
		}

		select {
		case <-waitCtx.Done():
			// 超时或上层取消时结束等待，错误信息中保留当前固定超时值。
			return node.Node{}, fmt.Errorf("timed out after %s", provisionedRuntimeNodeReadyTimeout)
		case <-ticker.C:
			// 到达下一次轮询周期后继续查询本地 node 表。
		}
	}
}

// buildRuntimeNodeInstanceName 根据前缀和 runtime node ID 生成云实例名称。
// 参数说明：prefix 是云资源命名前缀；runtimeNodeID 是 runtime node 唯一标识。
func buildRuntimeNodeInstanceName(prefix string, runtimeNodeID string) string {
	// 先清洗用户配置的前缀；为空时使用平台默认前缀。
	base := sanitizeRuntimeNodeNamePart(prefix)
	if base == "" {
		base = "mini-cloud-runtime-node"
	}

	// runtimeNodeID 生成稳定短 hash 并追加到名称末尾；名称较短时可降低冲突概率，截断后不应视为强唯一标识。
	hash := sha1.Sum([]byte(strings.TrimSpace(runtimeNodeID)))
	suffix := hex.EncodeToString(hash[:])[:10]
	name := base + "-" + suffix
	// 控制云资源名长度，截断后去掉边界连字符。
	if len(name) > 63 {
		name = strings.Trim(name[:63], "-")
	}
	// 返回前再次去掉首尾连字符，避免清洗/截断后形成非法边界字符。
	return strings.Trim(name, "-")
}

// buildRuntimeNodeClientToken 根据 runtime node ID 生成稳定的 provider 幂等 token。
// 参数说明：runtimeNodeID 是本地 runtime node intent 的唯一标识。
func buildRuntimeNodeClientToken(runtimeNodeID string) string {
	// client token 需要对同一个 runtime node intent 保持稳定，对不同 intent 尽量不同。
	hash := sha1.Sum([]byte(strings.TrimSpace(runtimeNodeID)))
	// 返回当前 provider driver 使用的短 token，前缀用于识别来源。
	return "mini-cloud-runtime-node-" + hex.EncodeToString(hash[:])[:32]
}

// sanitizeRuntimeNodeNamePart 将名称片段清洗为云实例名可复用的小写短横线格式。
// 参数说明：input 是待清洗的名称片段。
func sanitizeRuntimeNodeNamePart(input string) string {
	// 统一转小写并清理外围空白。
	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" {
		return ""
	}

	// 构造只包含小写字母、数字和单个连字符分隔的名称。
	var b strings.Builder
	lastHyphen := false
	for _, ch := range input {
		switch {
		case ch >= 'a' && ch <= 'z':
			// 字母原样写入，并重置连字符状态。
			b.WriteRune(ch)
			lastHyphen = false
		case ch >= '0' && ch <= '9':
			// 数字原样写入，并重置连字符状态。
			b.WriteRune(ch)
			lastHyphen = false
		default:
			// 其它字符折叠为单个连字符，且名称不能以连字符开头。
			if !lastHyphen && b.Len() > 0 {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}

	// 去掉尾部可能留下的连字符。
	return strings.Trim(b.String(), "-")
}
