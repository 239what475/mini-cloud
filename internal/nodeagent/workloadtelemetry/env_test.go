package workloadtelemetry

import (
	"strings"
	"testing"

	"mini-cloud/internal/contract/nodeagentapi"
)

// TestInjectWorkloadTelemetryEnvSetsDefaults 验证默认 OTEL exporter 和资源属性注入。
func TestInjectWorkloadTelemetryEnvSetsDefaults(t *testing.T) {
	t.Parallel()

	base := map[string]string{
		"APP_ENV": "test",
	}
	item := &nodeagentapi.WorkItem{
		ProjectID:    "prj_demo",
		ServiceID:    "svc_demo",
		ServiceName:  "hello",
		DeploymentID: "dep_demo",
		ExecutionID:  "exec_demo",
		ReplicaIndex: 2,
	}

	got := InjectWorkloadTelemetryEnv(base, item, WorkloadTelemetryEnvOptions{
		PlatformName:         "mini-cloud-lab",
		WorkloadOTLPEndpoint: "http://otel-collector:4318",
	})

	if got["APP_ENV"] != "test" {
		t.Fatalf("APP_ENV = %q, want test", got["APP_ENV"])
	}
	if got["OTEL_EXPORTER_OTLP_ENDPOINT"] != "http://otel-collector:4318" {
		t.Fatalf("OTEL_EXPORTER_OTLP_ENDPOINT = %q, want collector endpoint", got["OTEL_EXPORTER_OTLP_ENDPOINT"])
	}
	if got["OTEL_EXPORTER_OTLP_PROTOCOL"] != "http/protobuf" {
		t.Fatalf("OTEL_EXPORTER_OTLP_PROTOCOL = %q, want http/protobuf", got["OTEL_EXPORTER_OTLP_PROTOCOL"])
	}
	if got["OTEL_SERVICE_NAME"] != "hello" {
		t.Fatalf("OTEL_SERVICE_NAME = %q, want hello", got["OTEL_SERVICE_NAME"])
	}
	for _, want := range []string{
		"mini_cloud.project_id=prj_demo",
		"mini_cloud.service_id=svc_demo",
		"mini_cloud.deployment_id=dep_demo",
		"mini_cloud.execution_id=exec_demo",
		"mini_cloud.replica_index=2",
		"mini_cloud.platform_name=mini-cloud-lab",
	} {
		if !strings.Contains(got["OTEL_RESOURCE_ATTRIBUTES"], want) {
			t.Fatalf("OTEL_RESOURCE_ATTRIBUTES = %q, want substring %q", got["OTEL_RESOURCE_ATTRIBUTES"], want)
		}
	}
	if _, exists := base["OTEL_EXPORTER_OTLP_ENDPOINT"]; exists {
		t.Fatalf("InjectWorkloadTelemetryEnv should not mutate input map")
	}
}

// TestInjectWorkloadTelemetryEnvKeepsUserProvidedIdentityFields 验证用户服务名保留、保留资源属性覆盖同名已有值，并覆盖 OTLP endpoint。
func TestInjectWorkloadTelemetryEnvKeepsUserProvidedIdentityFields(t *testing.T) {
	t.Parallel()

	got := InjectWorkloadTelemetryEnv(map[string]string{
		"OTEL_SERVICE_NAME":           "custom-service-name",
		"OTEL_RESOURCE_ATTRIBUTES":    "service.version=1.2.3,mini_cloud.service_id=wrong",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "http://custom-collector:4318",
	}, &nodeagentapi.WorkItem{
		ProjectID:    "prj_demo",
		ServiceID:    "svc_demo",
		DeploymentID: "dep_demo",
		ExecutionID:  "exec_demo",
		ReplicaIndex: 1,
		ServiceName:  "ignored-by-test",
	}, WorkloadTelemetryEnvOptions{
		PlatformName:         "mini-cloud-lab",
		WorkloadOTLPEndpoint: "http://platform-collector:4318",
	})

	if got["OTEL_SERVICE_NAME"] != "custom-service-name" {
		t.Fatalf("OTEL_SERVICE_NAME = %q, want custom-service-name", got["OTEL_SERVICE_NAME"])
	}
	for _, want := range []string{
		"service.version=1.2.3",
		"mini_cloud.project_id=prj_demo",
		"mini_cloud.service_id=svc_demo",
		"mini_cloud.deployment_id=dep_demo",
		"mini_cloud.execution_id=exec_demo",
		"mini_cloud.replica_index=1",
		"mini_cloud.platform_name=mini-cloud-lab",
	} {
		if !strings.Contains(got["OTEL_RESOURCE_ATTRIBUTES"], want) {
			t.Fatalf("OTEL_RESOURCE_ATTRIBUTES = %q, want substring %q", got["OTEL_RESOURCE_ATTRIBUTES"], want)
		}
	}
	if got["OTEL_EXPORTER_OTLP_ENDPOINT"] != "http://platform-collector:4318" {
		t.Fatalf("OTEL_EXPORTER_OTLP_ENDPOINT = %q, want platform endpoint override", got["OTEL_EXPORTER_OTLP_ENDPOINT"])
	}
}

// TestMergeOTelResourceAttributesKeepsEscapedUserValues 验证合并资源属性时保留用户值中的转义逗号和等号。
func TestMergeOTelResourceAttributesKeepsEscapedUserValues(t *testing.T) {
	t.Parallel()

	got := mergeOTelResourceAttributes(`service.version=1.2.3,team.name=core\,platform,team.note=owns\=api`, map[string]string{
		"mini_cloud.service_id": "svc_demo",
	})

	for _, want := range []string{
		`service.version=1.2.3`,
		`team.name=core\,platform`,
		`team.note=owns\=api`,
		`mini_cloud.service_id=svc_demo`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("merged resource attributes = %q, want substring %q", got, want)
		}
	}
}

// TestMergeOTelResourceAttributesKeepsEscapedBackslashValues 验证合并资源属性时保留用户值中的转义反斜杠。
func TestMergeOTelResourceAttributesKeepsEscapedBackslashValues(t *testing.T) {
	t.Parallel()

	got := mergeOTelResourceAttributes(`team.path=C:\\ops\\svc`, map[string]string{
		"mini_cloud.service_id": "svc_demo",
	})

	for _, want := range []string{
		`team.path=C:\\ops\\svc`,
		`mini_cloud.service_id=svc_demo`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("merged resource attributes = %q, want substring %q", got, want)
		}
	}
}
