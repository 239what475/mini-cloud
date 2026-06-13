package coordination

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"mini-cloud/internal/controlplane/model"
	controlplanestore "mini-cloud/internal/controlplane/store"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"
	"mini-cloud/internal/testutil"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestCreateDispatchesServiceToSpecPlane(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeServer := startServiceOperationsPlane(t)
	planeItem := mustCreateReadyPlane(t, db, "plane-create-dispatch", planeServer.endpoint)

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
	dispatchRequests := planeServer.dispatchRequests()
	if len(dispatchRequests) != 1 || dispatchRequests[0].GetServiceName() != "web" {
		t.Fatalf("unexpected dispatch requests: %+v", dispatchRequests)
	}
	if dispatchRequests[0].GetHost() != "web.apps.example.test" {
		t.Fatalf("dispatch host = %q, want web.apps.example.test", dispatchRequests[0].GetHost())
	}
}

func TestCreateSyncsPlaneAfterDispatchToAdvanceFrontDoorDNS(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeServer := startServiceOperationsPlane(t)
	planeServer.frontDoorFromDispatch = true
	planeItem := mustCreateReadyPlane(t, db, "plane-create-frontdoor", planeServer.endpoint)
	dns := &fakeDNSClient{}
	syncer := NewPlaneSyncer(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, "southbound-token", dns)
	operations := NewServiceOperations(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, "southbound-token", "apps.example.test", syncer)

	if _, err := operations.Create(ctx, createInput(planeItem.ID, "frontdoor-web", "Frontdoor Web", "nginx:1.27-alpine")); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if len(dns.records) != 2 {
		t.Fatalf("DNS records = %+v, want verification and CNAME from create request sync", dns.records)
	}
	if dns.records[0].host != "_cdnauth.frontdoor-web.apps.example.test" || dns.records[1].host != "frontdoor-web.apps.example.test" {
		t.Fatalf("DNS records = %+v, want frontdoor verification and CNAME", dns.records)
	}
}

