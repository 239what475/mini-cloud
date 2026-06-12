package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"mini-cloud/internal/controlplane/model"
	controlplanestore "mini-cloud/internal/controlplane/store"
	"mini-cloud/internal/testutil"
)

func TestIntegrationPlaneStatusCapacityAndNodeInventoryLifecycle(t *testing.T) {
	db := testutil.OpenControlPlaneTestDatabase(t)

	createdPlane, err := db.Store.RegisterPlane(context.Background(), controlplanestore.RegisterPlaneInput{
		Name:         "aliyun-bj-primary",
		DisplayName:  "Aliyun Beijing Primary",
		Provider:     "aliyun",
		Region:       "cn-beijing",
		GRPCEndpoint: "plane-a.example.com:443",
	})
	if err != nil {
		t.Fatalf("RegisterPlane returned error: %v", err)
	}
	if createdPlane.Status.Status != model.StatusSyncing {
		t.Fatalf("initial plane status = %v, want %v", createdPlane.Status.Status, model.StatusSyncing)
	}
	if createdPlane.GRPCEndpoint != "plane-a.example.com:443" {
		t.Fatalf("plane grpcEndpoint = %q", createdPlane.GRPCEndpoint)
	}
	planeIDs, err := db.Store.ListPlaneIDs(context.Background())
	if err != nil {
		t.Fatalf("ListPlaneIDs returned error: %v", err)
	}
	if len(planeIDs) != 1 || planeIDs[0] != createdPlane.ID {
		t.Fatalf("unexpected plane ids: %+v", planeIDs)
	}

	if err := db.Store.UpdatePlaneStatus(context.Background(), createdPlane.ID, controlplanestore.UpdatePlaneStatusInput{
		Status:  model.StatusReady,
		Message: "heartbeat and snapshot are healthy",
	}); err != nil {
		t.Fatalf("UpdatePlaneStatus returned error: %v", err)
	}
	gotPlane, err := db.Store.GetPlane(context.Background(), createdPlane.ID)
	if err != nil {
		t.Fatalf("GetPlane returned error: %v", err)
	}
	if gotPlane.Status.Status != model.StatusReady {
		t.Fatalf("GetPlane status = %v, want ready", gotPlane.Status.Status)
	}
	if err := db.Store.ReplacePlaneNodeInventory(context.Background(), createdPlane.ID, controlplanestore.RecordNodeInventoryInput{
		ObservedAt:        time.Now().UTC(),
		NodesTotal:        2,
		NodesReady:        2,
		CPUMilliCapacity:  4000,
		CPUMilliAllocated: 1500,
		MemoryMiCapacity:  8192,
		MemoryMiAllocated: 2048,
		Nodes: []model.PlaneNode{
			{
				NodeID:            "node-a",
				Name:              "node-a",
				Provider:          "aliyun",
				Region:            "cn-beijing",
				InstanceID:        "i-node-a",
				InstanceType:      "ecs.u1-c1m1.large",
				Status:            "ready",
				Schedulable:       true,
				Elastic:           true,
				CPUMilliCapacity:  2000,
				CPUMilliAllocated: 500,
				MemoryMiCapacity:  4096,
				MemoryMiAllocated: 1024,
			},
			{
				NodeID:            "node-b",
				Name:              "node-b",
				Provider:          "aliyun",
				Region:            "cn-beijing",
				InstanceID:        "i-node-b",
				InstanceType:      "ecs.u1-c1m1.large",
				Status:            "ready",
				Schedulable:       true,
				Elastic:           false,
				CPUMilliCapacity:  2000,
				CPUMilliAllocated: 1000,
				MemoryMiCapacity:  4096,
				MemoryMiAllocated: 1024,
			},
		},
	}); err != nil {
		t.Fatalf("ReplacePlaneNodeInventory returned error: %v", err)
	}
	gotPlane, err = db.Store.GetPlane(context.Background(), createdPlane.ID)
	if err != nil {
		t.Fatalf("GetPlane(after node inventory) returned error: %v", err)
	}
	if gotPlane.LatestNodeInventory == nil {
		t.Fatalf("expected latest node inventory to be populated")
	}

	listedPlanes, err := db.Store.ListPlanes(context.Background())
	if err != nil {
		t.Fatalf("ListPlanes returned error: %v", err)
	}
	if len(listedPlanes) != 1 {
		t.Fatalf("expected 1 plane, got %d", len(listedPlanes))
	}
	if listedPlanes[0].LatestNodeInventory == nil {
		t.Fatalf("expected listed plane node inventory to be populated")
	}

	if err := db.Store.DeletePlane(context.Background(), createdPlane.ID); err != nil {
		t.Fatalf("DeletePlane returned error: %v", err)
	}
	if _, err := db.Store.GetPlane(context.Background(), createdPlane.ID); !errors.Is(err, controlplanestore.ErrPlaneNotFound) {
		t.Fatalf("GetPlane after delete error = %v, want ErrPlaneNotFound", err)
	}
}

