package api

import (
	"encoding/json"
	"strings"
	"testing"

	"mini-cloud/internal/controlplane/model"
)

func TestBuildServiceResourceIncludesEnv(t *testing.T) {
	t.Parallel()

	resource := buildServiceResource(model.Service{
		Metadata: model.ServiceMetadata{
			ID:          "svc_test",
			Name:        "demo-api",
			DisplayName: "Demo API",
			Generation:  1,
		},
		Spec: model.ServiceSpec{
			PlaneID:       "pln_test",
			InstanceClass: model.InstanceClassSmall,
			Exposure:      "public",
			Image:         "registry.example.com/demo/api:v1",
			DefaultPort:   8080,
			ReadinessPath: "/healthz",
			Env: map[string]string{
				"MODE":      "demo",
				"API_TOKEN": "service-token",
			},
		},
		Status: model.ServiceStatus{
			DesiredState: model.DesiredStateActive,
			Observed: model.ServiceObservedStatus{
				Phase: model.PhasePending,
			},
			Run: model.RunStatus{Phase: model.RunPhasePending},
		},
	})

	payload, err := json.Marshal(resource)
	if err != nil {
		t.Fatalf("marshal service resource returned error: %v", err)
	}
	body := string(payload)
	if !strings.Contains(body, "API_TOKEN") || !strings.Contains(body, "service-token") {
		t.Fatalf("service resource did not include env: %s", body)
	}
}
