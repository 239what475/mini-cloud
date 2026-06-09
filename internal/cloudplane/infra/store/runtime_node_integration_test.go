package store_test

import (
	"context"
	"testing"
	"time"

	"mini-cloud/internal/cloudplane/infra/runtimepool"
	cloudmodel "mini-cloud/internal/cloudplane/model"
	"mini-cloud/internal/testutil"
)

// TestIntegrationRuntimeNodeScaleInLifecycle 验证 runtime node 自动缩容所需的本地状态流转。
func TestIntegrationRuntimeNodeScaleInLifecycle(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)

	// 准备一台已经由 node-agent 注册并同步为 ready 的 runtime node。
	runtimeNode, backingNode := seedReadyRuntimeNode(t, ctx, db.Store, "scale-in-lifecycle", "i-scale-in-lifecycle")

	// 按缩容会关注的状态集合读取 runtime node；是否真正删除由 control 层结合 active execution 判断。
	candidates, err := db.Store.ListRuntimeNodesByStatuses(ctx, runtimepool.StatusReady, runtimepool.StatusTerminating)
	if err != nil {
		t.Fatalf("ListRuntimeNodesByStatuses returned error: %v", err)
	}
	if !containsRuntimeNode(candidates, runtimeNode.ID) {
		t.Fatalf("expected runtime node %s to be listed by status, got %+v", runtimeNode.ID, candidates)
	}

	// 进入 terminating 必须同时关闭 backing node 调度入口。
	terminating, changed, err := db.Store.MarkRuntimeNodeTerminating(ctx, runtimeNode.ID, "test terminating", time.Now().UTC())
	if err != nil {
		t.Fatalf("MarkRuntimeNodeTerminating returned error: %v", err)
	}
	if !changed {
		t.Fatalf("expected MarkRuntimeNodeTerminating to change ready runtime node")
	}
	if terminating.Status != runtimepool.StatusTerminating {
		t.Fatalf("runtime node status = %s, want %s", terminating.Status, runtimepool.StatusTerminating)
	}
	drainedNode, err := db.Store.GetNode(ctx, backingNode.ID)
	if err != nil {
		t.Fatalf("GetNode after terminating returned error: %v", err)
	}
	if drainedNode.Status != cloudmodel.StatusDraining || drainedNode.Schedulable {
		t.Fatalf("backing node after terminating = status %s schedulable %v, want draining false", drainedNode.Status, drainedNode.Schedulable)
	}
	// 如果进程在 provider 删除前重启，terminating 记录必须还能被后续轮次重新扫描并推进。
	terminatingCandidates, err := db.Store.ListRuntimeNodesByStatuses(ctx, runtimepool.StatusReady, runtimepool.StatusTerminating)
	if err != nil {
		t.Fatalf("ListRuntimeNodesByStatuses after terminating returned error: %v", err)
	}
	if !containsRuntimeNode(terminatingCandidates, runtimeNode.ID) {
		t.Fatalf("expected terminating runtime node %s to be listed for deletion recovery, got %+v", runtimeNode.ID, terminatingCandidates)
	}

	// 删除前的二次检查应看到 active execution 为 0。
	activeCount, err := db.Store.CountActiveExecutionsByNode(ctx, backingNode.ID)
	if err != nil {
		t.Fatalf("CountActiveExecutionsByNode returned error: %v", err)
	}
	if activeCount != 0 {
		t.Fatalf("active execution count = %d, want 0", activeCount)
	}

	deleted, err := db.Store.MarkRuntimeNodeDeleted(ctx, runtimeNode.ID, "test deleted", time.Now().UTC())
	if err != nil {
		t.Fatalf("MarkRuntimeNodeDeleted returned error: %v", err)
	}
	if deleted.Status != runtimepool.StatusDeleted {
		t.Fatalf("runtime node status = %s, want %s", deleted.Status, runtimepool.StatusDeleted)
	}
	offlineNode, err := db.Store.GetNode(ctx, backingNode.ID)
	if err != nil {
		t.Fatalf("GetNode after deleted returned error: %v", err)
	}
	if offlineNode.Status != cloudmodel.StatusOffline || offlineNode.Schedulable {
		t.Fatalf("backing node after delete = status %s schedulable %v, want offline false", offlineNode.Status, offlineNode.Schedulable)
	}

	// 删除后的最后几次 node-agent 心跳不能把 node 重新打开调度。
	if _, _, err := db.Store.RecordNodeHeartbeat(ctx, backingNode.ID, cloudmodel.HeartbeatInput{
		ReportedAt:          time.Now().UTC(),
		AgentVersion:        "test-agent",
		CPUMilliAllocatable: 1500,
		MemoryMiAllocatable: 3584,
		RunningContainers:   0,
		Status:              cloudmodel.StatusReady,
	}); err != nil {
		t.Fatalf("RecordNodeHeartbeat after deleted returned error: %v", err)
	}
	afterHeartbeat, err := db.Store.GetNode(ctx, backingNode.ID)
	if err != nil {
		t.Fatalf("GetNode after deleted heartbeat returned error: %v", err)
	}
	if afterHeartbeat.Status != cloudmodel.StatusOffline || afterHeartbeat.Schedulable {
		t.Fatalf("deleted runtime node heartbeat revived backing node: status %s schedulable %v", afterHeartbeat.Status, afterHeartbeat.Schedulable)
	}
}

