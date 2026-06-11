package store_test

import (
	"context"
	"testing"
	"time"

	cloudmodel "mini-cloud/internal/cloudplane/model"
	"mini-cloud/internal/projectedfile"
	"mini-cloud/internal/testutil"
)

// TestIntegrationCreateExecutionClaimUsesPlanRuntimeInputs 验证 work item 直接使用 control-plane 下发的 execution plan 输入。
func TestIntegrationCreateExecutionClaimUsesPlanRuntimeInputs(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)
	node := seedReadyNode(t, ctx, db, "node-plan-inputs", "i-node-plan-inputs")

	if _, err := db.Store.ApplyExecutionPlan(ctx, cloudmodel.PlanInput{
		PlanID:            "svc-demo-g1",
		ServiceID:         "svc-demo",
		ServiceName:       "demo-web",
		ServiceGeneration: 1,
		Image:             "registry.example.com/demo:v1",
		Env: map[string]string{
			"SERVICE_MODE": "plan-v1",
			"LOG_LEVEL":    "debug",
		},
		ProjectedFiles: []projectedfile.File{
			{MountPath: "/etc/demo/config.yaml", Content: "mode: plan-v1\n", Mode: 0o644},
			{MountPath: "/etc/demo/token", Content: "token-v1", Mode: 0o400, Sensitive: true},
		},
		ContainerPort:   8080,
		ReadinessPath:   "/healthz",
		CPUMilliRequest: 500,
		MemoryMiRequest: 512,
		Exposure:        cloudmodel.ExposurePublic,
	}); err != nil {
		t.Fatalf("ApplyExecutionPlan returned error: %v", err)
	}

	work, err := db.Store.CreateExecutionClaim(ctx, node.ID)
	if err != nil {
		t.Fatalf("CreateExecutionClaim returned error: %v", err)
	}
	if work == nil {
		t.Fatal("CreateExecutionClaim returned nil work item")
	}
	if work.PlanID != "svc-demo-g1" {
		t.Fatalf("work plan identity = %q, want svc-demo-g1", work.PlanID)
	}
	if work.Env["SERVICE_MODE"] != "plan-v1" || work.Env["LOG_LEVEL"] != "debug" {
		t.Fatalf("work env = %+v, want execution plan env", work.Env)
	}
	if len(work.ProjectedFiles) != 2 {
		t.Fatalf("work projected files len = %d, want 2", len(work.ProjectedFiles))
	}
	if work.ProjectedFiles[0].MountPath != "/etc/demo/config.yaml" || work.ProjectedFiles[0].Content != "mode: plan-v1\n" {
		t.Fatalf("unexpected first projected file: %+v", work.ProjectedFiles[0])
	}
	if work.ProjectedFiles[1].MountPath != "/etc/demo/token" || work.ProjectedFiles[1].Content != "token-v1" || !work.ProjectedFiles[1].Sensitive {
		t.Fatalf("unexpected second projected file: %+v", work.ProjectedFiles[1])
	}
}

// TestIntegrationDeleteExecutionPlanClaimsRunningIntentAndReportsSnapshot 验证 v8 delete plan 会转成原节点 delete work，并在清理上报后形成可完成删除的 snapshot。
func TestIntegrationDeleteExecutionPlanClaimsRunningIntentAndReportsSnapshot(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)
	node := seedReadyNode(t, ctx, db, "node-delete", "i-node-delete")

	if _, err := db.Store.ApplyExecutionPlan(ctx, cloudmodel.PlanInput{
		PlanID:            "svc-delete-g1",
		ServiceID:         "svc-delete",
		ServiceName:       "delete-web",
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

	if err := db.Store.DeleteExecutionPlansForService(ctx, cloudmodel.DeletePlanInput{
		ServiceID:         "svc-delete",
		ServiceGeneration: 2,
		PlanID:            "svc-delete-delete-g2",
	}); err != nil {
		t.Fatalf("DeleteExecutionPlansForService returned error: %v", err)
	}

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
		t.Fatalf("delete work runtime fields = containerID %q containerName %q hostPort %d", deleteWork.ContainerID, deleteWork.ContainerName, deleteWork.HostPort)
	}

	if _, err := db.Store.UpdateExecutionFromNodeReport(ctx, node.ID, deleteWork.ExecutionID, cloudmodel.ReportInput{
		Status:        cloudmodel.StatusSuperseded,
		Reason:        "service deletion stopped container",
		ContainerID:   deleteWork.ContainerID,
		ContainerName: deleteWork.ContainerName,
		HostPort:      deleteWork.HostPort,
	}); err != nil {
		t.Fatalf("UpdateExecutionFromNodeReport(delete superseded) returned error: %v", err)
	}

	snapshots, err := db.Store.ListExecutionSnapshots(ctx)
	if err != nil {
		t.Fatalf("ListExecutionSnapshots returned error: %v", err)
	}
	deleteSnapshot := findExecutionSnapshot(snapshots, "svc-delete-delete-g2")
	if deleteSnapshot == nil {
		t.Fatalf("delete execution snapshot not found in %+v", snapshots)
	}
	if deleteSnapshot.Status != cloudmodel.StatusSuperseded {
		t.Fatalf("delete snapshot = %+v, want superseded status", *deleteSnapshot)
	}
}

