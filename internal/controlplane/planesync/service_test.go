package planesync

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"mini-cloud/internal/contract/cloudplaneapi"
	plane "mini-cloud/internal/controlplane/plane"
	controlservice "mini-cloud/internal/controlplane/service"
	"mini-cloud/internal/testutil"
)

func TestDerivePlaneStatusReadyAndDegraded(t *testing.T) {
	planeDetail := plane.Detail{
		Plane: plane.Plane{
			Provider: "aliyun",
			Region:   "cn-beijing",
		},
	}

	readyStatus, readyMessage, readyAlerts := derivePlaneStatus(planeDetail, planeSnapshot{
		Plane: cloudplaneapi.PlaneSummary{
			Configured: true,
			Provider:   "aliyun",
			Region:     "cn-beijing",
		},
		Overview: cloudplaneapi.OverviewSummary{
			NodesTotal:          1,
			ServicesTotal:       2,
			ExecutionPlansTotal: 3,
			NodesReady:          1,
			NodesNotReady:       0,
			NodesOffline:        0,
			NodesDraining:       0,
		},
	})
	if readyStatus != plane.StatusReady {
		t.Fatalf("ready status = %v, want ready", readyStatus)
	}
	if readyAlerts != 0 {
		t.Fatalf("ready alerts = %d, want 0", readyAlerts)
	}
	if !strings.Contains(readyMessage, "sync healthy") {
		t.Fatalf("ready message = %q", readyMessage)
	}

	degradedStatus, degradedMessage, degradedAlerts := derivePlaneStatus(planeDetail, planeSnapshot{
		Plane: cloudplaneapi.PlaneSummary{
			Configured: true,
			Provider:   "tencent",
			Region:     "ap-beijing",
		},
		Health: cloudplaneapi.HealthSummary{
			Service: "degraded",
		},
		Overview: cloudplaneapi.OverviewSummary{
			NodesTotal:          2,
			NodesReady:          1,
			NodesNotReady:       1,
			ExecutionPlansTotal: 1,
		},
		Reliability: cloudplaneapi.ReliabilitySummary{
			AlertsFiring: 1,
		},
	})
	if degradedStatus != plane.StatusDegraded {
		t.Fatalf("degraded status = %v, want degraded", degradedStatus)
	}
	if degradedAlerts != 1 {
		t.Fatalf("degraded alerts = %d, want 1", degradedAlerts)
	}
	if !strings.Contains(degradedMessage, "provider mismatch") || !strings.Contains(degradedMessage, "reliability alert") || !strings.Contains(degradedMessage, "remote plane service health is degraded") {
		t.Fatalf("unexpected degraded message: %q", degradedMessage)
	}
}

func TestBuildRuntimeConfigUsesObservedSnapshot(t *testing.T) {
	observedAt := time.Now().UTC()
	input := buildRuntimeConfig(planeSnapshot{
		RuntimeConfig: cloudplaneapi.RuntimeConfigSnapshot{
			ObservedAt:  observedAt,
			Fingerprint: "runtime-fingerprint",
			Summary: map[string]any{
				"provider": map[string]any{
					"name": "aliyun",
				},
			},
		},
	})

	if !input.ObservedAt.Equal(observedAt) {
		t.Fatalf("observedAt = %v, want %v", input.ObservedAt, observedAt)
	}
	if input.Fingerprint != "runtime-fingerprint" {
		t.Fatalf("fingerprint = %q, want runtime-fingerprint", input.Fingerprint)
	}
	provider, ok := input.Summary["provider"].(map[string]any)
	if !ok || provider["name"] != "aliyun" {
		t.Fatalf("unexpected runtime config summary: %+v", input.Summary)
	}
}

