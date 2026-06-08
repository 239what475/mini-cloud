package inventory

import (
	"testing"
	"time"

	domain "mini-cloud/internal/controlplane/domain"
)

func TestBuildAggregatesSummaryProvidersRegionsAndPlanes(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	view := Build([]domain.Detail{
		{
			Plane: domain.Plane{
				ID:           "pln_a",
				Name:         "aliyun-bj-primary",
				DisplayName:  "Aliyun Beijing Primary",
				Provider:     "aliyun",
				Region:       "cn-beijing",
				GRPCEndpoint: "plane-a.example.com:443",
			},
			Status: domain.PlaneStatus{
				Status:          domain.StatusReady,
				Message:         "healthy",
				LastHeartbeatAt: &now,
				LastSyncAt:      &now,
			},
			Registration: domain.Registration{
				Registered: true,
			},
			Operation: domain.Operation{
				State:     domain.OperationStateActive,
				UpdatedAt: now,
			},
			LatestRuntimeInventory: &domain.RuntimeInventorySnapshot{
				NodesTotal:        2,
				NodesReady:        2,
				CPUMilliCapacity:  4000,
				CPUMilliAllocated: 1500,
				MemoryMiCapacity:  8192,
				MemoryMiAllocated: 2048,
				ObservedAt:        now,
			},
		},
		{
			Plane: domain.Plane{
				ID:           "pln_b",
				Name:         "tencent-bj-primary",
				DisplayName:  "Tencent Beijing Primary",
				Provider:     "tencent",
				Region:       "ap-beijing",
				GRPCEndpoint: "plane-b.example.com:443",
			},
			Status: domain.PlaneStatus{
				Status:          domain.StatusDegraded,
				Message:         "one node offline",
				LastHeartbeatAt: &now,
				LastSyncAt:      &now,
			},
			Registration: domain.Registration{
				Registered: true,
			},
			Operation: domain.Operation{
				State:     domain.OperationStateDraining,
				Reason:    "planned evacuation",
				UpdatedAt: now,
			},
			LatestRuntimeInventory: &domain.RuntimeInventorySnapshot{
				NodesTotal:        1,
				NodesReady:        0,
				CPUMilliCapacity:  2000,
				CPUMilliAllocated: 500,
				MemoryMiCapacity:  4096,
				MemoryMiAllocated: 1024,
				ObservedAt:        now,
			},
		},
		{
			Plane: domain.Plane{
				ID:           "pln_c",
				Name:         "aliyun-hz-secondary",
				DisplayName:  "Aliyun Hangzhou Secondary",
				Provider:     "aliyun",
				Region:       "cn-hangzhou",
				GRPCEndpoint: "plane-c.example.com:443",
			},
			Status: domain.PlaneStatus{
				Status:  domain.StatusRegistering,
				Message: "awaiting registration handshake",
			},
			Registration: domain.Registration{
				Registered: false,
			},
			Operation: domain.Operation{
				State:     domain.OperationStateMaintenance,
				Reason:    "bootstrap fixes",
				UpdatedAt: now,
			},
		},
	})

	if view.Summary.PlanesTotal != 3 {
		t.Fatalf("planesTotal = %d, want 3", view.Summary.PlanesTotal)
	}
	if view.Summary.PlanesRegistered != 2 {
		t.Fatalf("planesRegistered = %d, want 2", view.Summary.PlanesRegistered)
	}
	if view.Summary.PlanesReady != 1 || view.Summary.PlanesDegraded != 1 || view.Summary.PlanesRegistering != 1 {
		t.Fatalf("unexpected plane status summary: %+v", view.Summary)
	}
	if view.Summary.PlanesActive != 1 || view.Summary.PlanesMaintenance != 1 || view.Summary.PlanesDraining != 1 {
		t.Fatalf("unexpected plane operation summary: %+v", view.Summary)
	}
	if view.Summary.NodesTotal != 3 || view.Summary.NodesReady != 2 || view.Summary.NodesUnavailable != 1 {
		t.Fatalf("unexpected node summary: %+v", view.Summary)
	}
	if view.Summary.ServicesTotal != 0 || view.Summary.RunsTotal != 0 {
		t.Fatalf("unexpected service/run summary: %+v", view.Summary)
	}
	if view.Summary.CPUMilliCapacity != 6000 || view.Summary.CPUMilliAllocated != 2000 || view.Summary.CPUMilliFree != 4000 {
		t.Fatalf("unexpected cpu summary: %+v", view.Summary)
	}
	if view.Summary.MemoryMiCapacity != 12288 || view.Summary.MemoryMiAllocated != 3072 || view.Summary.MemoryMiFree != 9216 {
		t.Fatalf("unexpected memory summary: %+v", view.Summary)
	}
	if len(view.Providers) != 2 {
		t.Fatalf("providers length = %d, want 2", len(view.Providers))
	}
	if view.Providers[0].Name != "aliyun" || view.Providers[0].Summary.PlanesTotal != 2 {
		t.Fatalf("unexpected aliyun provider summary: %+v", view.Providers[0])
	}
	if view.Providers[1].Name != "tencent" || view.Providers[1].Summary.NodesUnavailable != 1 {
		t.Fatalf("unexpected tencent provider summary: %+v", view.Providers[1])
	}

	if len(view.Regions) != 3 {
		t.Fatalf("regions length = %d, want 3", len(view.Regions))
	}
	if view.Regions[0].Name != "ap-beijing" || view.Regions[1].Name != "cn-beijing" || view.Regions[2].Name != "cn-hangzhou" {
		t.Fatalf("unexpected region order: %+v", view.Regions)
	}

	if len(view.Planes) != 3 {
		t.Fatalf("planes length = %d, want 3", len(view.Planes))
	}
	if view.Planes[0].ID != "pln_a" || view.Planes[1].ID != "pln_c" || view.Planes[2].ID != "pln_b" {
		t.Fatalf("unexpected plane order: %+v", view.Planes)
	}
	if view.Planes[0].CPUMilliFree != 2500 || view.Planes[2].NodesUnavailable != 1 {
		t.Fatalf("unexpected plane rows: %+v", view.Planes)
	}
	if !view.Planes[0].AcceptingNewRuns || view.Planes[1].AcceptingNewRuns || view.Planes[2].AcceptingNewRuns {
		t.Fatalf("unexpected acceptingNewRuns flags: %+v", view.Planes)
	}
}
