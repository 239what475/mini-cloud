package store_test

import (
	"context"
	"errors"
	"testing"

	"mini-cloud/internal/controlplane/model"
	controlplanestore "mini-cloud/internal/controlplane/store"
	"mini-cloud/internal/testutil"
)

func TestIntegrationPlaneLifecycle(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)

	created, err := db.Store.RegisterPlane(ctx, controlplanestore.RegisterPlaneInput{
		Name:         "aliyun-bj-primary",
		DisplayName:  "Aliyun Beijing Primary",
		Provider:     "aliyun",
		Region:       "cn-beijing",
		GRPCEndpoint: "grpc://plane-a.example.com:443",
	})
	if err != nil {
		t.Fatalf("RegisterPlane returned error: %v", err)
	}
	if created.Status.Status != model.StatusSyncing {
		t.Fatalf("initial status = %s, want syncing", created.Status.Status)
	}
	if created.GRPCEndpoint != "plane-a.example.com:443" {
		t.Fatalf("grpcEndpoint = %q, want normalized target", created.GRPCEndpoint)
	}

	if err := db.Store.UpdatePlaneStatus(ctx, created.ID, controlplanestore.UpdatePlaneStatusInput{
		Status:  model.StatusReady,
		Message: "snapshot healthy",
	}); err != nil {
		t.Fatalf("UpdatePlaneStatus returned error: %v", err)
	}
	got, err := db.Store.GetPlane(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetPlane returned error: %v", err)
	}
	if got.Status.Status != model.StatusReady || got.Status.Message != "snapshot healthy" {
		t.Fatalf("status = %+v, want ready snapshot healthy", got.Status)
	}

	ids, err := db.Store.ListPlaneIDs(ctx)
	if err != nil {
		t.Fatalf("ListPlaneIDs returned error: %v", err)
	}
	if len(ids) != 1 || ids[0] != created.ID {
		t.Fatalf("plane ids = %+v, want created plane", ids)
	}

	if err := db.Store.DeletePlane(ctx, created.ID); err != nil {
		t.Fatalf("DeletePlane returned error: %v", err)
	}
	if _, err := db.Store.GetPlane(ctx, created.ID); !errors.Is(err, controlplanestore.ErrPlaneNotFound) {
		t.Fatalf("GetPlane after delete error = %v, want ErrPlaneNotFound", err)
	}
}

func TestIntegrationRegisterPlaneRefreshDoesNotResetCurrentStatus(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)

	created, err := db.Store.RegisterPlane(ctx, controlplanestore.RegisterPlaneInput{
		Name:         "refresh-plane",
		DisplayName:  "Refresh Plane",
		Provider:     "tencent",
		Region:       "ap-guangzhou",
		GRPCEndpoint: "refresh-plane.example.com:18081",
	})
	if err != nil {
		t.Fatalf("RegisterPlane returned error: %v", err)
	}
	if err := db.Store.UpdatePlaneStatus(ctx, created.ID, controlplanestore.UpdatePlaneStatusInput{
		Status:  model.StatusReady,
		Message: "last snapshot healthy",
	}); err != nil {
		t.Fatalf("UpdatePlaneStatus returned error: %v", err)
	}

	refreshed, err := db.Store.RegisterPlane(ctx, controlplanestore.RegisterPlaneInput{
		Name:         "refresh-plane",
		DisplayName:  "Refresh Plane",
		Provider:     "tencent",
		Region:       "ap-guangzhou",
		GRPCEndpoint: "refresh-plane.example.com:18081",
	})
	if err != nil {
		t.Fatalf("RegisterPlane(refresh) returned error: %v", err)
	}
	if refreshed.ID != created.ID {
		t.Fatalf("refreshed plane id = %q, want existing %q", refreshed.ID, created.ID)
	}
	if refreshed.Status.Status != model.StatusReady || refreshed.Status.Message != "last snapshot healthy" {
		t.Fatalf("refreshed status = %+v, want existing ready status", refreshed.Status)
	}
	if refreshed.Status.LastHeartbeatAt == nil {
		t.Fatalf("refreshed status did not update heartbeat")
	}
}

