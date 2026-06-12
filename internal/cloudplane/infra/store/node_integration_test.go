package store_test

import (
	"context"
	"strings"
	"testing"
	"time"

	cloudmodel "mini-cloud/internal/cloudplane/model"
	"mini-cloud/internal/testutil"
)

func TestIntegrationRecordNodeHeartbeatDoesNotInferAllocatedFromAllocatable(t *testing.T) {
	db := testutil.OpenCloudPlaneTestDatabase(t)

	registered, err := db.Store.RegisterNode(context.Background(), cloudmodel.RegisterInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		Name:          "node-a",
		PrivateIP:     "10.0.0.10",
		InstanceID:    "i-node-a",
		InstanceType:  "ecs.u1-c1m2.large",
		CPUMilliTotal: 2000,
		MemoryMiTotal: 4096,
	})
	if err != nil {
		t.Fatalf("RegisterNode returned error: %v", err)
	}

	if _, err := db.DB.ExecContext(context.Background(), `
		UPDATE nodes
		SET cpu_milli_allocated = 600,
			memory_mi_allocated = 1024
		WHERE id = $1
	`, registered.ID); err != nil {
		t.Fatalf("seed reserved allocations: %v", err)
	}
	_, err = db.Store.RecordNodeHeartbeat(context.Background(), registered.ID, cloudmodel.HeartbeatInput{
		CPUMilliAllocatable: 1500,
		MemoryMiAllocatable: 3584,
	})
	if err != nil {
		t.Fatalf("RecordNodeHeartbeat returned error: %v", err)
	}

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

func TestIntegrationRecordNodeHeartbeatKeepsUpdatedAtStableWhenInventoryIsUnchanged(t *testing.T) {
	db := testutil.OpenCloudPlaneTestDatabase(t)

	registered, err := db.Store.RegisterNode(context.Background(), cloudmodel.RegisterInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		Name:          "node-stable",
		PrivateIP:     "10.0.0.11",
		InstanceID:    "i-node-stable",
		InstanceType:  "ecs.u1-c1m2.large",
		CPUMilliTotal: 2000,
		MemoryMiTotal: 4096,
	})
	if err != nil {
		t.Fatalf("RegisterNode returned error: %v", err)
	}
	if _, err := db.Store.RecordNodeHeartbeat(context.Background(), registered.ID, cloudmodel.HeartbeatInput{
		CPUMilliAllocatable: 1500,
		MemoryMiAllocatable: 3584,
	}); err != nil {
		t.Fatalf("first RecordNodeHeartbeat returned error: %v", err)
	}

	first, err := db.Store.GetNode(context.Background(), registered.ID)
	if err != nil {
		t.Fatalf("GetNode after first heartbeat returned error: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	if _, err := db.Store.RecordNodeHeartbeat(context.Background(), registered.ID, cloudmodel.HeartbeatInput{
		CPUMilliAllocatable: 1500,
		MemoryMiAllocatable: 3584,
	}); err != nil {
		t.Fatalf("second RecordNodeHeartbeat returned error: %v", err)
	}

	second, err := db.Store.GetNode(context.Background(), registered.ID)
	if err != nil {
		t.Fatalf("GetNode after second heartbeat returned error: %v", err)
	}
	if first.LastHeartbeatAt == nil || second.LastHeartbeatAt == nil {
		t.Fatalf("expected last_heartbeat_at to be populated, got first=%v second=%v", first.LastHeartbeatAt, second.LastHeartbeatAt)
	}
	if !second.LastHeartbeatAt.After(*first.LastHeartbeatAt) {
		t.Fatalf("last_heartbeat_at did not advance: first=%s second=%s", first.LastHeartbeatAt, second.LastHeartbeatAt)
	}
	if !second.UpdatedAt.Equal(first.UpdatedAt) {
		t.Fatalf("updated_at advanced even though inventory was unchanged: first=%s second=%s", first.UpdatedAt, second.UpdatedAt)
	}
}

