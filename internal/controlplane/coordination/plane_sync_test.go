package coordination

import (
	"strings"
	"testing"
	"time"

	"mini-cloud/internal/controlplane/model"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"

	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestDerivePlaneStatusReadyAndDegraded(t *testing.T) {
	planeDetail := model.PlaneDetail{
		Plane: model.Plane{
			Provider: "aliyun",
			Region:   "cn-beijing",
		},
	}

	readyStatus, readyMessage := derivePlaneStatus(planeDetail, &cloudplanev1.PlaneSnapshot{
		Plane: &cloudplanev1.PlaneSummary{
			Provider: "aliyun",
			Region:   "cn-beijing",
		},
		NodeInventory: &cloudplanev1.PlaneNodeInventory{
			Nodes: []*cloudplanev1.PlaneNode{
				{Status: "ready"},
			},
		},
		Executions: []*cloudplanev1.PlaneExecutionSnapshot{
			{PlanId: "plan-a"},
			{PlanId: "plan-b"},
			{PlanId: "plan-c"},
		},
	})
	if readyStatus != model.StatusReady {
		t.Fatalf("ready status = %v, want ready", readyStatus)
	}
	if !strings.Contains(readyMessage, "sync healthy") {
		t.Fatalf("ready message = %q", readyMessage)
	}

	degradedStatus, degradedMessage := derivePlaneStatus(planeDetail, &cloudplanev1.PlaneSnapshot{
		Plane: &cloudplanev1.PlaneSummary{
			Provider: "tencent",
			Region:   "ap-beijing",
		},
		NodeInventory: &cloudplanev1.PlaneNodeInventory{
			Nodes: []*cloudplanev1.PlaneNode{
				{Status: "ready"},
				{Status: "offline"},
			},
		},
		Executions: []*cloudplanev1.PlaneExecutionSnapshot{
			{PlanId: "plan-a"},
		},
		Reliability: &cloudplanev1.PlaneReliability{
			AlertsFiring: 1,
		},
	})
	if degradedStatus != model.StatusDegraded {
		t.Fatalf("degraded status = %v, want degraded", degradedStatus)
	}
	if !strings.Contains(degradedMessage, "provider mismatch") || !strings.Contains(degradedMessage, "reliability alert") {
		t.Fatalf("unexpected degraded message: %q", degradedMessage)
	}
}

func TestBuildNodeInventoryIncludesElasticNodeSource(t *testing.T) {
	observedAt := time.Now().UTC()
	input := buildNodeInventory(&cloudplanev1.PlaneSnapshot{
		NodeInventory: &cloudplanev1.PlaneNodeInventory{
			ObservedAt: timestamppb.New(observedAt),
			Nodes: []*cloudplanev1.PlaneNode{
				{
					NodeId:              "node-a",
					Name:                "node-a",
					Status:              "ready",
					Schedulable:         true,
					Elastic:             true,
					CpuMilliAllocatable: 1000,
					MemoryMiAllocatable: 1024,
				},
			},
		},
	})

	if !input.ObservedAt.Equal(observedAt) {
		t.Fatalf("observedAt = %v, want %v", input.ObservedAt, observedAt)
	}
	if len(input.Nodes) != 1 || !input.Nodes[0].Elastic {
		t.Fatalf("node inventory nodes = %+v, want elastic node", input.Nodes)
	}
}

func TestServiceStatusFromExecutionSnapshotRunningPromotesCurrentRun(t *testing.T) {
	observedAt := time.Now().UTC()
	serviceItem := model.Service{
		Status: model.ServiceStatus{
			Run: model.RunStatus{
				CurrentRunID: "svc-api-g1",
				LatestRunID:  "svc-api-g2",
				Phase:        model.RunPhaseDispatching,
			},
		},
	}

	status := serviceStatusFromExecutionSnapshot(serviceItem, &cloudplanev1.PlaneExecutionSnapshot{
		PlanId:            "svc-api-g2",
		ServiceId:         "svc-api",
		ServiceGeneration: 2,
		Status:            "running",
		ObservedAt:        timestamppb.New(observedAt),
	})

	if status.Observed.Phase != model.PhaseReady {
		t.Fatalf("status = %+v, want ready", status.Observed)
	}
	if status.Run.CurrentRunID != "svc-api-g2" || status.Run.LatestRunID != "svc-api-g2" {
		t.Fatalf("run ids = %+v, want current/latest g2", status.Run)
	}
	if status.Run.Phase != model.RunPhaseRunning {
		t.Fatalf("run = %+v, want running", status.Run)
	}
}

func TestServiceStatusFromExecutionSnapshotFailedDoesNotRollbackCurrentRun(t *testing.T) {
	observedAt := time.Now().UTC()
	serviceItem := model.Service{
		Status: model.ServiceStatus{
			Run: model.RunStatus{
				CurrentRunID: "svc-api-g1",
				LatestRunID:  "svc-api-g2",
				Phase:        model.RunPhaseDispatching,
			},
		},
	}

	status := serviceStatusFromExecutionSnapshot(serviceItem, &cloudplanev1.PlaneExecutionSnapshot{
		PlanId:            "svc-api-g2",
		ServiceId:         "svc-api",
		ServiceGeneration: 2,
		Status:            "failed",
		ObservedAt:        timestamppb.New(observedAt),
	})

	if status.Observed.Phase != model.PhaseDegraded {
		t.Fatalf("status = %+v, want degraded", status.Observed)
	}
	if status.Run.CurrentRunID != "svc-api-g1" {
		t.Fatalf("current run = %q, want previous successful run", status.Run.CurrentRunID)
	}
	if status.Run.LatestRunID != "svc-api-g2" || status.Run.Phase != model.RunPhaseFailed {
		t.Fatalf("run = %+v, want latest failed g2", status.Run)
	}
}

func TestServiceStatusFromExecutionSnapshotProgressingKeepsCurrentRun(t *testing.T) {
	observedAt := time.Now().UTC()
	serviceItem := model.Service{
		Status: model.ServiceStatus{
			Run: model.RunStatus{
				CurrentRunID: "svc-api-g1",
				LatestRunID:  "svc-api-g2",
				Phase:        model.RunPhaseDispatching,
			},
		},
	}

	status := serviceStatusFromExecutionSnapshot(serviceItem, &cloudplanev1.PlaneExecutionSnapshot{
		PlanId:            "svc-api-g2",
		ServiceId:         "svc-api",
		ServiceGeneration: 2,
		Status:            "deploying",
		ObservedAt:        timestamppb.New(observedAt),
	})

	if status.Observed.Phase != model.PhaseProgressing {
		t.Fatalf("status = %+v, want progressing", status.Observed)
	}
	if status.Run.CurrentRunID != "svc-api-g1" {
		t.Fatalf("current run = %q, want previous successful run", status.Run.CurrentRunID)
	}
	if status.Run.LatestRunID != "svc-api-g2" || status.Run.Phase != model.RunPhaseDispatching {
		t.Fatalf("run = %+v, want latest g2 dispatching", status.Run)
	}
}
