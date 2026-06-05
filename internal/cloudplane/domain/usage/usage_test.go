package usage

import (
	"testing"

	"mini-cloud/internal/cloudplane/domain/workload"
)

// TestPreviewServicePlanAllowsWithinGuardrail 验证 service plan 在 resource guardrail 内时允许创建。
func TestPreviewServicePlanAllowsWithinGuardrail(t *testing.T) {
	t.Parallel()

	// resource guardrail 足够容纳现有服务和新增 small 服务。
	preview, err := PreviewServicePlan(Guardrails{
		MaxServices: 3,
		CPUMilli:    2000,
		MemoryMi:    4096,
	}, []workload.Service{
		{
			Metadata: workload.Metadata{ID: "service_01", Name: "demo-one", DisplayName: "Demo One"},
			Status:   workload.ServiceStatus{Phase: workload.StatusRunning},
			Spec:     workload.Spec{InstanceClass: workload.InstanceClassSmall},
		},
	}, ServicePlanPreviewInput{
		InstanceClass: workload.InstanceClassSmall,
	})
	if err != nil {
		t.Fatalf("PreviewServicePlan returned error: %v", err)
	}
	// admission 应允许本次容量变化，且没有拒绝原因。
	if !preview.Allowed {
		t.Fatalf("expected preview to be allowed, got reject reasons: %+v", preview.RejectReasons)
	}
	if preview.RequestedDelta.Services != 1 {
		t.Fatalf("expected requested services delta to be 1, got %d", preview.RequestedDelta.Services)
	}
	// 断言 projected usage 包含现有服务和本次请求的增量。
	if preview.ProjectedUsage.Services != 2 {
		t.Fatalf("expected projected services to be 2, got %d", preview.ProjectedUsage.Services)
	}
	if preview.ProjectedUsage.CPUMilli != 1000 {
		t.Fatalf("expected projected cpu to be 1000, got %d", preview.ProjectedUsage.CPUMilli)
	}
}

// TestPreviewServicePlanRejectsServiceCountGuardrailOverflow 验证 service 数量超限会被 guardrail admission 拒绝。
func TestPreviewServicePlanRejectsServiceCountGuardrailOverflow(t *testing.T) {
	t.Parallel()

	// 当前服务数已达到 max services，新增服务应被 service count guardrail 拒绝。
	preview, err := PreviewServicePlan(Guardrails{
		MaxServices: 1,
		CPUMilli:    4000,
		MemoryMi:    8192,
	}, []workload.Service{
		{
			Metadata: workload.Metadata{ID: "service_01", Name: "demo-one", DisplayName: "Demo One"},
			Status:   workload.ServiceStatus{Phase: workload.StatusIdle},
			Spec:     workload.Spec{InstanceClass: workload.InstanceClassMedium},
		},
	}, ServicePlanPreviewInput{
		InstanceClass: workload.InstanceClassLarge,
	})
	if err != nil {
		t.Fatalf("PreviewServicePlan returned error: %v", err)
	}
	// 请求语义合法但resource guardrail不允许，因此 preview.Allowed=false。
	if preview.Allowed {
		t.Fatalf("expected preview to be rejected")
	}
	if len(preview.RejectReasons) == 0 {
		t.Fatalf("expected at least one rejection reason")
	}
	// 第一条拒绝原因应明确指向 service 数量resource guardrail。
	if preview.RejectReasons[0].Code != RejectReasonServicesQuotaExceeded {
		t.Fatalf("expected service count guardrail rejection, got %+v", preview.RejectReasons)
	}
}
