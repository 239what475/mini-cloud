// Package scheduler 提供基于节点状态和剩余容量的 workload 放置计划算法。
package scheduler

import (
	"errors"
	"fmt"
	"time"

	"mini-cloud/internal/cloudplane/domain/node"
)

var (
	// ErrProviderRequired 表示调度请求缺少云厂商标识。
	ErrProviderRequired = errors.New("provider is required")
	// ErrRegionRequired 表示调度请求缺少目标地域。
	ErrRegionRequired = errors.New("region is required")
	// ErrInvalidCPUMilliReq 表示单副本 CPU 请求不是正数。
	ErrInvalidCPUMilliReq = errors.New("cpuMilliRequest must be greater than 0")
	// ErrInvalidMemoryMiReq 表示单副本内存请求不是正数。
	ErrInvalidMemoryMiReq = errors.New("memoryMiRequest must be greater than 0")
	// ErrInvalidReplicas 表示调度请求副本数不是正数。
	ErrInvalidReplicas = errors.New("replicas must be greater than 0")
)

// PlacementRequest 描述一次 deployment 副本放置请求。
type PlacementRequest struct {
	// DeploymentID 表示所属 deployment 的唯一标识。
	DeploymentID string `json:"deploymentID"`
	// Provider 表示云厂商标识。
	Provider string `json:"provider"`
	// Region 是本次放置要求匹配的目标地域。
	Region string `json:"region"`
	// CPUMilliRequest 记录 CPU 资源，单位为 millicore。
	CPUMilliRequest int `json:"cpuMilliRequest"`
	// MemoryMiRequest 记录内存资源，单位为 MiB。
	MemoryMiRequest int `json:"memoryMiRequest"`
	// Replicas 表示期望副本数。
	Replicas int `json:"replicas"`
}

// FilteredCounts 记录调度过程中各过滤阶段剔除的节点数量。
type FilteredCounts struct {
	// Provider 表示因 provider 不匹配被过滤掉的节点数量。
	Provider int `json:"provider"`
	// Role 表示因不是 runtime 角色被过滤掉的节点数量。
	Role int `json:"role"`
	// Status 表示因 node 状态不是 ready 被过滤掉的节点数量。
	Status int `json:"status"`
	// Schedulable 表示因不可调度被过滤掉的节点数量。
	Schedulable int `json:"schedulable"`
	// Region 表示因 region 不匹配被过滤掉的节点数量。
	Region int `json:"region"`
	// Capacity 表示因剩余 CPU 或内存不足被过滤掉的节点数量。
	Capacity int `json:"capacity"`
}

// PlacementDecision 表示一个副本最终选择的 node 和放置原因。
type PlacementDecision struct {
	// DeploymentID 表示所属 deployment 的唯一标识。
	DeploymentID string `json:"deploymentID"`
	// ReplicaIndex 表示副本序号，从 0 开始。
	ReplicaIndex int `json:"replicaIndex"`
	// NodeID 是该副本被放置到的目标 node 标识。
	NodeID string `json:"nodeID"`
	// Region 是目标 node 所在地域。
	Region string `json:"region"`
	// Score 表示本次放置后的资源评分，用于记录和展示；实际选点规则见 better。
	Score int64 `json:"score"`
	// Reason 说明该副本为何被放置到该 node。
	Reason string `json:"reason"`
}

// StoredDecision 是带 service、node 展示信息的已持久化放置决策。
type StoredDecision struct {
	// ID 是已持久化 selection decision 的唯一标识。
	ID string `json:"id"`
	// DeploymentID 表示所属 deployment 的唯一标识。
	DeploymentID string `json:"deploymentID"`
	// ServiceID 表示所属 service 的唯一标识。
	ServiceID string `json:"serviceID,omitempty"`
	// ServiceName 表示 service 名称。
	ServiceName string `json:"serviceName,omitempty"`
	// NodeID 是该 selection decision 绑定的目标 node 标识。
	NodeID string `json:"nodeID"`
	// NodeName 表示 node 名称。
	NodeName string `json:"nodeName"`
	// ReplicaIndex 表示副本序号，从 0 开始。
	ReplicaIndex int `json:"replicaIndex"`
	// Region 是目标 node 所在地域。
	Region string `json:"region"`
	// CPUMilliRequest 记录 CPU 资源，单位为 millicore。
	CPUMilliRequest int `json:"cpuMilliRequest"`
	// MemoryMiRequest 记录内存资源，单位为 MiB。
	MemoryMiRequest int `json:"memoryMiRequest"`
	// Replicas 是生成该放置决策时 deployment 请求的总副本数。
	Replicas int `json:"replicas"`
	// Score 表示本次放置后的资源评分，用于记录和展示；实际选点规则见 better。
	Score int64 `json:"score"`
	// Reason 是持久化的副本放置原因。
	Reason string `json:"reason"`
	// CreatedAt 是资源创建时间。
	CreatedAt time.Time `json:"createdAt"`
}

