package ingress

import (
	"context"
	"testing"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	"mini-cloud/internal/cloudplane/domain/deployment"
	"mini-cloud/internal/cloudplane/domain/execution"
	domainingress "mini-cloud/internal/cloudplane/domain/ingress"
	"mini-cloud/internal/cloudplane/domain/node"
	"mini-cloud/internal/cloudplane/domain/workload"
)

// TestBuildRoutesPublishesOnlyPublicReadyBackends 验证 ingress 只发布 public service 且只包含 ready node backend。
func TestBuildRoutesPublishesOnlyPublicReadyBackends(t *testing.T) {
	t.Parallel()

	stores := &fakeStore{
		services: []workload.Service{
			{Metadata: workload.Metadata{ID: "svc-public", Name: "api"}, Spec: workload.Spec{Exposure: workload.ExposurePublic}},
			{Metadata: workload.Metadata{ID: "svc-private", Name: "worker"}, Spec: workload.Spec{Exposure: workload.ExposurePrivate}},
		},
		deployments: map[string]*deployment.Deployment{
			"svc-public":  {ID: "dep-public", ServiceID: "svc-public"},
			"svc-private": {ID: "dep-private", ServiceID: "svc-private"},
		},
		executions: map[string][]execution.Record{
			"dep-public": {
				{ID: "exec-ready", DeploymentID: "dep-public", NodeID: "node-ready", HostPort: 30080, Status: execution.StatusRunning},
				{ID: "exec-offline", DeploymentID: "dep-public", NodeID: "node-offline", HostPort: 30081, Status: execution.StatusRunning},
			},
		},
		nodes: map[string]node.Node{
			"node-ready":   {ID: "node-ready", PrivateIP: "10.0.1.20", Status: node.StatusReady},
			"node-offline": {ID: "node-offline", PrivateIP: "10.0.1.21", Status: node.StatusOffline},
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
	services    []workload.Service
	deployments map[string]*deployment.Deployment
	executions  map[string][]execution.Record
	nodes       map[string]node.Node
}

// ListServices 返回预设 service 列表。
func (f *fakeStore) ListServices(context.Context) ([]workload.Service, error) {
	return f.services, nil
}

// GetPromotedDeploymentByService 返回预设 deployment。
func (f *fakeStore) GetPromotedDeploymentByService(_ context.Context, serviceID string) (*deployment.Deployment, error) {
	return f.deployments[serviceID], nil
}

// ListRunningExecutionsByDeployment 返回预设 execution 列表。
func (f *fakeStore) ListRunningExecutionsByDeployment(_ context.Context, deploymentID string) ([]execution.Record, error) {
	return f.executions[deploymentID], nil
}

// GetNode 返回预设 node。
func (f *fakeStore) GetNode(_ context.Context, id string) (node.Node, error) {
	return f.nodes[id], nil
}

// fakeSink 记录 Apply 调用次数。
type fakeSink struct {
	calls  int
	routes []domainingress.Route
}

// Apply 记录路由快照。
func (f *fakeSink) Apply(_ context.Context, routes []domainingress.Route) error {
	f.calls++
	f.routes = routes
	return nil
}
