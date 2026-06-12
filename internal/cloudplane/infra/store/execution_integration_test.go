package store_test

import (
	"context"
	"testing"

	cloudmodel "mini-cloud/internal/cloudplane/model"
	"mini-cloud/internal/testutil"
)

func TestIntegrationCreateExecutionClaimUsesPlanWorkloadInputs(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)
	node := seedReadyNode(t, ctx, db, "node-plan-inputs", "i-node-plan-inputs")

	upsertTestService(t, ctx, db, testServiceInput{
		ID:         "svc-demo",
		Name:       "demo-web",
		Generation: 1,
		Image:      "registry.example.com/demo:v1",
		Env: map[string]string{
			"SERVICE_MODE": "plan-v1",
			"LOG_LEVEL":    "debug",
		},
	})

	work, err := db.Store.CreateExecutionClaim(ctx, node.ID)
	if err != nil {
		t.Fatalf("CreateExecutionClaim returned error: %v", err)
	}
	if work == nil {
		t.Fatal("CreateExecutionClaim returned nil work item")
	}
	if work.IntentKey != "svc-demo-g1" {
		t.Fatalf("work intent key = %q, want svc-demo-g1", work.IntentKey)
	}
	if work.Env["SERVICE_MODE"] != "plan-v1" || work.Env["LOG_LEVEL"] != "debug" {
		t.Fatalf("work env = %+v, want execution intent env", work.Env)
	}
}

func TestIntegrationReplacementRunStopsCurrentContainerBeforeStartingNewRun(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)
	currentNode := seedReadyNode(t, ctx, db, "node-replace-current", "i-node-replace-current")
	otherNode := seedReadyNode(t, ctx, db, "node-replace-other", "i-node-replace-other")

	upsertTestService(t, ctx, db, testServiceInput{
		ID:         "svc-replace",
		Name:       "replace-web",
		Generation: 1,
		Image:      "nginx:1.27-alpine",
	})
	firstWork, err := db.Store.CreateExecutionClaim(ctx, currentNode.ID)
	if err != nil {
		t.Fatalf("CreateExecutionClaim(first) returned error: %v", err)
	}
	if firstWork == nil {
		t.Fatal("CreateExecutionClaim(first) returned nil work item")
	}
	if _, err := db.Store.UpdateExecutionFromNodeReport(ctx, currentNode.ID, firstWork.ExecutionID, cloudmodel.ReportInput{
		Status:        cloudmodel.StatusRunning,
		Reason:        "execution is healthy",
		ContainerID:   "ctr-replace-v1",
		ContainerName: firstWork.ContainerName,
		HostPort:      18080,
	}); err != nil {
		t.Fatalf("UpdateExecutionFromNodeReport(first running) returned error: %v", err)
	}

	upsertTestService(t, ctx, db, testServiceInput{
		ID:         "svc-replace",
		Name:       "replace-web",
		Generation: 2,
		Image:      "nginx:1.28-alpine",
	})

	otherWork, err := db.Store.CreateExecutionClaim(ctx, otherNode.ID)
	if err != nil {
		t.Fatalf("CreateExecutionClaim(other node) returned error: %v", err)
	}
	if otherWork != nil {
		t.Fatalf("other node claimed replacement work: %+v", otherWork)
	}

	deleteWork, err := db.Store.CreateExecutionClaim(ctx, currentNode.ID)
	if err != nil {
		t.Fatalf("CreateExecutionClaim(delete old run) returned error: %v", err)
	}
	if deleteWork == nil {
		t.Fatal("CreateExecutionClaim(delete old run) returned nil work item")
	}
	if deleteWork.Action != cloudmodel.WorkActionDelete || deleteWork.ContainerID != "ctr-replace-v1" {
		t.Fatalf("delete old run work = %+v, want delete work for old container", deleteWork)
	}
	if _, err := db.Store.UpdateExecutionFromNodeReport(ctx, currentNode.ID, deleteWork.ExecutionID, cloudmodel.ReportInput{
		Status:        cloudmodel.StatusSucceeded,
		Reason:        "replacement stopped previous container",
		ContainerID:   deleteWork.ContainerID,
		ContainerName: deleteWork.ContainerName,
		HostPort:      deleteWork.HostPort,
	}); err != nil {
		t.Fatalf("UpdateExecutionFromNodeReport(delete old run) returned error: %v", err)
	}

	replacementWork, err := db.Store.CreateExecutionClaim(ctx, currentNode.ID)
	if err != nil {
		t.Fatalf("CreateExecutionClaim(new run) returned error: %v", err)
	}
	if replacementWork == nil {
		t.Fatal("CreateExecutionClaim(new run) returned nil work item")
	}
	if replacementWork.Action != cloudmodel.WorkActionRun || replacementWork.IntentKey != "svc-replace-g2" {
		t.Fatalf("replacement work = %+v, want run work for svc-replace-g2", replacementWork)
	}
	snapshots, err := db.Store.ListExecutionSnapshots(ctx)
	if err != nil {
		t.Fatalf("ListExecutionSnapshots returned error: %v", err)
	}
	oldDeleteSnapshot := findExecutionSnapshot(snapshots, "svc-replace-stop-before-g2")
	if oldDeleteSnapshot == nil || oldDeleteSnapshot.Status != cloudmodel.StatusSucceeded {
		t.Fatalf("old delete snapshot = %+v, want succeeded", oldDeleteSnapshot)
	}
}

