package control

import (
	"context"
	"testing"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	cloudmodel "mini-cloud/internal/cloudplane/model"
)

func TestBuildRoutesPublishesOnlyPublicReadyBackends(t *testing.T) {
	t.Parallel()

	stores := &fakeStore{
		sources: []cloudmodel.RouteSource{
			{ServiceName: "api", NodeID: "node-ready", HostPort: 30080, HasBackend: true},
			{ServiceName: "api", NodeID: "node-offline", HostPort: 30081, HasBackend: true},
		},
		nodes: map[string]cloudmodel.Node{
			"node-ready":   {ID: "node-ready", PrivateIP: "10.0.1.20", Status: cloudmodel.StatusReady},
			"node-offline": {ID: "node-offline", PrivateIP: "10.0.1.21", Status: cloudmodel.StatusOffline},
		},
	}
	controller := newIngressReconciler(nil, stores, cloudplaneconfig.Config{Ingress: cloudplaneconfig.IngressConfig{BaseDomain: "apps.example.test"}}, nil, nil)

	routes, err := controller.buildRoutes(context.Background())
	if err != nil {
		t.Fatalf("buildRoutes returned error: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("routes len = %d, want 1: %+v", len(routes), routes)
	}
	if routes[0].Host != "api.apps.example.test" {
		t.Fatalf("route host = %q", routes[0].Host)
	}
	if len(routes[0].Backends) != 1 || routes[0].Backends[0] != "10.0.1.20:30080" {
		t.Fatalf("route backends = %+v, want ready backend only", routes[0].Backends)
	}
}

func TestBuildRoutesKeepsPublicRouteWithoutReadyBackends(t *testing.T) {
	t.Parallel()

	stores := &fakeStore{
		sources: []cloudmodel.RouteSource{
			{ServiceName: "api"},
		},
		nodes: map[string]cloudmodel.Node{},
	}
	controller := newIngressReconciler(nil, stores, cloudplaneconfig.Config{Ingress: cloudplaneconfig.IngressConfig{BaseDomain: "apps.example.test"}}, nil, nil)

	routes, err := controller.buildRoutes(context.Background())
	if err != nil {
		t.Fatalf("buildRoutes returned error: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("routes len = %d, want 1: %+v", len(routes), routes)
	}
	if routes[0].Host != "api.apps.example.test" {
		t.Fatalf("route host = %q", routes[0].Host)
	}
	if len(routes[0].Backends) != 0 {
		t.Fatalf("route backends = %+v, want empty", routes[0].Backends)
	}
}

func TestBuildRoutesUsesServiceNameHost(t *testing.T) {
	t.Parallel()

	stores := &fakeStore{
		sources: []cloudmodel.RouteSource{
			{ServiceName: "sub2-api", NodeID: "node-ready", HostPort: 30080, HasBackend: true},
		},
		nodes: map[string]cloudmodel.Node{
			"node-ready": {ID: "node-ready", PrivateIP: "10.0.1.20", Status: cloudmodel.StatusReady},
		},
	}
	controller := newIngressReconciler(nil, stores, cloudplaneconfig.Config{Ingress: cloudplaneconfig.IngressConfig{BaseDomain: ".apps.example.test."}}, nil, nil)

	routes, err := controller.buildRoutes(context.Background())
	if err != nil {
		t.Fatalf("buildRoutes returned error: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("routes len = %d, want 1: %+v", len(routes), routes)
	}
	if routes[0].Host != "sub2-api.apps.example.test" {
		t.Fatalf("route host = %q", routes[0].Host)
	}
}

func TestIngressReconcileDisabledDoesNotCallSink(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{}
	controller := newIngressReconciler(nil, &fakeStore{}, cloudplaneconfig.Config{}, sink, nil)
	if err := controller.reconcileOnce(context.Background()); err != nil {
		t.Fatalf("reconcileOnce returned error: %v", err)
	}
	if sink.calls != 0 {
		t.Fatalf("sink calls = %d, want 0", sink.calls)
	}
}

func TestIngressReconcileAppliesEmptyRoutesWhenOnlyCaddyIsConfigured(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{}
	controller := newIngressReconciler(nil, &fakeStore{}, cloudplaneconfig.Config{
		Ingress: cloudplaneconfig.IngressConfig{CaddyAdminURL: "http://127.0.0.1:2019"},
	}, sink, nil)
	if err := controller.reconcileOnce(context.Background()); err != nil {
		t.Fatalf("reconcileOnce returned error: %v", err)
	}
	if sink.calls != 1 {
		t.Fatalf("sink calls = %d, want 1", sink.calls)
	}
	if len(sink.routes) != 0 {
		t.Fatalf("routes = %+v, want empty", sink.routes)
	}
}

func TestIngressReconcileAppliesRoutes(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{}
	frontDoor := &fakeSink{}
	stores := &fakeStore{
		sources: []cloudmodel.RouteSource{{ServiceName: "api"}},
		nodes:   map[string]cloudmodel.Node{},
	}
	controller := newIngressReconciler(nil, stores, cloudplaneconfig.Config{Ingress: cloudplaneconfig.IngressConfig{BaseDomain: "apps.example.test"}}, sink, frontDoor)

	if err := controller.reconcileOnce(context.Background()); err != nil {
		t.Fatalf("reconcileOnce returned error: %v", err)
	}
	if sink.calls != 1 {
		t.Fatalf("sink calls = %d, want 1", sink.calls)
	}
	if frontDoor.calls != 1 {
		t.Fatalf("frontDoor calls = %d, want 1", frontDoor.calls)
	}
}

type fakeStore struct {
	sources []cloudmodel.RouteSource
	nodes   map[string]cloudmodel.Node
}

func (f *fakeStore) ListIngressRouteSources(context.Context) ([]cloudmodel.RouteSource, error) {
	return f.sources, nil
}

func (f *fakeStore) GetNode(_ context.Context, id string) (cloudmodel.Node, error) {
	return f.nodes[id], nil
}

type fakeSink struct {
	calls  int
	routes []cloudmodel.Route
}

func (f *fakeSink) Apply(_ context.Context, routes []cloudmodel.Route) error {
	f.calls++
	f.routes = routes
	return nil
}