func TestIntegrationStaleNodeHeartbeatFailsExecutionIntent(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)

	registered, err := db.Store.RegisterNode(ctx, cloudmodel.RegisterInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		Name:          "node-stale",
		PrivateIP:     "10.0.0.12",
		InstanceID:    "i-node-stale",
		InstanceType:  "ecs.u1-c1m2.large",
		CPUMilliTotal: 2000,
		MemoryMiTotal: 4096,
	})
	if err != nil {
		t.Fatalf("RegisterNode returned error: %v", err)
	}
	if _, err := db.Store.RecordNodeHeartbeat(ctx, registered.ID, cloudmodel.HeartbeatInput{
		CPUMilliAllocatable: 1500,
		MemoryMiAllocatable: 3584,
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

	result, err := db.Store.MarkStaleNodeHeartbeatsOffline(ctx, time.Minute)
	if err != nil {
		t.Fatalf("MarkStaleNodeHeartbeatsOffline returned error: %v", err)
	}
	if result.OfflineNodes != 1 || result.FailedExecutions != 1 {
		t.Fatalf("stale heartbeat result = %+v, want one offline node and one failed execution", result)
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

	again, err := db.Store.MarkStaleNodeHeartbeatsOffline(ctx, time.Minute)
	if err != nil {
		t.Fatalf("MarkStaleNodeHeartbeatsOffline(second) returned error: %v", err)
	}
	if again.OfflineNodes != 0 || again.FailedExecutions != 0 {
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

	node := seedReadyElasticNode(t, ctx, db.Store, "scale-in-lifecycle", "i-scale-in-lifecycle")

	draining, deletable, err := db.Store.PrepareNodeDeletion(ctx, node.ID, "test draining", time.Now().UTC())
	if err != nil {
		t.Fatalf("PrepareNodeDeletion returned error: %v", err)
	}
	if !deletable {
		t.Fatalf("expected ready node to be deletable")
	}
	if draining.Status != cloudmodel.StatusDraining || draining.Schedulable {
		t.Fatalf("node after draining = status %s schedulable %v, want draining false", draining.Status, draining.Schedulable)
	}

	refreshed, err := db.Store.GetNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("GetNode after PrepareNodeDeletion returned error: %v", err)
	}
	if refreshed.Status != cloudmodel.StatusDraining {
		t.Fatalf("node status after PrepareNodeDeletion = %s, want draining", refreshed.Status)
	}

	deleted, err := db.Store.MarkNodeDeleted(ctx, node.ID, "test deleted", time.Now().UTC())
	if err != nil {
		t.Fatalf("MarkNodeDeleted returned error: %v", err)
	}
	if deleted.Status != cloudmodel.StatusDeleted || deleted.Schedulable {
		t.Fatalf("node after delete = status %s schedulable %v, want deleted false", deleted.Status, deleted.Schedulable)
	}

	if _, err := db.Store.RecordNodeHeartbeat(ctx, node.ID, cloudmodel.HeartbeatInput{
		CPUMilliAllocatable: 1500,
		MemoryMiAllocatable: 3584,
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

	node := seedReadyElasticNode(t, ctx, db.Store, "scale-in-active", "i-scale-in-active")
	seedActiveExecutionOnNode(t, ctx, db, node.ID)

	updated, deletable, err := db.Store.PrepareNodeDeletion(ctx, node.ID, "test draining active node", time.Now().UTC())
	if err != nil {
		t.Fatalf("PrepareNodeDeletion returned error: %v", err)
	}
	if deletable {
		t.Fatalf("PrepareNodeDeletion allowed active node deletion: %+v", updated)
	}
	if updated.Status != cloudmodel.StatusReady {
		t.Fatalf("node status = %s, want ready", updated.Status)
	}
}

func TestIntegrationNodeScaleInSkipsUnsettledExecutionIntents(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)

	_ = seedReadyElasticNode(t, ctx, db.Store, "scale-in-unsettled", "i-scale-in-unsettled")
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

func seedReadyElasticNode(t *testing.T, ctx context.Context, stores interface {
	CreateProvisioningNode(context.Context, cloudmodel.ProvisioningInput) (cloudmodel.Node, error)
	BindProvisionedNode(context.Context, string, string, string, string, string, time.Time) (cloudmodel.Node, error)
	RegisterNode(context.Context, cloudmodel.RegisterInput) (cloudmodel.Node, error)
	RecordNodeHeartbeat(context.Context, string, cloudmodel.HeartbeatInput) (string, error)
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

	if _, err := stores.RecordNodeHeartbeat(ctx, node.ID, cloudmodel.HeartbeatInput{
		CPUMilliAllocatable: 1500,
		MemoryMiAllocatable: 3584,
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