func TestIntegrationDeleteExecutionIntentClaimsRunningIntentAndReportsSnapshot(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)
	node := seedReadyNode(t, ctx, db, "node-delete", "i-node-delete")

	upsertTestService(t, ctx, db, testServiceInput{
		ID:         "svc-delete",
		Name:       "delete-web",
		Generation: 1,
		Image:      "nginx:1.27-alpine",
	})

	runWork, err := db.Store.CreateExecutionClaim(ctx, node.ID)
	if err != nil {
		t.Fatalf("CreateExecutionClaim(run) returned error: %v", err)
	}
	if runWork == nil {
		t.Fatal("CreateExecutionClaim(run) returned nil work item")
	}
	if runWork.Action != cloudmodel.WorkActionRun {
		t.Fatalf("run action = %q, want %q", runWork.Action, cloudmodel.WorkActionRun)
	}
	if _, err := db.Store.UpdateExecutionFromNodeReport(ctx, node.ID, runWork.ExecutionID, cloudmodel.ReportInput{
		Status:        cloudmodel.StatusRunning,
		Reason:        "execution is healthy",
		ContainerID:   "ctr-delete-0",
		ContainerName: runWork.ContainerName,
		HostPort:      18080,
	}); err != nil {
		t.Fatalf("UpdateExecutionFromNodeReport(running) returned error: %v", err)
	}

	deleteTestService(t, ctx, db, "svc-delete", 2)

	deleteWork, err := db.Store.CreateExecutionClaim(ctx, node.ID)
	if err != nil {
		t.Fatalf("CreateExecutionClaim(delete) returned error: %v", err)
	}
	if deleteWork == nil {
		t.Fatal("CreateExecutionClaim(delete) returned nil work item")
	}
	if deleteWork.Action != cloudmodel.WorkActionDelete {
		t.Fatalf("delete action = %q, want %q", deleteWork.Action, cloudmodel.WorkActionDelete)
	}
	if deleteWork.NodeID != node.ID {
		t.Fatalf("delete nodeID = %q, want original node %q", deleteWork.NodeID, node.ID)
	}
	if deleteWork.ContainerID != "ctr-delete-0" || deleteWork.ContainerName != runWork.ContainerName || deleteWork.HostPort != 18080 {
		t.Fatalf("delete work container fields = containerID %q containerName %q hostPort %d", deleteWork.ContainerID, deleteWork.ContainerName, deleteWork.HostPort)
	}

	if _, err := db.Store.UpdateExecutionFromNodeReport(ctx, node.ID, deleteWork.ExecutionID, cloudmodel.ReportInput{
		Status:        cloudmodel.StatusSucceeded,
		Reason:        "service deletion stopped container",
		ContainerID:   deleteWork.ContainerID,
		ContainerName: deleteWork.ContainerName,
		HostPort:      deleteWork.HostPort,
	}); err != nil {
		t.Fatalf("UpdateExecutionFromNodeReport(delete succeeded) returned error: %v", err)
	}

	deleteTestService(t, ctx, db, "svc-delete", 2)

	afterSecondDelete, err := db.Store.GetNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("GetNode after second delete returned error: %v", err)
	}
	if afterSecondDelete.CPUMilliAllocated != 0 || afterSecondDelete.MemoryMiAllocated != 0 {
		t.Fatalf("node allocation after idempotent delete = cpu %d memory %d, want 0/0", afterSecondDelete.CPUMilliAllocated, afterSecondDelete.MemoryMiAllocated)
	}

	snapshots, err := db.Store.ListExecutionSnapshots(ctx)
	if err != nil {
		t.Fatalf("ListExecutionSnapshots returned error: %v", err)
	}
	deleteSnapshot := findExecutionSnapshot(snapshots, "svc-delete-delete-g2")
	if deleteSnapshot == nil {
		t.Fatalf("delete service delete snapshot not found in %+v", snapshots)
	}
	if deleteSnapshot.Status != cloudmodel.StatusSucceeded {
		t.Fatalf("delete snapshot = %+v, want succeeded status", *deleteSnapshot)
	}
}

func TestIntegrationDeletePendingExecutionIntentCompletesWithoutNodeAgent(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)

	upsertTestService(t, ctx, db, testServiceInput{
		ID:         "svc-delete-pending",
		Name:       "delete-pending-web",
		Generation: 1,
		Image:      "nginx:1.27-alpine",
	})

	deleteTestService(t, ctx, db, "svc-delete-pending", 2)

	snapshots, err := db.Store.ListExecutionSnapshots(ctx)
	if err != nil {
		t.Fatalf("ListExecutionSnapshots returned error: %v", err)
	}
	runSnapshot := findExecutionSnapshot(snapshots, "svc-delete-pending-g1")
	if runSnapshot == nil || runSnapshot.Status != cloudmodel.StatusFailed {
		t.Fatalf("run snapshot = %+v, want failed", runSnapshot)
	}
	deleteSnapshot := findExecutionSnapshot(snapshots, "svc-delete-pending-delete-g2")
	if deleteSnapshot == nil {
		t.Fatalf("delete service delete snapshot not found in %+v", snapshots)
	}
	if deleteSnapshot.Status != cloudmodel.StatusSucceeded {
		t.Fatalf("delete snapshot = %+v, want succeeded status", *deleteSnapshot)
	}
}

