package model

import "testing"

func TestReportInputValidateAcceptsRunningWithHostPort(t *testing.T) {
	t.Parallel()

	err := (ReportInput{
		Status:        StatusRunning,
		Reason:        "healthy",
		ContainerName: "mini-cloud-dep-01",
		HostPort:      32774,
	}).Validate()
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestReportInputValidateRejectsRunningWithoutHostPort(t *testing.T) {
	t.Parallel()

	err := (ReportInput{
		Status:        StatusRunning,
		Reason:        "healthy",
		ContainerName: "mini-cloud-dep-01",
	}).Validate()
	if err == nil || err.Error() != "hostPort must be greater than 0 when status is running" {
		t.Fatalf("Validate error = %v", err)
	}
}

func TestReportInputValidateRejectsMissingContainerName(t *testing.T) {
	t.Parallel()

	err := (ReportInput{
		Status: StatusFailed,
		Reason: "readiness check never passed",
	}).Validate()
	if err == nil || err.Error() != "containerName is required" {
		t.Fatalf("Validate error = %v", err)
	}
}
