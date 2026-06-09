package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"mini-cloud/internal/common/projectedfile"
	"mini-cloud/internal/controlplane/model"
	controlplanestore "mini-cloud/internal/controlplane/store"
	"mini-cloud/internal/testutil"
)

func TestIntegrationPlaneStatusCapacityAndRuntimeInventoryLifecycle(t *testing.T) {
	db := testutil.OpenControlPlaneTestDatabase(t)

	createdPlane, err := db.Store.CreatePlane(context.Background(), controlplanestore.CreatePlaneInput{
		Name:            "aliyun-bj-primary",
		DisplayName:     "Aliyun Beijing Primary",
		Provider:        "aliyun",
		Region:          "cn-beijing",
		GRPCEndpoint:    "plane-a.example.com:443",
		SouthboundToken: "plane-southbound-secret",
	})
	if err != nil {
		t.Fatalf("CreatePlane returned error: %v", err)
	}
	if createdPlane.Status.Status != model.StatusRegistering {
		t.Fatalf("initial plane status = %v, want %v", createdPlane.Status.Status, model.StatusRegistering)
	}
	if createdPlane.GRPCEndpoint != "plane-a.example.com:443" {
		t.Fatalf("plane grpcEndpoint = %q", createdPlane.GRPCEndpoint)
	}
	if !createdPlane.Registration.Registered {
		t.Fatalf("expected created plane to have southbound token registration")
	}
	verifiedAt := time.Now().UTC().Truncate(time.Second)
	if err := db.Store.MarkPlaneSouthboundTokenVerified(context.Background(), createdPlane.ID, verifiedAt); err != nil {
		t.Fatalf("MarkPlaneSouthboundTokenVerified returned error: %v", err)
	}
	token, err := db.Store.GetPlaneSouthboundToken(context.Background(), createdPlane.ID)
	if err != nil {
		t.Fatalf("GetPlaneSouthboundToken returned error: %v", err)
	}
	if token != "plane-southbound-secret" {
		t.Fatalf("southbound token = %q, want plane-southbound-secret", token)
	}
	registeredPlaneIDs, err := db.Store.ListRegisteredPlaneIDs(context.Background())
	if err != nil {
		t.Fatalf("ListRegisteredPlaneIDs returned error: %v", err)
	}
	if len(registeredPlaneIDs) != 1 || registeredPlaneIDs[0] != createdPlane.ID {
		t.Fatalf("unexpected registered plane ids: %+v", registeredPlaneIDs)
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
	if !gotPlane.Registration.Registered || gotPlane.Registration.LastVerifiedAt == nil {
		t.Fatalf("expected plane registration metadata to be populated, got %+v", gotPlane.Registration)
	}
	if err := db.Store.ReplacePlaneRuntimeInventory(context.Background(), createdPlane.ID, controlplanestore.RecordRuntimeInventoryInput{
		SyncVersion:       7,
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
		t.Fatalf("ReplacePlaneRuntimeInventory returned error: %v", err)
	}
	gotPlane, err = db.Store.GetPlane(context.Background(), createdPlane.ID)
	if err != nil {
		t.Fatalf("GetPlane(after runtime inventory) returned error: %v", err)
	}
	if gotPlane.LatestRuntimeInventory == nil {
		t.Fatalf("expected latest runtime inventory to be populated")
	}
	if gotPlane.LatestRuntimeInventory.SyncVersion != 7 {
		t.Fatalf("latest runtime inventory syncVersion = %d, want 7", gotPlane.LatestRuntimeInventory.SyncVersion)
	}

	listedPlanes, err := db.Store.ListPlanes(context.Background())
	if err != nil {
		t.Fatalf("ListPlanes returned error: %v", err)
	}
	if len(listedPlanes) != 1 {
		t.Fatalf("expected 1 plane, got %d", len(listedPlanes))
	}
	if listedPlanes[0].LatestRuntimeInventory == nil {
		t.Fatalf("expected listed plane runtime inventory to be populated")
	}

	if err := db.Store.DeletePlane(context.Background(), createdPlane.ID); err != nil {
		t.Fatalf("DeletePlane returned error: %v", err)
	}
	if _, err := db.Store.GetPlane(context.Background(), createdPlane.ID); !errors.Is(err, controlplanestore.ErrPlaneNotFound) {
		t.Fatalf("GetPlane after delete error = %v, want ErrPlaneNotFound", err)
	}
}

func TestIntegrationCreateServicePersistsProjectedFiles(t *testing.T) {
	db := testutil.OpenControlPlaneTestDatabase(t)
	ctx := context.Background()
	planeItem, err := db.Store.CreatePlane(ctx, controlplanestore.CreatePlaneInput{
		Name:            "service-plane",
		DisplayName:     "Service Plane",
		Provider:        "aliyun",
		Region:          "cn-beijing",
		GRPCEndpoint:    "service-model.example.com:443",
		SouthboundToken: "service-plane-token",
	})
	if err != nil {
		t.Fatalf("CreatePlane returned error: %v", err)
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
			Files: []projectedfile.File{
				{
					MountPath: "/etc/cliproxy/config.yaml",
					Content:   "listen: :8317\n",
				},
				{
					MountPath: "/etc/cliproxy/auth/token",
					Content:   "token-v1",
					Sensitive: true,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateService returned error: %v", err)
	}
	if len(serviceItem.Spec.Files) != 2 {
		t.Fatalf("service files len = %d, want 2", len(serviceItem.Spec.Files))
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
	if len(reloaded.Spec.Files) != 2 {
		t.Fatalf("reloaded files len = %d, want 2", len(reloaded.Spec.Files))
	}
	if reloaded.Spec.RegistryCredential == nil ||
		reloaded.Spec.RegistryCredential.Server != "ghcr.io" ||
		reloaded.Spec.RegistryCredential.Username != "cliproxy" ||
		reloaded.Spec.RegistryCredential.Password != "registry-token" {
		t.Fatalf("unexpected reloaded registry credential: %+v", reloaded.Spec.RegistryCredential)
	}
	if reloaded.Spec.Files[0].MountPath != "/etc/cliproxy/auth/token" {
		t.Fatalf("first file mountPath = %q, want /etc/cliproxy/auth/token", reloaded.Spec.Files[0].MountPath)
	}
	if !reloaded.Spec.Files[0].Sensitive {
		t.Fatalf("first file should be sensitive")
	}
}