func TestIntegrationRegisterPlaneRefreshDoesNotResetCurrentStatus(t *testing.T) {
	db := testutil.OpenControlPlaneTestDatabase(t)
	ctx := context.Background()
	createdPlane, err := db.Store.RegisterPlane(ctx, controlplanestore.RegisterPlaneInput{
		Name:         "refresh-plane",
		DisplayName:  "Refresh Plane",
		Provider:     "tencent",
		Region:       "ap-guangzhou",
		GRPCEndpoint: "refresh-plane.example.com:18081",
	})
	if err != nil {
		t.Fatalf("RegisterPlane returned error: %v", err)
	}
	if err := db.Store.UpdatePlaneStatus(ctx, createdPlane.ID, controlplanestore.UpdatePlaneStatusInput{
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
	if refreshed.ID != createdPlane.ID {
		t.Fatalf("refreshed plane id = %q, want existing %q", refreshed.ID, createdPlane.ID)
	}
	if refreshed.Status.Status != model.StatusReady || refreshed.Status.Message != "last snapshot healthy" {
		t.Fatalf("refreshed status = %+v, want existing ready status", refreshed.Status)
	}
	if refreshed.Status.LastHeartbeatAt == nil {
		t.Fatalf("refreshed status did not update heartbeat")
	}
}

func TestIntegrationDeletePlaneWithServicesReturnsConflict(t *testing.T) {
	db := testutil.OpenControlPlaneTestDatabase(t)
	ctx := context.Background()
	planeItem, err := db.Store.RegisterPlane(ctx, controlplanestore.RegisterPlaneInput{
		Name:         "plane-with-service",
		DisplayName:  "Plane With Service",
		Provider:     "tencent",
		Region:       "ap-guangzhou",
		GRPCEndpoint: "plane-with-service.example.com:18081",
	})
	if err != nil {
		t.Fatalf("RegisterPlane returned error: %v", err)
	}
	if _, err := db.Store.CreateService(ctx, controlplanestore.CreateServiceInput{
		Name:        "bound-service",
		DisplayName: "Bound Service",
		Host:        "bound-service.apps.example.test",
		Spec: model.ServiceSpec{
			PlaneID:       planeItem.ID,
			InstanceClass: model.InstanceClassSmall,
			Exposure:      model.ExposurePublic,
			Image:         "nginx:1.27-alpine",
			DefaultPort:   80,
			ReadinessPath: "/",
		},
	}); err != nil {
		t.Fatalf("CreateService returned error: %v", err)
	}

	if err := db.Store.DeletePlane(ctx, planeItem.ID); !errors.Is(err, controlplanestore.ErrPlaneHasServices) {
		t.Fatalf("DeletePlane error = %v, want ErrPlaneHasServices", err)
	}
}

func TestIntegrationCreateServicePersistsEnv(t *testing.T) {
	db := testutil.OpenControlPlaneTestDatabase(t)
	ctx := context.Background()
	planeItem, err := db.Store.RegisterPlane(ctx, controlplanestore.RegisterPlaneInput{
		Name:         "service-plane",
		DisplayName:  "Service Plane",
		Provider:     "aliyun",
		Region:       "cn-beijing",
		GRPCEndpoint: "service-model.example.com:443",
	})
	if err != nil {
		t.Fatalf("RegisterPlane returned error: %v", err)
	}

	serviceItem, err := db.Store.CreateService(ctx, controlplanestore.CreateServiceInput{
		Name:        "cliproxyapi",
		DisplayName: "CLI Proxy API",
		Host:        "cliproxyapi.apps.example.test",
		Spec: model.ServiceSpec{
			PlaneID:       planeItem.ID,
			InstanceClass: model.InstanceClassSmall,
			Exposure:      "public",
			Image:         "ghcr.io/example/cliproxyapi:v1",
			DefaultPort:   8317,
			ReadinessPath: "/healthz",
			Env: map[string]string{
				"CLIPROXY_TOKEN": "token-v1",
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateService returned error: %v", err)
	}
	if serviceItem.Spec.Env["CLIPROXY_TOKEN"] != "token-v1" {
		t.Fatalf("service env was not persisted")
	}

	reloaded, err := db.Store.GetService(ctx, serviceItem.Metadata.ID)
	if err != nil {
		t.Fatalf("GetService returned error: %v", err)
	}
	if reloaded.Spec.Env["CLIPROXY_TOKEN"] != "token-v1" {
		t.Fatalf("unexpected reloaded env: %+v", reloaded.Spec.Env)
	}
}

func TestIntegrationUpsertServiceSnapshotRefreshesActiveServiceCacheOnly(t *testing.T) {
	db := testutil.OpenControlPlaneTestDatabase(t)
	ctx := context.Background()
	planeItem, err := db.Store.RegisterPlane(ctx, controlplanestore.RegisterPlaneInput{
		Name:         "cache-plane",
		DisplayName:  "Cache Plane",
		Provider:     "tencent",
		Region:       "ap-guangzhou",
		GRPCEndpoint: "cache-plane.example.com:18081",
	})
	if err != nil {
		t.Fatalf("RegisterPlane returned error: %v", err)
	}
	serviceItem, err := db.Store.CreateService(ctx, controlplanestore.CreateServiceInput{
		Name:        "cache-api",
		DisplayName: "Cache API",
		Host:        "cache-api.apps.example.test",
		Spec: model.ServiceSpec{
			PlaneID:       planeItem.ID,
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

	if err := db.Store.UpsertServiceSnapshot(ctx, controlplanestore.UpsertServiceSnapshotInput{
		PlaneID:      planeItem.ID,
		ServiceID:    serviceItem.Metadata.ID,
		Name:         "cache-api",
		Host:         "cache-api.apps.example.test",
		Generation:   serviceItem.Metadata.Generation,
		DesiredState: model.DesiredStateActive,
	}); err != nil {
		t.Fatalf("UpsertServiceSnapshot returned error: %v", err)
	}
	reloaded, err := db.Store.GetService(ctx, serviceItem.Metadata.ID)
	if err != nil {
		t.Fatalf("GetService returned error: %v", err)
	}
	if reloaded.Metadata.DisplayName != "Cache API" {
		t.Fatalf("displayName = %q, want control-plane binding value", reloaded.Metadata.DisplayName)
	}
	if reloaded.Metadata.Generation != serviceItem.Metadata.Generation {
		t.Fatalf("generation = %d, want %d", reloaded.Metadata.Generation, serviceItem.Metadata.Generation)
	}
	if reloaded.Status.DesiredState != model.DesiredStateActive {
		t.Fatalf("desired state = %q, want active", reloaded.Status.DesiredState)
	}
	if reloaded.Spec.Image != "nginx:1.27-alpine" {
		t.Fatalf("image = %q, want control-plane cached spec", reloaded.Spec.Image)
	}
}

func TestIntegrationUpsertServiceSnapshotDoesNotChangeControlPlaneBinding(t *testing.T) {
	db := testutil.OpenControlPlaneTestDatabase(t)
	ctx := context.Background()
	planeItem, err := db.Store.RegisterPlane(ctx, controlplanestore.RegisterPlaneInput{
		Name:         "binding-owner-plane",
		DisplayName:  "Binding Owner Plane",
		Provider:     "tencent",
		Region:       "ap-guangzhou",
		GRPCEndpoint: "binding-owner-plane.example.com:18081",
	})
	if err != nil {
		t.Fatalf("RegisterPlane returned error: %v", err)
	}
	serviceItem, err := db.Store.CreateService(ctx, controlplanestore.CreateServiceInput{
		Name:        "binding-api",
		DisplayName: "Binding API",
		Host:        "binding-api.apps.example.test",
		Spec: model.ServiceSpec{
			PlaneID:       planeItem.ID,
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

	if err := db.Store.UpsertServiceSnapshot(ctx, controlplanestore.UpsertServiceSnapshotInput{
		PlaneID:      planeItem.ID,
		ServiceID:    serviceItem.Metadata.ID,
		Name:         "wrong-name",
		Host:         "wrong.apps.example.test",
		Generation:   serviceItem.Metadata.Generation,
		DesiredState: model.DesiredStateActive,
	}); err != nil {
		t.Fatalf("UpsertServiceSnapshot returned error: %v", err)
	}
	reloaded, err := db.Store.GetService(ctx, serviceItem.Metadata.ID)
	if err != nil {
		t.Fatalf("GetService returned error: %v", err)
	}
	if reloaded.Metadata.Name != serviceItem.Metadata.Name || reloaded.Metadata.DisplayName != serviceItem.Metadata.DisplayName || reloaded.Metadata.Host != serviceItem.Metadata.Host {
		t.Fatalf("binding changed after mismatched snapshot: %+v", reloaded.Metadata)
	}
	if reloaded.Spec.Image != "nginx:1.27-alpine" {
		t.Fatalf("cache image = %q, want mismatched snapshot ignored", reloaded.Spec.Image)
	}
}

func TestIntegrationUpsertServiceSnapshotDoesNotReviveDeletingService(t *testing.T) {
	db := testutil.OpenControlPlaneTestDatabase(t)
	ctx := context.Background()
	planeItem, err := db.Store.RegisterPlane(ctx, controlplanestore.RegisterPlaneInput{
		Name:         "delete-cache-plane",
		DisplayName:  "Delete Cache Plane",
		Provider:     "tencent",
		Region:       "ap-guangzhou",
		GRPCEndpoint: "delete-cache-plane.example.com:18081",
	})
	if err != nil {
		t.Fatalf("RegisterPlane returned error: %v", err)
	}
	serviceItem, err := db.Store.CreateService(ctx, controlplanestore.CreateServiceInput{
		Name:        "delete-cache-api",
		DisplayName: "Delete Cache API",
		Host:        "delete-cache-api.apps.example.test",
		Spec: model.ServiceSpec{
			PlaneID:       planeItem.ID,
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
	deleting, err := db.Store.MarkServiceDeletionRequested(ctx, serviceItem.Metadata.ID)
	if err != nil {
		t.Fatalf("MarkServiceDeletionRequested returned error: %v", err)
	}

	if err := db.Store.UpsertServiceSnapshot(ctx, controlplanestore.UpsertServiceSnapshotInput{
		PlaneID:      planeItem.ID,
		ServiceID:    serviceItem.Metadata.ID,
		Name:         serviceItem.Metadata.Name,
		Host:         serviceItem.Metadata.Host,
		Generation:   deleting.Metadata.Generation,
		DesiredState: model.DesiredStateActive,
	}); err != nil {
		t.Fatalf("UpsertServiceSnapshot returned error: %v", err)
	}
	reloaded, err := db.Store.GetService(ctx, serviceItem.Metadata.ID)
	if err != nil {
		t.Fatalf("GetService returned error: %v", err)
	}
	if reloaded.Status.DesiredState != model.DesiredStateDeleted {
		t.Fatalf("desired state = %q, want deleted", reloaded.Status.DesiredState)
	}
	if reloaded.Spec.Image != "nginx:1.27-alpine" {
		t.Fatalf("image = %q, want deleting service cache unchanged", reloaded.Spec.Image)
	}
}

func TestIntegrationUpsertServiceSnapshotIgnoresUnknownService(t *testing.T) {
	db := testutil.OpenControlPlaneTestDatabase(t)
	ctx := context.Background()
	planeItem, err := db.Store.RegisterPlane(ctx, controlplanestore.RegisterPlaneInput{
		Name:         "unknown-snapshot-plane",
		DisplayName:  "Unknown Snapshot Plane",
		Provider:     "tencent",
		Region:       "ap-guangzhou",
		GRPCEndpoint: "unknown-snapshot-plane.example.com:18081",
	})
	if err != nil {
		t.Fatalf("RegisterPlane returned error: %v", err)
	}

	if err := db.Store.UpsertServiceSnapshot(ctx, controlplanestore.UpsertServiceSnapshotInput{
		PlaneID:      planeItem.ID,
		ServiceID:    "svc-unknown",
		Name:         "unknown-api",
		Host:         "unknown-api.apps.example.test",
		Generation:   1,
		DesiredState: model.DesiredStateActive,
	}); err != nil {
		t.Fatalf("UpsertServiceSnapshot returned error: %v", err)
	}
	if _, err := db.Store.GetService(ctx, "svc-unknown"); !errors.Is(err, controlplanestore.ErrServiceNotFound) {
		t.Fatalf("GetService unknown snapshot error = %v, want service not found", err)
	}
}

func TestIntegrationGetDeletingServiceReturnsOnlyPendingDelete(t *testing.T) {
	db := testutil.OpenControlPlaneTestDatabase(t)
	ctx := context.Background()
	planeItem, err := db.Store.RegisterPlane(ctx, controlplanestore.RegisterPlaneInput{
		Name:         "delete-list-plane",
		DisplayName:  "Delete List Plane",
		Provider:     "tencent",
		Region:       "ap-guangzhou",
		GRPCEndpoint: "delete-list-plane.example.com:18081",
	})
	if err != nil {
		t.Fatalf("RegisterPlane returned error: %v", err)
	}
	active, err := db.Store.CreateService(ctx, controlplanestore.CreateServiceInput{
		Name:        "active-api",
		DisplayName: "Active API",
		Host:        "active-api.apps.example.test",
		Spec: model.ServiceSpec{
			PlaneID:       planeItem.ID,
			InstanceClass: model.InstanceClassSmall,
			Exposure:      model.ExposurePublic,
			Image:         "nginx:1.27-alpine",
			DefaultPort:   80,
			ReadinessPath: "/",
		},
	})
	if err != nil {
		t.Fatalf("CreateService(active) returned error: %v", err)
	}
	deleting, err := db.Store.CreateService(ctx, controlplanestore.CreateServiceInput{
		Name:        "deleting-api",
		DisplayName: "Deleting API",
		Host:        "deleting-api.apps.example.test",
		Spec: model.ServiceSpec{
			PlaneID:       planeItem.ID,
			InstanceClass: model.InstanceClassSmall,
			Exposure:      model.ExposurePublic,
			Image:         "nginx:1.27-alpine",
			DefaultPort:   80,
			ReadinessPath: "/",
		},
	})
	if err != nil {
		t.Fatalf("CreateService(deleting) returned error: %v", err)
	}
	if _, err := db.Store.MarkServiceDeletionRequested(ctx, deleting.Metadata.ID); err != nil {
		t.Fatalf("MarkServiceDeletionRequested returned error: %v", err)
	}

	item, ok, err := db.Store.GetDeletingService(ctx, deleting.Metadata.ID)
	if err != nil {
		t.Fatalf("GetDeletingService returned error: %v", err)
	}
	if !ok || item.Metadata.ID != deleting.Metadata.ID {
		t.Fatalf("GetDeletingService = %+v ok=%v, want deleting service", item, ok)
	}
	if _, ok, err := db.Store.GetDeletingService(ctx, active.Metadata.ID); err != nil || ok {
		t.Fatalf("GetDeletingService(active) ok=%v err=%v, want false nil", ok, err)
	}
}

func TestIntegrationGetPendingApplyServiceReturnsOnlyUnobservedActiveService(t *testing.T) {
	db := testutil.OpenControlPlaneTestDatabase(t)
	ctx := context.Background()
	planeItem, err := db.Store.RegisterPlane(ctx, controlplanestore.RegisterPlaneInput{
		Name:         "pending-apply-plane",
		DisplayName:  "Pending Apply Plane",
		Provider:     "tencent",
		Region:       "ap-guangzhou",
		GRPCEndpoint: "pending-apply-plane.example.com:18081",
	})
	if err != nil {
		t.Fatalf("RegisterPlane returned error: %v", err)
	}
	pending, err := db.Store.CreateService(ctx, controlplanestore.CreateServiceInput{
		Name:        "pending-apply",
		DisplayName: "Pending Apply",
		Host:        "pending-apply.apps.example.test",
		Spec: model.ServiceSpec{
			PlaneID:       planeItem.ID,
			InstanceClass: model.InstanceClassSmall,
			Exposure:      model.ExposurePublic,
			Image:         "nginx:1.27-alpine",
			DefaultPort:   80,
			ReadinessPath: "/",
		},
	})
	if err != nil {
		t.Fatalf("CreateService(pending) returned error: %v", err)
	}
	observed, err := db.Store.CreateService(ctx, controlplanestore.CreateServiceInput{
		Name:        "observed-apply",
		DisplayName: "Observed Apply",
		Host:        "observed-apply.apps.example.test",
		Spec: model.ServiceSpec{
			PlaneID:       planeItem.ID,
			InstanceClass: model.InstanceClassSmall,
			Exposure:      model.ExposurePublic,
			Image:         "nginx:1.27-alpine",
			DefaultPort:   80,
			ReadinessPath: "/",
		},
	})
	if err != nil {
		t.Fatalf("CreateService(observed) returned error: %v", err)
	}
	if err := db.Store.UpdateServiceStatusForGeneration(ctx, observed.Metadata.ID, observed.Metadata.Generation, controlplanestore.UpdateServiceStatusInput{
		ObservedGeneration: observed.Metadata.Generation,
		Phase:              model.PhaseReady,
		Message:            "observed",
	}); err != nil {
		t.Fatalf("UpdateServiceStatusForGeneration returned error: %v", err)
	}
	deleting, err := db.Store.CreateService(ctx, controlplanestore.CreateServiceInput{
		Name:        "delete-pending-apply",
		DisplayName: "Delete Pending Apply",
		Host:        "delete-pending-apply.apps.example.test",
		Spec: model.ServiceSpec{
			PlaneID:       planeItem.ID,
			InstanceClass: model.InstanceClassSmall,
			Exposure:      model.ExposurePublic,
			Image:         "nginx:1.27-alpine",
			DefaultPort:   80,
			ReadinessPath: "/",
		},
	})
	if err != nil {
		t.Fatalf("CreateService(deleting) returned error: %v", err)
	}
	if _, err := db.Store.MarkServiceDeletionRequested(ctx, deleting.Metadata.ID); err != nil {
		t.Fatalf("MarkServiceDeletionRequested returned error: %v", err)
	}

	item, ok, err := db.Store.GetPendingApplyService(ctx, pending.Metadata.ID)
	if err != nil {
		t.Fatalf("GetPendingApplyService returned error: %v", err)
	}
	if !ok || item.Metadata.ID != pending.Metadata.ID {
		t.Fatalf("GetPendingApplyService = %+v ok=%v, want pending service", item, ok)
	}
	if _, ok, err := db.Store.GetPendingApplyService(ctx, observed.Metadata.ID); err != nil || ok {
		t.Fatalf("GetPendingApplyService(observed) ok=%v err=%v, want false nil", ok, err)
	}
}

func TestIntegrationUpsertServiceSnapshotIgnoresStaleGeneration(t *testing.T) {
	db := testutil.OpenControlPlaneTestDatabase(t)
	ctx := context.Background()
	planeItem, err := db.Store.RegisterPlane(ctx, controlplanestore.RegisterPlaneInput{
		Name:         "stale-cache-plane",
		DisplayName:  "Stale Cache Plane",
		Provider:     "tencent",
		Region:       "ap-guangzhou",
		GRPCEndpoint: "stale-cache-plane.example.com:18081",
	})
	if err != nil {
		t.Fatalf("RegisterPlane returned error: %v", err)
	}
	serviceItem, err := db.Store.CreateService(ctx, controlplanestore.CreateServiceInput{
		Name:        "stale-cache-api",
		DisplayName: "Stale Cache API",
		Host:        "stale-cache-api.apps.example.test",
		Spec: model.ServiceSpec{
			PlaneID:       planeItem.ID,
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
	updated, err := db.Store.UpdateService(ctx, serviceItem.Metadata.ID, controlplanestore.UpdateServiceInput{
		DisplayName: "Stale Cache API",
		Spec: model.WorkloadSpec{
			InstanceClass: model.InstanceClassSmall,
			Exposure:      model.ExposurePublic,
			Image:         "nginx:1.28-alpine",
			DefaultPort:   80,
			ReadinessPath: "/",
		},
	})
	if err != nil {
		t.Fatalf("UpdateService returned error: %v", err)
	}

	if err := db.Store.UpsertServiceSnapshot(ctx, controlplanestore.UpsertServiceSnapshotInput{
		PlaneID:      planeItem.ID,
		ServiceID:    serviceItem.Metadata.ID,
		Name:         serviceItem.Metadata.Name,
		Host:         serviceItem.Metadata.Host,
		Generation:   serviceItem.Metadata.Generation,
		DesiredState: model.DesiredStateActive,
	}); err != nil {
		t.Fatalf("UpsertServiceSnapshot(stale) returned error: %v", err)
	}
	reloaded, err := db.Store.GetService(ctx, serviceItem.Metadata.ID)
	if err != nil {
		t.Fatalf("GetService returned error: %v", err)
	}
	if reloaded.Metadata.Generation != updated.Metadata.Generation {
		t.Fatalf("generation = %d, want %d", reloaded.Metadata.Generation, updated.Metadata.Generation)
	}
	if reloaded.Spec.Image != "nginx:1.28-alpine" {
		t.Fatalf("image = %q, want stale snapshot ignored", reloaded.Spec.Image)
	}
}

func TestIntegrationCreateServiceNormalizesStoredSpec(t *testing.T) {
	db := testutil.OpenControlPlaneTestDatabase(t)
	ctx := context.Background()
	planeItem, err := db.Store.RegisterPlane(ctx, controlplanestore.RegisterPlaneInput{
		Name:         "normalization-plane",
		DisplayName:  "Normalization Plane",
		Provider:     "aliyun",
		Region:       "cn-beijing",
		GRPCEndpoint: "normalization.example.com:443",
	})
	if err != nil {
		t.Fatalf("RegisterPlane returned error: %v", err)
	}

	serviceItem, err := db.Store.CreateService(ctx, controlplanestore.CreateServiceInput{
		Name:        " normalized-api ",
		DisplayName: " Normalized API ",
		Host:        " normalized-api.apps.example.test ",
		Spec: model.ServiceSpec{
			PlaneID:       " " + planeItem.ID + " ",
			InstanceClass: model.InstanceClassSmall,
			Image:         " ghcr.io/example/normalized-api:v1 ",
			DefaultPort:   8080,
			ReadinessPath: " /healthz ",
		},
	})
	if err != nil {
		t.Fatalf("CreateService returned error: %v", err)
	}

	if serviceItem.Metadata.Name != "normalized-api" || serviceItem.Metadata.DisplayName != "Normalized API" {
		t.Fatalf("service metadata was not normalized: %+v", serviceItem.Metadata)
	}
	if serviceItem.Spec.PlaneID != planeItem.ID {
		t.Fatalf("planeID = %q, want %q", serviceItem.Spec.PlaneID, planeItem.ID)
	}
	if serviceItem.Spec.Exposure != "public" {
		t.Fatalf("exposure = %q, want public", serviceItem.Spec.Exposure)
	}
	if serviceItem.Spec.Image != "ghcr.io/example/normalized-api:v1" || serviceItem.Spec.ReadinessPath != "/healthz" {
		t.Fatalf("service spec was not normalized: %+v", serviceItem.Spec)
	}
}

func TestIntegrationServiceGenerationChangeResetsRunStatus(t *testing.T) {
	db := testutil.OpenControlPlaneTestDatabase(t)
	ctx := context.Background()
	planeItem, err := db.Store.RegisterPlane(ctx, controlplanestore.RegisterPlaneInput{
		Name:         "run-reset-plane",
		DisplayName:  "Run Reset Plane",
		Provider:     "aliyun",
		Region:       "cn-beijing",
		GRPCEndpoint: "run-reset.example.com:443",
	})
	if err != nil {
		t.Fatalf("RegisterPlane returned error: %v", err)
	}

	serviceItem, err := db.Store.CreateService(ctx, controlplanestore.CreateServiceInput{
		Name:        "run-reset-api",
		DisplayName: "Run Reset API",
		Host:        "run-reset-api.apps.example.test",
		Spec: model.ServiceSpec{
			PlaneID:       planeItem.ID,
			InstanceClass: model.InstanceClassSmall,
			Exposure:      "public",
			Image:         "ghcr.io/example/run-reset-api:v1",
			DefaultPort:   8080,
			ReadinessPath: "/healthz",
		},
	})
	if err != nil {
		t.Fatalf("CreateService returned error: %v", err)
	}

	if err := db.Store.UpdateServiceStatusForGeneration(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, controlplanestore.UpdateServiceStatusInput{
		ObservedGeneration: serviceItem.Metadata.Generation,
		Phase:              model.PhaseReady,
		Message:            "running",
		Run: &model.RunStatus{
			Phase:   model.RunPhaseRunning,
			Message: "old run is running",
		},
	}); err != nil {
		t.Fatalf("UpdateServiceStatusForGeneration returned error: %v", err)
	}

	updated, err := db.Store.UpdateService(ctx, serviceItem.Metadata.ID, controlplanestore.UpdateServiceInput{
		DisplayName: "Run Reset API v2",
		Spec: model.WorkloadSpec{
			InstanceClass: model.InstanceClassSmall,
			Exposure:      "public",
			Image:         "ghcr.io/example/run-reset-api:v2",
			DefaultPort:   8080,
			ReadinessPath: "/healthz",
		},
	})
	if err != nil {
		t.Fatalf("UpdateService returned error: %v", err)
	}
	if updated.Status.Run.Phase != model.RunPhasePending ||
		updated.Status.Run.Message != "waiting for cloud-plane service apply" {
		t.Fatalf("run after update = %+v, want pending without old run message", updated.Status.Run)
	}

	if err := db.Store.UpdateServiceStatusForGeneration(ctx, updated.Metadata.ID, updated.Metadata.Generation, controlplanestore.UpdateServiceStatusInput{
		ObservedGeneration: updated.Metadata.Generation,
		Phase:              model.PhaseReady,
		Message:            "running",
		Run: &model.RunStatus{
			Phase:   model.RunPhaseRunning,
			Message: "new run is running",
		},
	}); err != nil {
		t.Fatalf("UpdateServiceStatusForGeneration for updated service returned error: %v", err)
	}
	var runJSON []byte
	if err := db.DB.QueryRowContext(ctx, `SELECT run_json FROM service_caches WHERE service_id = $1`, updated.Metadata.ID).Scan(&runJSON); err != nil {
		t.Fatalf("query service run json returned error: %v", err)
	}
	var runRecord map[string]any
	if err := json.Unmarshal(runJSON, &runRecord); err != nil {
		t.Fatalf("decode raw service run json returned error: %v", err)
	}
	if runRecord["phase"] != model.RunPhaseRunning || runRecord["message"] != "new run is running" {
		t.Fatalf("raw service run json = %s, want lower-case persistent keys", string(runJSON))
	}
	if _, ok := runRecord["executionID"]; ok {
		t.Fatalf("raw service run json = %s, must not store executionID", string(runJSON))
	}
	if _, ok := runRecord["Phase"]; ok {
		t.Fatalf("raw service run json = %s, must not use Go field names", string(runJSON))
	}

	if err := db.Store.UpdateServiceStatusForGeneration(ctx, updated.Metadata.ID, updated.Metadata.Generation, controlplanestore.UpdateServiceStatusInput{
		ObservedGeneration: updated.Metadata.Generation,
		Phase:              model.PhaseProgressing,
		Message:            "status refreshed without run change",
	}); err != nil {
		t.Fatalf("UpdateServiceStatusForGeneration without run returned error: %v", err)
	}
	refreshed, err := db.Store.GetService(ctx, updated.Metadata.ID)
	if err != nil {
		t.Fatalf("GetService after status refresh returned error: %v", err)
	}
	if refreshed.Status.Run.Phase != model.RunPhaseRunning || refreshed.Status.Run.Message != "new run is running" {
		t.Fatalf("run after status-only refresh = %+v, want previous running run", refreshed.Status.Run)
	}

	deleting, err := db.Store.MarkServiceDeletionRequested(ctx, serviceItem.Metadata.ID)
	if err != nil {
		t.Fatalf("MarkServiceDeletionRequested returned error: %v", err)
	}
	if deleting.Status.Run.Phase != model.RunPhasePending ||
		deleting.Status.Run.Message != "waiting for remote service teardown" {
		t.Fatalf("run after delete request = %+v, want pending delete without old run message", deleting.Status.Run)
	}
}