func TestIntegrationServiceBindingLifecycle(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	plane := createPlane(t, db)

	service, err := db.Store.CreateService(ctx, controlplanestore.CreateServiceInput{
		Name:        "api",
		DisplayName: "API",
		Host:        "api.apps.example.test",
		Spec: model.ServiceSpec{
			PlaneID:       plane.ID,
			InstanceClass: model.InstanceClassSmall,
			Exposure:      model.ExposurePublic,
			Image:         "nginx:1.27-alpine",
			DefaultPort:   80,
			ReadinessPath: "/",
		},
	})
	if err != nil {
		t.Fatalf("CreateService returned error: %v", err)
	}
	if service.Spec.PlaneID != plane.ID {
		t.Fatalf("planeID = %q, want %q", service.Spec.PlaneID, plane.ID)
	}
	if service.Spec.Image != "" {
		t.Fatalf("control-plane persisted workload image = %q, want empty binding-only spec", service.Spec.Image)
	}

	updated, err := db.Store.UpdateServiceBinding(ctx, service.Metadata.ID, controlplanestore.UpdateServiceInput{
		DisplayName: "API v2",
		Spec: model.WorkloadSpec{
			InstanceClass: model.InstanceClassSmall,
			Exposure:      model.ExposurePublic,
			Image:         "nginx:1.28-alpine",
			DefaultPort:   80,
			ReadinessPath: "/",
		},
	})
	if err != nil {
		t.Fatalf("UpdateServiceBinding returned error: %v", err)
	}
	if updated.Metadata.Generation != service.Metadata.Generation+1 || updated.Metadata.DisplayName != "API v2" {
		t.Fatalf("updated binding = %+v, want generation increment and display name update", updated.Metadata)
	}
	if updated.Spec.Image != "" {
		t.Fatalf("updated binding image = %q, want workload spec not persisted in control-plane", updated.Spec.Image)
	}

	deleting, err := db.Store.MarkServiceDeletionRequested(ctx, service.Metadata.ID)
	if err != nil {
		t.Fatalf("MarkServiceDeletionRequested returned error: %v", err)
	}
	if deleting.Status.DesiredState != model.DesiredStateDeleted || deleting.Metadata.Generation != updated.Metadata.Generation+1 {
		t.Fatalf("deleting binding = %+v, want deleted state and generation increment", deleting)
	}
	if _, err := db.Store.UpdateServiceBinding(ctx, service.Metadata.ID, controlplanestore.UpdateServiceInput{
		DisplayName: "API v3",
		Spec: model.WorkloadSpec{
			InstanceClass: model.InstanceClassSmall,
			Exposure:      model.ExposurePublic,
			Image:         "nginx:1.29-alpine",
			DefaultPort:   80,
			ReadinessPath: "/",
		},
	}); !errors.Is(err, controlplanestore.ErrServiceDeleting) {
		t.Fatalf("UpdateServiceBinding deleting error = %v, want ErrServiceDeleting", err)
	}

	if err := db.Store.DeleteServiceForGeneration(ctx, service.Metadata.ID, deleting.Metadata.Generation); err != nil {
		t.Fatalf("DeleteServiceForGeneration returned error: %v", err)
	}
	if _, err := db.Store.GetService(ctx, service.Metadata.ID); !errors.Is(err, controlplanestore.ErrServiceNotFound) {
		t.Fatalf("GetService after delete error = %v, want ErrServiceNotFound", err)
	}
}

func TestIntegrationDeletePlaneWithServicesReturnsConflict(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	plane := createPlane(t, db)

	if _, err := db.Store.CreateService(ctx, controlplanestore.CreateServiceInput{
		Name:        "bound-service",
		DisplayName: "Bound Service",
		Host:        "bound-service.apps.example.test",
		Spec: model.ServiceSpec{
			PlaneID:       plane.ID,
			InstanceClass: model.InstanceClassSmall,
			Exposure:      model.ExposurePublic,
			Image:         "nginx:1.27-alpine",
			DefaultPort:   80,
			ReadinessPath: "/",
		},
	}); err != nil {
		t.Fatalf("CreateService returned error: %v", err)
	}

	if err := db.Store.DeletePlane(ctx, plane.ID); !errors.Is(err, controlplanestore.ErrPlaneHasServices) {
		t.Fatalf("DeletePlane error = %v, want ErrPlaneHasServices", err)
	}
}

func TestIntegrationDeletingServiceLookupAndDNSRecords(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenControlPlaneTestDatabase(t)
	plane := createPlane(t, db)
	service, err := db.Store.CreateService(ctx, controlplanestore.CreateServiceInput{
		Name:        "delete-api",
		DisplayName: "Delete API",
		Host:        "delete-api.apps.example.test",
		Spec: model.ServiceSpec{
			PlaneID:       plane.ID,
			InstanceClass: model.InstanceClassSmall,
			Exposure:      model.ExposurePublic,
			Image:         "nginx:1.27-alpine",
			DefaultPort:   80,
			ReadinessPath: "/",
		},
	})
	if err != nil {
		t.Fatalf("CreateService returned error: %v", err)
	}
	deleting, err := db.Store.MarkServiceDeletionRequested(ctx, service.Metadata.ID)
	if err != nil {
		t.Fatalf("MarkServiceDeletionRequested returned error: %v", err)
	}

	items, err := db.Store.ListDeletingServices(ctx)
	if err != nil {
		t.Fatalf("ListDeletingServices returned error: %v", err)
	}
	if len(items) != 1 || items[0].Metadata.ID != service.Metadata.ID {
		t.Fatalf("ListDeletingServices = %+v, want deleting service", items)
	}

	if err := db.Store.SaveServiceDNSRecord(ctx, controlplanestore.DNSRecord{
		ServiceID:  deleting.Metadata.ID,
		Host:       deleting.Metadata.Host,
		RecordType: "CNAME",
		Value:      "delete-api.apps.example.test.cdn.example.net",
		Purpose:    controlplanestore.DNSRecordPurposeFrontDoorCNAME,
	}); err != nil {
		t.Fatalf("SaveServiceDNSRecord returned error: %v", err)
	}
	records, err := db.Store.ListServiceDNSRecords(ctx, deleting.Metadata.ID)
	if err != nil {
		t.Fatalf("ListServiceDNSRecords returned error: %v", err)
	}
	if len(records) != 1 || records[0].Host != deleting.Metadata.Host {
		t.Fatalf("DNS records = %+v, want saved record", records)
	}
}

func createPlane(t *testing.T, db testutil.ControlPlaneTestDatabase) model.PlaneDetail {
	t.Helper()
	plane, err := db.Store.RegisterPlane(context.Background(), controlplanestore.RegisterPlaneInput{
		Name:         "test-plane",
		DisplayName:  "Test Plane",
		Provider:     "tencent",
		Region:       "ap-guangzhou",
		GRPCEndpoint: "test-plane.example.test:18081",
	})
	if err != nil {
		t.Fatalf("RegisterPlane returned error: %v", err)
	}
	return plane
}
