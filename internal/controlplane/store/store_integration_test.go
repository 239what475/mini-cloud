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

func TestIntegrationCreateServicePersistsSecretEnvAndRegistryCredential(t *testing.T) {
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
		Spec: model.ServiceSpec{
			PlaneID:       planeItem.ID,
			InstanceClass: model.InstanceClassSmall,
			Exposure:      "public",
			Image:         "ghcr.io/example/cliproxyapi:v1",
			DefaultPort:   8317,
			ReadinessPath: "/healthz",
			SecretEnv: map[string]string{
				"CLIPROXY_TOKEN": "token-v1",
			},
			RegistryCredential: &model.ServiceRegistryCredential{
				Server:   "ghcr.io",
				Username: "cliproxy",
				Password: "registry-token",
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateService returned error: %v", err)
	}
	if serviceItem.Spec.SecretEnv["CLIPROXY_TOKEN"] != "token-v1" {
		t.Fatalf("service secret env was not persisted")
	}
	if serviceItem.Spec.RegistryCredential == nil || serviceItem.Spec.RegistryCredential.Password != "registry-token" {
		t.Fatalf("service registry credential was not persisted")
	}

	reloaded, err := db.Store.GetService(ctx, serviceItem.Metadata.ID)
	if err != nil {
		t.Fatalf("GetService returned error: %v", err)
	}
	if reloaded.Spec.RegistryCredential == nil ||
		reloaded.Spec.RegistryCredential.Server != "ghcr.io" ||
		reloaded.Spec.RegistryCredential.Username != "cliproxy" ||
		reloaded.Spec.RegistryCredential.Password != "registry-token" {
		t.Fatalf("unexpected reloaded registry credential: %+v", reloaded.Spec.RegistryCredential)
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
		Spec: model.ServiceSpec{
			PlaneID:       " " + planeItem.ID + " ",
			InstanceClass: model.InstanceClassSmall,
			Image:         " ghcr.io/example/normalized-api:v1 ",
			DefaultPort:   8080,
			ReadinessPath: " /healthz ",
			RegistryCredential: &model.ServiceRegistryCredential{
				Server:   " ghcr.io ",
				Username: " normalized-api ",
				Password: "registry-token",
			},
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
	if serviceItem.Spec.RegistryCredential == nil ||
		serviceItem.Spec.RegistryCredential.Server != "ghcr.io" ||
		serviceItem.Spec.RegistryCredential.Username != "normalized-api" {
		t.Fatalf("registry credential was not normalized: %+v", serviceItem.Spec.RegistryCredential)
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
			CurrentRunID: "old-run",
			LatestRunID:  "old-run",
			Phase:        model.RunPhaseRunning,
			Message:      "old run is running",
		},
	}); err != nil {
		t.Fatalf("UpdateServiceStatusForGeneration returned error: %v", err)
	}

	updated, err := db.Store.UpdateService(ctx, serviceItem.Metadata.ID, controlplanestore.UpdateServiceInput{
		DisplayName: "Run Reset API v2",
		Spec: model.ServiceSpec{
			PlaneID:       planeItem.ID,
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
		updated.Status.Run.CurrentRunID != "" ||
		updated.Status.Run.LatestRunID != "" ||
		updated.Status.Run.Message != "waiting for service reconcile" {
		t.Fatalf("run after update = %+v, want pending without old run IDs", updated.Status.Run)
	}

	if err := db.Store.UpdateServiceStatusForGeneration(ctx, updated.Metadata.ID, updated.Metadata.Generation, controlplanestore.UpdateServiceStatusInput{
		ObservedGeneration: updated.Metadata.Generation,
		Phase:              model.PhaseReady,
		Message:            "running",
		Run: &model.RunStatus{
			CurrentRunID: "new-run",
			LatestRunID:  "new-run",
			Phase:        model.RunPhaseRunning,
			Message:      "new run is running",
		},
	}); err != nil {
		t.Fatalf("UpdateServiceStatusForGeneration for updated service returned error: %v", err)
	}
	var runJSON []byte
	if err := db.DB.QueryRowContext(ctx, `SELECT status_run_json FROM services WHERE id = $1`, updated.Metadata.ID).Scan(&runJSON); err != nil {
		t.Fatalf("query service run json returned error: %v", err)
	}
	var runRecord map[string]any
	if err := json.Unmarshal(runJSON, &runRecord); err != nil {
		t.Fatalf("decode raw service run json returned error: %v", err)
	}
	if runRecord["phase"] != model.RunPhaseRunning || runRecord["latestRunID"] != "new-run" {
		t.Fatalf("raw service run json = %s, want lower-case persistent keys", string(runJSON))
	}
	if _, ok := runRecord["Phase"]; ok {
		t.Fatalf("raw service run json = %s, must not use Go field names", string(runJSON))
	}

	deleting, err := db.Store.MarkServiceDeletionRequested(ctx, serviceItem.Metadata.ID)
	if err != nil {
		t.Fatalf("MarkServiceDeletionRequested returned error: %v", err)
	}
	if deleting.Status.Run.Phase != model.RunPhasePending ||
		deleting.Status.Run.CurrentRunID != "" ||
		deleting.Status.Run.LatestRunID != "" ||
		deleting.Status.Run.Message != "waiting for remote service teardown" {
		t.Fatalf("run after delete request = %+v, want pending delete without old run IDs", deleting.Status.Run)
	}
}
