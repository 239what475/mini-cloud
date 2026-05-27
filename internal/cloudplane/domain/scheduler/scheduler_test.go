package scheduler

import (
	"testing"

	"mini-cloud/internal/cloudplane/domain/node"
)

// TestPlanSelectsOnlyReadyCandidate 验证调度器会选择唯一满足条件的 ready runtime node。
func TestPlanSelectsOnlyReadyCandidate(t *testing.T) {
	t.Parallel()

	// 准备一个满足 provider、region、ready、schedulable 和容量条件的 runtime node。
	result, err := Plan([]node.Node{
		makeNode("node_a", node.RoleRuntime, "aliyun", "cn-beijing", node.StatusReady, true, 2000, 4096, 0, 0),
	}, PlacementRequest{
		Provider:        "aliyun",
		Region:          "cn-beijing",
		CPUMilliRequest: 500,
		MemoryMiRequest: 512,
		Replicas:        1,
	})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	// 调度成功时应返回至少一个 decision，并且目标就是唯一候选节点。
	if len(result.Decisions) == 0 {
		t.Fatalf("expected decision, got failure: %s", result.FailureReason)
	}
	if result.Decisions[0].NodeID != "node_a" {
		t.Fatalf("expected node_a, got %s", result.Decisions[0].NodeID)
	}
}

// TestPlanFailsWhenRegionDoesNotMatch 验证 region 不匹配时调度失败并记录过滤计数。
func TestPlanFailsWhenRegionDoesNotMatch(t *testing.T) {
	t.Parallel()

	// 节点 provider 匹配但 region 不匹配，用来验证 region 过滤和失败原因。
	result, err := Plan([]node.Node{
		makeNode("node_a", node.RoleRuntime, "aliyun", "cn-hangzhou", node.StatusReady, true, 2000, 4096, 0, 0),
	}, PlacementRequest{
		Provider:        "aliyun",
		Region:          "cn-beijing",
		CPUMilliRequest: 500,
		MemoryMiRequest: 512,
		Replicas:        1,
	})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	// 不匹配 region 时不应产生调度决策。
	if len(result.Decisions) != 0 {
		t.Fatalf("expected no decision, got %+v", result.Decisions)
	}
	if result.FailureReason != "no runtime nodes matched requested region cn-beijing" {
		t.Fatalf("unexpected failure reason: %s", result.FailureReason)
	}
	// FilteredCounts.Region 应记录被 region 条件排除的节点数量。
	if result.FilteredCounts.Region != 1 {
		t.Fatalf("expected one region-filtered node, got %d", result.FilteredCounts.Region)
	}
}

// TestPlanFiltersNonReadyNodes 验证非 ready runtime node 会被状态过滤。
func TestPlanFiltersNonReadyNodes(t *testing.T) {
	t.Parallel()

	// 两个 runtime node 都不是 ready 状态，用来验证状态过滤。
	result, err := Plan([]node.Node{
		makeNode("node_a", node.RoleRuntime, "aliyun", "cn-beijing", node.StatusRegistering, true, 2000, 4096, 0, 0),
		makeNode("node_b", node.RoleRuntime, "aliyun", "cn-beijing", node.StatusNotReady, true, 2000, 4096, 0, 0),
	}, PlacementRequest{
		Provider:        "aliyun",
		Region:          "cn-beijing",
		CPUMilliRequest: 500,
		MemoryMiRequest: 512,
		Replicas:        1,
	})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	// 没有 ready node 时不应返回 decision，并应给出状态过滤原因。
	if len(result.Decisions) != 0 {
		t.Fatalf("expected no decision, got %+v", result.Decisions)
	}
	if result.FailureReason != "no ready runtime nodes were available after provider filtering" {
		t.Fatalf("unexpected failure reason: %s", result.FailureReason)
	}
	// 两个候选都应计入状态过滤计数。
	if result.FilteredCounts.Status != 2 {
		t.Fatalf("expected two status-filtered nodes, got %d", result.FilteredCounts.Status)
	}
}

