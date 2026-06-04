package planeselector_test

import (
	"context"
	"io"
	"log/slog"
	"mini-cloud/internal/controlplane/deploy"
	plane "mini-cloud/internal/controlplane/plane"
	"mini-cloud/internal/controlplane/planeselector"
	"mini-cloud/internal/controlplane/runtimepool"
	"mini-cloud/internal/testutil"
	"testing"
	"time"
)

func TestPreviewSelectionSelectsPlaneWithMoreRemainingCapacity(t *testing.T) {
	db := testutil.OpenControlPlaneTestDatabase(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := planeselector.NewService(logger, db.Store, nil)
	planeA := createReadyPlane(t, db.Store, "aliyun-bj-a", "Aliyun Beijing A", 2000, 1000, 4096, 1024)
	planeB := createReadyPlane(t, db.Store, "aliyun-bj-b", "Aliyun Beijing B", 3000, 1000, 4096, 1024)

	result, err := service.PreviewSelection(context.Background(), planeselector.SelectionInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		InstanceClass: deploy.InstanceClassSmall,
		Replicas:      1,
	})
	if err != nil {
		t.Fatalf("PreviewSelection returned error: %v", err)
	}
	if result.Decision == nil {
		t.Fatalf("expected selection decision, got failure=%q", result.FailureReason)
	}
	if result.Decision.PlaneID != planeB.ID {
		t.Fatalf("selected plane = %s, want %s", result.Decision.PlaneID, planeB.ID)
	}
	if len(result.Candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(result.Candidates))
	}
	if result.Candidates[1].PlaneID != planeB.ID || !result.Candidates[1].Selected {
		t.Fatalf("expected second candidate to be selected: %+v", result.Candidates)
	}
	if result.Candidates[0].PlaneID != planeA.ID || result.Candidates[0].Selected {
		t.Fatalf("expected first candidate to remain unselected: %+v", result.Candidates)
	}
}

func TestPreviewSelectionHonorsPinnedPlane(t *testing.T) {
	db := testutil.OpenControlPlaneTestDatabase(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := planeselector.NewService(logger, db.Store, nil)
	planeA := createReadyPlane(t, db.Store, "aliyun-bj-pinned-a", "Aliyun Beijing Pinned A", 2000, 1000, 4096, 1024)
	planeB := createReadyPlane(t, db.Store, "aliyun-bj-pinned-b", "Aliyun Beijing Pinned B", 3000, 1000, 4096, 1024)

	result, err := service.PreviewSelection(context.Background(), planeselector.SelectionInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		PinnedPlaneID: planeA.ID,
		InstanceClass: deploy.InstanceClassSmall,
		Replicas:      1,
	})
	if err != nil {
		t.Fatalf("PreviewSelection returned error: %v", err)
	}
	if result.Decision == nil {
		t.Fatalf("expected selection decision, got failure=%q", result.FailureReason)
	}
	if result.Decision.PlaneID != planeA.ID {
		t.Fatalf("selected plane = %s, want %s", result.Decision.PlaneID, planeA.ID)
	}
	if result.FilteredCounts.PinnedPlane != 1 {
		t.Fatalf("pinned plane filtered count = %d, want 1", result.FilteredCounts.PinnedPlane)
	}
	if len(result.Candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(result.Candidates))
	}
	if !result.Candidates[0].Selected || result.Candidates[0].PlaneID != planeA.ID {
		t.Fatalf("expected pinned plane candidate to be selected, got %+v", result.Candidates)
	}
	if result.Candidates[1].PlaneID != planeB.ID || result.Candidates[1].Reason == "" {
		t.Fatalf("expected non-pinned plane candidate to explain rejection, got %+v", result.Candidates)
	}
}

func TestPreviewSelectionDoesNotFallbackWhenPinnedPlaneIsUnavailable(t *testing.T) {
	db := testutil.OpenControlPlaneTestDatabase(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := planeselector.NewService(logger, db.Store, nil)
	createReadyPlane(t, db.Store, "aliyun-bj-pinned-healthy", "Aliyun Beijing Pinned Healthy", 2000, 1000, 4096, 1024)
	pinnedPlane := createReadyPlane(t, db.Store, "aliyun-bj-pinned-maint", "Aliyun Beijing Pinned Maintenance", 3000, 1000, 4096, 1024)
	if _, err := db.Store.UpdatePlaneOperation(context.Background(), pinnedPlane.ID, plane.UpdateOperationInput{
		State:  plane.OperationStateMaintenance,
		Reason: "operator placed plane into maintenance",
	}); err != nil {
		t.Fatalf("UpdatePlaneOperation returned error: %v", err)
	}

	result, err := service.PreviewSelection(context.Background(), planeselector.SelectionInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		PinnedPlaneID: pinnedPlane.ID,
		InstanceClass: deploy.InstanceClassSmall,
		Replicas:      1,
	})
	if err != nil {
		t.Fatalf("PreviewSelection returned error: %v", err)
	}
	if result.Decision != nil {
		t.Fatalf("expected no selection decision, got %+v", result.Decision)
	}
	if result.FailureReason != "the pinned plane is not ready" {
		t.Fatalf("failure reason = %q, want pinned-plane failure", result.FailureReason)
	}
	if result.FilteredCounts.PinnedPlane != 1 {
		t.Fatalf("pinned plane filtered count = %d, want 1", result.FilteredCounts.PinnedPlane)
	}
	if result.FilteredCounts.Operation != 1 {
		t.Fatalf("operation filtered count = %d, want 1", result.FilteredCounts.Operation)
	}
}

