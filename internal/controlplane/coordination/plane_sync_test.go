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
			{ServiceId: "svc-a"},
			{ServiceId: "svc-b"},
			{ServiceId: "svc-c"},
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
			{ServiceId: "svc-a"},
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

func TestServiceStatusFromExecutionSnapshotRunning(t *testing.T) {
	observedAt := time.Now().UTC()

	status := serviceStatusFromExecutionSnapshot(&cloudplanev1.PlaneExecutionSnapshot{
		ServiceId:         "svc-api",
		ServiceGeneration: 2,
		Status:            "running",
		ObservedAt:        timestamppb.New(observedAt),
	})

	if status.Observed.Phase != model.PhaseReady {
		t.Fatalf("status = %+v, want ready", status.Observed)
	}
	if status.Run.Phase != model.RunPhaseRunning {
		t.Fatalf("run = %+v, want running", status.Run)
	}
}

func TestServiceStatusFromExecutionSnapshotFailed(t *testing.T) {
	observedAt := time.Now().UTC()

	status := serviceStatusFromExecutionSnapshot(&cloudplanev1.PlaneExecutionSnapshot{
		ServiceId:         "svc-api",
		ServiceGeneration: 2,
		Status:            "failed",
		ObservedAt:        timestamppb.New(observedAt),
	})

	if status.Observed.Phase != model.PhaseDegraded {
		t.Fatalf("status = %+v, want degraded", status.Observed)
	}
	if status.Run.Phase != model.RunPhaseFailed {
		t.Fatalf("run = %+v, want failed", status.Run)
	}
}

func TestServiceStatusFromExecutionSnapshotProgressing(t *testing.T) {
	observedAt := time.Now().UTC()

	status := serviceStatusFromExecutionSnapshot(&cloudplanev1.PlaneExecutionSnapshot{
		ServiceId:         "svc-api",
		ServiceGeneration: 2,
		Status:            "deploying",
		ObservedAt:        timestamppb.New(observedAt),
	})

	if status.Observed.Phase != model.PhaseProgressing {
		t.Fatalf("status = %+v, want progressing", status.Observed)
	}
	if status.Run.Phase != model.RunPhaseDispatching {
		t.Fatalf("run = %+v, want dispatching", status.Run)
	}
}
