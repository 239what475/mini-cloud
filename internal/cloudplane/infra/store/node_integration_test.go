package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	cloudplaneidentity "mini-cloud/internal/cloudplane/control/identity"
	"mini-cloud/internal/cloudplane/infra/store"
	cloudmodel "mini-cloud/internal/cloudplane/model"
	"mini-cloud/internal/testutil"
)

// TestIntegrationRecordNodeHeartbeatDoesNotInferAllocatedFromAllocatable 验证心跳不会用 allocatable 覆盖 allocated。
func TestIntegrationRecordNodeHeartbeatDoesNotInferAllocatedFromAllocatable(t *testing.T) {
	// 使用真实测试数据库验证 heartbeat 不会用 allocatable 反推 allocated。
	db := testutil.OpenCloudPlaneTestDatabase(t)

	// 注册一个 node，初始 allocated 为 0。
	registered, err := db.Store.RegisterNode(context.Background(), cloudmodel.RegisterInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		Name:          "node-a",
		PrivateIP:     "10.0.0.10",
		PublicIP:      "203.0.113.10",
		InstanceID:    "i-node-a",
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
	_, _, err = db.Store.RecordNodeHeartbeat(context.Background(), registered.ID, cloudmodel.HeartbeatInput{
		ReportedAt:          time.Now().UTC(),
		AgentVersion:        "test-agent",
		CPUMilliAllocatable: 1500,
		MemoryMiAllocatable: 3584,
		RunningContainers:   0,
		Status:              cloudmodel.StatusReady,
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
	if refreshed.Status != cloudmodel.StatusReady {
		t.Fatalf("status = %s, want %s", refreshed.Status, cloudmodel.StatusReady)
	}
}

// TestIntegrationRecordNodeHeartbeatKeepsUpdatedAtStableWhenInventoryIsUnchanged 验证库存未变时 updated_at 不推进。
func TestIntegrationRecordNodeHeartbeatKeepsUpdatedAtStableWhenInventoryIsUnchanged(t *testing.T) {
	// 使用真实测试数据库验证库存字段不变时 cloudmodel.updated_at 不推进。
	db := testutil.OpenCloudPlaneTestDatabase(t)

	// 注册一个 node，作为两次 heartbeat 的目标。
	registered, err := db.Store.RegisterNode(context.Background(), cloudmodel.RegisterInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		Name:          "node-stable",
		PrivateIP:     "10.0.0.11",
		PublicIP:      "203.0.113.11",
		InstanceID:    "i-node-stable",
		InstanceType:  "ecs.u1-c1m2.large",
		CPUMilliTotal: 2000,
		MemoryMiTotal: 4096,
	})
	if err != nil {
		t.Fatalf("RegisterNode returned error: %v", err)
	}

	// 第一次 heartbeat 建立 last_heartbeat_at 和 node 摘要。
	firstReportedAt := time.Now().UTC()
	if _, _, err := db.Store.RecordNodeHeartbeat(context.Background(), registered.ID, cloudmodel.HeartbeatInput{
		ReportedAt:          firstReportedAt,
		AgentVersion:        "test-agent",
		CPUMilliAllocatable: 1500,
		MemoryMiAllocatable: 3584,
		RunningContainers:   1,
		Status:              cloudmodel.StatusReady,
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
	if _, _, err := db.Store.RecordNodeHeartbeat(context.Background(), registered.ID, cloudmodel.HeartbeatInput{
		ReportedAt:          firstReportedAt.Add(30 * time.Second),
		AgentVersion:        "test-agent",
		CPUMilliAllocatable: 1500,
		MemoryMiAllocatable: 3584,
		RunningContainers:   1,
		Status:              cloudmodel.StatusReady,
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

	registered, err := db.Store.RegisterNode(ctx, cloudmodel.RegisterInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		Name:          "node-stale",
		PrivateIP:     "10.0.0.12",
		PublicIP:      "203.0.113.12",
		InstanceID:    "i-node-stale",
		InstanceType:  "ecs.u1-c1m2.large",
		CPUMilliTotal: 2000,
		MemoryMiTotal: 4096,
	})
	if err != nil {
		t.Fatalf("RegisterNode returned error: %v", err)
	}
	if _, _, err := db.Store.RecordNodeHeartbeat(ctx, registered.ID, cloudmodel.HeartbeatInput{
		ReportedAt:          time.Now().UTC(),
		AgentVersion:        "test-agent",
		CPUMilliAllocatable: 1500,
		MemoryMiAllocatable: 3584,
		RunningContainers:   0,
		Status:              cloudmodel.StatusReady,
	}); err != nil {
		t.Fatalf("RecordNodeHeartbeat returned error: %v", err)
	}

	if _, err := db.Store.ApplyExecutionPlan(ctx, cloudmodel.PlanInput{
		PlanID:            "svc-stale-g1",
		ServiceID:         "svc-stale",
		ServiceName:       "stale-web",
		ServiceGeneration: 1,
		Image:             "nginx:1.27-alpine",
		ContainerPort:     8080,
		ReadinessPath:     "/healthz",
		CPUMilliRequest:   500,
		MemoryMiRequest:   512,
		Exposure:          cloudmodel.ExposurePublic,
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
	if _, err := db.Store.UpdateExecutionFromNodeReport(ctx, registered.ID, work.ExecutionID, cloudmodel.ReportInput{
		Status:        cloudmodel.StatusRunning,
		Reason:        "execution is healthy",
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
	if len(result.ImpactedPlans) != 1 || result.ImpactedPlans[0].PlanID != "svc-stale-g1" || result.ImpactedPlans[0].ServiceID != "svc-stale" {
		t.Fatalf("ImpactedPlans = %+v, want svc-stale-g1 impact", result.ImpactedPlans)
	}
	if !strings.Contains(result.ImpactedPlans[0].Reason, "marked offline") {
		t.Fatalf("impact reason = %q, want marked offline", result.ImpactedPlans[0].Reason)
	}

	offlineNode, err := db.Store.GetNode(ctx, registered.ID)
	if err != nil {
		t.Fatalf("GetNode after stale reconcile returned error: %v", err)
	}
	if offlineNode.Status != cloudmodel.StatusOffline || offlineNode.Schedulable {
		t.Fatalf("node after stale reconcile = status %s schedulable %v, want offline false", offlineNode.Status, offlineNode.Schedulable)
	}
	if offlineNode.CPUMilliAllocated != 0 || offlineNode.MemoryMiAllocated != 0 {
		t.Fatalf("node allocation after stale reconcile = cpu %d memory %d, want 0/0", offlineNode.CPUMilliAllocated, offlineNode.MemoryMiAllocated)
	}

	again, err := db.Store.UpdateStaleNodeHeartbeatState(ctx, time.Minute)
	if err != nil {
		t.Fatalf("UpdateStaleNodeHeartbeatState(second) returned error: %v", err)
	}
	if len(again.NodesMarkedOffline) != 0 || len(again.ImpactedPlans) != 0 {
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
		if item.Status != cloudmodel.StatusFailed {
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

func TestIntegrationNodeScaleInLifecycle(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)

	node := seedReadyPoolNode(t, ctx, db.Store, "scale-in-lifecycle", "i-scale-in-lifecycle")

	candidates, err := db.Store.ListNodesByStatuses(ctx, cloudmodel.StatusReady, cloudmodel.StatusDraining)
	if err != nil {
		t.Fatalf("ListNodesByStatuses returned error: %v", err)
	}
	if !containsNode(candidates, node.ID) {
		t.Fatalf("expected node %s to be listed by status, got %+v", node.ID, candidates)
	}

	draining, changed, err := db.Store.MarkNodeDraining(ctx, node.ID, "test draining", time.Now().UTC())
	if err != nil {
		t.Fatalf("MarkNodeDraining returned error: %v", err)
	}
	if !changed {
		t.Fatalf("expected MarkNodeDraining to change ready node")
	}
	if draining.Status != cloudmodel.StatusDraining || draining.Schedulable {
		t.Fatalf("node after draining = status %s schedulable %v, want draining false", draining.Status, draining.Schedulable)
	}

	drainingCandidates, err := db.Store.ListNodesByStatuses(ctx, cloudmodel.StatusReady, cloudmodel.StatusDraining)
	if err != nil {
		t.Fatalf("ListNodesByStatuses after draining returned error: %v", err)
	}
	if !containsNode(drainingCandidates, node.ID) {
		t.Fatalf("expected draining node %s to be listed for deletion recovery, got %+v", node.ID, drainingCandidates)
	}

	deleted, err := db.Store.MarkNodeDeleted(ctx, node.ID, "test deleted", time.Now().UTC())
	if err != nil {
		t.Fatalf("MarkNodeDeleted returned error: %v", err)
	}
	if deleted.Status != cloudmodel.StatusDeleted || deleted.Schedulable {
		t.Fatalf("node after delete = status %s schedulable %v, want deleted false", deleted.Status, deleted.Schedulable)
	}

	if _, _, err := db.Store.RecordNodeHeartbeat(ctx, node.ID, cloudmodel.HeartbeatInput{
		ReportedAt:          time.Now().UTC(),
		AgentVersion:        "test-agent",
		CPUMilliAllocatable: 1500,
		MemoryMiAllocatable: 3584,
		RunningContainers:   0,
		Status:              cloudmodel.StatusReady,
	}); err != nil {
		t.Fatalf("RecordNodeHeartbeat after deleted returned error: %v", err)
	}
	afterHeartbeat, err := db.Store.GetNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("GetNode after deleted heartbeat returned error: %v", err)
	}
	if afterHeartbeat.Status != cloudmodel.StatusDeleted || afterHeartbeat.Schedulable {
		t.Fatalf("deleted node heartbeat revived node: status %s schedulable %v", afterHeartbeat.Status, afterHeartbeat.Schedulable)
	}
}

func TestIntegrationNodeScaleInSkipsActiveExecutions(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)

	node := seedReadyPoolNode(t, ctx, db.Store, "scale-in-active", "i-scale-in-active")
	seedActiveExecutionOnNode(t, ctx, db, node.ID)

	updated, changed, err := db.Store.MarkNodeDraining(ctx, node.ID, "test draining active node", time.Now().UTC())
	if err != nil {
		t.Fatalf("MarkNodeDraining returned error: %v", err)
	}
	if changed {
		t.Fatalf("MarkNodeDraining changed active node: %+v", updated)
	}
	if updated.Status != cloudmodel.StatusReady {
		t.Fatalf("node status = %s, want ready", updated.Status)
	}
}

func TestIntegrationNodeScaleInSkipsUnsettledExecutionIntents(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)

	_ = seedReadyPoolNode(t, ctx, db.Store, "scale-in-unsettled", "i-scale-in-unsettled")
	seedPendingExecutionIntent(t, ctx, db)

	hasUnsettled, err := db.Store.HasUnsettledExecutionIntents(ctx)
	if err != nil {
		t.Fatalf("HasUnsettledExecutionIntents returned error: %v", err)
	}
	if !hasUnsettled {
		t.Fatal("expected pending execution intent to be visible through HasUnsettledExecutionIntents")
	}
}

func TestIntegrationProvisioningNodeIsCompletedByAgentRegistration(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)

	provisioning, err := db.Store.CreateProvisioningNode(ctx, cloudmodel.ProvisioningInput{
		Provider:     "aliyun",
		Region:       "cn-beijing",
		Name:         "provisioning-node",
		InstanceType: "ecs.u1-c1m2.large",
		StatusReason: "test scale out",
	})
	if err != nil {
		t.Fatalf("CreateProvisioningNode returned error: %v", err)
	}
	if _, err := db.Store.BindProvisionedNode(ctx, provisioning.ID, "i-provisioning-node", "provisioning-node", "ecs.u1-c1m2.large", "provider accepted", time.Now().UTC()); err != nil {
		t.Fatalf("BindProvisionedNode returned error: %v", err)
	}

	registered, err := db.Store.RegisterNode(ctx, cloudmodel.RegisterInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		Name:          "provisioning-node",
		PrivateIP:     "10.0.0.10",
		PublicIP:      "",
		InstanceID:    "i-provisioning-node",
		InstanceType:  "ecs.u1-c1m2.large",
		CPUMilliTotal: 2000,
		MemoryMiTotal: 4096,
	})
	if err != nil {
		t.Fatalf("RegisterNode returned error: %v", err)
	}
	if registered.ID != provisioning.ID {
		t.Fatalf("RegisterNode created a new node %s, want existing provisioning node %s", registered.ID, provisioning.ID)
	}
	if registered.Status != cloudmodel.StatusRegistering {
		t.Fatalf("registered node status = %s, want registering", registered.Status)
	}
}

func TestIssueNodeAgentSessionTokenReplacesPreviousToken(t *testing.T) {
	db := testutil.OpenCloudPlaneTestDatabase(t)

	registered, err := db.Store.RegisterNode(context.Background(), cloudmodel.RegisterInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		Name:          "node-token-test",
		PrivateIP:     "10.0.0.21",
		InstanceID:    "i-node-token-test",
		InstanceType:  "ecs.u1-c1m1.large",
		CPUMilliTotal: 2000,
		MemoryMiTotal: 4096,
	})
	if err != nil {
		t.Fatalf("register node returned error: %v", err)
	}

	token1, err := cloudplaneidentity.NewService(db.Store).IssueNodeAgentSessionToken(context.Background(), registered.ID, time.Hour)
	if err != nil {
		t.Fatalf("issue first session token returned error: %v", err)
	}
	resolved1, err := cloudplaneidentity.NewService(db.Store).ResolveNodeAgentSessionTokenBySecret(context.Background(), token1)
	if err != nil {
		t.Fatalf("resolve first session token returned error: %v", err)
	}
	if resolved1 != registered.ID {
		t.Fatalf("resolved nodeID = %q, want %q", resolved1, registered.ID)
	}

	token2, err := cloudplaneidentity.NewService(db.Store).IssueNodeAgentSessionToken(context.Background(), registered.ID, time.Hour)
	if err != nil {
		t.Fatalf("issue second session token returned error: %v", err)
	}
	if token2 == token1 {
		t.Fatalf("expected reissued session token to change, got %q", token2)
	}

	_, err = cloudplaneidentity.NewService(db.Store).ResolveNodeAgentSessionTokenBySecret(context.Background(), token1)
	if !errors.Is(err, store.ErrNodeAgentSessionTokenNotFound) {
		t.Fatalf("resolve stale session token error = %v, want %v", err, store.ErrNodeAgentSessionTokenNotFound)
	}

	resolved2, err := cloudplaneidentity.NewService(db.Store).ResolveNodeAgentSessionTokenBySecret(context.Background(), token2)
	if err != nil {
		t.Fatalf("resolve second session token returned error: %v", err)
	}
	if resolved2 != registered.ID {
		t.Fatalf("resolved nodeID = %q, want %q", resolved2, registered.ID)
	}
}

func TestIssueNodeAgentSessionTokenExpires(t *testing.T) {
	db := testutil.OpenCloudPlaneTestDatabase(t)

	registered, err := db.Store.RegisterNode(context.Background(), cloudmodel.RegisterInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		Name:          "node-token-expiry-test",
		PrivateIP:     "10.0.0.22",
		InstanceID:    "i-node-token-expiry-test",
		InstanceType:  "ecs.u1-c1m1.large",
		CPUMilliTotal: 2000,
		MemoryMiTotal: 4096,
	})
	if err != nil {
		t.Fatalf("register node returned error: %v", err)
	}

	token, err := cloudplaneidentity.NewService(db.Store).IssueNodeAgentSessionToken(context.Background(), registered.ID, -time.Second)
	if err != nil {
		t.Fatalf("issue expiring session token returned error: %v", err)
	}

	_, err = cloudplaneidentity.NewService(db.Store).ResolveNodeAgentSessionTokenBySecret(context.Background(), token)
	if !errors.Is(err, store.ErrNodeAgentSessionTokenNotFound) {
		t.Fatalf("resolve expired session token error = %v, want %v", err, store.ErrNodeAgentSessionTokenNotFound)
	}
}

func seedReadyPoolNode(t *testing.T, ctx context.Context, stores interface {
	CreateProvisioningNode(context.Context, cloudmodel.ProvisioningInput) (cloudmodel.Node, error)
	BindProvisionedNode(context.Context, string, string, string, string, string, time.Time) (cloudmodel.Node, error)
	RegisterNode(context.Context, cloudmodel.RegisterInput) (cloudmodel.Node, error)
	RecordNodeHeartbeat(context.Context, string, cloudmodel.HeartbeatInput) (cloudmodel.HeartbeatSummary, time.Time, error)
}, name string, instanceID string) cloudmodel.Node {
	t.Helper()

	provisioning, err := stores.CreateProvisioningNode(ctx, cloudmodel.ProvisioningInput{
		Provider:     "aliyun",
		Region:       "cn-beijing",
		Name:         name,
		InstanceType: "ecs.u1-c1m2.large",
		StatusReason: "test node",
	})
	if err != nil {
		t.Fatalf("CreateProvisioningNode returned error: %v", err)
	}
	if _, err := stores.BindProvisionedNode(ctx, provisioning.ID, instanceID, name, "ecs.u1-c1m2.large", "test node provisioned", time.Now().UTC()); err != nil {
		t.Fatalf("BindProvisionedNode returned error: %v", err)
	}

	node, err := stores.RegisterNode(ctx, cloudmodel.RegisterInput{
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
	if node.ID != provisioning.ID {
		t.Fatalf("registered node ID = %s, want provisioning node ID %s", node.ID, provisioning.ID)
	}

	if _, _, err := stores.RecordNodeHeartbeat(ctx, node.ID, cloudmodel.HeartbeatInput{
		ReportedAt:          time.Now().UTC(),
		AgentVersion:        "test-agent",
		CPUMilliAllocatable: 1500,
		MemoryMiAllocatable: 3584,
		RunningContainers:   0,
		Status:              cloudmodel.StatusReady,
	}); err != nil {
		t.Fatalf("RecordNodeHeartbeat returned error: %v", err)
	}
	return node
}

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
		CPUMilliRequest:   500,
		MemoryMiRequest:   512,
		Exposure:          cloudmodel.ExposurePublic,
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
		CPUMilliRequest:   500,
		MemoryMiRequest:   512,
		Exposure:          cloudmodel.ExposurePublic,
	}); err != nil {
		t.Fatalf("ApplyExecutionPlan(pending) returned error: %v", err)
	}
}

func containsNode(items []cloudmodel.Node, id string) bool {
	for _, item := range items {
		if strings.TrimSpace(item.ID) == id {
			return true
		}
	}
	return false
}
