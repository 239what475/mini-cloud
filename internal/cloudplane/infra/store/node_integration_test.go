package store_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"mini-cloud/internal/cloudplane/domain/execution"
	"mini-cloud/internal/cloudplane/domain/node"
	"mini-cloud/internal/cloudplane/domain/workload"
	"mini-cloud/internal/testutil"
)

// TestIntegrationRecordNodeHeartbeatDoesNotInferAllocatedFromAllocatable 验证心跳不会用 allocatable 覆盖 allocated。
func TestIntegrationRecordNodeHeartbeatDoesNotInferAllocatedFromAllocatable(t *testing.T) {
	// 使用真实测试数据库验证 heartbeat 不会用 allocatable 反推 allocated。
	db := testutil.OpenCloudPlaneTestDatabase(t)

	// 注册一个 runtime node，初始 allocated 为 0。
	registered, err := db.Store.RegisterNode(context.Background(), node.RegisterInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		Name:          "runtime-node-a",
		Role:          node.RoleRuntime,
		PrivateIP:     "10.0.0.10",
		PublicIP:      "203.0.113.10",
		InstanceID:    "i-runtime-node-a",
		InstanceType:  "ecs.u1-c1m2.large",
		CPUMilliTotal: 2000,
		MemoryMiTotal: 4096,
	})
	if err != nil {
		t.Fatalf("RegisterNode returned error: %v", err)
	}

	// 手动种子已分配资源，模拟已有 placement/execution 占用。
	if _, err := db.DB.ExecContext(context.Background(), `
		UPDATE nodes
		SET cpu_milli_allocated = 600,
			memory_mi_allocated = 1024
		WHERE id = $1
	`, registered.ID); err != nil {
		t.Fatalf("seed reserved allocations: %v", err)
	}

	// 心跳只上报 allocatable，不应该覆盖已有 allocated。
	_, _, err = db.Store.RecordNodeHeartbeat(context.Background(), registered.ID, node.HeartbeatInput{
		ReportedAt:          time.Now().UTC(),
		AgentVersion:        "test-agent",
		CPUMilliAllocatable: 1500,
		MemoryMiAllocatable: 3584,
		RunningContainers:   0,
		Status:              node.StatusReady,
	})
	if err != nil {
		t.Fatalf("RecordNodeHeartbeat returned error: %v", err)
	}

	// 重新读取 node，分别断言 allocated 保持不变、allocatable 按心跳更新。
	refreshed, err := db.Store.GetNode(context.Background(), registered.ID)
	if err != nil {
		t.Fatalf("GetNode returned error: %v", err)
	}
	if refreshed.CPUMilliAllocated != 600 {
		t.Fatalf("cpu_milli_allocated = %d, want 600", refreshed.CPUMilliAllocated)
	}
	if refreshed.MemoryMiAllocated != 1024 {
		t.Fatalf("memory_mi_allocated = %d, want 1024", refreshed.MemoryMiAllocated)
	}
	if refreshed.CPUMilliAllocatable != 1500 {
		t.Fatalf("cpu_milli_allocatable = %d, want 1500", refreshed.CPUMilliAllocatable)
	}
	if refreshed.MemoryMiAllocatable != 3584 {
		t.Fatalf("memory_mi_allocatable = %d, want 3584", refreshed.MemoryMiAllocatable)
	}
	if refreshed.Status != node.StatusReady {
		t.Fatalf("status = %s, want %s", refreshed.Status, node.StatusReady)
	}
}

