package observability

import (
	"testing"
	"time"
)

// TestBuildReliabilitySnapshot 验证可靠性快照会根据异常信号生成 breached SLO 和 firing alert。
func TestBuildReliabilitySnapshot(t *testing.T) {
	// 构造固定时间，确保 snapshot 中所有派生时间可预测。
	now := time.Date(2026, 4, 11, 9, 0, 0, 0, time.UTC)
	// 输入同时触发 rollout SLO、runtime node SLO 和多个 alert。
	snapshot := BuildReliabilitySnapshot(now, ReliabilityInputs{
		Overview: Overview{
			DeploymentsFailed: 1,
		},
		TerminalDeploymentsLast24h:   10,
		SuccessfulDeploymentsLast24h: 8,
		RuntimeNodesTotal:            4,
		RuntimeNodesReady:            3,
		RuntimeNodesOffline:          1,
		DeploymentStuck: DeploymentStuckSignal{
			ThresholdSeconds: DeploymentStuckThresholdSeconds,
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

	// 当前可靠性视图应包含两个 SLO。
	if len(snapshot.SLOs) != 2 {
		t.Fatalf("expected 2 slos, got %d", len(snapshot.SLOs))
	}
	if snapshot.SLOs[0].Status != "breached" {
		t.Fatalf("revision rollout slo status = %s, want breached", snapshot.SLOs[0].Status)
	}
	if snapshot.SLOs[1].Status != "breached" {
		t.Fatalf("runtime node slo status = %s, want breached", snapshot.SLOs[1].Status)
	}
	// 所有输入信号都处于异常条件时，四类 alert 都应 firing。
	if len(snapshot.Alerts) != 4 {
		t.Fatalf("expected 4 alerts, got %d", len(snapshot.Alerts))
	}
	for _, alert := range snapshot.Alerts {
		if alert.State != "firing" {
			t.Fatalf("expected alert %s to be firing, got %s", alert.ID, alert.State)
		}
	}

	// runbook 和 daily check 数量是 UI/文档展示依赖的固定集合。
	if len(snapshot.Runbooks) != 4 {
		t.Fatalf("expected 4 runbooks, got %d", len(snapshot.Runbooks))
	}
	if len(snapshot.DailyChecks) != 3 {
		t.Fatalf("expected 3 daily checks, got %d", len(snapshot.DailyChecks))
	}
}

// TestBuildReliabilitySnapshotWithNoData 验证无样本时 SLO 为 no_data 且 alert 保持 ok。
func TestBuildReliabilitySnapshotWithNoData(t *testing.T) {
	// 空输入表示当前没有可用于计算比例 SLO 的样本。
	snapshot := BuildReliabilitySnapshot(time.Now().UTC(), ReliabilityInputs{})

	// 无样本时 SLO 状态应为 no_data，而不是 ok 或 breached。
	for _, slo := range snapshot.SLOs {
		if slo.Status != "no_data" {
			t.Fatalf("expected slo %s to be no_data, got %s", slo.ID, slo.Status)
		}
	}
	if len(snapshot.Alerts) != 4 {
		t.Fatalf("expected 4 alerts, got %d", len(snapshot.Alerts))
	}
	// 没有异常信号时 alert 集合仍存在，但状态应为 ok。
	for _, alert := range snapshot.Alerts {
		if alert.State != "ok" {
			t.Fatalf("expected alert %s to be ok when no signals, got %s", alert.ID, alert.State)
		}
	}
}
