package observability

import (
	"testing"
	"time"
)

// TestBuildReliabilitySnapshot 验证可靠性快照会根据异常信号生成 firing alert。
func TestBuildReliabilitySnapshot(t *testing.T) {
	now := time.Date(2026, 4, 11, 9, 0, 0, 0, time.UTC)
	snapshot := BuildReliabilitySnapshot(now, ReliabilityInputs{
		Overview: Overview{
			ExecutionPlansFailed: 1,
		},
		RuntimeNodesOffline: 1,
		ExecutionPlanStuck: ExecutionPlanStuckSignal{
			ThresholdSeconds: ExecutionPlanStuckThresholdSeconds,
			Total:            1,
			Deploying:        1,
			OldestAgeSeconds: 900,
		},
		RuntimeNodeRegistration: RuntimeNodeRegistrationSignal{
			ThresholdSeconds:             RuntimeNodeRegistrationTimeoutSeconds,
			Total:                        1,
			Provisioning:                 1,
			ProvisioningTimedOut:         1,
			OldestProvisioningAgeSeconds: 1200,
		},
	})

	if !snapshot.GeneratedAt.Equal(now) {
		t.Fatalf("generatedAt = %s, want %s", snapshot.GeneratedAt, now)
	}
	if len(snapshot.Alerts) != 4 {
		t.Fatalf("expected 4 alerts, got %d", len(snapshot.Alerts))
	}
	for _, alert := range snapshot.Alerts {
		if alert.State != "firing" {
			t.Fatalf("expected alert %s to be firing, got %s", alert.ID, alert.State)
		}
	}
}

// TestBuildReliabilitySnapshotWithNoSignals 验证无异常信号时 alert 保持 ok。
func TestBuildReliabilitySnapshotWithNoSignals(t *testing.T) {
	snapshot := BuildReliabilitySnapshot(time.Now().UTC(), ReliabilityInputs{})

	if len(snapshot.Alerts) != 4 {
		t.Fatalf("expected 4 alerts, got %d", len(snapshot.Alerts))
	}
	for _, alert := range snapshot.Alerts {
		if alert.State != "ok" {
			t.Fatalf("expected alert %s to be ok when no signals, got %s", alert.ID, alert.State)
		}
	}
}