func TestPreviewSelectionExplainsWhyNoPlaneWasEligible(t *testing.T) {
	db := testutil.OpenControlPlaneTestDatabase(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := planeselector.NewService(logger, db.Store, nil)
	createReadyPlane(t, db.Store, "aliyun-bj-tight", "Aliyun Beijing Tight", 500, 250, 1024, 512)

	result, err := service.PreviewSelection(context.Background(), planeselector.SelectionInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		InstanceClass: deploy.InstanceClassLarge,
		Replicas:      1,
	})
	if err != nil {
		t.Fatalf("PreviewSelection returned error: %v", err)
	}
	if result.Decision != nil {
		t.Fatalf("expected no decision, got %+v", result.Decision)
	}
	if result.FailureReason == "" {
		t.Fatalf("expected failure reason, got empty")
	}
	if result.FilteredCounts.Capacity != 1 {
		t.Fatalf("capacity filtered count = %d, want 1", result.FilteredCounts.Capacity)
	}
	if len(result.Candidates) != 1 || result.Candidates[0].Reason == "" {
		t.Fatalf("expected candidate rejection reason, got %+v", result.Candidates)
	}
}

func TestPreviewSelectionFiltersPlanesThatAreNotAcceptingDeployments(t *testing.T) {
	db := testutil.OpenControlPlaneTestDatabase(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := planeselector.NewService(logger, db.Store, nil)
	planeItem := createReadyPlane(t, db.Store, "aliyun-bj-maint", "Aliyun Beijing Maintenance", 2000, 500, 4096, 1024)
	if _, err := db.Store.UpdatePlaneOperation(context.Background(), planeItem.ID, plane.UpdateOperationInput{
		State:  plane.OperationStateMaintenance,
		Reason: "kernel upgrade",
	}); err != nil {
		t.Fatalf("UpdatePlaneOperation returned error: %v", err)
	}

	result, err := service.PreviewSelection(context.Background(), planeselector.SelectionInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		InstanceClass: deploy.InstanceClassSmall,
		Replicas:      1,
	})
	if err != nil {
		t.Fatalf("PreviewSelection returned error: %v", err)
	}
	if result.Decision != nil {
		t.Fatalf("expected no decision, got %+v", result.Decision)
	}
	if result.FilteredCounts.Operation != 1 {
		t.Fatalf("operation filtered count = %d, want 1", result.FilteredCounts.Operation)
	}
	if result.FailureReason != "no ready planes are currently accepting new deployments" {
		t.Fatalf("failure reason = %q", result.FailureReason)
	}
	if len(result.Candidates) != 1 || result.Candidates[0].OperationState != "maintenance" {
		t.Fatalf("unexpected candidates: %+v", result.Candidates)
	}
}

func TestPreviewSelectionPrefersPlaneThatPreservesHeadroom(t *testing.T) {
	db := testutil.OpenControlPlaneTestDatabase(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := planeselector.NewService(logger, db.Store, nil)
	planeA := createReadyPlane(t, db.Store, "aliyun-bj-headroom-a", "Aliyun Beijing Headroom A", 2000, 1000, 4096, 1024)
	planeB := createReadyPlane(t, db.Store, "aliyun-bj-headroom-b", "Aliyun Beijing Headroom B", 3000, 1000, 4096, 1024)
	if _, err := db.Store.UpsertRuntimeNodePool(context.Background(), planeA.ID, runtimepool.UpsertInput{
		MinReady:         1,
		MaxReady:         2,
		HeadroomCPUMilli: 800,
		HeadroomMemoryMi: 512,
	}); err != nil {
		t.Fatalf("UpsertRuntimeNodePool returned error: %v", err)
	}

	result, err := service.PreviewSelection(context.Background(), planeselector.SelectionInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		InstanceClass: deploy.InstanceClassSmall,
		Replicas:      1,
	})
	if err != nil {
		t.Fatalf("PreviewSelection returned error: %v", err)
	}
	if result.Decision == nil {
		t.Fatalf("expected selection decision, got failure=%q", result.FailureReason)
	}
	if result.Decision.PlaneID != planeB.ID {
		t.Fatalf("selected plane = %s, want %s", result.Decision.PlaneID, planeB.ID)
	}
	if result.FilteredCounts.Headroom != 1 {
		t.Fatalf("headroom filtered count = %d, want 1", result.FilteredCounts.Headroom)
	}
	if len(result.Candidates) != 2 || result.Candidates[0].Reason == "" {
		t.Fatalf("unexpected candidates: %+v", result.Candidates)
	}
}

