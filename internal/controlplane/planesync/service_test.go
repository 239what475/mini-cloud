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
			NodesTotal:       1,
			ServicesTotal:    2,
			DeploymentsTotal: 3,
			NodesReady:       1,
			NodesNotReady:    0,
			NodesOffline:     0,
			NodesDraining:    0,
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
			NodesTotal:       2,
			NodesReady:       1,
			NodesNotReady:    1,
			DeploymentsTotal: 1,
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

func TestBuildCapacitySnapshotUsesRuntimeNodeCapacityCounts(t *testing.T) {
	capturedAt := time.Now().UTC()
	snapshot := buildCapacitySnapshot(planeSnapshot{
		Health: cloudplaneapi.HealthSummary{
			CheckedAt: capturedAt,
		},
		Overview: cloudplaneapi.OverviewSummary{
			NodesTotal:       3,
			NodesReady:       2,
			ServicesTotal:    1,
			DeploymentsTotal: 2,
		},
		Capacity: cloudplaneapi.CapacitySummary{
			RuntimeNodesTotal:   2,
			RuntimeNodesReady:   2,
			CPUMilliAllocatable: 3000,
			CPUMilliAllocated:   750,
			MemoryMiAllocatable: 6144,
			MemoryMiAllocated:   1536,
		},
	})

	if snapshot.NodesTotal != 2 || snapshot.NodesReady != 2 {
		t.Fatalf("unexpected node totals: %+v", snapshot)
	}
	if snapshot.CPUMilliCapacity != 3000 || snapshot.CPUMilliAllocated != 750 {
		t.Fatalf("unexpected cpu totals: %+v", snapshot)
	}
	if snapshot.MemoryMiCapacity != 6144 || snapshot.MemoryMiAllocated != 1536 {
		t.Fatalf("unexpected memory totals: %+v", snapshot)
	}
	if !snapshot.CapturedAt.Equal(capturedAt) {
		t.Fatalf("capturedAt = %v, want %v", snapshot.CapturedAt, capturedAt)
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
						NodesTotal:       1,
						NodesReady:       1,
						ServicesTotal:    2,
						DeploymentsTotal: 3,
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