// TestIntegrationRecordNodeHeartbeatKeepsUpdatedAtStableWhenInventoryIsUnchanged 验证库存未变时 updated_at 不推进。
func TestIntegrationRecordNodeHeartbeatKeepsUpdatedAtStableWhenInventoryIsUnchanged(t *testing.T) {
	// 使用真实测试数据库验证库存字段不变时 node.updated_at 不推进。
	db := testutil.OpenCloudPlaneTestDatabase(t)

	// 注册一个 runtime node，作为两次 heartbeat 的目标。
	registered, err := db.Store.RegisterNode(context.Background(), node.RegisterInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		Name:          "runtime-node-stable",
		Role:          node.RoleRuntime,
		PrivateIP:     "10.0.0.11",
		PublicIP:      "203.0.113.11",
		InstanceID:    "i-runtime-node-stable",
		InstanceType:  "ecs.u1-c1m2.large",
		CPUMilliTotal: 2000,
		MemoryMiTotal: 4096,
	})
	if err != nil {
		t.Fatalf("RegisterNode returned error: %v", err)
	}

	// 第一次 heartbeat 建立 last_heartbeat_at 和 node 摘要。
	firstReportedAt := time.Now().UTC()
	if _, _, err := db.Store.RecordNodeHeartbeat(context.Background(), registered.ID, node.HeartbeatInput{
		ReportedAt:          firstReportedAt,
		AgentVersion:        "test-agent",
		CPUMilliAllocatable: 1500,
		MemoryMiAllocatable: 3584,
		RunningContainers:   1,
		Status:              node.StatusReady,
	}); err != nil {
		t.Fatalf("first RecordNodeHeartbeat returned error: %v", err)
	}

	// 保存第一次心跳后的 node 时间戳，用于和第二次比较。
	first, err := db.Store.GetNode(context.Background(), registered.ID)
	if err != nil {
		t.Fatalf("GetNode after first heartbeat returned error: %v", err)
	}

	// 确保 wall clock 有可观察差异；如果 updated_at 被错误刷新，测试能捕获。
	time.Sleep(10 * time.Millisecond)

	// 第二次 heartbeat 只推进 reportedAt，库存字段保持完全一致。
	if _, _, err := db.Store.RecordNodeHeartbeat(context.Background(), registered.ID, node.HeartbeatInput{
		ReportedAt:          firstReportedAt.Add(30 * time.Second),
		AgentVersion:        "test-agent",
		CPUMilliAllocatable: 1500,
		MemoryMiAllocatable: 3584,
		RunningContainers:   1,
		Status:              node.StatusReady,
	}); err != nil {
		t.Fatalf("second RecordNodeHeartbeat returned error: %v", err)
	}

	// 重新读取 node，用于断言 last_heartbeat_at 推进但 updated_at 不变。
	second, err := db.Store.GetNode(context.Background(), registered.ID)
	if err != nil {
		t.Fatalf("GetNode after second heartbeat returned error: %v", err)
	}

	// last_heartbeat_at 表示最新心跳时间，应随第二次心跳推进。
	if first.LastHeartbeatAt == nil || second.LastHeartbeatAt == nil {
		t.Fatalf("expected last_heartbeat_at to be populated, got first=%v second=%v", first.LastHeartbeatAt, second.LastHeartbeatAt)
	}
	if !second.LastHeartbeatAt.After(*first.LastHeartbeatAt) {
		t.Fatalf("last_heartbeat_at did not advance: first=%s second=%s", first.LastHeartbeatAt, second.LastHeartbeatAt)
	}
	// updated_at 代表 inventory 版本，库存未变时不能推进，避免 control-plane 误判 inventory 变化。
	if !second.UpdatedAt.Equal(first.UpdatedAt) {
		t.Fatalf("updated_at advanced even though inventory was unchanged: first=%s second=%s", first.UpdatedAt, second.UpdatedAt)
	}
}

