package store_test

import (
	"context"
	"testing"

	cloudmodel "mini-cloud/internal/cloudplane/model"
	"mini-cloud/internal/testutil"
)

func TestIntegrationClaimServiceRunUsesWorkloadInputs(t *testing.T) {
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

	work, err := db.Store.ClaimServiceRun(ctx, node.ID)
	if err != nil {
		t.Fatalf("ClaimServiceRun returned error: %v", err)
	}
	if work == nil {
		t.Fatal("ClaimServiceRun returned nil work item")
	}
	if work.Action != cloudmodel.WorkActionRun {
		t.Fatalf("work action = %q, want run", work.Action)
	}
	if work.ServiceID != "svc-demo" || work.Image != "registry.example.com/demo:v1" {
		t.Fatalf("work = %+v, want svc-demo image", work)
	}
	if work.Env["SERVICE_MODE"] != "plan-v1" || work.Env["LOG_LEVEL"] != "debug" {
		t.Fatalf("work env = %+v, want service env", work.Env)
	}
}

func TestIntegrationUpdateReusesCurrentNodeAndReplacesCurrentRun(t *testing.T) {
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
	firstWork, err := db.Store.ClaimServiceRun(ctx, currentNode.ID)
	if err != nil {
		t.Fatalf("ClaimServiceRun(first) returned error: %v", err)
	}
	if firstWork == nil {
		t.Fatal("ClaimServiceRun(first) returned nil work item")
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
	runningNode, err := db.Store.GetNode(ctx, currentNode.ID)
	if err != nil {
		t.Fatalf("GetNode(running) returned error: %v", err)
	}
	if runningNode.CPUMilliAllocated == 0 || runningNode.MemoryMiAllocated == 0 {
		t.Fatalf("running node allocation = cpu %d memory %d, want reserved", runningNode.CPUMilliAllocated, runningNode.MemoryMiAllocated)
	}

	upsertTestService(t, ctx, db, testServiceInput{
		ID:         "svc-replace",
		Name:       "replace-web",
		Generation: 2,
		Image:      "nginx:1.28-alpine",
	})

	otherWork, err := db.Store.ClaimServiceRun(ctx, otherNode.ID)
	if err != nil {
		t.Fatalf("ClaimServiceRun(other node) returned error: %v", err)
	}
	if otherWork != nil {
		t.Fatalf("other node claimed update work: %+v", otherWork)
	}

	updateWork, err := db.Store.ClaimServiceRun(ctx, currentNode.ID)
	if err != nil {
		t.Fatalf("ClaimServiceRun(update) returned error: %v", err)
	}
	if updateWork == nil {
		t.Fatal("ClaimServiceRun(update) returned nil work item")
	}
	if updateWork.Action != cloudmodel.WorkActionRun || updateWork.ExecutionID != firstWork.ExecutionID {
		t.Fatalf("update work = %+v, want same current service run", updateWork)
	}
	if updateWork.Image != "nginx:1.28-alpine" {
		t.Fatalf("update image = %q, want nginx:1.28-alpine", updateWork.Image)
	}
	updatedNode, err := db.Store.GetNode(ctx, currentNode.ID)
	if err != nil {
		t.Fatalf("GetNode(updated) returned error: %v", err)
	}
	if updatedNode.CPUMilliAllocated != runningNode.CPUMilliAllocated || updatedNode.MemoryMiAllocated != runningNode.MemoryMiAllocated {
		t.Fatalf("updated node allocation = cpu %d memory %d, want unchanged from one current run cpu %d memory %d",
			updatedNode.CPUMilliAllocated,
			updatedNode.MemoryMiAllocated,
			runningNode.CPUMilliAllocated,
			runningNode.MemoryMiAllocated,
		)
	}
}

func TestIntegrationUpdatePendingRunReplacesCurrentSpec(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)
	node := seedReadyNode(t, ctx, db, "node-update-pending", "i-node-update-pending")

	upsertTestService(t, ctx, db, testServiceInput{
		ID:         "svc-update-pending",
		Name:       "update-pending-web",
		Generation: 1,
		Image:      "nginx:1.27-alpine",
	})
	upsertTestService(t, ctx, db, testServiceInput{
		ID:         "svc-update-pending",
		Name:       "update-pending-web",
		Generation: 2,
		Image:      "nginx:1.28-alpine",
	})

	work, err := db.Store.ClaimServiceRun(ctx, node.ID)
	if err != nil {
		t.Fatalf("ClaimServiceRun returned error: %v", err)
	}
	if work == nil || work.Image != "nginx:1.28-alpine" {
		t.Fatalf("work = %+v, want latest pending spec", work)
	}

	snapshots, err := db.Store.ListExecutionSnapshots(ctx)
	if err != nil {
		t.Fatalf("ListExecutionSnapshots returned error: %v", err)
	}
	if len(snapshots) != 1 || snapshots[0].ServiceGeneration != 2 || snapshots[0].Status != cloudmodel.StatusDeploying {
		t.Fatalf("snapshots = %+v, want one deploying generation 2 run", snapshots)
	}
}

func TestIntegrationDeleteRunningServiceClaimsCurrentRunAndCompletes(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)
	node := seedReadyNode(t, ctx, db, "node-delete", "i-node-delete")

	upsertTestService(t, ctx, db, testServiceInput{
		ID:         "svc-delete",
		Name:       "delete-web",
		Generation: 1,
		Image:      "nginx:1.27-alpine",
	})

	runWork, err := db.Store.ClaimServiceRun(ctx, node.ID)
	if err != nil {
		t.Fatalf("ClaimServiceRun(run) returned error: %v", err)
	}
	if runWork == nil {
		t.Fatal("ClaimServiceRun(run) returned nil work item")
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

	deleteWork, err := db.Store.ClaimServiceRun(ctx, node.ID)
	if err != nil {
		t.Fatalf("ClaimServiceRun(delete) returned error: %v", err)
	}
	if deleteWork == nil || deleteWork.Action != cloudmodel.WorkActionDelete {
		t.Fatalf("delete work = %+v, want delete action", deleteWork)
	}
	if deleteWork.NodeID != node.ID || deleteWork.ContainerID != "ctr-delete-0" || deleteWork.HostPort != 18080 {
		t.Fatalf("delete work = %+v, want original container on node", deleteWork)
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

	snapshots, err := db.Store.ListExecutionSnapshots(ctx)
	if err != nil {
		t.Fatalf("ListExecutionSnapshots returned error: %v", err)
	}
	if len(snapshots) != 0 {
		t.Fatalf("snapshots after completed delete = %+v, want no current run", snapshots)
	}
}

func TestIntegrationDeletePendingServiceRemovesCurrentRun(t *testing.T) {
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
	if len(snapshots) != 0 {
		t.Fatalf("snapshots after deleting pending service = %+v, want no current run", snapshots)
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
		InstanceType:  "ecs.u1-c1m2.large",
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
