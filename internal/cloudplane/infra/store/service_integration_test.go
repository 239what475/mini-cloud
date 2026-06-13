package store_test

import (
	"context"
	"testing"

	cloudmodel "mini-cloud/internal/cloudplane/model"
	"mini-cloud/internal/testutil"
)

func TestIntegrationDeleteServiceWithoutRunningContainerHidesServiceAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)

	if _, err := db.Store.UpsertService(ctx, cloudmodel.UpsertServiceInput{
		ID:          "svc-delete-idle",
		Name:        "delete-idle",
		DisplayName: "delete idle",
		Host:        "delete-idle.apps.example.com",
		Generation:  1,
		Spec:        serviceSpec(),
	}); err != nil {
		t.Fatalf("UpsertService returned error: %v", err)
	}

	if err := db.Store.DeleteService(ctx, cloudmodel.DeleteServiceInput{
		ID:         "svc-delete-idle",
		Generation: 2,
	}); err != nil {
		t.Fatalf("DeleteService returned error: %v", err)
	}

	services, err := db.Store.ListServices(ctx)
	if err != nil {
		t.Fatalf("ListServices returned error: %v", err)
	}
	if serviceByID(services, "svc-delete-idle") != nil {
		t.Fatalf("deleted idle service is still exposed in service snapshot: %+v", services)
	}
	if deleteSnapshot := findExecutionSnapshotForTest(t, ctx, db, "svc-delete-idle-delete-g2"); deleteSnapshot != nil {
		t.Fatalf("delete idle service created unexpected delete snapshot: %+v", deleteSnapshot)
	}
	if err := db.Store.DeleteService(ctx, cloudmodel.DeleteServiceInput{
		ID:         "svc-delete-idle",
		Generation: 2,
	}); err != nil {
		t.Fatalf("DeleteService(retry) returned error: %v", err)
	}
}

func TestIntegrationServiceIngressRouteSourcesRespectExposure(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)

	if _, err := db.Store.UpsertService(ctx, cloudmodel.UpsertServiceInput{
		ID:          "svc-public-route",
		Name:        "public-route",
		DisplayName: "public route",
		Host:        "public-route.apps.example.com",
		Generation:  1,
		Spec:        serviceSpec(),
	}); err != nil {
		t.Fatalf("UpsertService(public) returned error: %v", err)
	}
	privateSpec := serviceSpec()
	privateSpec.Exposure = cloudmodel.ExposurePrivate
	if _, err := db.Store.UpsertService(ctx, cloudmodel.UpsertServiceInput{
		ID:          "svc-private-route",
		Name:        "private-route",
		DisplayName: "private route",
		Host:        "private-route.apps.example.com",
		Generation:  1,
		Spec:        privateSpec,
	}); err != nil {
		t.Fatalf("UpsertService(private) returned error: %v", err)
	}

	sources, err := db.Store.ListIngressRouteSources(ctx)
	if err != nil {
		t.Fatalf("ListIngressRouteSources returned error: %v", err)
	}
	if len(sources) != 1 || sources[0].Host != "public-route.apps.example.com" || sources[0].HasBackend {
		t.Fatalf("route sources = %+v, want one public host without backend", sources)
	}
}

func TestIntegrationStaleServiceUpsertDoesNotResurrectDeletedService(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)

	input := cloudmodel.UpsertServiceInput{
		ID:          "svc-stale-upsert-delete",
		Name:        "stale-upsert-delete",
		DisplayName: "stale upsert delete",
		Host:        "stale-upsert-delete.apps.example.com",
		Generation:  1,
		Spec:        serviceSpec(),
	}
	if _, err := db.Store.UpsertService(ctx, input); err != nil {
		t.Fatalf("UpsertService(initial) returned error: %v", err)
	}
	if err := db.Store.DeleteService(ctx, cloudmodel.DeleteServiceInput{
		ID:         input.ID,
		Generation: input.Generation,
	}); err != nil {
		t.Fatalf("DeleteService returned error: %v", err)
	}
	if _, err := db.Store.UpsertService(ctx, input); err != nil {
		t.Fatalf("UpsertService(stale) returned error: %v", err)
	}
	services, err := db.Store.ListServices(ctx)
	if err != nil {
		t.Fatalf("ListServices after stale upsert returned error: %v", err)
	}
	if serviceByID(services, input.ID) != nil {
		t.Fatalf("stale upsert resurrected deleted service: %+v", services)
	}

	input.Generation = 2
	input.DisplayName = "stale upsert delete v2"
	if _, err := db.Store.UpsertService(ctx, input); err != nil {
		t.Fatalf("UpsertService(new generation) returned error: %v", err)
	}
	services, err = db.Store.ListServices(ctx)
	if err != nil {
		t.Fatalf("ListServices after new generation upsert returned error: %v", err)
	}
	service := serviceByID(services, input.ID)
	if service == nil || service.Generation != input.Generation || service.DisplayName != input.DisplayName {
		t.Fatalf("new generation service = %+v, services = %+v", service, services)
	}
}

