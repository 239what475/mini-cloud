package execution

import "testing"

// TestReportInputValidateAcceptsRunningWithHostPort 验证 running execution 上报接受有效 host port。
func TestReportInputValidateAcceptsRunningWithHostPort(t *testing.T) {
	t.Parallel()

	// running 上报必须携带容器名和 host port，表示容器已经可被访问。
	err := (ReportInput{
		Status:        StatusRunning,
		Reason:        "healthy",
		ContainerName: "mini-cloud-dep-01",
		HostPort:      32774,
	}).Validate()
	// 完整 running 输入应通过校验。
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

// TestReportInputValidateRejectsRunningWithoutHostPort 验证 running execution 上报必须包含 host port。
func TestReportInputValidateRejectsRunningWithoutHostPort(t *testing.T) {
	t.Parallel()

	// running 状态缺少 host port 时，cloud-plane 无法构造可访问 runtime 状态。
	err := (ReportInput{
		Status:        StatusRunning,
		Reason:        "healthy",
		ContainerName: "mini-cloud-dep-01",
	}).Validate()
	// 校验应返回 host port 专用错误。
	if err != ErrHostPortInvalid {
		t.Fatalf("expected ErrHostPortInvalid, got %v", err)
	}
}

// TestReportInputValidateRejectsMissingContainerName 验证 execution 上报必须包含容器名。
func TestReportInputValidateRejectsMissingContainerName(t *testing.T) {
	t.Parallel()

	// 即使失败状态也需要容器名，用于后续清理或诊断定位。
	err := (ReportInput{
		Status: StatusFailed,
		Reason: "readiness check never passed",
	}).Validate()
	// 缺少容器名应返回专用错误。
	if err != ErrContainerNameRequired {
		t.Fatalf("expected ErrContainerNameRequired, got %v", err)
	}
}
