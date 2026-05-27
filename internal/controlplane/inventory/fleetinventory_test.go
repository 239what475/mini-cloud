package inventory

import (
	"testing"
	"time"

	plane "mini-cloud/internal/controlplane/plane"
	"mini-cloud/internal/controlplane/runtimepool"
)

func TestBuildAggregatesSummaryProvidersRegionsAndPlanes(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	view := Build([]plane.Detail{
		{
			Plane: plane.Plane{
				ID:           "pln_a",
				Name:         "aliyun-bj-primary",
				DisplayName:  "Aliyun Beijing Primary",
				Provider:     "aliyun",
				Region:       "cn-beijing",
				GRPCEndpoint: "plane-a.example.com:443",
			},
			Status: plane.PlaneStatus{
				Status:          plane.StatusReady,
				Message:         "healthy",
				LastHeartbeatAt: &now,
				LastSyncAt:      &now,
			},
			Registration: plane.Registration{
				Registered: true,
			},
			Operation: plane.Operation{
				State:     plane.OperationStateActive,
				UpdatedAt: now,
			},
			LatestCapacityRecord: &plane.CapacitySnapshot{
				NodesTotal:        2,
				NodesReady:        2,
				ServicesTotal:     3,
				DeploymentsTotal:  4,
				CPUMilliCapacity:  4000,
				CPUMilliAllocated: 1500,
				MemoryMiCapacity:  8192,
				MemoryMiAllocated: 2048,
				CapturedAt:        now,
			},
		},
		{
			Plane: plane.Plane{
				ID:           "pln_b",
				Name:         "tencent-bj-primary",
				DisplayName:  "Tencent Beijing Primary",
				Provider:     "tencent",
				Region:       "ap-beijing",
				GRPCEndpoint: "plane-b.example.com:443",
			},
			Status: plane.PlaneStatus{
				Status:          plane.StatusDegraded,
				Message:         "one node offline",
				LastHeartbeatAt: &now,
				LastSyncAt:      &now,
			},
			Registration: plane.Registration{
				Registered: true,
			},
			Operation: plane.Operation{
				State:     plane.OperationStateDraining,
				Reason:    "planned evacuation",
				UpdatedAt: now,
			},
			LatestCapacityRecord: &plane.CapacitySnapshot{
				NodesTotal:        1,
				NodesReady:        0,
				ServicesTotal:     1,
				DeploymentsTotal:  2,
				CPUMilliCapacity:  2000,
				CPUMilliAllocated: 500,
				MemoryMiCapacity:  4096,
				MemoryMiAllocated: 1024,
				CapturedAt:        now,
			},
		},
		{
			Plane: plane.Plane{
				ID:           "pln_c",
				Name:         "aliyun-hz-secondary",
				DisplayName:  "Aliyun Hangzhou Secondary",
				Provider:     "aliyun",
				Region:       "cn-hangzhou",
				GRPCEndpoint: "plane-c.example.com:443",
			},
			Status: plane.PlaneStatus{
				Status:  plane.StatusRegistering,
				Message: "awaiting registration handshake",
			},
			Registration: plane.Registration{
				Registered: false,
			},
			Operation: plane.Operation{
				State:     plane.OperationStateMaintenance,
				Reason:    "bootstrap fixes",
				UpdatedAt: now,
			},
		},
	}, []runtimepool.Pool{
		{
			PlaneID:          "pln_a",
			MinReady:         2,
			MaxReady:         4,
			HeadroomCPUMilli: 3000,
			HeadroomMemoryMi: 1024,
			CreatedAt:        now,
			UpdatedAt:        now,
		},
		{
			PlaneID:          "pln_b",
			MinReady:         1,
			MaxReady:         2,
			HeadroomCPUMilli: 2000,
			HeadroomMemoryMi: 1024,
			CreatedAt:        now,
			UpdatedAt:        now,
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
	if view.Summary.ServicesTotal != 4 || view.Summary.DeploymentsTotal != 6 {
		t.Fatalf("unexpected service/deployment summary: %+v", view.Summary)
	}
	if view.Summary.CPUMilliCapacity != 6000 || view.Summary.CPUMilliAllocated != 2000 || view.Summary.CPUMilliFree != 4000 {
		t.Fatalf("unexpected cpu summary: %+v", view.Summary)
	}
	if view.Summary.MemoryMiCapacity != 12288 || view.Summary.MemoryMiAllocated != 3072 || view.Summary.MemoryMiFree != 9216 {
		t.Fatalf("unexpected memory summary: %+v", view.Summary)
	}
	if view.Summary.PlanesWithRuntimePool != 2 || view.Summary.PoolsBelowHeadroom != 1 || view.Summary.PoolsBelowMinReady != 1 {
		t.Fatalf("unexpected runtime pool summary: %+v", view.Summary)
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
	if !view.Planes[0].AcceptingNewDeployments || view.Planes[1].AcceptingNewDeployments || view.Planes[2].AcceptingNewDeployments {
		t.Fatalf("unexpected acceptingNewDeployments flags: %+v", view.Planes)
	}
	if !view.Planes[0].RuntimePoolConfigured || view.Planes[0].RuntimePoolPhase != "below_headroom" {
		t.Fatalf("unexpected plane runtime pool view: %+v", view.Planes[0])
	}
	if !view.Planes[2].RuntimePoolConfigured || view.Planes[2].RuntimePoolPhase != "below_min_ready" {
		t.Fatalf("unexpected plane runtime pool view: %+v", view.Planes[2])
	}
}
