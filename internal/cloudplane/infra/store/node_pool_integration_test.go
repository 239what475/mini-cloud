package store_test

import (
	"context"
	"strings"
	"testing"
	"time"

	cloudmodel "mini-cloud/internal/cloudplane/model"
	"mini-cloud/internal/testutil"
)

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

func containsNode(items []cloudmodel.Node, id string) bool {
	for _, item := range items {
		if strings.TrimSpace(item.ID) == id {
			return true
		}
	}
	return false
}