// TestPlanFailsWhenCapacityIsInsufficient 验证剩余容量不足时调度失败。
func TestPlanFailsWhenCapacityIsInsufficient(t *testing.T) {
	t.Parallel()

	// 节点状态满足条件，但剩余 CPU/内存不足以容纳请求。
	result, err := Plan([]node.Node{
		makeNode("node_a", node.RoleRuntime, "aliyun", "cn-beijing", node.StatusReady, true, 1000, 1024, 800, 800),
	}, PlacementRequest{
		Provider:        "aliyun",
		Region:          "cn-beijing",
		CPUMilliRequest: 400,
		MemoryMiRequest: 400,
		Replicas:        1,
	})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	// 容量不足时不产生 decision，并返回容量相关失败原因。
	if len(result.Decisions) != 0 {
		t.Fatalf("expected no decision, got %+v", result.Decisions)
	}
	if result.FailureReason != "runtime nodes matched provider/region/status, but none had enough free cpu/memory" {
		t.Fatalf("unexpected failure reason: %s", result.FailureReason)
	}
	// 容量过滤计数应记录被资源不足排除的节点。
	if result.FilteredCounts.Capacity != 1 {
		t.Fatalf("expected one capacity-filtered node, got %d", result.FilteredCounts.Capacity)
	}
}

// TestPlanSelectsBestNodeDeterministically 验证多个候选节点下调度选择保持确定性。
func TestPlanSelectsBestNodeDeterministically(t *testing.T) {
	t.Parallel()

	// 准备多个可用节点，其中 node_b/node_c 剩余资源相同，用来验证确定性 tie-break。
	result, err := Plan([]node.Node{
		makeNode("node_a", node.RoleRuntime, "aliyun", "cn-beijing", node.StatusReady, true, 2000, 4096, 600, 1500),
		makeNode("node_b", node.RoleRuntime, "aliyun", "cn-beijing", node.StatusReady, true, 2000, 4096, 300, 800),
		makeNode("node_c", node.RoleRuntime, "aliyun", "cn-beijing", node.StatusReady, true, 2000, 4096, 300, 800),
	}, PlacementRequest{
		Provider:        "aliyun",
		Region:          "cn-beijing",
		CPUMilliRequest: 500,
		MemoryMiRequest: 512,
		Replicas:        1,
	})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	// 调度器应稳定选择排序更靠前的最佳节点。
	if len(result.Decisions) == 0 {
		t.Fatalf("expected decision, got failure: %s", result.FailureReason)
	}
	if result.Decisions[0].NodeID != "node_b" {
		t.Fatalf("expected node_b, got %s", result.Decisions[0].NodeID)
	}
}

// TestPlanFiltersUnschedulableNodes 验证 schedulable=false 的 node 不参与调度。
func TestPlanFiltersUnschedulableNodes(t *testing.T) {
	t.Parallel()

	// ready 但 schedulable=false 的节点不能承接新副本。
	result, err := Plan([]node.Node{
		makeNode("node_a", node.RoleRuntime, "aliyun", "cn-beijing", node.StatusReady, false, 2000, 4096, 0, 0),
	}, PlacementRequest{
		Provider:        "aliyun",
		Region:          "cn-beijing",
		CPUMilliRequest: 500,
		MemoryMiRequest: 512,
		Replicas:        1,
	})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	// 不可调度节点应被过滤，不产生 decision。
	if len(result.Decisions) != 0 {
		t.Fatalf("expected no decision, got %+v", result.Decisions)
	}
	if result.FailureReason != "no schedulable runtime nodes were available after status filtering" {
		t.Fatalf("unexpected failure reason: %s", result.FailureReason)
	}
	// schedulable 过滤计数应为 1。
	if result.FilteredCounts.Schedulable != 1 {
		t.Fatalf("expected one schedulable-filtered node, got %d", result.FilteredCounts.Schedulable)
	}
}