// PlanResult 汇总一次调度计划的成功决策或失败原因。
type PlanResult struct {
	// Decisions 表示调度器为每个副本生成的放置决策。
	Decisions []PlacementDecision `json:"decisions"`
	// FailureReason 表示调度或操作失败的可读原因。
	FailureReason string `json:"failureReason"`
	// FilteredCounts 表示每个调度过滤阶段剔除的节点数量。
	FilteredCounts FilteredCounts `json:"filteredCounts"`
}

// candidate 表示一个节点放置当前副本后的临时评分状态。
type candidate struct {
	// nodeID 表示 node 的唯一标识。
	nodeID string
	// unused 表示本次 plan 中还没有任何副本落到该节点；多副本优先分散到不同节点。
	unused bool
	// postPlacementCPU 是放置当前副本后节点剩余 CPU 毫核数。
	postPlacementCPU int
	// postPlacementMem 是放置当前副本后节点剩余内存 MiB 数。
	postPlacementMem int
	// score 是节点放置优先级分数。
	score int64
}

// Validate 校验放置请求是否满足 scheduler 输入约束。
func (r PlacementRequest) Validate() error {
	// provider 为空时无法确定候选节点所属云厂商。
	if r.Provider == "" {
		return ErrProviderRequired
	}
	// region 为空时无法执行地域过滤。
	if r.Region == "" {
		return ErrRegionRequired
	}
	// 单副本 CPU 请求必须为正数。
	if r.CPUMilliRequest <= 0 {
		return ErrInvalidCPUMilliReq
	}
	// 单副本内存请求必须为正数。
	if r.MemoryMiRequest <= 0 {
		return ErrInvalidMemoryMiReq
	}
	// 调度副本数必须为正数。
	if r.Replicas <= 0 {
		return ErrInvalidReplicas
	}
	// 调度请求满足当前 scheduler 的基础输入约束。
	return nil
}

// Plan 在一组节点里为请求的所有副本生成可解释的放置计划。
// 参数说明：nodes 是当前可参与调度判断的节点列表；request 是本次 deployment 副本放置请求。
func Plan(nodes []node.Node, request PlacementRequest) (PlanResult, error) {
	// 复杂流程说明：调度先逐层过滤 provider/role/status/schedulable/region，再按剩余容量选点。
	// 多副本会逐个扣减临时容量，确保同一次 plan 不会把多个副本塞爆同一节点。
	// 先校验请求本身；节点列表为空不是输入错误，而是返回可解释 failure reason。
	if err := request.Validate(); err != nil {
		return PlanResult{}, err
	}

	// result 用于累积过滤计数、成功决策或失败原因。
	result := PlanResult{}
	// eligible 保存通过 provider/role/status/schedulable/region 过滤的候选节点。
	eligible := make([]node.Node, 0, len(nodes))
	// 以下计数记录每一层过滤后还剩多少节点，用于构造失败原因。
	var providerMatched int
	var runtimeRoleMatched int
	var readyMatched int
	var schedulableMatched int
	var regionMatched int

	// 第一阶段逐层过滤节点，并记录各阶段被过滤的数量。
	for _, item := range nodes {
		// provider 不匹配的节点不属于本次调度目标云厂商。
		if item.Provider != request.Provider {
			result.FilteredCounts.Provider++
			continue
		}
		providerMatched++

		// 只有 runtime 角色节点承载用户 workload，platform 节点不参与调度。
		if item.ResolvedRole() != node.RoleRuntime {
			result.FilteredCounts.Role++
			continue
		}
		runtimeRoleMatched++

		// 只有 ready 节点参与调度。
		if item.Status != node.StatusReady {
			result.FilteredCounts.Status++
			continue
		}
		readyMatched++

		// schedulable=false 的节点即使 ready，也不接受新 placement。
		if !item.Schedulable {
			result.FilteredCounts.Schedulable++
			continue
		}
		schedulableMatched++

		// service region 必须与节点 region 精确匹配。
		if item.Region != request.Region {
			result.FilteredCounts.Region++
			continue
		}
		regionMatched++

		// 通过所有静态过滤的节点进入容量选择阶段。
		eligible = append(eligible, item)
	}

	// 没有候选节点时，根据过滤阶段命中情况返回最具体的失败原因。
	if len(eligible) == 0 {
		result.FailureReason = buildFailureReason(providerMatched, runtimeRoleMatched, readyMatched, schedulableMatched, regionMatched, request)
		return result, nil
	}

	// simulatedCPU/Mem 保存本次 plan 内的临时已分配量，不直接修改 node 对象或持久化状态。
	simulatedCPU := make(map[string]int, len(eligible))
	simulatedMem := make(map[string]int, len(eligible))
	usedInPlan := make(map[string]bool, len(eligible))
	for _, item := range eligible {
		// 初始值来自 cloud-plane 已持久化的 selection 分配量。
		simulatedCPU[item.ID] = item.CPUMilliAllocated
		simulatedMem[item.ID] = item.MemoryMiAllocated
	}

	// 第二阶段逐个副本选择节点，并在内存中扣减临时容量。
	result.Decisions = make([]PlacementDecision, 0, request.Replicas)
	for replicaIndex := 0; replicaIndex < request.Replicas; replicaIndex++ {
		// best 保存当前副本遍历到的最优候选节点。
		var best *candidate
		for _, item := range eligible {
			// 根据模拟已分配量计算当前节点剩余容量。
			remainingCPU := item.CPUMilliAllocatable - simulatedCPU[item.ID]
			remainingMem := item.MemoryMiAllocatable - simulatedMem[item.ID]
			// 当前节点无法容纳该副本时跳过。
			if remainingCPU < request.CPUMilliRequest || remainingMem < request.MemoryMiRequest {
				continue
			}

			// 计算放置当前副本后的剩余资源，用于打分。
			postCPU := remainingCPU - request.CPUMilliRequest
			postMem := remainingMem - request.MemoryMiRequest
			current := candidate{
				nodeID:           item.ID,
				unused:           !usedInPlan[item.ID],
				postPlacementCPU: postCPU,
				postPlacementMem: postMem,
				score:            score(postCPU, postMem),
			}
			// 如果当前节点比当前最佳节点更合适，就更新 best。
			if best == nil || better(current, *best) {
				copied := current
				best = &copied
			}
		}

		// 任一副本无法放置时，整个 plan 失败；不返回部分副本决策。
		if best == nil {
			result.Decisions = nil
			result.FilteredCounts.Capacity = len(eligible)
			// 单副本和多副本使用不同提示，便于区分单节点容量不足与总副本放置失败。
			if request.Replicas == 1 {
				result.FailureReason = "runtime nodes matched provider/region/status, but none had enough free cpu/memory"
			} else {
				result.FailureReason = "runtime nodes matched provider/region/status, but none had enough free cpu/memory to place all requested replicas"
			}
			return result, nil
		}

		// 在模拟容量中扣减本副本资源，影响后续副本选择。
		simulatedCPU[best.nodeID] += request.CPUMilliRequest
		simulatedMem[best.nodeID] += request.MemoryMiRequest
		usedInPlan[best.nodeID] = true
		// 记录当前副本的 selection decision。
		result.Decisions = append(result.Decisions, PlacementDecision{
			DeploymentID: request.DeploymentID,
			ReplicaIndex: replicaIndex,
			NodeID:       best.nodeID,
			Region:       request.Region,
			Score:        best.score,
			Reason: fmt.Sprintf(
				"placed replica %d on provider %s, region %s, runtime node with the highest remaining cpu/memory after this assignment",
				replicaIndex,
				request.Provider,
				request.Region,
			),
		})
	}

	// 所有副本都成功放置，返回 decisions 且 FailureReason 保持空。
	return result, nil
}