func TestSyncPlaneAppliesExecutionSnapshotToServiceStatus(t *testing.T) {
	db := testutil.OpenControlPlaneTestDatabase(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()
	checkedAt := time.Now().UTC()

	planeItem, err := db.Store.CreatePlane(ctx, plane.CreateInput{
		Name:         "aliyun-prod-a",
		DisplayName:  "Aliyun Prod A",
		Provider:     "aliyun",
		Region:       "cn-beijing",
		GRPCEndpoint: "plane-a.example.com:443",
	})
	if err != nil {
		t.Fatalf("CreatePlane returned error: %v", err)
	}
	if _, err := db.Store.SetPlaneSouthboundToken(ctx, planeItem.ID, "southbound-a"); err != nil {
		t.Fatalf("SetPlaneSouthboundToken returned error: %v", err)
	}

	serviceItem, err := db.Store.CreateService(ctx, controlservice.CreateInput{
		Name:        "api",
		DisplayName: "API",
		Spec: controlservice.Spec{
			Provider:      "aliyun",
			Region:        "cn-beijing",
			InstanceClass: controlservice.InstanceClassSmall,
			Exposure:      "public",
			Image:         "nginx:latest",
			DefaultPort:   8080,
			ReadinessPath: "/healthz",
		},
	})
	if err != nil {
		t.Fatalf("CreateService returned error: %v", err)
	}
	if _, err := db.Store.UpdateServiceStatusForGeneration(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.Generation, controlservice.UpdateStatusInput{
		ObservedGeneration: serviceItem.Status.Observed.ObservedGeneration,
		Phase:              serviceItem.Status.Observed.Phase,
		Healthy:            serviceItem.Status.Observed.Healthy,
		Message:            serviceItem.Status.Observed.Message,
		Conditions:         serviceItem.Status.Observed.Conditions,
		LastReconciledAt:   serviceItem.Status.Observed.LastReconciledAt,
		AssignedPlaneID:    &planeItem.ID,
	}); err != nil {
		t.Fatalf("UpdateServiceStatusForGeneration returned error: %v", err)
	}

	service := NewServiceWithFetcher(logger, db.Store, fakePlaneSnapshotFetcher{
		responses: map[string]fakePlaneSnapshotResponse{
			"plane-a.example.com:443": {
				snapshot: planeSnapshot{
					Plane: cloudplaneapi.PlaneSummary{
						Configured: true,
						Provider:   "aliyun",
						Region:     "cn-beijing",
					},
					Health: cloudplaneapi.HealthSummary{
						CheckedAt: checkedAt,
						Service:   "ok",
						Database:  "ok",
					},
					Capacity: cloudplaneapi.CapacitySummary{
						RuntimeNodesTotal:   1,
						RuntimeNodesReady:   1,
						CPUMilliAllocatable: 2000,
						MemoryMiAllocatable: 4096,
					},
					Runtime: cloudplaneapi.RuntimeInventory{
						SyncVersion: 1,
						ObservedAt:  checkedAt,
					},
					RuntimeConfig: cloudplaneapi.RuntimeConfigSnapshot{
						ObservedAt:  checkedAt,
						Fingerprint: "runtime-fp-a",
					},
					Executions: []cloudplaneapi.ExecutionSnapshot{
						{
							PlanID:            serviceItem.Metadata.ID + "-g1",
							ServiceID:         serviceItem.Metadata.ID,
							ServiceName:       serviceItem.Metadata.Name,
							ServiceGeneration: serviceItem.Metadata.Generation,
							Status:            "running",
							ObservedAt:        checkedAt,
						},
					},
				},
			},
		},
	})

	if _, err := service.SyncPlane(ctx, planeItem.ID); err != nil {
		t.Fatalf("SyncPlane returned error: %v", err)
	}

	reloaded, err := db.Store.GetService(ctx, serviceItem.Metadata.ID)
	if err != nil {
		t.Fatalf("GetService returned error: %v", err)
	}
	if reloaded.Status.Observed.Phase != controlservice.PhaseReady || !reloaded.Status.Observed.Healthy {
		t.Fatalf("service status = %+v, want ready healthy", reloaded.Status.Observed)
	}
	if reloaded.Status.Run.Phase != controlservice.RunPhaseRunning || reloaded.Status.Run.CurrentRunID != serviceItem.Metadata.ID+"-g1" {
		t.Fatalf("service run = %+v, want running current run", reloaded.Status.Run)
	}
}

func TestServiceStatusFromExecutionSnapshotRunningPromotesCurrentRun(t *testing.T) {
	observedAt := time.Now().UTC()
	serviceItem := controlservice.Service{
		Status: controlservice.ServiceStatus{
			Run: controlservice.RunStatus{
				CurrentRunID: "svc-api-g1",
				LatestRunID:  "svc-api-g2",
				Phase:        controlservice.RunPhaseDispatching,
			},
		},
	}

	status := serviceStatusFromExecutionSnapshot(serviceItem, cloudplaneapi.ExecutionSnapshot{
		PlanID:            "svc-api-g2",
		ServiceID:         "svc-api",
		ServiceGeneration: 2,
		Status:            "running",
		ObservedAt:        observedAt,
	})

	if status.Phase != controlservice.PhaseReady || !status.Healthy {
		t.Fatalf("status = %+v, want ready healthy", status.Status)
	}
	if status.Run.CurrentRunID != "svc-api-g2" || status.Run.LatestRunID != "svc-api-g2" {
		t.Fatalf("run ids = %+v, want current/latest g2", status.Run)
	}
	if status.Run.Phase != controlservice.RunPhaseRunning {
		t.Fatalf("run = %+v, want running", status.Run)
	}
}

func TestServiceStatusFromExecutionSnapshotFailedDoesNotRollbackCurrentRun(t *testing.T) {
	observedAt := time.Now().UTC()
	serviceItem := controlservice.Service{
		Status: controlservice.ServiceStatus{
			Run: controlservice.RunStatus{
				CurrentRunID: "svc-api-g1",
				LatestRunID:  "svc-api-g2",
				Phase:        controlservice.RunPhaseDispatching,
			},
		},
	}

	status := serviceStatusFromExecutionSnapshot(serviceItem, cloudplaneapi.ExecutionSnapshot{
		PlanID:            "svc-api-g2",
		ServiceID:         "svc-api",
		ServiceGeneration: 2,
		Status:            "failed",
		ObservedAt:        observedAt,
	})

	if status.Phase != controlservice.PhaseDegraded || status.Healthy {
		t.Fatalf("status = %+v, want degraded unhealthy", status.Status)
	}
	if status.Run.CurrentRunID != "svc-api-g1" {
		t.Fatalf("current run = %q, want previous successful run", status.Run.CurrentRunID)
	}
	if status.Run.LatestRunID != "svc-api-g2" || status.Run.Phase != controlservice.RunPhaseFailed {
		t.Fatalf("run = %+v, want latest failed g2", status.Run)
	}
}

func TestServiceStatusFromExecutionSnapshotProgressingKeepsCurrentRun(t *testing.T) {
	observedAt := time.Now().UTC()
	serviceItem := controlservice.Service{
		Status: controlservice.ServiceStatus{
			Run: controlservice.RunStatus{
				CurrentRunID: "svc-api-g1",
				LatestRunID:  "svc-api-g2",
				Phase:        controlservice.RunPhaseDispatching,
			},
		},
	}

	status := serviceStatusFromExecutionSnapshot(serviceItem, cloudplaneapi.ExecutionSnapshot{
		PlanID:            "svc-api-g2",
		ServiceID:         "svc-api",
		ServiceGeneration: 2,
		Status:            "deploying",
		ObservedAt:        observedAt,
	})

	if status.Phase != controlservice.PhaseProgressing || status.Healthy {
		t.Fatalf("status = %+v, want progressing unhealthy", status.Status)
	}
	if status.Run.CurrentRunID != "svc-api-g1" {
		t.Fatalf("current run = %q, want previous successful run", status.Run.CurrentRunID)
	}
	if status.Run.LatestRunID != "svc-api-g2" || status.Run.Phase != controlservice.RunPhaseDispatching {
		t.Fatalf("run = %+v, want latest g2 dispatching", status.Run)
	}
}

func TestApplyExecutionSnapshotsPromotesCurrentRunAfterNewRunRunning(t *testing.T) {
	observedAt := time.Now().UTC()
	store := &fakeExecutionSnapshotStore{
		planeID: "plane-a",
		service: controlservice.Service{
			Metadata: controlservice.Metadata{
				ID:         "svc-api",
				Generation: 2,
			},
			Status: controlservice.ServiceStatus{
				Observed: controlservice.Status{
					AssignedPlaneID: "plane-a",
				},
				Run: controlservice.RunStatus{
					CurrentRunID: "svc-api-g1",
					LatestRunID:  "svc-api-g2",
					Phase:        controlservice.RunPhaseDispatching,
				},
			},
		},
	}
	service := &Service{store: store}

	err := service.applyExecutionSnapshots(context.Background(), "plane-a", []cloudplaneapi.ExecutionSnapshot{
		{
			PlanID:            "svc-api-g2",
			ServiceID:         "svc-api",
			ServiceGeneration: 2,
			Status:            "running",
			ObservedAt:        observedAt,
		},
	})
	if err != nil {
		t.Fatalf("applyExecutionSnapshots returned error: %v", err)
	}

	if store.service.Status.Run.CurrentRunID != "svc-api-g2" {
		t.Fatalf("current run = %q, want new run", store.service.Status.Run.CurrentRunID)
	}
	if store.service.Status.Run.Phase != controlservice.RunPhaseRunning {
		t.Fatalf("run phase = %s, want running", store.service.Status.Run.Phase)
	}
}

func TestApplyExecutionSnapshotsMarksServiceDegradedAfterFailedExecution(t *testing.T) {
	observedAt := time.Now().UTC()
	store := &fakeExecutionSnapshotStore{
		planeID: "plane-a",
		service: controlservice.Service{
			Metadata: controlservice.Metadata{
				ID:         "svc-api",
				Generation: 1,
			},
			Status: controlservice.ServiceStatus{
				DesiredState: controlservice.DesiredStateActive,
				Observed: controlservice.Status{
					AssignedPlaneID: "plane-a",
				},
				Run: controlservice.RunStatus{
					LatestRunID: "svc-api-g1",
					Phase:       controlservice.RunPhaseDispatching,
				},
			},
		},
	}
	service := &Service{store: store}

	err := service.applyExecutionSnapshots(context.Background(), "plane-a", []cloudplaneapi.ExecutionSnapshot{
		{
			PlanID:            "svc-api-g1",
			ServiceID:         "svc-api",
			ServiceGeneration: 1,
			Status:            "failed",
			LastStatusReason:  "node runtime-node-a marked offline",
			ObservedAt:        observedAt,
		},
	})
	if err != nil {
		t.Fatalf("applyExecutionSnapshots returned error: %v", err)
	}
	if store.service.Status.Observed.Phase != controlservice.PhaseDegraded || store.service.Status.Observed.Healthy {
		t.Fatalf("service observed status = %+v, want degraded unhealthy", store.service.Status.Observed)
	}
	if store.service.Status.Run.Phase != controlservice.RunPhaseFailed {
		t.Fatalf("service run = %+v, want failed", store.service.Status.Run)
	}
}

func TestApplyExecutionSnapshotsDeletesServiceAfterDeletePlanComplete(t *testing.T) {
	observedAt := time.Now().UTC()
	store := &fakeExecutionSnapshotStore{
		planeID: "plane-a",
		service: controlservice.Service{
			Metadata: controlservice.Metadata{
				ID:         "svc-api",
				Generation: 2,
			},
			Status: controlservice.ServiceStatus{
				DesiredState: controlservice.DesiredStateDeleted,
				Observed: controlservice.Status{
					AssignedPlaneID: "plane-a",
				},
				Run: controlservice.RunStatus{
					LatestRunID: "svc-api-delete-g2",
					Phase:       controlservice.RunPhaseDispatching,
				},
			},
		},
	}
	service := &Service{store: store}

	err := service.applyExecutionSnapshots(context.Background(), "plane-a", []cloudplaneapi.ExecutionSnapshot{
		{
			PlanID:            "svc-api-delete-g2",
			ServiceID:         "svc-api",
			ServiceGeneration: 2,
			Status:            "superseded",
			ObservedAt:        observedAt,
		},
	})
	if err != nil {
		t.Fatalf("applyExecutionSnapshots returned error: %v", err)
	}
	if !store.deletedService {
		t.Fatalf("deletedService=%v, want true", store.deletedService)
	}
}

func TestSyncRegisteredPlanesKeepsPlaneOutcomesIndependent(t *testing.T) {
	db := testutil.OpenControlPlaneTestDatabase(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	checkedAt := time.Now().UTC()

	planeA, err := db.Store.CreatePlane(context.Background(), plane.CreateInput{
		Name:         "aliyun-prod-a",
		DisplayName:  "Aliyun Prod A",
		Provider:     "aliyun",
		Region:       "cn-beijing",
		GRPCEndpoint: "plane-a.example.com:443",
	})
	if err != nil {
		t.Fatalf("CreatePlane A returned error: %v", err)
	}
	planeB, err := db.Store.CreatePlane(context.Background(), plane.CreateInput{
		Name:         "aliyun-prod-b",
		DisplayName:  "Aliyun Prod B",
		Provider:     "aliyun",
		Region:       "cn-beijing",
		GRPCEndpoint: "plane-b.example.com:443",
	})
	if err != nil {
		t.Fatalf("CreatePlane B returned error: %v", err)
	}
	if _, err := db.Store.SetPlaneSouthboundToken(context.Background(), planeA.ID, "southbound-a"); err != nil {
		t.Fatalf("SetPlaneSouthboundToken A returned error: %v", err)
	}
	if _, err := db.Store.SetPlaneSouthboundToken(context.Background(), planeB.ID, "southbound-b"); err != nil {
		t.Fatalf("SetPlaneSouthboundToken B returned error: %v", err)
	}

	service := NewServiceWithFetcher(logger, db.Store, fakePlaneSnapshotFetcher{
		responses: map[string]fakePlaneSnapshotResponse{
			"plane-a.example.com:443": {
				snapshot: planeSnapshot{
					Plane: cloudplaneapi.PlaneSummary{
						Configured: true,
						Provider:   "aliyun",
						Region:     "cn-beijing",
					},
					Health: cloudplaneapi.HealthSummary{
						CheckedAt: checkedAt,
					},
					Overview: cloudplaneapi.OverviewSummary{
						NodesTotal:          1,
						NodesReady:          1,
						ServicesTotal:       2,
						ExecutionPlansTotal: 3,
					},
					Capacity: cloudplaneapi.CapacitySummary{
						RuntimeNodesTotal:   1,
						RuntimeNodesReady:   1,
						CPUMilliAllocatable: 2000,
						MemoryMiAllocatable: 4096,
					},
					Runtime: cloudplaneapi.RuntimeInventory{
						SyncVersion: 1,
						ObservedAt:  checkedAt,
						Nodes: []cloudplaneapi.RuntimeNodeView{
							{
								NodeID:              "node-a",
								NodeEpoch:           1,
								Name:                "node-a",
								Provider:            "aliyun",
								Region:              "cn-beijing",
								InstanceID:          "i-plane-a",
								InstanceType:        "ecs.u1-c1m1.large",
								Status:              "ready",
								Schedulable:         true,
								CPUMilliAllocatable: 2000,
								MemoryMiAllocatable: 4096,
								LastHeartbeatAt:     &checkedAt,
							},
						},
					},
					RuntimeConfig: cloudplaneapi.RuntimeConfigSnapshot{
						ObservedAt:  checkedAt,
						Fingerprint: "runtime-fp-a",
						Summary: map[string]any{
							"provider": map[string]any{
								"name": "aliyun",
							},
							"nodeAgent": map[string]any{
								"bootstrapTokenConfigured": true,
							},
						},
					},
				},
			},
			"plane-b.example.com:443": {
				err: &syncError{
					status:  plane.StatusOffline,
					message: "plane heartbeat timed out during sync",
				},
			},
		},
	})

	outcomes, err := service.SyncRegisteredPlanes(context.Background())
	if err != nil {
		t.Fatalf("SyncRegisteredPlanes returned error: %v", err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("outcomes length = %d, want 2", len(outcomes))
	}

	var outcomeA, outcomeB *PlaneSyncOutcome
	for index := range outcomes {
		switch outcomes[index].PlaneID {
		case planeA.ID:
			outcomeA = &outcomes[index]
		case planeB.ID:
			outcomeB = &outcomes[index]
		}
	}
	if outcomeA == nil || outcomeB == nil {
		t.Fatalf("expected outcomes for both planes, got %+v", outcomes)
	}
	if outcomeA.Error != "" || outcomeA.Result == nil {
		t.Fatalf("plane A outcome = %+v, want successful result", outcomeA)
	}
	if outcomeA.Result.Plane.Status.Status != plane.StatusReady {
		t.Fatalf("plane A result status = %s, want ready", outcomeA.Result.Plane.Status.Status)
	}
	if outcomeB.Error == "" || outcomeB.Result != nil {
		t.Fatalf("plane B outcome = %+v, want failure without result", outcomeB)
	}

	storedPlaneA, err := db.Store.GetPlane(context.Background(), planeA.ID)
	if err != nil {
		t.Fatalf("GetPlane A returned error: %v", err)
	}
	if storedPlaneA.Status.Status != plane.StatusReady {
		t.Fatalf("stored plane A status = %s, want ready", storedPlaneA.Status.Status)
	}
	if storedPlaneA.Status.LastSyncAt == nil {
		t.Fatalf("expected plane A lastSyncAt to be populated")
	}
	if storedPlaneA.LatestRuntimeInventory == nil || storedPlaneA.LatestRuntimeInventory.NodesTotal != 1 {
		t.Fatalf("expected plane A runtime inventory to be recorded, got %+v", storedPlaneA.LatestRuntimeInventory)
	}
	if storedPlaneA.LatestRuntimeConfig == nil || storedPlaneA.LatestRuntimeConfig.Fingerprint != "runtime-fp-a" {
		t.Fatalf("expected plane A runtime config to be recorded, got %+v", storedPlaneA.LatestRuntimeConfig)
	}

	storedPlaneB, err := db.Store.GetPlane(context.Background(), planeB.ID)
	if err != nil {
		t.Fatalf("GetPlane B returned error: %v", err)
	}
	if storedPlaneB.Status.Status != plane.StatusOffline {
		t.Fatalf("stored plane B status = %s, want offline", storedPlaneB.Status.Status)
	}
	if !strings.Contains(storedPlaneB.Status.Message, "heartbeat timed out") {
		t.Fatalf("stored plane B status message = %q, want heartbeat timeout", storedPlaneB.Status.Message)
	}
	if storedPlaneB.LatestRuntimeInventory != nil {
		t.Fatalf("expected plane B runtime inventory to stay empty on failed sync, got %+v", storedPlaneB.LatestRuntimeInventory)
	}
	if storedPlaneB.LatestRuntimeConfig != nil {
		t.Fatalf("expected plane B runtime config to stay empty on failed sync, got %+v", storedPlaneB.LatestRuntimeConfig)
	}
}

type fakePlaneSnapshotFetcher struct {
	responses map[string]fakePlaneSnapshotResponse
}

type fakePlaneSnapshotResponse struct {
	snapshot planeSnapshot
	err      error
}

func (f fakePlaneSnapshotFetcher) Fetch(_ context.Context, grpcEndpoint string, _ string) (planeSnapshot, error) {
	response, ok := f.responses[grpcEndpoint]
	if !ok {
		return planeSnapshot{}, &syncError{
			status:  plane.StatusOffline,
			message: "unexpected plane grpc endpoint in test fetcher",
		}
	}
	return response.snapshot, response.err
}

type fakeExecutionSnapshotStore struct {
	planeID        string
	service        controlservice.Service
	deletedService bool
}

func (f *fakeExecutionSnapshotStore) GetPlane(context.Context, string) (plane.Detail, error) {
	return plane.Detail{}, nil
}

func (f *fakeExecutionSnapshotStore) SetPlaneSouthboundToken(context.Context, string, string) (plane.Registration, error) {
	return plane.Registration{}, nil
}

func (f *fakeExecutionSnapshotStore) MarkPlaneSouthboundTokenVerified(context.Context, string, time.Time) (plane.Registration, error) {
	return plane.Registration{}, nil
}

func (f *fakeExecutionSnapshotStore) GetPlaneSouthboundToken(context.Context, string) (string, error) {
	return "", nil
}

func (f *fakeExecutionSnapshotStore) ListRegisteredPlaneIDs(context.Context) ([]string, error) {
	return nil, nil
}

func (f *fakeExecutionSnapshotStore) UpdatePlaneStatus(context.Context, string, plane.UpdateStatusInput) (plane.PlaneStatus, error) {
	return plane.PlaneStatus{}, nil
}

func (f *fakeExecutionSnapshotStore) ReplacePlaneRuntimeInventory(context.Context, string, plane.RecordRuntimeInventoryInput) (plane.RuntimeInventorySnapshot, []plane.RuntimeNode, error) {
	return plane.RuntimeInventorySnapshot{}, nil, nil
}

func (f *fakeExecutionSnapshotStore) RecordPlaneRuntimeConfig(context.Context, string, plane.RecordRuntimeConfigInput) (plane.RuntimeConfigSnapshot, error) {
	return plane.RuntimeConfigSnapshot{}, nil
}

func (f *fakeExecutionSnapshotStore) GetService(context.Context, string) (controlservice.Service, error) {
	return f.service, nil
}

func (f *fakeExecutionSnapshotStore) UpdateServiceStatusForGeneration(_ context.Context, _ string, expectedGeneration int64, input controlservice.UpdateStatusInput) (controlservice.Service, error) {
	if f.service.Metadata.Generation != expectedGeneration {
		return f.service, nil
	}
	f.service.Status.Observed = controlservice.Status{
		ObservedGeneration: input.ObservedGeneration,
		Phase:              input.Phase,
		Healthy:            input.Healthy,
		Message:            input.Message,
		Conditions:         controlservice.CloneConditions(input.Conditions),
		LastReconciledAt:   input.LastReconciledAt,
		AssignedPlaneID:    f.service.Status.Observed.AssignedPlaneID,
	}
	if input.RemoteStatus != nil {
		f.service.Status.Observed.RemoteStatus = *input.RemoteStatus
	}
	if input.RemoteMessage != nil {
		f.service.Status.Observed.RemoteMessage = *input.RemoteMessage
	}
	if input.Run != nil {
		f.service.Status.Run = controlservice.CloneRunStatus(*input.Run)
	}
	return f.service, nil
}

func (f *fakeExecutionSnapshotStore) DeleteServiceForGeneration(context.Context, string, int64) error {
	f.deletedService = true
	return nil
}