// TestPlanFiltersPlatformNodes 验证 platform node 不参与 workload runtime 调度。
func TestPlanFiltersPlatformNodes(t *testing.T) {
	t.Parallel()

	// platform node 不承担 workload runtime 调度，即使其它条件满足也必须过滤。
	result, err := Plan([]node.Node{
		makeNode("node_platform", node.RolePlatform, "aliyun", "cn-beijing", node.StatusReady, true, 2000, 4096, 0, 0),
	}, PlacementRequest{
		Provider:        "aliyun",
		Region:          "cn-beijing",
		CPUMilliRequest: 500,
		MemoryMiRequest: 512,
		Replicas:        1,
	})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	// 非 runtime role 不应产生 selection decision。
	if len(result.Decisions) != 0 {
		t.Fatalf("expected no decision, got %+v", result.Decisions)
	}
	if result.FailureReason != "no runtime nodes were available after provider filtering" {
		t.Fatalf("unexpected failure reason: %s", result.FailureReason)
	}
	// role 过滤计数应记录 platform node。
	if result.FilteredCounts.Role != 1 {
		t.Fatalf("expected one role-filtered node, got %d", result.FilteredCounts.Role)
	}
}

// TestPlanDistributesMultipleReplicasWithinCapacity 验证多副本调度会按临时容量分布到多个节点。
func TestPlanDistributesMultipleReplicasWithinCapacity(t *testing.T) {
	t.Parallel()

	// 两个 runtime node 的总容量足以承载三个副本，用来验证多副本分配会更新临时容量。
	result, err := Plan([]node.Node{
		makeNode("node_a", node.RoleRuntime, "aliyun", "cn-beijing", node.StatusReady, true, 2000, 4096, 0, 0),
		makeNode("node_b", node.RoleRuntime, "aliyun", "cn-beijing", node.StatusReady, true, 2000, 4096, 0, 0),
	}, PlacementRequest{
		Provider:        "aliyun",
		Region:          "cn-beijing",
		CPUMilliRequest: 1000,
		MemoryMiRequest: 1024,
		Replicas:        3,
	})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	// 三个副本都应成功产生 selection decision。
	if result.FailureReason != "" {
		t.Fatalf("Plan failure = %q, want success", result.FailureReason)
	}
	if len(result.Decisions) != 3 {
		t.Fatalf("expected 3 selection decisions, got %d", len(result.Decisions))
	}

	// 统计每个节点获得的副本数，同时确认 replicaIndex 按请求顺序生成。
	counts := map[string]int{}
	for idx, item := range result.Decisions {
		if item.ReplicaIndex != idx {
			t.Fatalf("decision[%d].ReplicaIndex = %d, want %d", idx, item.ReplicaIndex, idx)
		}
		counts[item.NodeID]++
	}
	if counts["node_a"] == 0 || counts["node_b"] == 0 {
		t.Fatalf("expected placements to use both nodes, got %+v", counts)
	}
}

// makeNode 构造 scheduler 单测使用的 node 对象。
func makeNode(id string, role string, provider string, region string, status string, schedulable bool, cpuCapacity int, memoryCapacity int, cpuAllocated int, memoryAllocated int) node.Node {
	// 测试 helper 使用同一个值填充 total 和 allocatable，便于测试只关注 scheduler 过滤逻辑。
	return node.Node{
		// 身份、角色、provider、region 和状态字段来自各测试用例参数。
		ID:                  id,
		Role:                role,
		Provider:            provider,
		Region:              region,
		Status:              status,
		Schedulable:         schedulable,
		CPUMilliTotal:       cpuCapacity,
		MemoryMiTotal:       memoryCapacity,
		CPUMilliAllocatable: cpuCapacity,
		MemoryMiAllocatable: memoryCapacity,
		CPUMilliAllocated:   cpuAllocated,
		MemoryMiAllocated:   memoryAllocated,
	}
}
