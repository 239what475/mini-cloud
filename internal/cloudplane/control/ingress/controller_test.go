package ingress

import (
	"context"
	"testing"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	cloudmodel "mini-cloud/internal/cloudplane/model"
)

// TestBuildRoutesPublishesOnlyPublicReadyBackends 验证 ingress 只发布 public service 且只包含 ready node backend。
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
	controller := NewController(nil, stores, cloudplaneconfig.Config{Ingress: cloudplaneconfig.IngressConfig{BaseDomain: "apps.example.test"}}, nil)

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

// TestBuildRoutesKeepsPublicRouteWithoutReadyBackends 验证 public service 暂无可用 backend 时仍发布 503 route。
func TestBuildRoutesKeepsPublicRouteWithoutReadyBackends(t *testing.T) {
	t.Parallel()

	stores := &fakeStore{
		sources: []cloudmodel.RouteSource{
			{ServiceName: "api"},
		},
		nodes: map[string]cloudmodel.Node{},
	}
	controller := NewController(nil, stores, cloudplaneconfig.Config{Ingress: cloudplaneconfig.IngressConfig{BaseDomain: "apps.example.test"}}, nil)

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

// TestBuildRoutesUsesSingleManagedServiceHost 验证入口域名只由 serviceName 和 baseDomain 组成。
func TestBuildRoutesUsesSingleManagedServiceHost(t *testing.T) {
	t.Parallel()

	stores := &fakeStore{
		sources: []cloudmodel.RouteSource{
			{ServiceName: "Sub2 API", NodeID: "node-ready", HostPort: 30080, HasBackend: true},
		},
		nodes: map[string]cloudmodel.Node{
			"node-ready": {ID: "node-ready", PrivateIP: "10.0.1.20", Status: cloudmodel.StatusReady},
		},
	}
	controller := NewController(nil, stores, cloudplaneconfig.Config{Ingress: cloudplaneconfig.IngressConfig{BaseDomain: ".apps.example.test."}}, nil)

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

// TestReconcileOnceDisabledDoesNotCallSink 验证 ingress 关闭时不构造或应用路由。
func TestReconcileOnceDisabledDoesNotCallSink(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{}
	controller := NewController(nil, &fakeStore{}, cloudplaneconfig.Config{}, sink)
	if err := controller.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce returned error: %v", err)
	}
	if sink.calls != 0 {
		t.Fatalf("sink calls = %d, want 0", sink.calls)
	}
}

// fakeStore 是 ingress controller 单测使用的只读状态集合。
type fakeStore struct {
	sources []cloudmodel.RouteSource
	nodes   map[string]cloudmodel.Node
}

// ListIngressRouteSources 返回预设 ingress route source 列表。
func (f *fakeStore) ListIngressRouteSources(context.Context) ([]cloudmodel.RouteSource, error) {
	return f.sources, nil
}

// GetNode 返回预设 node。
func (f *fakeStore) GetNode(_ context.Context, id string) (cloudmodel.Node, error) {
	return f.nodes[id], nil
}

// fakeSink 记录 Apply 调用次数。
type fakeSink struct {
	calls  int
	routes []cloudmodel.Route
}

// Apply 记录路由快照。
func (f *fakeSink) Apply(_ context.Context, routes []cloudmodel.Route) error {
	f.calls++
	f.routes = routes
	return nil
}
