package coordination

import (
	"strings"
	"testing"
	"time"

	"mini-cloud/internal/contract/cloudplaneapi"
	"mini-cloud/internal/controlplane/model"
)

func TestDerivePlaneStatusReadyAndDegraded(t *testing.T) {
	planeDetail := model.PlaneDetail{
		Plane: model.Plane{
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
	if readyStatus != model.StatusReady {
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

	status := serviceStatusFromExecutionSnapshot(serviceItem, cloudplaneapi.ExecutionSnapshot{
		PlanID:            "svc-api-g2",
		ServiceID:         "svc-api",
		ServiceGeneration: 2,
		Status:            "running",
		ObservedAt:        observedAt,
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

	status := serviceStatusFromExecutionSnapshot(serviceItem, cloudplaneapi.ExecutionSnapshot{
		PlanID:            "svc-api-g2",
		ServiceID:         "svc-api",
		ServiceGeneration: 2,
		Status:            "failed",
		ObservedAt:        observedAt,
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

	status := serviceStatusFromExecutionSnapshot(serviceItem, cloudplaneapi.ExecutionSnapshot{
		PlanID:            "svc-api-g2",
		ServiceID:         "svc-api",
		ServiceGeneration: 2,
		Status:            "deploying",
		ObservedAt:        observedAt,
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