func TestPreviewSelectionExplainsPoolMinReadyBlock(t *testing.T) {
	db := testutil.OpenControlPlaneTestDatabase(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := planeselector.NewService(logger, db.Store, nil)
	planeItem := createReadyPlane(t, db.Store, "aliyun-bj-min-ready", "Aliyun Beijing Min Ready", 3000, 1000, 4096, 1024)
	if _, err := db.Store.UpsertRuntimeNodePool(context.Background(), planeItem.ID, runtimepool.UpsertInput{
		MinReady:         2,
		MaxReady:         4,
		HeadroomCPUMilli: 0,
		HeadroomMemoryMi: 0,
	}); err != nil {
		t.Fatalf("UpsertRuntimeNodePool returned error: %v", err)
	}

	result, err := service.PreviewSelection(context.Background(), planeselector.SelectionInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		InstanceClass: deploy.InstanceClassSmall,
		Replicas:      1,
	})
	if err != nil {
		t.Fatalf("PreviewSelection returned error: %v", err)
	}
	if result.Decision != nil {
		t.Fatalf("expected no decision, got %+v", result.Decision)
	}
	if result.FilteredCounts.PoolMinReady != 1 {
		t.Fatalf("pool minReady filtered count = %d, want 1", result.FilteredCounts.PoolMinReady)
	}
	if result.FailureReason != "ready planes had enough raw cpu/memory, but none currently satisfy runtime node pool minReady" {
		t.Fatalf("failure reason = %q", result.FailureReason)
	}
	if len(result.Candidates) != 1 || result.Candidates[0].Reason == "" {
		t.Fatalf("unexpected candidates: %+v", result.Candidates)
	}
}

func createReadyPlane(t *testing.T, stores interface {
	CreatePlane(context.Context, plane.CreateInput) (plane.Detail, error)
	SetPlaneSouthboundToken(context.Context, string, string) (plane.Registration, error)
	UpdatePlaneStatus(context.Context, string, plane.UpdateStatusInput) (plane.PlaneStatus, error)
	RecordPlaneCapacitySnapshot(context.Context, string, plane.RecordCapacitySnapshotInput) (plane.CapacitySnapshot, error)
	ReplacePlaneRuntimeInventory(context.Context, string, plane.RecordRuntimeInventoryInput) (plane.RuntimeInventorySnapshot, []plane.RuntimeNode, error)
}, name string, displayName string, cpuCapacity int, cpuAllocated int, memoryCapacity int, memoryAllocated int) plane.Detail {
	t.Helper()

	created, err := stores.CreatePlane(context.Background(), plane.CreateInput{
		Name:         name,
		DisplayName:  displayName,
		Provider:     "aliyun",
		Region:       "cn-beijing",
		GRPCEndpoint: name + ".example.com:443",
	})
	if err != nil {
		t.Fatalf("CreatePlane returned error: %v", err)
	}
	if _, err := stores.SetPlaneSouthboundToken(context.Background(), created.ID, "southbound-"+name); err != nil {
		t.Fatalf("SetPlaneSouthboundToken returned error: %v", err)
	}
	if _, err := stores.UpdatePlaneStatus(context.Background(), created.ID, plane.UpdateStatusInput{
		Status:  plane.StatusReady,
		Message: "healthy",
	}); err != nil {
		t.Fatalf("UpdatePlaneStatus returned error: %v", err)
	}
	if _, err := stores.RecordPlaneCapacitySnapshot(context.Background(), created.ID, plane.RecordCapacitySnapshotInput{
		NodesTotal:        1,
		NodesReady:        1,
		ServicesTotal:     0,
		DeploymentsTotal:  0,
		CPUMilliCapacity:  cpuCapacity,
		CPUMilliAllocated: cpuAllocated,
		MemoryMiCapacity:  memoryCapacity,
		MemoryMiAllocated: memoryAllocated,
	}); err != nil {
		t.Fatalf("RecordPlaneCapacitySnapshot returned error: %v", err)
	}
	if _, _, err := stores.ReplacePlaneRuntimeInventory(context.Background(), created.ID, plane.RecordRuntimeInventoryInput{
		SyncVersion:       1,
		ObservedAt:        time.Now().UTC(),
		NodesTotal:        1,
		NodesReady:        1,
		CPUMilliCapacity:  cpuCapacity,
		CPUMilliAllocated: cpuAllocated,
		MemoryMiCapacity:  memoryCapacity,
		MemoryMiAllocated: memoryAllocated,
		Nodes: []plane.RuntimeNode{
			{
				NodeID:            "node-" + name,
				NodeEpoch:         1,
				Name:              "node-" + name,
				Provider:          "aliyun",
				Region:            "cn-beijing",
				InstanceID:        "inst-" + name,
				InstanceType:      "ecs.u1-c1m1.large",
				Status:            "ready",
				Schedulable:       true,
				CPUMilliCapacity:  cpuCapacity,
				CPUMilliAllocated: cpuAllocated,
				MemoryMiCapacity:  memoryCapacity,
				MemoryMiAllocated: memoryAllocated,
			},
		},
	}); err != nil {
		t.Fatalf("ReplacePlaneRuntimeInventory returned error: %v", err)
	}
	return created
}