// buildFailureReason 根据各过滤阶段命中数量生成可解释的调度失败原因。
// 参数说明：providerMatched 是匹配 provider 的节点数；runtimeRoleMatched 是 runtime 角色节点数；readyMatched 是 ready 节点数；schedulableMatched 是允许调度的节点数；regionMatched 是匹配 region 的节点数；request 是本次放置请求。
func buildFailureReason(providerMatched int, runtimeRoleMatched int, readyMatched int, schedulableMatched int, regionMatched int, request PlacementRequest) string {
	// 按过滤阶段从前到后判断，返回最早导致候选集为空的原因。
	switch {
	case providerMatched == 0:
		return fmt.Sprintf("no nodes matched provider %s", request.Provider)
	case runtimeRoleMatched == 0:
		return "no runtime nodes were available after provider filtering"
	case readyMatched == 0:
		return "no ready runtime nodes were available after provider filtering"
	case schedulableMatched == 0:
		return "no schedulable runtime nodes were available after status filtering"
	case regionMatched == 0:
		return fmt.Sprintf("no runtime nodes matched requested region %s", request.Region)
	default:
		// 走到 default 表示静态条件都有命中，但容量阶段没有可用节点。
		return "runtime nodes matched provider/region/status, but none had enough free cpu/memory"
	}
}

// better 判断 current 是否比 best 更适合作为当前副本的放置节点。
// 参数说明：current 是当前待比较节点；best 是当前已选出的最佳节点。
func better(current candidate, best candidate) bool {
	// 多副本默认优先打散到尚未使用的节点；节点数量不足时才允许同一节点承载多个副本。
	if current.unused != best.unused {
		return current.unused
	}
	// 优先选择放置后 CPU 剩余更多的节点。
	if current.postPlacementCPU != best.postPlacementCPU {
		return current.postPlacementCPU > best.postPlacementCPU
	}
	// CPU 相同则选择内存剩余更多的节点。
	if current.postPlacementMem != best.postPlacementMem {
		return current.postPlacementMem > best.postPlacementMem
	}
	// CPU 和内存剩余都相同时按 nodeID 字典序打破平局，保证结果确定。
	return current.nodeID < best.nodeID
}

// score 根据放置后的剩余 CPU 和内存生成可比较的节点分数。
// 参数说明：postCPU 是放置后节点剩余 CPU 毫核数；postMem 是放置后节点剩余内存 MiB 数。
func score(postCPU int, postMem int) int64 {
	return int64(postCPU)*1_000_000 + int64(postMem)
}