// TestIntegrationRuntimeNodeScaleInSkipsActiveExecutions 验证 store 提供 active execution 计数供缩容策略判断。
func TestIntegrationRuntimeNodeScaleInSkipsActiveExecutions(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)

	// 准备一台 ready runtime node，并在其上种子一个 deploying execution。
	_, backingNode := seedReadyRuntimeNode(t, ctx, db.Store, "scale-in-active", "i-scale-in-active")
	seedActiveExecutionOnNode(t, ctx, db, backingNode.ID)

	activeCount, err := db.Store.CountActiveExecutionsByNode(ctx, backingNode.ID)
	if err != nil {
		t.Fatalf("CountActiveExecutionsByNode returned error: %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("active execution count = %d, want 1", activeCount)
	}

	// 是否因为 active execution 跳过缩容属于 reconciler 策略，store 只暴露计数 primitive。
}

// TestIntegrationRuntimeNodeScaleInSkipsUnsettledExecutionIntents 验证 store 提供 execution intent 状态存在性供缩容策略判断。
func TestIntegrationRuntimeNodeScaleInSkipsUnsettledExecutionIntents(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)

	// 准备 ready runtime node 和一个 pending execution intent；此时没有 active execution，但系统正在产生 workload。
	_, _ = seedReadyRuntimeNode(t, ctx, db.Store, "scale-in-unsettled", "i-scale-in-unsettled")
	seedPendingExecutionIntent(t, ctx, db)

	hasUnsettled, err := db.Store.HasExecutionIntentsWithStatuses(ctx, cloudmodel.StatusPending, cloudmodel.StatusDeploying)
	if err != nil {
		t.Fatalf("HasExecutionIntentsWithStatuses returned error: %v", err)
	}
	if !hasUnsettled {
		t.Fatal("expected pending execution intent to be visible through HasExecutionIntentsWithStatuses")
	}
}