func TestIntegrationDeleteServiceAfterRunningContainerHidesServiceAfterAgentReport(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)
	node := seedReadyNode(t, ctx, db, "node-delete-service", "i-node-delete-service")

	if _, err := db.Store.UpsertService(ctx, cloudmodel.UpsertServiceInput{
		ID:          "svc-delete-running",
		Name:        "delete-running",
		DisplayName: "delete running",
		Host:        "delete-running.apps.example.com",
		Generation:  1,
		Spec:        serviceSpec(),
	}); err != nil {
		t.Fatalf("UpsertService returned error: %v", err)
	}
	runWork, err := db.Store.CreateExecutionClaim(ctx, node.ID)
	if err != nil {
		t.Fatalf("CreateExecutionClaim(run) returned error: %v", err)
	}
	if runWork == nil {
		t.Fatal("CreateExecutionClaim(run) returned nil work item")
	}
	if _, err := db.Store.UpdateExecutionFromNodeReport(ctx, node.ID, runWork.ExecutionID, cloudmodel.ReportInput{
		Status:        cloudmodel.StatusRunning,
		Reason:        "execution is healthy",
		ContainerID:   "ctr-delete-running",
		ContainerName: runWork.ContainerName,
		HostPort:      18080,
	}); err != nil {
		t.Fatalf("UpdateExecutionFromNodeReport(running) returned error: %v", err)
	}

	if err := db.Store.DeleteService(ctx, cloudmodel.DeleteServiceInput{
		ID:         "svc-delete-running",
		Generation: 2,
	}); err != nil {
		t.Fatalf("DeleteService returned error: %v", err)
	}
	services, err := db.Store.ListServices(ctx)
	if err != nil {
		t.Fatalf("ListServices while delete pending returned error: %v", err)
	}
	if serviceByID(services, "svc-delete-running") != nil {
		t.Fatalf("delete-pending service is still exposed in active service snapshot: %+v", services)
	}

	deleteWork, err := db.Store.CreateExecutionClaim(ctx, node.ID)
	if err != nil {
		t.Fatalf("CreateExecutionClaim(delete) returned error: %v", err)
	}
	if deleteWork == nil || deleteWork.Action != cloudmodel.WorkActionDelete {
		t.Fatalf("delete work = %+v, want delete action", deleteWork)
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

	services, err = db.Store.ListServices(ctx)
	if err != nil {
		t.Fatalf("ListServices after delete succeeded returned error: %v", err)
	}
	if serviceByID(services, "svc-delete-running") != nil {
		t.Fatalf("deleted running service is still exposed in service snapshot: %+v", services)
	}
	deleteSnapshot := executionSnapshotByIntentKey(t, ctx, db, "svc-delete-running-delete-g2")
	if deleteSnapshot.Status != cloudmodel.StatusSucceeded {
		t.Fatalf("delete execution snapshot = %+v, want succeeded", deleteSnapshot)
	}
	if err := db.Store.DeleteService(ctx, cloudmodel.DeleteServiceInput{
		ID:         "svc-delete-running",
		Generation: 2,
	}); err != nil {
		t.Fatalf("DeleteService retry after completed delete work returned error: %v", err)
	}
}

func serviceSpec() cloudmodel.ServiceSpec {
	return cloudmodel.ServiceSpec{
		InstanceClass: "small",
		Exposure:      cloudmodel.ExposurePublic,
		Image:         "nginx:1.27-alpine",
		ContainerPort: 8080,
		ReadinessPath: "/healthz",
	}
}

func serviceByID(items []cloudmodel.Service, serviceID string) *cloudmodel.Service {
	for i := range items {
		if items[i].ID == serviceID {
			return &items[i]
		}
	}
	return nil
}

func executionSnapshotByIntentKey(t *testing.T, ctx context.Context, db testutil.TestDatabase, intentKey string) cloudmodel.ExecutionSnapshot {
	t.Helper()

	snapshot := findExecutionSnapshotForTest(t, ctx, db, intentKey)
	if snapshot == nil {
		t.Fatalf("execution snapshot %q not found", intentKey)
	}
	return *snapshot
}

func findExecutionSnapshotForTest(t *testing.T, ctx context.Context, db testutil.TestDatabase, intentKey string) *cloudmodel.ExecutionSnapshot {
	t.Helper()

	snapshots, err := db.Store.ListExecutionSnapshots(ctx)
	if err != nil {
		t.Fatalf("ListExecutionSnapshots returned error: %v", err)
	}
	for _, snapshot := range snapshots {
		if snapshot.IntentKey == intentKey {
			return &snapshot
		}
	}
	return nil
}
