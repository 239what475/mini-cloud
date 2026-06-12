package api

import (
	"encoding/json"
	"strings"
	"testing"

	"mini-cloud/internal/controlplane/model"
)

func TestBuildServiceResourceRedactsSecrets(t *testing.T) {
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
				"MODE": "demo",
			},
			SecretEnv: map[string]string{
				"API_TOKEN": "super-secret-token",
			},
			RegistryCredential: &model.ServiceRegistryCredential{
				Server:   "registry.example.com",
				Username: "demo",
				Password: "registry-password",
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
	if strings.Contains(body, "super-secret-token") || strings.Contains(body, "registry-password") {
		t.Fatalf("service resource leaked secret material: %s", body)
	}
	if !strings.Contains(body, "API_TOKEN") || !strings.Contains(body, `"passwordConfigured":true`) {
		t.Fatalf("service resource did not expose redacted secret metadata: %s", body)
	}
}