// seedReadyRuntimeNode 创建一台本地 ready runtime node，供缩容 store 测试复用。
func seedReadyRuntimeNode(t *testing.T, ctx context.Context, store interface {
	CreateRuntimeNodeIntent(context.Context, runtimepool.CreateIntentInput) (runtimepool.Record, error)
	BindRuntimeNodeProvisioned(context.Context, string, string, string, string, string, time.Time) (runtimepool.Record, error)
	RegisterNode(context.Context, cloudmodel.RegisterInput) (cloudmodel.Node, error)
	RecordNodeHeartbeat(context.Context, string, cloudmodel.HeartbeatInput) (cloudmodel.HeartbeatSummary, time.Time, error)
	ListRuntimeNodesByStatuses(context.Context, ...string) ([]runtimepool.Record, error)
}, name string, instanceID string) (runtimepool.Record, cloudmodel.Node) {
	t.Helper()

	runtimeNode, err := store.CreateRuntimeNodeIntent(ctx, runtimepool.CreateIntentInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		InstanceName:  name,
		InstanceType:  "ecs.u1-c1m2.large",
		StatusReason:  "test runtime node",
		ProvisionedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("CreateRuntimeNodeIntent returned error: %v", err)
	}
	if _, err := store.BindRuntimeNodeProvisioned(
		ctx,
		runtimeNode.ID,
		instanceID,
		name,
		"ecs.u1-c1m2.large",
		"test runtime node provisioned",
		time.Now().UTC(),
	); err != nil {
		t.Fatalf("BindRuntimeNodeProvisioned returned error: %v", err)
	}

	backingNode, err := store.RegisterNode(ctx, cloudmodel.RegisterInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		Name:          name,
		PrivateIP:     "10.0.0.10",
		PublicIP:      "",
		InstanceID:    instanceID,
		InstanceType:  "ecs.u1-c1m2.large",
		CPUMilliTotal: 2000,
		MemoryMiTotal: 4096,
	})
	if err != nil {
		t.Fatalf("RegisterNode returned error: %v", err)
	}

	if _, _, err := store.RecordNodeHeartbeat(ctx, backingNode.ID, cloudmodel.HeartbeatInput{
		ReportedAt:          time.Now().UTC(),
		AgentVersion:        "test-agent",
		CPUMilliAllocatable: 1500,
		MemoryMiAllocatable: 3584,
		RunningContainers:   0,
		Status:              cloudmodel.StatusReady,
	}); err != nil {
		t.Fatalf("RecordNodeHeartbeat returned error: %v", err)
	}

	runtimeNodes, err := store.ListRuntimeNodesByStatuses(ctx, cloudmodel.StatusReady)
	if err != nil {
		t.Fatalf("ListRuntimeNodesByStatuses returned error: %v", err)
	}
	readyRuntimeNode, ok := findRuntimeNode(runtimeNodes, runtimeNode.ID)
	if !ok {
		t.Fatalf("runtime node %s missing from ListRuntimeNodes result: %+v", runtimeNode.ID, runtimeNodes)
	}
	if readyRuntimeNode.Status != cloudmodel.StatusReady || readyRuntimeNode.NodeID != backingNode.ID {
		t.Fatalf("runtime node after heartbeat = status %s nodeID %s, want ready %s", readyRuntimeNode.Status, readyRuntimeNode.NodeID, backingNode.ID)
	}
	return readyRuntimeNode, backingNode
}

// seedActiveExecutionOnNode 创建一个 deploying execution，用于验证 scale-in active 判断。
func seedActiveExecutionOnNode(t *testing.T, ctx context.Context, db testutil.TestDatabase, nodeID string) {
	t.Helper()

	if _, err := db.Store.ApplyExecutionPlan(ctx, cloudmodel.PlanInput{
		PlanID:            "svc-scale-in-active-g1",
		ServiceID:         "svc-scale-in-active",
		ServiceName:       "scale-in-active",
		ServiceGeneration: 1,
		Image:             "nginx:1.27-alpine",
		ContainerPort:     8080,
		ReadinessPath:     "/",
		InstanceClass:     "small",
		Exposure:          "public",
	}); err != nil {
		t.Fatalf("ApplyExecutionPlan(active) returned error: %v", err)
	}
	work, err := db.Store.CreateExecutionClaim(ctx, nodeID)
	if err != nil {
		t.Fatalf("CreateExecutionClaim(active) returned error: %v", err)
	}
	if work == nil {
		t.Fatal("CreateExecutionClaim(active) returned nil work item")
	}
}

// seedPendingExecutionIntent 创建一个 pending execution intent，表示系统仍在调度或发布 workload。
func seedPendingExecutionIntent(t *testing.T, ctx context.Context, db testutil.TestDatabase) {
	t.Helper()

	if _, err := db.Store.ApplyExecutionPlan(ctx, cloudmodel.PlanInput{
		PlanID:            "svc-scale-in-pending-g1",
		ServiceID:         "svc-scale-in-pending",
		ServiceName:       "scale-in-pending",
		ServiceGeneration: 1,
		Image:             "nginx:1.27-alpine",
		ContainerPort:     8080,
		ReadinessPath:     "/",
		InstanceClass:     "small",
		Exposure:          "public",
	}); err != nil {
		t.Fatalf("ApplyExecutionPlan(pending) returned error: %v", err)
	}
}

// containsRuntimeNode 判断列表中是否包含指定 runtime node ID。
func containsRuntimeNode(items []runtimepool.Record, id string) bool {
	_, ok := findRuntimeNode(items, id)
	return ok
}

// findRuntimeNode 从列表中取出指定 runtime node。
func findRuntimeNode(items []runtimepool.Record, id string) (runtimepool.Record, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return runtimepool.Record{}, false
}
