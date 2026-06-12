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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

func TestUpdateSyncsPlaneAfterDispatchToAdvanceFrontDoorDNS(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeServer := startServiceOperationsPlane(t)
	planeItem := mustCreateReadyPlane(t, db, "plane-update-frontdoor", planeServer.endpoint)
	dns := &fakeDNSClient{}
	syncer := NewPlaneSyncer(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, "southbound-token", dns)
	operations := NewServiceOperations(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, "southbound-token", "apps.example.test", syncer)

	created, err := operations.Create(ctx, createInput(planeItem.ID, "frontdoor-update", "Frontdoor Update", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	dns.records = nil
	planeServer.frontDoorFromDispatch = true

	if _, err := operations.Update(ctx, created.Metadata.ID, updateInput("Frontdoor Update v2", "nginx:1.28-alpine")); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if len(dns.records) != 2 {
		t.Fatalf("DNS records = %+v, want verification and CNAME from update request sync", dns.records)
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
	if len(planeServer.dispatchRequests()) != 1 {
		t.Fatalf("dispatch requests = %d, want 1", len(planeServer.dispatchRequests()))
	}
}

func TestCreateKeepsServiceWhenInitialDispatchFails(t *testing.T) {
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
		t.Fatalf("service status = %+v, want active degraded after failed initial dispatch", service.Status)
	}
	if service.Status.Observed.ObservedGeneration != 0 {
		t.Fatalf("observed generation = %d, want 0 so failed dispatch remains retryable", service.Status.Observed.ObservedGeneration)
	}
	_, pending, err := db.Store.GetPendingDispatchService(ctx, service.Metadata.ID)
	if err != nil {
		t.Fatalf("GetPendingDispatchService returned error: %v", err)
	}
	if !pending {
		t.Fatalf("service is not pending dispatch, want failed service to remain retryable")
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

func TestUpdateKeepsServiceRetryableWhenDispatchFails(t *testing.T) {
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

	updated, err := operations.Update(ctx, created.Metadata.ID, updateInput("Update Offline v2", "nginx:1.28-alpine"))
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if updated.Metadata.Generation != created.Metadata.Generation+1 {
		t.Fatalf("generation = %d, want %d", updated.Metadata.Generation, created.Metadata.Generation+1)
	}
	if updated.Status.Observed.ObservedGeneration != created.Metadata.Generation {
		t.Fatalf("observed generation = %d, want previous generation %d", updated.Status.Observed.ObservedGeneration, created.Metadata.Generation)
	}
	_, pending, err := db.Store.GetPendingDispatchService(ctx, updated.Metadata.ID)
	if err != nil {
		t.Fatalf("GetPendingDispatchService returned error: %v", err)
	}
	if !pending {
		t.Fatalf("service is not pending dispatch, want failed update to remain retryable")
	}
}

func TestUpdateKeepsExistingPlaneBinding(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeServerA := startServiceOperationsPlane(t)
	planeA := mustCreateReadyPlane(t, db, "plane-move-a", planeServerA.endpoint)

	operations := NewServiceOperations(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, "southbound-token", "apps.example.test", nil)

	created, err := operations.Create(ctx, createInput(planeA.ID, "move", "Move", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	updated, err := operations.Update(ctx, created.Metadata.ID, updateInput("Move", "nginx:1.28-alpine"))
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if updated.Spec.PlaneID != planeA.ID {
		t.Fatalf("updated planeID = %q, want original plane %s", updated.Spec.PlaneID, planeA.ID)
	}
	dispatchRequests := planeServerA.dispatchRequests()
	if len(dispatchRequests) != 2 {
		t.Fatalf("plane A dispatch requests = %d, want create and update dispatch requests", len(dispatchRequests))
	}
	if len(planeServerA.deleteRequests()) != 0 {
		t.Fatalf("plane A delete requests = %d, want 0", len(planeServerA.deleteRequests()))
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

func TestDeleteRemovesBindingAndDNSWhenRemoteServiceAlreadyMissing(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeServer := startServiceOperationsPlane(t)
	planeServer.deleteError = status.Error(codes.NotFound, "service not found")
	planeItem := mustCreateReadyPlane(t, db, "plane-delete-missing", planeServer.endpoint)
	dns := &fakeDNSClient{}
	operations := NewServiceOperations(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		db.Store,
		"southbound-token",
		"apps.example.test",
		NewPlaneSyncer(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, "southbound-token", dns),
	)

	created, err := operations.Create(ctx, createInput(planeItem.ID, "missing-remote", "Missing Remote", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if err := db.Store.SaveServiceDNSRecord(ctx, controlplanestore.DNSRecord{
		ServiceID:  created.Metadata.ID,
		Host:       created.Metadata.Host,
		RecordType: "CNAME",
		Value:      "missing-remote.apps.example.test.cdn.example.net",
		Purpose:    controlplanestore.DNSRecordPurposeFrontDoorCNAME,
	}); err != nil {
		t.Fatalf("SaveServiceDNSRecord returned error: %v", err)
	}

	deleting, err := operations.Delete(ctx, created.Metadata.ID)
	if err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if deleting.Metadata.ID != created.Metadata.ID || deleting.Status.DesiredState != model.DesiredStateDeleted {
		t.Fatalf("Delete returned %+v, want last deleting service resource", deleting)
	}
	if _, err := db.Store.GetService(ctx, created.Metadata.ID); !errors.Is(err, controlplanestore.ErrServiceNotFound) {
		t.Fatalf("GetService after missing remote delete error = %v, want service not found", err)
	}
	if len(dns.deleted) != 1 || dns.deleted[0].host != created.Metadata.Host || dns.deleted[0].recordType != "CNAME" {
		t.Fatalf("deleted DNS records = %+v, want CNAME cleanup", dns.deleted)
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

func TestGetAdvancesOnlyRequestedService(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeServer := startServiceOperationsPlane(t)
	planeItem := mustCreateReadyPlane(t, db, "plane-get-advance", planeServer.endpoint)

	target, err := db.Store.CreateService(ctx, createInput(planeItem.ID, "target-dispatch", "Target Dispatch", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("CreateService(target) returned error: %v", err)
	}
	other, err := db.Store.CreateService(ctx, createInput(planeItem.ID, "other-dispatch", "Other Dispatch", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("CreateService(other) returned error: %v", err)
	}

	operations := NewServiceOperations(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, "southbound-token", "apps.example.test", nil)
	if _, err := operations.Get(ctx, target.Metadata.ID); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}

	dispatchRequests := planeServer.dispatchRequests()
	if len(dispatchRequests) != 1 {
		t.Fatalf("dispatchRequests len = %d, want 1", len(dispatchRequests))
	}
	if dispatchRequests[0].GetServiceId() != target.Metadata.ID {
		t.Fatalf("dispatch request serviceID = %q, want target %s", dispatchRequests[0].GetServiceId(), target.Metadata.ID)
	}
	_, targetPending, err := db.Store.GetPendingDispatchService(ctx, target.Metadata.ID)
	if err != nil {
		t.Fatalf("GetPendingDispatchService(target) returned error: %v", err)
	}
	if targetPending {
		t.Fatalf("target service is still pending after Get")
	}
	_, otherPending, err := db.Store.GetPendingDispatchService(ctx, other.Metadata.ID)
	if err != nil {
		t.Fatalf("GetPendingDispatchService(other) returned error: %v", err)
	}
	if !otherPending {
		t.Fatalf("other service is not pending; Get should only advance requested service")
	}
}

func TestListAdvancesPendingServicesAndSyncsBeforeReturning(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	planeServer := startServiceOperationsPlane(t)
	planeItem := mustCreateReadyPlane(t, db, "plane-list-sync", planeServer.endpoint)

	service, err := db.Store.CreateService(ctx, createInput(planeItem.ID, "listed", "Listed", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("CreateService returned error: %v", err)
	}
	deleting, err := db.Store.CreateService(ctx, createInput(planeItem.ID, "listed-delete", "Listed Delete", "nginx:1.27-alpine"))
	if err != nil {
		t.Fatalf("CreateService(deleting) returned error: %v", err)
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
	})

	operations := NewServiceOperations(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, "southbound-token", "apps.example.test", NewPlaneSyncer(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, "southbound-token", nil))
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
	if len(planeServer.dispatchRequests()) != 1 {
		t.Fatalf("dispatch requests = %d, want one pending dispatch advanced by List", len(planeServer.dispatchRequests()))
	}
	if len(planeServer.deleteRequests()) != 1 {
		t.Fatalf("delete requests = %d, want one pending delete advanced by List", len(planeServer.deleteRequests()))
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
