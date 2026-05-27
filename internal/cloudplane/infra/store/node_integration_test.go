package store_test

import (
	"context"
	"testing"
	"time"

	"mini-cloud/internal/cloudplane/domain/node"
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