func TestIntegrationDeleteDeployingExecutionWithoutContainerCompletesWithoutNodeAgent(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)
	node := seedReadyNode(t, ctx, db, "node-delete-deploying", "i-node-delete-deploying")

	upsertTestService(t, ctx, db, testServiceInput{
		ID:         "svc-delete-deploying",
		Name:       "delete-deploying-web",
		Generation: 1,
		Image:      "nginx:1.27-alpine",
	})
	work, err := db.Store.CreateExecutionClaim(ctx, node.ID)
	if err != nil {
		t.Fatalf("CreateExecutionClaim returned error: %v", err)
	}
	if work == nil {
		t.Fatal("CreateExecutionClaim returned nil work item")
	}

	deleteTestService(t, ctx, db, "svc-delete-deploying", 2)

	afterDelete, err := db.Store.GetNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("GetNode after DeleteService returned error: %v", err)
	}
	if afterDelete.CPUMilliAllocated != 0 || afterDelete.MemoryMiAllocated != 0 {
		t.Fatalf("node allocation after deleting containerless deploying intent = cpu %d memory %d, want 0/0", afterDelete.CPUMilliAllocated, afterDelete.MemoryMiAllocated)
	}

	snapshots, err := db.Store.ListExecutionSnapshots(ctx)
	if err != nil {
		t.Fatalf("ListExecutionSnapshots returned error: %v", err)
	}
	runSnapshot := findExecutionSnapshot(snapshots, "svc-delete-deploying-g1")
	if runSnapshot == nil || runSnapshot.Status != cloudmodel.StatusFailed {
		t.Fatalf("run snapshot = %+v, want failed", runSnapshot)
	}
	deleteSnapshot := findExecutionSnapshot(snapshots, "svc-delete-deploying-delete-g2")
	if deleteSnapshot == nil || deleteSnapshot.Status != cloudmodel.StatusSucceeded {
		t.Fatalf("delete snapshot = %+v, want succeeded", deleteSnapshot)
	}
}

func seedReadyNode(t *testing.T, ctx context.Context, db testutil.TestDatabase, name string, instanceID string) cloudmodel.Node {
	t.Helper()

	node, err := db.Store.RegisterNode(ctx, cloudmodel.RegisterInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		Name:          name,
		PrivateIP:     "10.0.0.10",
		InstanceID:    instanceID,
		InstanceType:  "ecs.u1-c1m1.large",
		CPUMilliTotal: 2000,
		MemoryMiTotal: 4096,
	})
	if err != nil {
		t.Fatalf("RegisterNode returned error: %v", err)
	}
	if _, err := db.Store.RecordNodeHeartbeat(ctx, node.ID, cloudmodel.HeartbeatInput{
		CPUMilliAllocatable: 1500,
		MemoryMiAllocatable: 3584,
	}); err != nil {
		t.Fatalf("RecordNodeHeartbeat returned error: %v", err)
	}
	return node
}

type testServiceInput struct {
	ID         string
	Name       string
	Generation int64
	Image      string
	Env        map[string]string
	Exposure   string
}

func upsertTestService(t *testing.T, ctx context.Context, db testutil.TestDatabase, input testServiceInput) {
	t.Helper()

	exposure := input.Exposure
	if exposure == "" {
		exposure = cloudmodel.ExposurePublic
	}
	if _, err := db.Store.UpsertService(ctx, cloudmodel.UpsertServiceInput{
		ID:          input.ID,
		Name:        input.Name,
		DisplayName: input.Name,
		Host:        input.Name + ".apps.example.test",
		Generation:  input.Generation,
		Spec: cloudmodel.ServiceSpec{
			InstanceClass: "small",
			Exposure:      exposure,
			Image:         input.Image,
			Env:           input.Env,
			ContainerPort: 8080,
			ReadinessPath: "/healthz",
		},
	}); err != nil {
		t.Fatalf("UpsertService returned error: %v", err)
	}
}

func deleteTestService(t *testing.T, ctx context.Context, db testutil.TestDatabase, serviceID string, generation int64) {
	t.Helper()

	if err := db.Store.DeleteService(ctx, cloudmodel.DeleteServiceInput{
		ID:         serviceID,
		Generation: generation,
	}); err != nil {
		t.Fatalf("DeleteService returned error: %v", err)
	}
}

func findExecutionSnapshot(items []cloudmodel.ExecutionSnapshot, intentKey string) *cloudmodel.ExecutionSnapshot {
	for i := range items {
		if items[i].IntentKey == intentKey {
			return &items[i]
		}
	}
	return nil
}
