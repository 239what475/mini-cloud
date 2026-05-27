package deployment

import "testing"

// TestValidateTransitionAllowsSchedulingChain 验证 deployment 正常调度链路状态迁移合法。
func TestValidateTransitionAllowsSchedulingChain(t *testing.T) {
	t.Parallel()

	// 列出 deployment 正常发布路径中允许的状态迁移。
	cases := []struct {
		from string
		to   string
	}{
		{from: StatusPending, to: StatusScheduling},
		{from: StatusScheduling, to: StatusAssigned},
		{from: StatusAssigned, to: StatusDeploying},
		{from: StatusDeploying, to: StatusRunning},
		{from: StatusRunning, to: StatusFailed},
	}

	// 每个迁移都带 reason，确保校验关注状态图而不是原因缺失。
	for _, tc := range cases {
		if err := ValidateTransition(tc.from, tc.to, "ok"); err != nil {
			t.Fatalf("expected transition %s -> %s to be valid: %v", tc.from, tc.to, err)
		}
	}
}

// TestValidateTransitionRejectsUnexpectedJump 验证 deployment 状态机拒绝跳跃迁移。
func TestValidateTransitionRejectsUnexpectedJump(t *testing.T) {
	t.Parallel()

	if err := ValidateTransition(StatusPending, StatusAssigned, "skip scheduling"); err == nil {
		t.Fatalf("expected pending -> assigned to fail")
	}
}
