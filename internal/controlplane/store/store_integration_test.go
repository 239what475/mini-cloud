package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"mini-cloud/internal/common/projectedfile"
	plane "mini-cloud/internal/controlplane/plane"
	controlservice "mini-cloud/internal/controlplane/service"
	controlplanestore "mini-cloud/internal/controlplane/store"
	"mini-cloud/internal/testutil"
)

func TestIntegrationPlaneStatusCapacityAndRuntimeInventoryLifecycle(t *testing.T) {
	db := testutil.OpenControlPlaneTestDatabase(t)

	createdPlane, err := db.Store.CreatePlane(context.Background(), plane.CreateInput{
		Name:         "aliyun-bj-primary",
		DisplayName:  "Aliyun Beijing Primary",
		Provider:     "aliyun",
		Region:       "cn-beijing",
		GRPCEndpoint: "plane-a.example.com:443",
	})
	if err != nil {
		t.Fatalf("CreatePlane returned error: %v", err)
	}
	if createdPlane.Status.Status != plane.StatusRegistering {
		t.Fatalf("initial plane status = %v, want %v", createdPlane.Status.Status, plane.StatusRegistering)
	}
	if createdPlane.Operation.State != plane.OperationStateActive {
		t.Fatalf("initial plane operation = %v, want %v", createdPlane.Operation.State, plane.OperationStateActive)
	}
	if createdPlane.GRPCEndpoint != "plane-a.example.com:443" {
		t.Fatalf("plane grpcEndpoint = %q", createdPlane.GRPCEndpoint)
	}
	if createdPlane.Registration.Registered {
		t.Fatalf("expected new plane to have no southbound token yet")
	}

	if _, err := db.Store.SetPlaneSouthboundToken(context.Background(), createdPlane.ID, "plane-southbound-secret"); err != nil {
		t.Fatalf("SetPlaneSouthboundToken returned error: %v", err)
	}
	verifiedAt := time.Now().UTC().Truncate(time.Second)
	if _, err := db.Store.MarkPlaneSouthboundTokenVerified(context.Background(), createdPlane.ID, verifiedAt); err != nil {
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

	updatedStatus, err := db.Store.UpdatePlaneStatus(context.Background(), createdPlane.ID, plane.UpdateStatusInput{
		Status:  plane.StatusReady,
		Message: "heartbeat and snapshot are healthy",
	})
	if err != nil {
		t.Fatalf("UpdatePlaneStatus returned error: %v", err)
	}
	if updatedStatus.Status != plane.StatusReady {
		t.Fatalf("updated status = %v, want ready", updatedStatus.Status)
	}
	if _, err := db.Store.UpdatePlaneOperation(context.Background(), createdPlane.ID, plane.UpdateOperationInput{
		State:  plane.OperationStateMaintenance,
		Reason: "kernel upgrade",
	}); err != nil {
		t.Fatalf("UpdatePlaneOperation(maintenance) returned error: %v", err)
	}
	if _, err := db.Store.UpdatePlaneOperation(context.Background(), createdPlane.ID, plane.UpdateOperationInput{
		State: plane.OperationStateActive,
	}); err != nil {
		t.Fatalf("UpdatePlaneOperation(active) returned error: %v", err)
	}

	gotPlane, err := db.Store.GetPlane(context.Background(), createdPlane.ID)
	if err != nil {
		t.Fatalf("GetPlane returned error: %v", err)
	}
	if gotPlane.Status.Status != plane.StatusReady {
		t.Fatalf("GetPlane status = %v, want ready", gotPlane.Status.Status)
	}
	if gotPlane.Operation.State != plane.OperationStateActive || !gotPlane.Operation.AcceptingNewRuns() {
		t.Fatalf("expected plane operation to be active, got %+v", gotPlane.Operation)
	}
	if !gotPlane.Registration.Registered || gotPlane.Registration.LastVerifiedAt == nil {
		t.Fatalf("expected plane registration metadata to be populated, got %+v", gotPlane.Registration)
	}
	if _, _, err := db.Store.ReplacePlaneRuntimeInventory(context.Background(), createdPlane.ID, plane.RecordRuntimeInventoryInput{
		SyncVersion:       7,
		ObservedAt:        time.Now().UTC(),
		NodesTotal:        2,
		NodesReady:        2,
		CPUMilliCapacity:  4000,
		CPUMilliAllocated: 1500,
		MemoryMiCapacity:  8192,
		MemoryMiAllocated: 2048,
		Nodes: []plane.RuntimeNode{
			{
				NodeID:            "node-a",
				NodeEpoch:         1,
				Name:              "node-a",
				Provider:          "aliyun",
				Region:            "cn-beijing",
				InstanceID:        "i-node-a",
				InstanceType:      "ecs.u1-c1m1.large",
				Status:            "ready",
				Schedulable:       true,
				CPUMilliCapacity:  2000,
				CPUMilliAllocated: 500,
				MemoryMiCapacity:  4096,
				MemoryMiAllocated: 1024,
			},
			{
				NodeID:            "node-b",
				NodeEpoch:         1,
				Name:              "node-b",
				Provider:          "aliyun",
				Region:            "cn-beijing",
				InstanceID:        "i-node-b",
				InstanceType:      "ecs.u1-c1m1.large",
				Status:            "ready",
				Schedulable:       true,
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
	runtimeConfig, err := db.Store.RecordPlaneRuntimeConfig(context.Background(), createdPlane.ID, plane.RecordRuntimeConfigInput{
		ObservedAt:  time.Now().UTC(),
		Fingerprint: "fp-123",
		Summary: map[string]any{
			"provider": map[string]any{
				"name":     "aliyun",
				"regionId": "cn-beijing",
			},
			"nodeAgent": map[string]any{
				"bootstrapTokenConfigured": true,
			},
		},
	})
	if err != nil {
		t.Fatalf("RecordPlaneRuntimeConfig returned error: %v", err)
	}
	gotPlane, err = db.Store.GetPlane(context.Background(), createdPlane.ID)
	if err != nil {
		t.Fatalf("GetPlane(after runtime config) returned error: %v", err)
	}
	if gotPlane.LatestRuntimeConfig == nil {
		t.Fatalf("expected latest runtime config to be populated")
	}
	if gotPlane.LatestRuntimeConfig.Fingerprint != runtimeConfig.Fingerprint {
		t.Fatalf("latest runtime config fingerprint = %q, want %q", gotPlane.LatestRuntimeConfig.Fingerprint, runtimeConfig.Fingerprint)
	}
	providerSummary, ok := gotPlane.LatestRuntimeConfig.Summary["provider"].(map[string]any)
	if !ok || providerSummary["name"] != "aliyun" {
		t.Fatalf("unexpected runtime config summary: %+v", gotPlane.LatestRuntimeConfig.Summary)
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
	if listedPlanes[0].LatestRuntimeConfig == nil {
		t.Fatalf("expected listed plane runtime config to be populated")
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

	serviceItem, err := db.Store.CreateService(ctx, controlservice.CreateInput{
		Name:        "cliproxyapi",
		DisplayName: "CLI Proxy API",
		Spec: controlservice.Spec{
			Provider:      "aliyun",
			Region:        "cn-beijing",
			InstanceClass: controlservice.InstanceClassSmall,
			Exposure:      "public",
			Image:         "ghcr.io/example/cliproxyapi:v1",
			DefaultPort:   8317,
			ReadinessPath: "/healthz",
			SecretEnv: map[string]string{
				"CLIPROXY_TOKEN": "token-v1",
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

	reloaded, err := db.Store.GetService(ctx, serviceItem.Metadata.ID)
	if err != nil {
		t.Fatalf("GetService returned error: %v", err)
	}
	if len(reloaded.Spec.Files) != 2 {
		t.Fatalf("reloaded files len = %d, want 2", len(reloaded.Spec.Files))
	}
	if reloaded.Spec.Files[0].MountPath != "/etc/cliproxy/auth/token" {
		t.Fatalf("first file mountPath = %q, want /etc/cliproxy/auth/token", reloaded.Spec.Files[0].MountPath)
	}
	if !reloaded.Spec.Files[0].Sensitive {
		t.Fatalf("first file should be sensitive")
	}
}
