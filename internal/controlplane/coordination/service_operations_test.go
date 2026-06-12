package coordination

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"mini-cloud/internal/controlplane/model"
	controlplanestore "mini-cloud/internal/controlplane/store"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"
	"mini-cloud/internal/testutil"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
)

func TestCreateAppliesServiceToSpecPlane(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeServer := startServiceOperationsPlane(t)
	planeItem := mustCreateReadyPlane(t, db, "plane-create-apply", planeServer.endpoint)

	operations := NewServiceOperations(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, "southbound-token", "apps.example.test", nil)

	service, err := operations.Create(ctx, createInput(planeItem.ID, "web", "Web", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if service.Metadata.Generation != 1 {
		t.Fatalf("generation = %d, want 1", service.Metadata.Generation)
	}
	if service.Status.Observed.Phase != model.PhaseProgressing {
		t.Fatalf("phase = %s, want progressing", service.Status.Observed.Phase)
	}
	if service.Spec.PlaneID != planeItem.ID {
		t.Fatalf("planeID = %q, want %s", service.Spec.PlaneID, planeItem.ID)
	}
	if service.Metadata.Host != "web.apps.example.test" {
		t.Fatalf("host = %q, want web.apps.example.test", service.Metadata.Host)
	}
	applyRequests := planeServer.applyRequests()
	if len(applyRequests) != 1 || applyRequests[0].GetServiceName() != "web" {
		t.Fatalf("unexpected apply requests: %+v", applyRequests)
	}
	if applyRequests[0].GetHost() != "web.apps.example.test" {
		t.Fatalf("apply host = %q, want web.apps.example.test", applyRequests[0].GetHost())
	}
}

func TestCreateAllowsDegradedPlane(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeServer := startServiceOperationsPlane(t)
	planeItem := mustCreateReadyPlane(t, db, "plane-degraded", planeServer.endpoint)
	if err := db.Store.UpdatePlaneStatus(ctx, planeItem.ID, controlplanestore.UpdatePlaneStatusInput{Status: model.StatusDegraded, Message: "alert firing"}); err != nil {
		t.Fatalf("UpdatePlaneStatus returned error: %v", err)
	}

	operations := NewServiceOperations(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, "southbound-token", "apps.example.test", nil)

	service, err := operations.Create(ctx, createInput(planeItem.ID, "degraded-web", "Degraded Web", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if service.Spec.PlaneID != planeItem.ID {
		t.Fatalf("planeID = %q, want %s", service.Spec.PlaneID, planeItem.ID)
	}
	if len(planeServer.applyRequests()) != 1 {
		t.Fatalf("apply requests = %d, want 1", len(planeServer.applyRequests()))
	}
}

func TestCreateKeepsServiceWhenInitialApplyFails(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeServer := startServiceOperationsPlane(t)
	planeItem := mustCreateReadyPlane(t, db, "plane-create-offline", planeServer.endpoint)
	if err := db.Store.UpdatePlaneStatus(ctx, planeItem.ID, controlplanestore.UpdatePlaneStatusInput{Status: model.StatusOffline, Message: "plane unavailable"}); err != nil {
		t.Fatalf("UpdatePlaneStatus returned error: %v", err)
	}

	operations := NewServiceOperations(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, "southbound-token", "apps.example.test", nil)

	service, err := operations.Create(ctx, createInput(planeItem.ID, "pending-web", "Pending Web", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if service.Status.DesiredState != model.DesiredStateActive || service.Status.Observed.Phase != model.PhaseDegraded {
		t.Fatalf("service status = %+v, want active degraded after failed initial apply", service.Status)
	}
	if service.Status.Observed.ObservedGeneration != 0 {
		t.Fatalf("observed generation = %d, want 0 so failed apply remains retryable", service.Status.Observed.ObservedGeneration)
	}
	_, pending, err := db.Store.GetPendingApplyService(ctx, service.Metadata.ID)
	if err != nil {
		t.Fatalf("GetPendingApplyService returned error: %v", err)
	}
	if !pending {
		t.Fatalf("service is not pending apply, want failed service to remain retryable")
	}
	if len(planeServer.applyRequests()) != 0 {
		t.Fatalf("apply requests = %d, want 0 while plane is offline", len(planeServer.applyRequests()))
	}
}

func TestUpdateDispatchesServiceToSpecPlane(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeServer := startServiceOperationsPlane(t)
	planeItem := mustCreateReadyPlane(t, db, "plane-update", planeServer.endpoint)

	operations := NewServiceOperations(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, "southbound-token", "apps.example.test", nil)

	created, err := operations.Create(ctx, createInput(planeItem.ID, "api", "API", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	updated, err := operations.Update(ctx, created.Metadata.ID, updateInput(planeItem.ID, "API v2", "nginx:1.28-alpine"))
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if updated.Spec.PlaneID != planeItem.ID {
		t.Fatalf("planeID = %q, want %s", updated.Spec.PlaneID, planeItem.ID)
	}
	if updated.Status.Observed.Phase != model.PhaseProgressing {
		t.Fatalf("phase = %s, want progressing after update", updated.Status.Observed.Phase)
	}
	applyRequests := planeServer.applyRequests()
	if len(applyRequests) != 2 {
		t.Fatalf("applyRequests = %d, want 2", len(applyRequests))
	}
	if applyRequests[1].GetImage() != "nginx:1.28-alpine" {
		t.Fatalf("updated image = %s, want nginx:1.28-alpine", applyRequests[1].GetImage())
	}
}

func TestUpdateKeepsServiceRetryableWhenApplyFails(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeServer := startServiceOperationsPlane(t)
	planeItem := mustCreateReadyPlane(t, db, "plane-update-offline", planeServer.endpoint)

	operations := NewServiceOperations(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, "southbound-token", "apps.example.test", nil)

	created, err := operations.Create(ctx, createInput(planeItem.ID, "update-offline", "Update Offline", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if err := db.Store.UpdateServiceStatusForGeneration(ctx, created.Metadata.ID, created.Metadata.Generation, controlplanestore.UpdateServiceStatusInput{
		ObservedGeneration: created.Metadata.Generation,
		Phase:              model.PhaseReady,
		Message:            "observed",
	}); err != nil {
		t.Fatalf("UpdateServiceStatusForGeneration returned error: %v", err)
	}
	if err := db.Store.UpdatePlaneStatus(ctx, planeItem.ID, controlplanestore.UpdatePlaneStatusInput{Status: model.StatusOffline, Message: "plane unavailable"}); err != nil {
		t.Fatalf("UpdatePlaneStatus returned error: %v", err)
	}

	updated, err := operations.Update(ctx, created.Metadata.ID, updateInput(planeItem.ID, "Update Offline v2", "nginx:1.28-alpine"))
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if updated.Metadata.Generation != created.Metadata.Generation+1 {
		t.Fatalf("generation = %d, want %d", updated.Metadata.Generation, created.Metadata.Generation+1)
	}
	if updated.Status.Observed.ObservedGeneration != created.Metadata.Generation {
		t.Fatalf("observed generation = %d, want previous generation %d", updated.Status.Observed.ObservedGeneration, created.Metadata.Generation)
	}
	_, pending, err := db.Store.GetPendingApplyService(ctx, updated.Metadata.ID)
	if err != nil {
		t.Fatalf("GetPendingApplyService returned error: %v", err)
	}
	if !pending {
		t.Fatalf("service is not pending apply, want failed update to remain retryable")
	}
}

func TestUpdateRejectsPlaneIDChange(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeServerA := startServiceOperationsPlane(t)
	planeServerB := startServiceOperationsPlane(t)
	planeA := mustCreateReadyPlane(t, db, "plane-move-a", planeServerA.endpoint)
	planeB := mustCreateReadyPlane(t, db, "plane-move-b", planeServerB.endpoint)

	operations := NewServiceOperations(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, "southbound-token", "apps.example.test", nil)

	created, err := operations.Create(ctx, createInput(planeA.ID, "move", "Move", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if _, err := operations.Update(ctx, created.Metadata.ID, updateInput(planeB.ID, "Move", "nginx:1.28-alpine")); err == nil {
		t.Fatalf("Update returned nil error, want immutable plane error")
	}
	if len(planeServerA.applyRequests()) != 1 {
		t.Fatalf("plane A apply requests = %d, want 1 create request", len(planeServerA.applyRequests()))
	}
	if len(planeServerB.applyRequests()) != 0 {
		t.Fatalf("plane B apply requests = %d, want 0", len(planeServerB.applyRequests()))
	}
	if len(planeServerA.deleteRequests()) != 0 {
		t.Fatalf("plane A delete requests = %d, want 0", len(planeServerA.deleteRequests()))
	}
}

func TestDeleteDispatchesServiceDeleteToCloudPlane(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeServer := startServiceOperationsPlane(t)
	planeItem := mustCreateReadyPlane(t, db, "plane-delete", planeServer.endpoint)

	operations := NewServiceOperations(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, "southbound-token", "apps.example.test", nil)

	created, err := operations.Create(ctx, createInput(planeItem.ID, "gone", "Gone", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	serviceID := created.Metadata.ID
	if _, err := operations.Delete(ctx, serviceID); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	reloaded, err := db.Store.GetService(ctx, serviceID)
	if err != nil {
		t.Fatalf("GetService after delete returned error: %v", err)
	}
	if reloaded.Status.DesiredState != model.DesiredStateDeleted || reloaded.Status.Observed.Phase != model.PhaseDeleting {
		t.Fatalf("service status after delete = %+v, want deleting", reloaded.Status)
	}
	deleteRequests := planeServer.deleteRequests()
	if len(deleteRequests) != 1 {
		t.Fatalf("deleteRequests len = %d, want 1", len(deleteRequests))
	}
	if deleteRequests[0].GetServiceId() != serviceID ||
		deleteRequests[0].GetServiceGeneration() != reloaded.Metadata.Generation {
		t.Fatalf("delete input = %+v, want service generation delete", deleteRequests[0])
	}
}

func TestDeleteKeepsServiceDeletingWhenRemoteDispatchFails(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeServer := startServiceOperationsPlane(t)
	planeItem := mustCreateReadyPlane(t, db, "plane-delete-offline", planeServer.endpoint)

	operations := NewServiceOperations(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, "southbound-token", "apps.example.test", nil)

	created, err := operations.Create(ctx, createInput(planeItem.ID, "delete-offline", "Delete Offline", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if err := db.Store.UpdatePlaneStatus(ctx, planeItem.ID, controlplanestore.UpdatePlaneStatusInput{Status: model.StatusOffline, Message: "plane unavailable"}); err != nil {
		t.Fatalf("UpdatePlaneStatus returned error: %v", err)
	}
	deleting, err := operations.Delete(ctx, created.Metadata.ID)
	if err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if deleting.Status.DesiredState != model.DesiredStateDeleted || deleting.Status.Observed.Phase != model.PhaseDeleting {
		t.Fatalf("service status = %+v, want deleting after failed remote dispatch", deleting.Status)
	}
	if len(planeServer.deleteRequests()) != 0 {
		t.Fatalf("delete requests = %d, want 0 while plane is offline", len(planeServer.deleteRequests()))
	}
}

func TestGetAdvancesOnlyRequestedService(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeServer := startServiceOperationsPlane(t)
	planeItem := mustCreateReadyPlane(t, db, "plane-get-advance", planeServer.endpoint)

	target, err := db.Store.CreateService(ctx, createInput(planeItem.ID, "target-apply", "Target Apply", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("CreateService(target) returned error: %v", err)
	}
	other, err := db.Store.CreateService(ctx, createInput(planeItem.ID, "other-apply", "Other Apply", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("CreateService(other) returned error: %v", err)
	}

	operations := NewServiceOperations(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, "southbound-token", "apps.example.test", nil)
	if _, err := operations.Get(ctx, target.Metadata.ID); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}

	applyRequests := planeServer.applyRequests()
	if len(applyRequests) != 1 {
		t.Fatalf("applyRequests len = %d, want 1", len(applyRequests))
	}
	if applyRequests[0].GetServiceId() != target.Metadata.ID {
		t.Fatalf("apply request serviceID = %q, want target %s", applyRequests[0].GetServiceId(), target.Metadata.ID)
	}
	_, targetPending, err := db.Store.GetPendingApplyService(ctx, target.Metadata.ID)
	if err != nil {
		t.Fatalf("GetPendingApplyService(target) returned error: %v", err)
	}
	if targetPending {
		t.Fatalf("target service is still pending after Get")
	}
	_, otherPending, err := db.Store.GetPendingApplyService(ctx, other.Metadata.ID)
	if err != nil {
		t.Fatalf("GetPendingApplyService(other) returned error: %v", err)
	}
	if !otherPending {
		t.Fatalf("other service is not pending; Get should only advance requested service")
	}
}

func createInput(planeID string, name string, displayName string, image string) controlplanestore.CreateServiceInput {
	return controlplanestore.CreateServiceInput{Name: name, DisplayName: displayName, Spec: serviceSpec(planeID, image)}
}

func updateInput(planeID string, displayName string, image string) controlplanestore.UpdateServiceInput {
	return controlplanestore.UpdateServiceInput{DisplayName: displayName, Spec: serviceSpec(planeID, image)}
}

func serviceSpec(planeID string, image string) model.ServiceSpec {
	return model.ServiceSpec{
		PlaneID:       planeID,
		InstanceClass: model.InstanceClassSmall,
		Exposure:      "public",
		Image:         image,
		DefaultPort:   80,
		ReadinessPath: "/",
	}
}

type serviceOperationsPlane struct {
	cloudplanev1.UnimplementedControlPlaneExecutionServiceServer

	mu       sync.Mutex
	endpoint string
	apply    []*cloudplanev1.ApplyServiceRequest
	delete   []*cloudplanev1.DeleteServiceRequest
}

func startServiceOperationsPlane(t *testing.T) *serviceOperationsPlane {
	t.Helper()

	grpcServer := grpc.NewServer()
	plane := &serviceOperationsPlane{}
	cloudplanev1.RegisterControlPlaneExecutionServiceServer(grpcServer, plane)

	server := httptest.NewServer(h2c.NewHandler(grpcServer, &http2.Server{}))
	t.Cleanup(func() {
		server.Close()
		grpcServer.Stop()
	})
	plane.endpoint = strings.TrimPrefix(server.URL, "http://")
	return plane
}

func (p *serviceOperationsPlane) ApplyService(_ context.Context, req *cloudplanev1.ApplyServiceRequest) (*cloudplanev1.ApplyServiceResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.apply = append(p.apply, req)
	return &cloudplanev1.ApplyServiceResponse{
		Service: &cloudplanev1.PlaneService{
			ServiceId:    req.GetServiceId(),
			Name:         req.GetServiceName(),
			DisplayName:  req.GetDisplayName(),
			Host:         req.GetHost(),
			Generation:   req.GetServiceGeneration(),
			DesiredState: model.DesiredStateActive,
			Spec: &cloudplanev1.PlaneServiceSpec{
				InstanceClass: req.GetInstanceClass(),
				Exposure:      req.GetExposure(),
				Image:         req.GetImage(),
				Command:       append([]string(nil), req.GetCommand()...),
				Args:          append([]string(nil), req.GetArgs()...),
				Env:           req.GetEnv(),
				ContainerPort: req.GetContainerPort(),
				ReadinessPath: req.GetReadinessPath(),
			},
		},
	}, nil
}

func (p *serviceOperationsPlane) DeleteService(_ context.Context, req *cloudplanev1.DeleteServiceRequest) (*cloudplanev1.DeleteServiceResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.delete = append(p.delete, req)
	return &cloudplanev1.DeleteServiceResponse{
		ServiceId: req.GetServiceId(),
		Deleted:   true,
	}, nil
}

func (p *serviceOperationsPlane) applyRequests() []*cloudplanev1.ApplyServiceRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*cloudplanev1.ApplyServiceRequest(nil), p.apply...)
}

func (p *serviceOperationsPlane) deleteRequests() []*cloudplanev1.DeleteServiceRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*cloudplanev1.DeleteServiceRequest(nil), p.delete...)
}

func mustCreateReadyPlane(t *testing.T, db testutil.ControlPlaneTestDatabase, name string, endpoint string) model.PlaneDetail {
	t.Helper()
	ctx := context.Background()
	item, err := db.Store.RegisterPlane(ctx, controlplanestore.RegisterPlaneInput{
		Name:         name,
		DisplayName:  name,
		Provider:     "aliyun",
		Region:       "cn-beijing",
		GRPCEndpoint: endpoint,
	})
	if err != nil {
		t.Fatalf("RegisterPlane returned error: %v", err)
	}
	if err := db.Store.UpdatePlaneStatus(ctx, item.ID, controlplanestore.UpdatePlaneStatusInput{Status: model.StatusReady, Message: "ready"}); err != nil {
		t.Fatalf("UpdatePlaneStatus returned error: %v", err)
	}
	detail, err := db.Store.GetPlane(ctx, item.ID)
	if err != nil {
		t.Fatalf("GetPlane returned error: %v", err)
	}
	return detail
}