// TestIntegrationStaleNodeHeartbeatFailsExecutionIntent 验证 node offline 链路会失败化 v8 execution intent，并通过 snapshot 暴露给 control-plane 聚合。
func TestIntegrationStaleNodeHeartbeatFailsExecutionIntent(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)

	registered, err := db.Store.RegisterNode(ctx, node.RegisterInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		Name:          "runtime-node-stale",
		Role:          node.RoleRuntime,
		PrivateIP:     "10.0.0.12",
		PublicIP:      "203.0.113.12",
		InstanceID:    "i-runtime-node-stale",
		InstanceType:  "ecs.u1-c1m2.large",
		CPUMilliTotal: 2000,
		MemoryMiTotal: 4096,
	})
	if err != nil {
		t.Fatalf("RegisterNode returned error: %v", err)
	}
	if _, _, err := db.Store.RecordNodeHeartbeat(ctx, registered.ID, node.HeartbeatInput{
		ReportedAt:          time.Now().UTC(),
		AgentVersion:        "test-agent",
		CPUMilliAllocatable: 1500,
		MemoryMiAllocatable: 3584,
		RunningContainers:   0,
		Status:              node.StatusReady,
	}); err != nil {
		t.Fatalf("RecordNodeHeartbeat returned error: %v", err)
	}

	if _, err := db.Store.ApplyExecutionPlan(ctx, execution.PlanInput{
		PlanID:            "svc-stale-g1",
		ServiceID:         "svc-stale",
		ServiceName:       "stale-web",
		ServiceGeneration: 1,
		Image:             "nginx:1.27-alpine",
		ContainerPort:     8080,
		ReadinessPath:     "/healthz",
		InstanceClass:     workload.InstanceClassSmall,
		Exposure:          "public",
	}); err != nil {
		t.Fatalf("ApplyExecutionPlan returned error: %v", err)
	}

	work, err := db.Store.CreateExecutionClaim(ctx, registered.ID)
	if err != nil {
		t.Fatalf("CreateExecutionClaim returned error: %v", err)
	}
	if work == nil {
		t.Fatal("CreateExecutionClaim returned nil work item")
	}
	if _, _, _, err := db.Store.UpdateExecutionFromNodeReport(ctx, registered.ID, work.ExecutionID, execution.ReportInput{
		Status:        execution.StatusRunning,
		Reason:        "replica is healthy",
		ContainerID:   "ctr-stale-0",
		ContainerName: work.ContainerName,
		HostPort:      18081,
	}); err != nil {
		t.Fatalf("UpdateExecutionFromNodeReport(running) returned error: %v", err)
	}

	runningNode, err := db.Store.GetNode(ctx, registered.ID)
	if err != nil {
		t.Fatalf("GetNode before stale reconcile returned error: %v", err)
	}
	if runningNode.CPUMilliAllocated == 0 || runningNode.MemoryMiAllocated == 0 {
		t.Fatalf("expected running execution to reserve node allocation, got cpu=%d memory=%d", runningNode.CPUMilliAllocated, runningNode.MemoryMiAllocated)
	}

	if _, err := db.DB.ExecContext(ctx, `
		UPDATE nodes
		SET last_heartbeat_at = $2
		WHERE id = $1
	`, registered.ID, time.Now().UTC().Add(-10*time.Minute)); err != nil {
		t.Fatalf("seed stale heartbeat timestamp: %v", err)
	}

	result, err := db.Store.UpdateStaleNodeHeartbeatState(ctx, time.Minute)
	if err != nil {
		t.Fatalf("UpdateStaleNodeHeartbeatState returned error: %v", err)
	}
	if len(result.NodesMarkedOffline) != 1 || result.NodesMarkedOffline[0].ID != registered.ID {
		t.Fatalf("NodesMarkedOffline = %+v, want node %s", result.NodesMarkedOffline, registered.ID)
	}
	if len(result.ImpactedDeployments) != 1 || result.ImpactedDeployments[0].DeploymentID != "svc-stale-g1" || result.ImpactedDeployments[0].ServiceID != "svc-stale" {
		t.Fatalf("ImpactedDeployments = %+v, want svc-stale-g1 impact", result.ImpactedDeployments)
	}
	if !strings.Contains(result.ImpactedDeployments[0].Reason, "marked offline") {
		t.Fatalf("impact reason = %q, want marked offline", result.ImpactedDeployments[0].Reason)
	}

	offlineNode, err := db.Store.GetNode(ctx, registered.ID)
	if err != nil {
		t.Fatalf("GetNode after stale reconcile returned error: %v", err)
	}
	if offlineNode.Status != node.StatusOffline || offlineNode.Schedulable {
		t.Fatalf("node after stale reconcile = status %s schedulable %v, want offline false", offlineNode.Status, offlineNode.Schedulable)
	}
	if offlineNode.CPUMilliAllocated != 0 || offlineNode.MemoryMiAllocated != 0 {
		t.Fatalf("node allocation after stale reconcile = cpu %d memory %d, want 0/0", offlineNode.CPUMilliAllocated, offlineNode.MemoryMiAllocated)
	}

	again, err := db.Store.UpdateStaleNodeHeartbeatState(ctx, time.Minute)
	if err != nil {
		t.Fatalf("UpdateStaleNodeHeartbeatState(second) returned error: %v", err)
	}
	if len(again.NodesMarkedOffline) != 0 || len(again.ImpactedDeployments) != 0 {
		t.Fatalf("second reconcile result = %+v, want no-op", again)
	}

	snapshots, err := db.Store.ListExecutionSnapshots(ctx)
	if err != nil {
		t.Fatalf("ListExecutionSnapshots returned error: %v", err)
	}
	var found bool
	for _, item := range snapshots {
		if item.PlanID != "svc-stale-g1" {
			continue
		}
		found = true
		if item.Status != execution.StatusFailed {
			t.Fatalf("execution snapshot = %+v, want failed status", item)
		}
		if !strings.Contains(item.LastStatusReason, "marked offline") {
			t.Fatalf("snapshot reason = %q, want marked offline", item.LastStatusReason)
		}
	}
	if !found {
		t.Fatalf("execution snapshot for svc-stale-g1 not found in %+v", snapshots)
	}
}
