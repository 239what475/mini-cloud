package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/projectedfile"
	"mini-cloud/internal/controlplane/incident"
	plane "mini-cloud/internal/controlplane/plane"
	"mini-cloud/internal/controlplane/resource"
	"mini-cloud/internal/controlplane/runtimepool"
	controlservice "mini-cloud/internal/controlplane/service"
	controlplanestore "mini-cloud/internal/controlplane/store"
	"mini-cloud/internal/testutil"
)

func TestIntegrationPlaneStatusCapacityAndIncidentLifecycle(t *testing.T) {
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

	snapshot, err := db.Store.RecordPlaneCapacitySnapshot(context.Background(), createdPlane.ID, plane.RecordCapacitySnapshotInput{
		NodesTotal:        4,
		NodesReady:        3,
		ServicesTotal:     7,
		RunsTotal:         8,
		CPUMilliCapacity:  16000,
		CPUMilliAllocated: 7000,
		MemoryMiCapacity:  32768,
		MemoryMiAllocated: 12288,
	})
	if err != nil {
		t.Fatalf("RecordPlaneCapacitySnapshot returned error: %v", err)
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
	if gotPlane.LatestCapacityRecord == nil {
		t.Fatalf("expected latest capacity snapshot to be populated")
	}
	if gotPlane.LatestCapacityRecord.ID != snapshot.ID {
		t.Fatalf("latest capacity snapshot id = %s, want %s", gotPlane.LatestCapacityRecord.ID, snapshot.ID)
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

	snapshots, err := db.Store.ListPlaneCapacitySnapshots(context.Background(), createdPlane.ID, 10)
	if err != nil {
		t.Fatalf("ListPlaneCapacitySnapshots returned error: %v", err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 capacity snapshot, got %d", len(snapshots))
	}

	pool, err := db.Store.UpsertRuntimeNodePool(context.Background(), createdPlane.ID, runtimepool.UpsertInput{
		MinReady:         2,
		MaxReady:         4,
		HeadroomCPUMilli: 2000,
		HeadroomMemoryMi: 4096,
	})
	if err != nil {
		t.Fatalf("UpsertRuntimeNodePool returned error: %v", err)
	}
	if pool.PlaneID != createdPlane.ID || pool.MinReady != 2 || pool.MaxReady != 4 {
		t.Fatalf("unexpected runtime node pool after upsert: %+v", pool)
	}
	gotPool, err := db.Store.GetRuntimeNodePoolByPlane(context.Background(), createdPlane.ID)
	if err != nil {
		t.Fatalf("GetRuntimeNodePoolByPlane returned error: %v", err)
	}
	if gotPool.HeadroomCPUMilli != 2000 || gotPool.HeadroomMemoryMi != 4096 {
		t.Fatalf("unexpected runtime node pool values: %+v", gotPool)
	}
	listedPools, err := db.Store.ListRuntimeNodePools(context.Background())
	if err != nil {
		t.Fatalf("ListRuntimeNodePools returned error: %v", err)
	}
	if len(listedPools) != 1 {
		t.Fatalf("expected 1 runtime node pool, got %d", len(listedPools))
	}
	if err := db.Store.DeleteRuntimeNodePoolByPlane(context.Background(), createdPlane.ID); err != nil {
		t.Fatalf("DeleteRuntimeNodePoolByPlane returned error: %v", err)
	}
	if _, err := db.Store.GetRuntimeNodePoolByPlane(context.Background(), createdPlane.ID); !errors.Is(err, controlplanestore.ErrRuntimeNodePoolNotFound) {
		t.Fatalf("GetRuntimeNodePoolByPlane after delete error = %v, want ErrRuntimeNodePoolNotFound", err)
	}

	createdIncident, err := db.Store.CreateIncident(context.Background(), incident.CreateInput{
		PlaneID:     createdPlane.ID,
		Severity:    incident.SeverityCritical,
		Summary:     "plane lost heartbeat",
		Description: "no heartbeat received for five minutes",
		RunbookURL:  "https://runbooks.example.com/control/plane-offline",
	})
	if err != nil {
		t.Fatalf("CreateIncident returned error: %v", err)
	}
	if createdIncident.State != incident.StateOpen {
		t.Fatalf("incident state = %v, want open", createdIncident.State)
	}

	updatedIncident, err := db.Store.UpdateIncident(context.Background(), createdIncident.ID, incident.UpdateInput{
		Severity:    incident.SeverityWarning,
		Summary:     "plane heartbeat unstable",
		Description: "heartbeat recovered but jitter remains",
		RunbookURL:  "https://runbooks.example.com/control/plane-degraded",
	})
	if err != nil {
		t.Fatalf("UpdateIncident returned error: %v", err)
	}
	if updatedIncident.Severity != incident.SeverityWarning || updatedIncident.Summary != "plane heartbeat unstable" {
		t.Fatalf("updated incident = %+v", updatedIncident)
	}

	openIncidents, err := db.Store.ListIncidents(context.Background(), incident.ListFilter{
		PlaneID: createdPlane.ID,
		State:   incident.StateOpen,
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("ListIncidents(open) returned error: %v", err)
	}
	if len(openIncidents) != 1 {
		t.Fatalf("expected 1 open incident, got %d", len(openIncidents))
	}

	resolved, err := db.Store.ResolveIncident(context.Background(), createdIncident.ID, incident.ResolveInput{
		Resolution: "heartbeat stream recovered after control-plane restart",
	})
	if err != nil {
		t.Fatalf("ResolveIncident returned error: %v", err)
	}
	if resolved.State != incident.StateResolved {
		t.Fatalf("resolved incident state = %v, want resolved", resolved.State)
	}
	if resolved.ResolvedAt == nil {
		t.Fatalf("expected resolved incident to expose ResolvedAt")
	}
	if _, err := db.Store.ResolveIncident(context.Background(), createdIncident.ID, incident.ResolveInput{
		Resolution: "redundant second resolve should fail",
	}); !errors.Is(err, controlplanestore.ErrIncidentAlreadyResolved) {
		t.Fatalf("second ResolveIncident error = %v, want ErrIncidentAlreadyResolved", err)
	}
	if _, err := db.Store.UpdateIncident(context.Background(), createdIncident.ID, incident.UpdateInput{
		Severity:    incident.SeverityWarning,
		Summary:     "should not update after resolve",
		Description: "resolved incidents are terminal",
	}); !errors.Is(err, controlplanestore.ErrIncidentAlreadyResolved) {
		t.Fatalf("UpdateIncident after resolve error = %v, want ErrIncidentAlreadyResolved", err)
	}

	resolvedIncidents, err := db.Store.ListIncidents(context.Background(), incident.ListFilter{
		State: incident.StateResolved,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListIncidents(resolved) returned error: %v", err)
	}
	if len(resolvedIncidents) != 1 {
		t.Fatalf("expected 1 resolved incident, got %d", len(resolvedIncidents))
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

	configSet, err := db.Store.CreateConfigSet(ctx, resource.CreateConfigSetInput{
		Name: "cliproxy-config",
		Values: map[string]string{
			"config.yaml": "listen: :8317\n",
		},
	})
	if err != nil {
		t.Fatalf("CreateConfigSet returned error: %v", err)
	}
	secretSet, err := db.Store.CreateSecretSet(ctx, resource.CreateSecretSetInput{
		Name: "cliproxy-secret",
		Values: map[string]string{
			"token": "token-v1",
		},
	})
	if err != nil {
		t.Fatalf("CreateSecretSet returned error: %v", err)
	}

	serviceItem, err := db.Store.CreateService(ctx, controlservice.CreateInput{
		Name:        "cliproxyapi",
		DisplayName: "CLI Proxy API",
		Spec: controlservice.Spec{
			Provider:      "aliyun",
			Region:        "cn-beijing",
			Replicas:      1,
			InstanceClass: controlservice.InstanceClassSmall,
			Exposure:      "public",
			Image:         "ghcr.io/example/cliproxyapi:v1",
			DefaultPort:   8317,
			ReadinessPath: "/healthz",
			ProjectedFiles: []projectedfile.Spec{
				{
					MountPath:  "/etc/cliproxy/config.yaml",
					SourceKind: projectedfile.SourceKindConfigSet,
					SourceID:   configSet.ID,
					SourceKey:  "config.yaml",
				},
				{
					MountPath:  "/etc/cliproxy/auth/token",
					SourceKind: projectedfile.SourceKindSecretSet,
					SourceID:   secretSet.ID,
					SourceKey:  "token",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateService returned error: %v", err)
	}
	if len(serviceItem.Spec.ProjectedFiles) != 2 {
		t.Fatalf("service projected files len = %d, want 2", len(serviceItem.Spec.ProjectedFiles))
	}

	reloaded, err := db.Store.GetService(ctx, serviceItem.Metadata.ID)
	if err != nil {
		t.Fatalf("GetService returned error: %v", err)
	}
	if len(reloaded.Spec.ProjectedFiles) != 2 {
		t.Fatalf("reloaded projected files len = %d, want 2", len(reloaded.Spec.ProjectedFiles))
	}
	if reloaded.Spec.ProjectedFiles[0].MountPath != "/etc/cliproxy/auth/token" {
		t.Fatalf("first projected mountPath = %q, want /etc/cliproxy/auth/token", reloaded.Spec.ProjectedFiles[0].MountPath)
	}
}

func TestIntegrationUpdateServiceRejectsPersistentDirRunChangeAfterRun(t *testing.T) {
	db := testutil.OpenControlPlaneTestDatabase(t)
	ctx := context.Background()

	serviceItem, err := db.Store.CreateService(ctx, controlservice.CreateInput{
		Name:        "cliproxy",
		DisplayName: "CLIProxy",
		Spec: controlservice.Spec{
			Provider:      "aliyun",
			Region:        "cn-beijing",
			Replicas:      1,
			InstanceClass: controlservice.InstanceClassSmall,
			Exposure:      "public",
			Image:         "ghcr.io/example/cliproxy:v1",
			DefaultPort:   8317,
			ReadinessPath: "/healthz",
			PersistentDirs: []persistentdir.Spec{
				{Name: "auth", MountPath: "/var/lib/cliproxy/auth"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateService returned error: %v", err)
	}

	_, err = db.Store.UpdateServiceStatus(ctx, serviceItem.Metadata.ID, controlservice.UpdateStatusInput{
		ObservedGeneration: serviceItem.Metadata.Generation,
		Phase:              controlservice.PhaseReady,
		Healthy:            true,
		Message:            "remote service is ready",
		Run: &controlservice.RunStatus{
			CurrentRunID:    "run-1",
			LatestRunID:     "run-1",
			Phase:           controlservice.RunPhaseRunning,
			DesiredReplicas: 1,
			RunningReplicas: 1,
		},
	})
	if err != nil {
		t.Fatalf("UpdateServiceStatus returned error: %v", err)
	}

	_, err = db.Store.UpdateService(ctx, serviceItem.Metadata.ID, controlservice.UpdateInput{
		DisplayName: "CLIProxy v2",
		Spec: controlservice.Spec{
			Provider:      "aliyun",
			Region:        "cn-beijing",
			Replicas:      1,
			InstanceClass: controlservice.InstanceClassSmall,
			Exposure:      "public",
			Image:         "ghcr.io/example/cliproxy:v2",
			DefaultPort:   8317,
			ReadinessPath: "/healthz",
			PersistentDirs: []persistentdir.Spec{
				{Name: "auth", MountPath: "/var/lib/cliproxy/auth"},
			},
		},
	})
	if !errors.Is(err, controlservice.ErrPersistentDirsRunUpdateUnsupported) {
		t.Fatalf("UpdateService error = %v, want %v", err, controlservice.ErrPersistentDirsRunUpdateUnsupported)
	}

	reloaded, err := db.Store.GetService(ctx, serviceItem.Metadata.ID)
	if err != nil {
		t.Fatalf("GetService returned error: %v", err)
	}
	if reloaded.Metadata.Generation != serviceItem.Metadata.Generation {
		t.Fatalf("generation = %d, want %d", reloaded.Metadata.Generation, serviceItem.Metadata.Generation)
	}
	if reloaded.Spec.Image != "ghcr.io/example/cliproxy:v1" {
		t.Fatalf("spec_image = %q, want ghcr.io/example/cliproxy:v1", reloaded.Spec.Image)
	}
}

func TestIntegrationUpdateServiceRejectsPersistentDirRunChangeWhenLockedWithoutRun(t *testing.T) {
	db := testutil.OpenControlPlaneTestDatabase(t)
	ctx := context.Background()

	serviceItem, err := db.Store.CreateService(ctx, controlservice.CreateInput{
		Name:        "cliproxy-locked",
		DisplayName: "CLIProxy Locked",
		Spec: controlservice.Spec{
			Provider:      "aliyun",
			Region:        "cn-beijing",
			Replicas:      1,
			InstanceClass: controlservice.InstanceClassSmall,
			Exposure:      "public",
			Image:         "ghcr.io/example/cliproxy:v1",
			DefaultPort:   8317,
			ReadinessPath: "/healthz",
			PersistentDirs: []persistentdir.Spec{
				{Name: "auth", MountPath: "/var/lib/cliproxy/auth"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateService returned error: %v", err)
	}

	if _, err := db.DB.ExecContext(ctx, `
		UPDATE fleet_services
		SET spec_persistent_dirs_locked = TRUE
		WHERE id = $1
	`, serviceItem.Metadata.ID); err != nil {
		t.Fatalf("force upgrade-like locked state returned error: %v", err)
	}

	_, err = db.Store.UpdateService(ctx, serviceItem.Metadata.ID, controlservice.UpdateInput{
		DisplayName: "CLIProxy Locked v2",
		Spec: controlservice.Spec{
			Provider:      "aliyun",
			Region:        "cn-beijing",
			Replicas:      1,
			InstanceClass: controlservice.InstanceClassSmall,
			Exposure:      "public",
			Image:         "ghcr.io/example/cliproxy:v2",
			DefaultPort:   8317,
			ReadinessPath: "/healthz",
			PersistentDirs: []persistentdir.Spec{
				{Name: "auth", MountPath: "/var/lib/cliproxy/auth"},
			},
		},
	})
	if !errors.Is(err, controlservice.ErrPersistentDirsRunUpdateUnsupported) {
		t.Fatalf("UpdateService error = %v, want %v", err, controlservice.ErrPersistentDirsRunUpdateUnsupported)
	}

	reloaded, err := db.Store.GetService(ctx, serviceItem.Metadata.ID)
	if err != nil {
		t.Fatalf("GetService returned error: %v", err)
	}
	if reloaded.Spec.Image != "ghcr.io/example/cliproxy:v1" {
		t.Fatalf("spec_image = %q, want ghcr.io/example/cliproxy:v1", reloaded.Spec.Image)
	}
}

func TestIntegrationUpdateServiceRejectsPersistentDirPlacementChangeWhenLockedWithoutRun(t *testing.T) {
	db := testutil.OpenControlPlaneTestDatabase(t)
	ctx := context.Background()

	serviceItem, err := db.Store.CreateService(ctx, controlservice.CreateInput{
		Name:        "cliproxy-placement-locked",
		DisplayName: "CLIProxy Placement Locked",
		Spec: controlservice.Spec{
			Provider:      "aliyun",
			Region:        "cn-beijing",
			PinnedPlaneID: "pln-001",
			Replicas:      1,
			InstanceClass: controlservice.InstanceClassSmall,
			Exposure:      "public",
			Image:         "ghcr.io/example/cliproxy:v1",
			DefaultPort:   8317,
			ReadinessPath: "/healthz",
			PersistentDirs: []persistentdir.Spec{
				{Name: "auth", MountPath: "/var/lib/cliproxy/auth"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateService returned error: %v", err)
	}

	if _, err := db.DB.ExecContext(ctx, `
		UPDATE fleet_services
		SET spec_persistent_dirs_locked = TRUE
		WHERE id = $1
	`, serviceItem.Metadata.ID); err != nil {
		t.Fatalf("force upgrade-like locked state returned error: %v", err)
	}

	_, err = db.Store.UpdateService(ctx, serviceItem.Metadata.ID, controlservice.UpdateInput{
		DisplayName: "CLIProxy Placement Locked",
		Spec: controlservice.Spec{
			Provider:      "tencent",
			Region:        "ap-beijing",
			PinnedPlaneID: "pln-002",
			Replicas:      1,
			InstanceClass: controlservice.InstanceClassSmall,
			Exposure:      "public",
			Image:         "ghcr.io/example/cliproxy:v1",
			DefaultPort:   8317,
			ReadinessPath: "/healthz",
			PersistentDirs: []persistentdir.Spec{
				{Name: "auth", MountPath: "/var/lib/cliproxy/auth"},
			},
		},
	})
	if !errors.Is(err, controlservice.ErrPersistentDirsPlacementChangeUnsupported) {
		t.Fatalf("UpdateService error = %v, want %v", err, controlservice.ErrPersistentDirsPlacementChangeUnsupported)
	}

	reloaded, err := db.Store.GetService(ctx, serviceItem.Metadata.ID)
	if err != nil {
		t.Fatalf("GetService returned error: %v", err)
	}
	if reloaded.Spec.Provider != "aliyun" || reloaded.Spec.Region != "cn-beijing" || reloaded.Spec.PinnedPlaneID != "pln-001" {
		t.Fatalf("service selection fields changed unexpectedly: provider=%q region=%q pinnedPlaneID=%q", reloaded.Spec.Provider, reloaded.Spec.Region, reloaded.Spec.PinnedPlaneID)
	}
}

func TestIntegrationCreateServicePersistsPersistentDirs(t *testing.T) {
	db := testutil.OpenControlPlaneTestDatabase(t)
	ctx := context.Background()

	serviceItem, err := db.Store.CreateService(ctx, controlservice.CreateInput{
		Name:        "cliproxyapi",
		DisplayName: "CLI Proxy API",
		Spec: controlservice.Spec{
			Provider:      "aliyun",
			Region:        "cn-beijing",
			Replicas:      1,
			InstanceClass: controlservice.InstanceClassSmall,
			Exposure:      "public",
			Image:         "ghcr.io/example/cliproxyapi:v1",
			DefaultPort:   8317,
			ReadinessPath: "/healthz",
			PersistentDirs: []persistentdir.Spec{
				{Name: "auth-dir", MountPath: "/var/lib/cliproxy/auth"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateService returned error: %v", err)
	}
	if len(serviceItem.Spec.PersistentDirs) != 1 {
		t.Fatalf("service persistent dirs len = %d, want 1", len(serviceItem.Spec.PersistentDirs))
	}

	reloaded, err := db.Store.GetService(ctx, serviceItem.Metadata.ID)
	if err != nil {
		t.Fatalf("GetService returned error: %v", err)
	}
	if len(reloaded.Spec.PersistentDirs) != 1 {
		t.Fatalf("reloaded persistent dirs len = %d, want 1", len(reloaded.Spec.PersistentDirs))
	}
	if reloaded.Spec.PersistentDirs[0].MountPath != "/var/lib/cliproxy/auth" {
		t.Fatalf("persistent dir mountPath = %q, want /var/lib/cliproxy/auth", reloaded.Spec.PersistentDirs[0].MountPath)
	}
}