func TestIntegrationDeletePendingExecutionPlanCompletesWithoutNodeAgent(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)

	if _, err := db.Store.ApplyExecutionPlan(ctx, cloudmodel.PlanInput{
		PlanID:            "svc-delete-pending-g1",
		ServiceID:         "svc-delete-pending",
		ServiceName:       "delete-pending-web",
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

	if err := db.Store.DeleteExecutionPlansForService(ctx, cloudmodel.DeletePlanInput{
		ServiceID:         "svc-delete-pending",
		ServiceGeneration: 2,
		PlanID:            "svc-delete-pending-delete-g2",
	}); err != nil {
		t.Fatalf("DeleteExecutionPlansForService returned error: %v", err)
	}

	snapshots, err := db.Store.ListExecutionSnapshots(ctx)
	if err != nil {
		t.Fatalf("ListExecutionSnapshots returned error: %v", err)
	}
	runSnapshot := findExecutionSnapshot(snapshots, "svc-delete-pending-g1")
	if runSnapshot == nil || runSnapshot.Status != cloudmodel.StatusSuperseded {
		t.Fatalf("run snapshot = %+v, want superseded", runSnapshot)
	}
	deleteSnapshot := findExecutionSnapshot(snapshots, "svc-delete-pending-delete-g2")
	if deleteSnapshot == nil {
		t.Fatalf("delete execution snapshot not found in %+v", snapshots)
	}
	if deleteSnapshot.Status != cloudmodel.StatusSuperseded {
		t.Fatalf("delete snapshot = %+v, want superseded status", *deleteSnapshot)
	}
}

func TestIntegrationDeleteDeployingExecutionWithoutContainerCompletesWithoutNodeAgent(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)
	node := seedReadyNode(t, ctx, db, "node-delete-deploying", "i-node-delete-deploying")

	if _, err := db.Store.ApplyExecutionPlan(ctx, cloudmodel.PlanInput{
		PlanID:            "svc-delete-deploying-g1",
		ServiceID:         "svc-delete-deploying",
		ServiceName:       "delete-deploying-web",
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
	work, err := db.Store.CreateExecutionClaim(ctx, node.ID)
	if err != nil {
		t.Fatalf("CreateExecutionClaim returned error: %v", err)
	}
	if work == nil {
		t.Fatal("CreateExecutionClaim returned nil work item")
	}

	if err := db.Store.DeleteExecutionPlansForService(ctx, cloudmodel.DeletePlanInput{
		ServiceID:         "svc-delete-deploying",
		ServiceGeneration: 2,
		PlanID:            "svc-delete-deploying-delete-g2",
	}); err != nil {
		t.Fatalf("DeleteExecutionPlansForService returned error: %v", err)
	}

	snapshots, err := db.Store.ListExecutionSnapshots(ctx)
	if err != nil {
		t.Fatalf("ListExecutionSnapshots returned error: %v", err)
	}
	runSnapshot := findExecutionSnapshot(snapshots, "svc-delete-deploying-g1")
	if runSnapshot == nil || runSnapshot.Status != cloudmodel.StatusSuperseded {
		t.Fatalf("run snapshot = %+v, want superseded", runSnapshot)
	}
	deleteSnapshot := findExecutionSnapshot(snapshots, "svc-delete-deploying-delete-g2")
	if deleteSnapshot == nil || deleteSnapshot.Status != cloudmodel.StatusSuperseded {
		t.Fatalf("delete snapshot = %+v, want superseded", deleteSnapshot)
	}
}

func seedReadyNode(t *testing.T, ctx context.Context, db testutil.TestDatabase, name string, instanceID string) cloudmodel.Node {
	t.Helper()

	node, err := db.Store.RegisterNode(ctx, cloudmodel.RegisterInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		Name:          name,
		PrivateIP:     "10.0.0.10",
		PublicIP:      "203.0.113.10",
		InstanceID:    instanceID,
		InstanceType:  "ecs.u1-c1m1.large",
		CPUMilliTotal: 2000,
		MemoryMiTotal: 4096,
	})
	if err != nil {
		t.Fatalf("RegisterNode returned error: %v", err)
	}
	if _, _, err := db.Store.RecordNodeHeartbeat(ctx, node.ID, cloudmodel.HeartbeatInput{
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

func findExecutionSnapshot(items []cloudmodel.ExecutionSnapshot, planID string) *cloudmodel.ExecutionSnapshot {
	for i := range items {
		if items[i].PlanID == planID {
			return &items[i]
		}
	}
	return nil
}
