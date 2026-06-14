package coordination

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	controlplaneconfig "mini-cloud/internal/controlplane/config"
	"mini-cloud/internal/controlplane/model"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestCreateDispatchesServiceToSpecPlane(t *testing.T) {
	ctx := context.Background()
	planeServer := startServiceOperationsPlane(t)
	catalog, planeItem := mustCreateCatalog(t, "plane-create-dispatch", planeServer.endpoint)

	operations := NewServiceOperations(testLogger(), catalog, "southbound-token", PlaneClientTLS{}, "apps.example.test", nil)

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
	planeServer := startServiceOperationsPlane(t)
	planeServer.frontDoorFromDispatch = true
	catalog, planeItem := mustCreateCatalog(t, "plane-create-frontdoor", planeServer.endpoint)
	dns := &fakeDNSClient{}
	syncer := NewPlaneSyncer(testLogger(), catalog, "southbound-token", PlaneClientTLS{}, dns)
	operations := NewServiceOperations(testLogger(), catalog, "southbound-token", PlaneClientTLS{}, "apps.example.test", syncer)

	if _, err := operations.Create(ctx, createInput(planeItem.ID, "frontdoor-web", "Frontdoor Web", "nginx:1.27-alpine")); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if len(dns.records) != 2 {
		t.Fatalf("DNS records = %+v, want verification and CNAME from create request sync", dns.records)
	}
	if dns.records[0].Host != "_cdnauth.frontdoor-web.apps.example.test" || dns.records[1].Host != "frontdoor-web.apps.example.test" || dns.records[0].PlaneID != planeItem.ID || dns.records[1].PlaneID != planeItem.ID {
		t.Fatalf("DNS records = %+v, want frontdoor verification and CNAME", dns.records)
	}
}

