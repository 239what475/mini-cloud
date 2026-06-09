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

	readyStatus, readyMessage, readyAlerts := derivePlaneStatus(planeDetail, &cloudplanev1.PlaneSnapshot{
		Plane: &cloudplanev1.PlaneSummary{
			Configured: true,
			Provider:   "aliyun",
			Region:     "cn-beijing",
		},
		RuntimeInventory: &cloudplanev1.PlaneRuntimeInventory{
			Nodes: []*cloudplanev1.PlaneRuntimeNode{
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
	if readyAlerts != 0 {
		t.Fatalf("ready alerts = %d, want 0", readyAlerts)
	}
	if !strings.Contains(readyMessage, "sync healthy") {
		t.Fatalf("ready message = %q", readyMessage)
	}

	degradedStatus, degradedMessage, degradedAlerts := derivePlaneStatus(planeDetail, &cloudplanev1.PlaneSnapshot{
		Plane: &cloudplanev1.PlaneSummary{
			Configured: true,
			Provider:   "tencent",
			Region:     "ap-beijing",
		},
		Health: &cloudplanev1.PlaneHealth{
			Service: "degraded",
		},
		RuntimeInventory: &cloudplanev1.PlaneRuntimeInventory{
			Nodes: []*cloudplanev1.PlaneRuntimeNode{
				{Status: "ready"},
				{Status: "not_ready"},
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
	if degradedAlerts != 1 {
		t.Fatalf("degraded alerts = %d, want 1", degradedAlerts)
	}
	if !strings.Contains(degradedMessage, "provider mismatch") || !strings.Contains(degradedMessage, "reliability alert") || !strings.Contains(degradedMessage, "remote plane service health is degraded") {
		t.Fatalf("unexpected degraded message: %q", degradedMessage)
	}
}

func TestBuildRuntimeInventoryIncludesElasticNodeSource(t *testing.T) {
	observedAt := time.Now().UTC()
	input := buildRuntimeInventory(&cloudplanev1.PlaneSnapshot{
		RuntimeInventory: &cloudplanev1.PlaneRuntimeInventory{
			SyncVersion: 12,
			ObservedAt:  timestamppb.New(observedAt),
			Nodes: []*cloudplanev1.PlaneRuntimeNode{
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
		t.Fatalf("runtime inventory nodes = %+v, want elastic node", input.Nodes)
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

	if status.Observed.Phase != model.PhaseReady || !status.Observed.Healthy {
		t.Fatalf("status = %+v, want ready healthy", status.Observed)
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

	if status.Observed.Phase != model.PhaseDegraded || status.Observed.Healthy {
		t.Fatalf("status = %+v, want degraded unhealthy", status.Observed)
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

	if status.Observed.Phase != model.PhaseProgressing || status.Observed.Healthy {
		t.Fatalf("status = %+v, want progressing unhealthy", status.Observed)
	}
	if status.Run.CurrentRunID != "svc-api-g1" {
		t.Fatalf("current run = %q, want previous successful run", status.Run.CurrentRunID)
	}
	if status.Run.LatestRunID != "svc-api-g2" || status.Run.Phase != model.RunPhaseDispatching {
		t.Fatalf("run = %+v, want latest g2 dispatching", status.Run)
	}
}
