package runtimepool_test

import (
	"testing"
	"time"

	plane "mini-cloud/internal/controlplane/plane"
	"mini-cloud/internal/controlplane/runtimepool"
)

func TestBuildViewReportsBelowHeadroom(t *testing.T) {
	now := time.Now().UTC()
	view := runtimepool.BuildView(runtimepool.Pool{
		PlaneID:          "pln_test",
		MinReady:         1,
		MaxReady:         3,
		HeadroomCPUMilli: 2000,
		HeadroomMemoryMi: 1024,
		CreatedAt:        now,
		UpdatedAt:        now,
	}, plane.Detail{
		Plane: plane.Plane{
			ID:          "pln_test",
			Name:        "aliyun-bj-a",
			DisplayName: "Aliyun Beijing A",
			Provider:    "aliyun",
			Region:      "cn-beijing",
		},
		LatestCapacityRecord: &plane.CapacitySnapshot{
			NodesTotal:        2,
			NodesReady:        2,
			CPUMilliCapacity:  3000,
			CPUMilliAllocated: 1500,
			MemoryMiCapacity:  4096,
			MemoryMiAllocated: 2048,
			CapturedAt:        now,
		},
	})

	if view.Status.Phase != runtimepool.StatusPhaseBelowHeadroom {
		t.Fatalf("view status phase = %v, want %v", view.Status.Phase, runtimepool.StatusPhaseBelowHeadroom)
	}
	if !view.Status.BelowHeadroom {
		t.Fatalf("expected belowHeadroom to be true: %+v", view.Status)
	}
	if view.Snapshot.CPUMilliFree != 1500 || view.Snapshot.MemoryMiFree != 2048 {
		t.Fatalf("unexpected snapshot free capacity: %+v", view.Snapshot)
	}
}

func TestEvaluatePlacementRejectsMinReadyAndHeadroom(t *testing.T) {
	pool := runtimepool.Pool{
		PlaneID:          "pln_test",
		MinReady:         2,
		MaxReady:         4,
		HeadroomCPUMilli: 1000,
		HeadroomMemoryMi: 512,
	}

	minReadyReject := runtimepool.EvaluatePlacement(pool, &plane.CapacitySnapshot{
		NodesTotal:        2,
		NodesReady:        1,
		CPUMilliCapacity:  3000,
		CPUMilliAllocated: 1000,
		MemoryMiCapacity:  4096,
		MemoryMiAllocated: 1024,
	}, 500, 512)
	if minReadyReject.Allowed {
		t.Fatalf("expected minReady rejection, got %+v", minReadyReject)
	}

	headroomReject := runtimepool.EvaluatePlacement(runtimepool.Pool{
		PlaneID:          "pln_test",
		MinReady:         1,
		MaxReady:         4,
		HeadroomCPUMilli: 1800,
		HeadroomMemoryMi: 512,
	}, &plane.CapacitySnapshot{
		NodesTotal:        2,
		NodesReady:        2,
		CPUMilliCapacity:  3000,
		CPUMilliAllocated: 1000,
		MemoryMiCapacity:  4096,
		MemoryMiAllocated: 1024,
	}, 500, 512)
	if headroomReject.Allowed {
		t.Fatalf("expected headroom rejection, got %+v", headroomReject)
	}
	if headroomReject.CPUMilliFreeAfter != 1500 {
		t.Fatalf("cpu free after = %d, want 1500", headroomReject.CPUMilliFreeAfter)
	}
}