func TestCreateFailsWithoutPersistingBindingWhenInitialDispatchFails(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeServer := startServiceOperationsPlane(t)
	planeItem := mustCreateReadyPlane(t, db, "plane-create-offline", planeServer.endpoint)
	if err := db.Store.UpdatePlaneStatus(ctx, planeItem.ID, controlplanestore.UpdatePlaneStatusInput{Status: model.StatusOffline, Message: "plane unavailable"}); err != nil {
		t.Fatalf("UpdatePlaneStatus returned error: %v", err)
	}

	operations := NewServiceOperations(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, "southbound-token", "apps.example.test", nil)

	_, err := operations.Create(ctx, createInput(planeItem.ID, "pending-web", "Pending Web", "nginx:1.27-alpine"))
	if err == nil {
		t.Fatalf("Create returned nil error, want dispatch failure")
	}
	_, err = db.Store.GetServiceByHost(ctx, "pending-web.apps.example.test")
	if !errors.Is(err, controlplanestore.ErrServiceNotFound) {
		t.Fatalf("GetServiceByHost after failed create error = %v, want ErrServiceNotFound", err)
	}
	if len(planeServer.dispatchRequests()) != 0 {
		t.Fatalf("dispatch requests = %d, want 0 while plane is offline", len(planeServer.dispatchRequests()))
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
	updated, err := operations.Update(ctx, created.Metadata.ID, updateInput("API v2", "nginx:1.28-alpine"))
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if updated.Spec.PlaneID != planeItem.ID {
		t.Fatalf("planeID = %q, want %s", updated.Spec.PlaneID, planeItem.ID)
	}
	if updated.Status.Observed.Phase != model.PhaseProgressing {
		t.Fatalf("phase = %s, want progressing after update", updated.Status.Observed.Phase)
	}
	dispatchRequests := planeServer.dispatchRequests()
	if len(dispatchRequests) != 2 {
		t.Fatalf("dispatchRequests = %d, want 2", len(dispatchRequests))
	}
	if dispatchRequests[1].GetImage() != "nginx:1.28-alpine" {
		t.Fatalf("updated image = %s, want nginx:1.28-alpine", dispatchRequests[1].GetImage())
	}
}

func TestUpdateFailsWithoutChangingBindingWhenDispatchFails(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeServer := startServiceOperationsPlane(t)
	planeItem := mustCreateReadyPlane(t, db, "plane-update-offline", planeServer.endpoint)

	operations := NewServiceOperations(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, "southbound-token", "apps.example.test", nil)

	created, err := operations.Create(ctx, createInput(planeItem.ID, "update-offline", "Update Offline", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if err := db.Store.UpdatePlaneStatus(ctx, planeItem.ID, controlplanestore.UpdatePlaneStatusInput{Status: model.StatusOffline, Message: "plane unavailable"}); err != nil {
		t.Fatalf("UpdatePlaneStatus returned error: %v", err)
	}

	_, err = operations.Update(ctx, created.Metadata.ID, updateInput("Update Offline v2", "nginx:1.28-alpine"))
	if err == nil {
		t.Fatalf("Update returned nil error, want dispatch failure")
	}
	current, err := db.Store.GetService(ctx, created.Metadata.ID)
	if err != nil {
		t.Fatalf("GetService returned error: %v", err)
	}
	if current.Metadata.Generation != created.Metadata.Generation || current.Metadata.DisplayName != created.Metadata.DisplayName {
		t.Fatalf("binding after failed update = %+v, want unchanged", current.Metadata)
	}
}

func TestUpdateRejectsDeletingService(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeServer := startServiceOperationsPlane(t)
	planeItem := mustCreateReadyPlane(t, db, "plane-update-deleting", planeServer.endpoint)

	operations := NewServiceOperations(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, "southbound-token", "apps.example.test", nil)

	created, err := operations.Create(ctx, createInput(planeItem.ID, "update-deleting", "Update Deleting", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if _, err := operations.Delete(ctx, created.Metadata.ID); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if _, err := operations.Update(ctx, created.Metadata.ID, updateInput("Update Deleting v2", "nginx:1.28-alpine")); !errors.Is(err, controlplanestore.ErrServiceDeleting) {
		t.Fatalf("Update deleting service error = %v, want ErrServiceDeleting", err)
	}
	current, err := db.Store.GetService(ctx, created.Metadata.ID)
	if err != nil {
		t.Fatalf("GetService returned error: %v", err)
	}
	if current.Status.DesiredState != model.DesiredStateDeleted {
		t.Fatalf("desired state = %s, want deleted", current.Status.DesiredState)
	}
	if len(planeServer.dispatchRequests()) != 1 {
		t.Fatalf("dispatch requests = %d, want only initial dispatch", len(planeServer.dispatchRequests()))
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

func TestDeleteSyncsPlaneAfterDispatchToAdvanceDNSCleanup(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeServer := startServiceOperationsPlane(t)
	planeServer.frontDoorFromDispatch = true
	planeItem := mustCreateReadyPlane(t, db, "plane-delete-frontdoor", planeServer.endpoint)
	dns := &fakeDNSClient{}
	syncer := NewPlaneSyncer(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, "southbound-token", dns)
	operations := NewServiceOperations(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, "southbound-token", "apps.example.test", syncer)

	created, err := operations.Create(ctx, createInput(planeItem.ID, "frontdoor-delete", "Frontdoor Delete", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	dns.deleted = nil

	if _, err := operations.Delete(ctx, created.Metadata.ID); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if len(dns.deleted) == 0 {
		t.Fatalf("deleted DNS records = %+v, want DNS cleanup from delete request sync", dns.deleted)
	}
}

func TestListMergesLiveCloudPlaneSnapshotsAndCleansDeletedServices(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeServer := startServiceOperationsPlane(t)
	planeItem := mustCreateReadyPlane(t, db, "plane-list-sync", planeServer.endpoint)

	operations := NewServiceOperations(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, "southbound-token", "apps.example.test", NewPlaneSyncer(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, "southbound-token", nil))
	service, err := operations.Create(ctx, createInput(planeItem.ID, "listed", "Listed", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	deleting, err := operations.Create(ctx, createInput(planeItem.ID, "listed-delete", "Listed Delete", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("Create(deleting) returned error: %v", err)
	}
	deleting, err = db.Store.MarkServiceDeletionRequested(ctx, deleting.Metadata.ID)
	if err != nil {
		t.Fatalf("MarkServiceDeletionRequested returned error: %v", err)
	}
	now := time.Now().UTC()
	planeServer.setSnapshot(&cloudplanev1.PlaneSnapshot{
		Plane: &cloudplanev1.PlaneSummary{
			Name:     "plane-list-sync",
			Provider: "aliyun",
			Region:   "cn-beijing",
		},
		CheckedAt:     timestamppb.New(now),
		NodeInventory: &cloudplanev1.PlaneNodeInventory{},
		Executions: []*cloudplanev1.PlaneExecutionSnapshot{
			{
				ServiceId:         service.Metadata.ID,
				ServiceName:       service.Metadata.Name,
				ServiceGeneration: service.Metadata.Generation,
				Status:            planeExecutionStatusRunning,
				ObservedAt:        timestamppb.New(now),
			},
			{
				ServiceId:         deleting.Metadata.ID,
				ServiceName:       deleting.Metadata.Name,
				ServiceGeneration: deleting.Metadata.Generation,
				Status:            planeExecutionStatusSucceeded,
				ObservedAt:        timestamppb.New(now),
			},
		},
		Services: []*cloudplanev1.PlaneService{
			{
				ServiceId:     service.Metadata.ID,
				Name:          service.Metadata.Name,
				DisplayName:   service.Metadata.DisplayName,
				Host:          service.Metadata.Host,
				Generation:    service.Metadata.Generation,
				DesiredState:  model.DesiredStateActive,
				InstanceClass: model.InstanceClassSmall,
				Exposure:      model.ExposurePublic,
				Image:         "nginx:1.27-alpine",
				ContainerPort: 80,
				ReadinessPath: "/",
			},
		},
	})

	services, err := operations.List(ctx)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("services len = %d, want 1", len(services))
	}
	if services[0].Status.Observed.Phase != model.PhaseReady || services[0].Status.Run.Phase != model.RunPhaseRunning {
		t.Fatalf("service status after List = %+v, want ready/running from cloud-plane snapshot", services[0].Status)
	}
}

func createInput(planeID string, name string, displayName string, image string) controlplanestore.CreateServiceInput {
	return controlplanestore.CreateServiceInput{Name: name, DisplayName: displayName, Spec: serviceSpec(planeID, image)}
}

func updateInput(displayName string, image string) controlplanestore.UpdateServiceInput {
	return controlplanestore.UpdateServiceInput{DisplayName: displayName, Spec: workloadSpec(image)}
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

func workloadSpec(image string) model.WorkloadSpec {
	return model.WorkloadSpec{
		InstanceClass: model.InstanceClassSmall,
		Exposure:      "public",
		Image:         image,
		DefaultPort:   80,
		ReadinessPath: "/",
	}
}

type serviceOperationsPlane struct {
	cloudplanev1.UnimplementedCloudPlaneServiceServer
	cloudplanev1.UnimplementedCloudPlaneSnapshotServiceServer

	mu       sync.Mutex
	endpoint string
	dispatch []*cloudplanev1.UpsertServiceRequest
	delete   []*cloudplanev1.DeleteServiceRequest
	snapshot *cloudplanev1.PlaneSnapshot

	frontDoorFromDispatch bool
	deleteError           error
}

func startServiceOperationsPlane(t *testing.T) *serviceOperationsPlane {
	t.Helper()

	grpcServer := grpc.NewServer()
	plane := &serviceOperationsPlane{}
	cloudplanev1.RegisterCloudPlaneServiceServer(grpcServer, plane)
	cloudplanev1.RegisterCloudPlaneSnapshotServiceServer(grpcServer, plane)

	server := httptest.NewServer(h2c.NewHandler(grpcServer, &http2.Server{}))
	t.Cleanup(func() {
		server.Close()
		grpcServer.Stop()
	})
	plane.endpoint = strings.TrimPrefix(server.URL, "http://")
	return plane
}

func (p *serviceOperationsPlane) GetSnapshot(context.Context, *cloudplanev1.GetSnapshotRequest) (*cloudplanev1.GetSnapshotResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.snapshot != nil {
		return &cloudplanev1.GetSnapshotResponse{Snapshot: p.snapshot}, nil
	}
	return &cloudplanev1.GetSnapshotResponse{Snapshot: &cloudplanev1.PlaneSnapshot{
		Plane: &cloudplanev1.PlaneSummary{
			Name:     "test-plane",
			Provider: "tencent",
			Region:   "ap-guangzhou",
		},
		NodeInventory: &cloudplanev1.PlaneNodeInventory{},
	}}, nil
}

func (p *serviceOperationsPlane) setSnapshot(snapshot *cloudplanev1.PlaneSnapshot) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.snapshot = snapshot
}

func (p *serviceOperationsPlane) UpsertService(_ context.Context, req *cloudplanev1.UpsertServiceRequest) (*cloudplanev1.UpsertServiceResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dispatch = append(p.dispatch, req)
	if p.frontDoorFromDispatch {
		p.snapshot = &cloudplanev1.PlaneSnapshot{
			Plane:         &cloudplanev1.PlaneSummary{Name: "test-plane", Provider: "aliyun", Region: "cn-beijing"},
			CheckedAt:     timestamppb.Now(),
			NodeInventory: &cloudplanev1.PlaneNodeInventory{},
			Services: []*cloudplanev1.PlaneService{
				{
					ServiceId:    req.GetServiceId(),
					Name:         req.GetServiceName(),
					Host:         req.GetHost(),
					Generation:   req.GetServiceGeneration(),
					DesiredState: model.DesiredStateActive,
				},
			},
			FrontdoorDomains: []*cloudplanev1.PlaneFrontDoorDomain{
				{
					Host:            req.GetHost(),
					Cname:           req.GetHost() + ".cdn.example.net",
					VerifySubdomain: "_cdnauth." + req.GetHost(),
					VerifyType:      "TXT",
					VerifyValue:     "verify-token",
				},
			},
		}
	}
	return &cloudplanev1.UpsertServiceResponse{}, nil
}

func (p *serviceOperationsPlane) DeleteService(_ context.Context, req *cloudplanev1.DeleteServiceRequest) (*cloudplanev1.DeleteServiceResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.delete = append(p.delete, req)
	if p.deleteError != nil {
		return nil, p.deleteError
	}
	if p.frontDoorFromDispatch {
		p.snapshot = &cloudplanev1.PlaneSnapshot{
			Plane:         &cloudplanev1.PlaneSummary{Name: "test-plane", Provider: "aliyun", Region: "cn-beijing"},
			CheckedAt:     timestamppb.Now(),
			NodeInventory: &cloudplanev1.PlaneNodeInventory{},
			Executions: []*cloudplanev1.PlaneExecutionSnapshot{
				{
					ServiceId:         req.GetServiceId(),
					ServiceGeneration: req.GetServiceGeneration(),
					Status:            planeExecutionStatusSucceeded,
					ObservedAt:        timestamppb.Now(),
				},
			},
		}
	}
	return &cloudplanev1.DeleteServiceResponse{}, nil
}

func (p *serviceOperationsPlane) dispatchRequests() []*cloudplanev1.UpsertServiceRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*cloudplanev1.UpsertServiceRequest(nil), p.dispatch...)
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