func TestUpdateDispatchesServiceToSpecPlane(t *testing.T) {
	ctx := context.Background()
	planeServer := startServiceOperationsPlane(t)
	catalog, planeItem := mustCreateCatalog(t, "plane-update", planeServer.endpoint)

	operations := NewServiceOperations(testLogger(), catalog, "southbound-token", PlaneClientTLS{}, "apps.example.test", nil)

	created, err := operations.Create(ctx, createInput(planeItem.ID, "api", "API", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	updated, err := operations.Update(ctx, updateInput(planeItem.ID, created.Metadata.ID, "API v2", "nginx:1.28-alpine"))
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

func TestDeleteDispatchesServiceDeleteToCloudPlane(t *testing.T) {
	ctx := context.Background()
	planeServer := startServiceOperationsPlane(t)
	catalog, planeItem := mustCreateCatalog(t, "plane-delete", planeServer.endpoint)

	operations := NewServiceOperations(testLogger(), catalog, "southbound-token", PlaneClientTLS{}, "apps.example.test", nil)

	created, err := operations.Create(ctx, createInput(planeItem.ID, "gone", "Gone", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	serviceID := created.Metadata.ID
	deleting, err := operations.Delete(ctx, DeleteServiceInput{PlaneID: planeItem.ID, ServiceID: serviceID})
	if err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if deleting.Status.Observed.Phase != model.PhaseDeleting {
		t.Fatalf("service status after delete = %+v, want deleting", deleting.Status)
	}
	deleteRequests := planeServer.deleteRequests()
	if len(deleteRequests) != 1 {
		t.Fatalf("deleteRequests len = %d, want 1", len(deleteRequests))
	}
	if deleteRequests[0].GetServiceId() != serviceID ||
		deleteRequests[0].GetServiceGeneration() != deleting.Metadata.Generation {
		t.Fatalf("delete input = %+v, want service generation delete", deleteRequests[0])
	}
}

func TestDeleteCleansServiceDNSByOwnershipRemark(t *testing.T) {
	ctx := context.Background()
	planeServer := startServiceOperationsPlane(t)
	planeServer.frontDoorFromDispatch = true
	catalog, planeItem := mustCreateCatalog(t, "plane-delete-frontdoor", planeServer.endpoint)
	dns := &fakeDNSClient{}
	syncer := NewPlaneSyncer(testLogger(), catalog, "southbound-token", PlaneClientTLS{}, dns)
	operations := NewServiceOperations(testLogger(), catalog, "southbound-token", PlaneClientTLS{}, "apps.example.test", syncer)

	created, err := operations.Create(ctx, createInput(planeItem.ID, "frontdoor-delete", "Frontdoor Delete", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	dns.deleted = nil
	dns.deletedServices = nil

	if _, err := operations.Delete(ctx, DeleteServiceInput{PlaneID: planeItem.ID, ServiceID: created.Metadata.ID}); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if len(dns.deletedServices) == 0 {
		t.Fatalf("deleted service DNS records = %+v, want service DNS cleanup", dns.deletedServices)
	}
}

func TestListUsesLiveCloudPlaneSnapshots(t *testing.T) {
	ctx := context.Background()
	planeServer := startServiceOperationsPlane(t)
	catalog, planeItem := mustCreateCatalog(t, "plane-list-sync", planeServer.endpoint)

	operations := NewServiceOperations(testLogger(), catalog, "southbound-token", PlaneClientTLS{}, "apps.example.test", NewPlaneSyncer(testLogger(), catalog, "southbound-token", PlaneClientTLS{}, nil))
	service, err := operations.Create(ctx, createInput(planeItem.ID, "listed", "Listed", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
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
		},
		Services: []*cloudplanev1.PlaneService{
			{
				ServiceId:     service.Metadata.ID,
				Name:          service.Metadata.Name,
				DisplayName:   service.Metadata.DisplayName,
				Host:          service.Metadata.Host,
				Generation:    service.Metadata.Generation,
				DesiredState:  "active",
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

func createInput(planeID string, name string, displayName string, image string) CreateServiceInput {
	return CreateServiceInput{Name: name, DisplayName: displayName, Spec: serviceSpec(planeID, image)}
}

func updateInput(planeID string, serviceID string, displayName string, image string) UpdateServiceInput {
	return UpdateServiceInput{PlaneID: planeID, ServiceID: serviceID, DisplayName: displayName, Spec: workloadSpec(image)}
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
	p.snapshot = snapshotFromUpsert(req, p.frontDoorFromDispatch)
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

func snapshotFromUpsert(req *cloudplanev1.UpsertServiceRequest, includeFrontDoor bool) *cloudplanev1.PlaneSnapshot {
	service := &cloudplanev1.PlaneService{
		ServiceId:     req.GetServiceId(),
		Name:          req.GetServiceName(),
		DisplayName:   req.GetDisplayName(),
		Host:          req.GetHost(),
		Generation:    req.GetServiceGeneration(),
		DesiredState:  "active",
		InstanceClass: req.GetInstanceClass(),
		Exposure:      req.GetExposure(),
		Image:         req.GetImage(),
		Command:       append([]string(nil), req.GetCommand()...),
		Args:          append([]string(nil), req.GetArgs()...),
		Env:           req.GetEnv(),
		ContainerPort: req.GetContainerPort(),
		ReadinessPath: req.GetReadinessPath(),
	}
	if includeFrontDoor {
		service.FrontdoorCname = req.GetHost() + ".cdn.example.net"
		service.FrontdoorVerifySubdomain = "_cdnauth." + req.GetHost()
		service.FrontdoorVerifyType = "TXT"
		service.FrontdoorVerifyValue = "verify-token"
	}
	return &cloudplanev1.PlaneSnapshot{
		Plane:         &cloudplanev1.PlaneSummary{Name: "test-plane", Provider: "aliyun", Region: "cn-beijing"},
		CheckedAt:     timestamppb.Now(),
		NodeInventory: &cloudplanev1.PlaneNodeInventory{},
		Services:      []*cloudplanev1.PlaneService{service},
	}
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

func mustCreateCatalog(t *testing.T, name string, endpoint string) (*PlaneCatalog, model.PlaneDetail) {
	t.Helper()
	catalog, err := NewPlaneCatalog([]controlplaneconfig.PlaneConfig{
		{
			ID:           "pln_" + strings.ReplaceAll(name, "-", "_"),
			Name:         name,
			DisplayName:  name,
			Provider:     "aliyun",
			Region:       "cn-beijing",
			GRPCEndpoint: endpoint,
		},
	})
	if err != nil {
		t.Fatalf("NewPlaneCatalog returned error: %v", err)
	}
	plane, err := catalog.GetPlane(context.Background(), "pln_"+strings.ReplaceAll(name, "-", "_"))
	if err != nil {
		t.Fatalf("GetPlane returned error: %v", err)
	}
	return catalog, plane
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
