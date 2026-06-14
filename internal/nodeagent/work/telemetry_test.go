package work

import (
	"strings"
	"testing"

	nodeagentv1 "mini-cloud/internal/gen/proto/minicloud/nodeagent/v1"
)

func TestInjectTelemetryEnvSetsDefaults(t *testing.T) {
	t.Parallel()

	base := map[string]string{
		"APP_ENV": "test",
	}
	item := &nodeagentv1.WorkItem{
		ServiceId:   "svc_demo",
		ServiceName: "hello",
		ExecutionId: "exec_demo",
	}

	got := injectTelemetryEnv(base, item, telemetryEnvOptions{
		PlatformName:         "mini-cloud-ops",
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
		"mini_cloud.service_id=svc_demo",
		"mini_cloud.execution_id=exec_demo",
		"mini_cloud.platform_name=mini-cloud-ops",
	} {
		if !strings.Contains(got["OTEL_RESOURCE_ATTRIBUTES"], want) {
			t.Fatalf("OTEL_RESOURCE_ATTRIBUTES = %q, want substring %q", got["OTEL_RESOURCE_ATTRIBUTES"], want)
		}
	}
	if _, exists := base["OTEL_EXPORTER_OTLP_ENDPOINT"]; exists {
		t.Fatalf("injectTelemetryEnv should not mutate input map")
	}
}

func TestInjectTelemetryEnvKeepsUserProvidedIdentityFields(t *testing.T) {
	t.Parallel()

	got := injectTelemetryEnv(map[string]string{
		"OTEL_SERVICE_NAME":           "custom-service-name",
		"OTEL_RESOURCE_ATTRIBUTES":    "service.version=1.2.3,mini_cloud.service_id=wrong",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "http://custom-collector:4318",
	}, &nodeagentv1.WorkItem{
		ServiceId:   "svc_demo",
		ExecutionId: "exec_demo",
		ServiceName: "ignored-by-test",
	}, telemetryEnvOptions{
		PlatformName:         "mini-cloud-ops",
		WorkloadOTLPEndpoint: "http://platform-collector:4318",
	})

	if got["OTEL_SERVICE_NAME"] != "custom-service-name" {
		t.Fatalf("OTEL_SERVICE_NAME = %q, want custom-service-name", got["OTEL_SERVICE_NAME"])
	}
	for _, want := range []string{
		"service.version=1.2.3",
		"mini_cloud.service_id=svc_demo",
		"mini_cloud.execution_id=exec_demo",
		"mini_cloud.platform_name=mini-cloud-ops",
	} {
		if !strings.Contains(got["OTEL_RESOURCE_ATTRIBUTES"], want) {
			t.Fatalf("OTEL_RESOURCE_ATTRIBUTES = %q, want substring %q", got["OTEL_RESOURCE_ATTRIBUTES"], want)
		}
	}
	if got["OTEL_EXPORTER_OTLP_ENDPOINT"] != "http://platform-collector:4318" {
		t.Fatalf("OTEL_EXPORTER_OTLP_ENDPOINT = %q, want platform endpoint override", got["OTEL_EXPORTER_OTLP_ENDPOINT"])
	}
}

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
