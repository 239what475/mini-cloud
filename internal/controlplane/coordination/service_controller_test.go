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
	planeServer := startServiceControllerPlane(t)
	planeItem := mustCreateReadyPlane(t, db, "plane-create-apply", planeServer.endpoint)

	controller := NewServiceController(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, "southbound-token", "apps.example.test")

	service, err := controller.Create(ctx, createInput(planeItem.ID, "web", "Web", "nginx:1.27-alpine"))
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
	planeServer := startServiceControllerPlane(t)
	planeItem := mustCreateReadyPlane(t, db, "plane-degraded", planeServer.endpoint)
	if err := db.Store.UpdatePlaneStatus(ctx, planeItem.ID, controlplanestore.UpdatePlaneStatusInput{Status: model.StatusDegraded, Message: "alert firing"}); err != nil {
		t.Fatalf("UpdatePlaneStatus returned error: %v", err)
	}

	controller := NewServiceController(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, "southbound-token", "apps.example.test")

	service, err := controller.Create(ctx, createInput(planeItem.ID, "degraded-web", "Degraded Web", "nginx:1.27-alpine"))
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

func TestUpdateDispatchesServiceToSpecPlane(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeServer := startServiceControllerPlane(t)
	planeItem := mustCreateReadyPlane(t, db, "plane-update", planeServer.endpoint)

	controller := NewServiceController(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, "southbound-token", "apps.example.test")

	created, err := controller.Create(ctx, createInput(planeItem.ID, "api", "API", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	updated, err := controller.Update(ctx, created.Metadata.ID, updateInput(planeItem.ID, "API v2", "nginx:1.28-alpine"))
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

func TestUpdateRejectsPlaneIDChange(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeServerA := startServiceControllerPlane(t)
	planeServerB := startServiceControllerPlane(t)
	planeA := mustCreateReadyPlane(t, db, "plane-move-a", planeServerA.endpoint)
	planeB := mustCreateReadyPlane(t, db, "plane-move-b", planeServerB.endpoint)

	controller := NewServiceController(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, "southbound-token", "apps.example.test")

	created, err := controller.Create(ctx, createInput(planeA.ID, "move", "Move", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if _, err := controller.Update(ctx, created.Metadata.ID, updateInput(planeB.ID, "Move", "nginx:1.28-alpine")); err == nil {
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
	planeServer := startServiceControllerPlane(t)
	planeItem := mustCreateReadyPlane(t, db, "plane-delete", planeServer.endpoint)

	controller := NewServiceController(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, "southbound-token", "apps.example.test")

	created, err := controller.Create(ctx, createInput(planeItem.ID, "gone", "Gone", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	serviceID := created.Metadata.ID
	if _, err := controller.Delete(ctx, serviceID); err != nil {
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

type serviceControllerPlane struct {
	cloudplanev1.UnimplementedControlPlaneExecutionServiceServer

	mu       sync.Mutex
	endpoint string
	apply    []*cloudplanev1.ApplyServiceRequest
	delete   []*cloudplanev1.DeleteServiceRequest
}

func startServiceControllerPlane(t *testing.T) *serviceControllerPlane {
	t.Helper()

	grpcServer := grpc.NewServer()
	plane := &serviceControllerPlane{}
	cloudplanev1.RegisterControlPlaneExecutionServiceServer(grpcServer, plane)

	server := httptest.NewServer(h2c.NewHandler(grpcServer, &http2.Server{}))
	t.Cleanup(func() {
		server.Close()
		grpcServer.Stop()
	})
	plane.endpoint = strings.TrimPrefix(server.URL, "http://")
	return plane
}

func (p *serviceControllerPlane) ApplyService(_ context.Context, req *cloudplanev1.ApplyServiceRequest) (*cloudplanev1.ApplyServiceResponse, error) {
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

func (p *serviceControllerPlane) DeleteService(_ context.Context, req *cloudplanev1.DeleteServiceRequest) (*cloudplanev1.DeleteServiceResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.delete = append(p.delete, req)
	return &cloudplanev1.DeleteServiceResponse{
		ServiceId: req.GetServiceId(),
		Deleted:   true,
	}, nil
}

func (p *serviceControllerPlane) applyRequests() []*cloudplanev1.ApplyServiceRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*cloudplanev1.ApplyServiceRequest(nil), p.apply...)
}

func (p *serviceControllerPlane) deleteRequests() []*cloudplanev1.DeleteServiceRequest {
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
