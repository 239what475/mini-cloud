package runtime

import (
	"testing"
)

func TestBuildContainerCreateConfigBuildsPublishedPortAndAutoRemove(t *testing.T) {
	t.Parallel()

	config, hostConfig, portSpec, err := buildContainerCreateConfig(RunInput{
		NodeID:        "node-a",
		ExecutionID:   "exec-a",
		PlanID:        "plan-a",
		Image:         "nginx:1.27-alpine",
		ContainerPort: 80,
		HostBindIP:    "10.0.0.20",
		Env: map[string]string{
			"Z_KEY": "z",
			"A_KEY": "a",
		},
	}, 31080)
	if err != nil {
		t.Fatalf("buildContainerCreateConfig returned error: %v", err)
	}

	if portSpec != "80/tcp" {
		t.Fatalf("portSpec = %q, want 80/tcp", portSpec)
	}
	if len(config.Env) != 2 || config.Env[0] != "A_KEY=a" || config.Env[1] != "Z_KEY=z" {
		t.Fatalf("unexpected env ordering: %+v", config.Env)
	}
	if config.Labels[dockerLabelManagedBy] != "node-agent" ||
		config.Labels[dockerLabelNodeID] != "node-a" ||
		config.Labels[dockerLabelExecutionID] != "exec-a" ||
		config.Labels[dockerLabelPlanID] != "plan-a" {
		t.Fatalf("unexpected mini-cloud labels: %+v", config.Labels)
	}
	if !hostConfig.AutoRemove {
		t.Fatalf("expected AutoRemove=true")
	}
	if len(hostConfig.ExtraHosts) != 1 || hostConfig.ExtraHosts[0] != "host.docker.internal:host-gateway" {
		t.Fatalf("unexpected extra hosts: %+v", hostConfig.ExtraHosts)
	}
	bindings := hostConfig.PortBindings[portSpec]
	if len(bindings) != 1 || bindings[0].HostIP != "10.0.0.20" || bindings[0].HostPort != "31080" {
		t.Fatalf("unexpected port bindings: %+v", bindings)
	}
}

func TestBuildContainerCreateConfigRequiresHostPort(t *testing.T) {
	t.Parallel()

	_, _, _, err := buildContainerCreateConfig(RunInput{
		Image:         "nginx:1.27-alpine",
		ContainerPort: 80,
		HostBindIP:    "127.0.0.1",
	}, 0)
	if err == nil {
		t.Fatal("buildContainerCreateConfig returned nil error without host port")
	}
}

func TestSelectAvailableHostPortRejectsInvalidRange(t *testing.T) {
	t.Parallel()

	if _, err := selectAvailableHostPort("127.0.0.1", 30010, 30000); err == nil {
		t.Fatal("selectAvailableHostPort returned nil error for invalid range")
	}
}

func TestResolveContainerCommandKeepsCurrentCliSemantics(t *testing.T) {
	t.Parallel()

	got := resolveContainerCommand(RunInput{
		Command: []string{"python"},
		Args:    []string{"service.py", "--port", "8080"},
	})

	want := []string{"python", "service.py", "--port", "8080"}
	if len(got) != len(want) {
		t.Fatalf("resolveContainerCommand length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("resolveContainerCommand[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
