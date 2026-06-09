package nodepool

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	"mini-cloud/internal/cloudplane/infra/runtimepool"
	"mini-cloud/internal/cloudplane/infra/store"
	cloudmodel "mini-cloud/internal/cloudplane/model"
	"mini-cloud/internal/testutil"
)

func TestScaleOutCreatesRuntimeNodeWhenPendingExecutionHasNoCapacity(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)
	driver := &fakeRuntimeDriver{}
	service := NewService(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, driver, testConfig(t))

	seedPendingExecution(t, ctx, db.Store, "scale-out-no-capacity", "medium")

	if err := service.ReconcileScaleOutOnce(ctx); err != nil {
		t.Fatalf("ReconcileScaleOutOnce returned error: %v", err)
	}
	if len(driver.createRequests) != 1 {
		t.Fatalf("create requests = %d, want 1", len(driver.createRequests))
	}
	if driver.createRequests[0].CPUMilli != 1000 || driver.createRequests[0].MemoryMi != 1024 {
		t.Fatalf("unexpected create resource request: %+v", driver.createRequests[0])
	}
	items, err := db.Store.ListRuntimeNodesByStatuses(ctx, runtimepool.StatusProvisioning)
	if err != nil {
		t.Fatalf("ListRuntimeNodesByStatuses returned error: %v", err)
	}
	if len(items) != 1 || items[0].InstanceID != "i-created-1" || items[0].InstanceType != "ecs.demo" {
		t.Fatalf("unexpected runtime nodes: %+v", items)
	}
}

func TestScaleOutSkipsWhenReadyNodeHasCapacity(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)
	driver := &fakeRuntimeDriver{}
	service := NewService(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, driver, testConfig(t))

	seedReadyNode(t, ctx, db.Store, "ready-capacity", 2000, 2048)
	seedPendingExecution(t, ctx, db.Store, "scale-out-has-capacity", "small")

	if err := service.ReconcileScaleOutOnce(ctx); err != nil {
		t.Fatalf("ReconcileScaleOutOnce returned error: %v", err)
	}
	if len(driver.createRequests) != 0 {
		t.Fatalf("create requests = %d, want 0", len(driver.createRequests))
	}
}

func TestScaleOutSkipsWhenRuntimeNodeAlreadyProvisioning(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)
	driver := &fakeRuntimeDriver{}
	service := NewService(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, driver, testConfig(t))

	if _, err := db.Store.CreateRuntimeNodeIntent(ctx, runtimepool.CreateIntentInput{
		Provider:     "aliyun",
		Region:       "cn-beijing",
		InstanceName: "demo-runtime-node-existing",
		InstanceType: "ecs.demo",
		StatusReason: "already provisioning",
	}); err != nil {
		t.Fatalf("CreateRuntimeNodeIntent returned error: %v", err)
	}
	seedPendingExecution(t, ctx, db.Store, "scale-out-existing-provisioning", "small")

	if err := service.ReconcileScaleOutOnce(ctx); err != nil {
		t.Fatalf("ReconcileScaleOutOnce returned error: %v", err)
	}
	if len(driver.createRequests) != 0 {
		t.Fatalf("create requests = %d, want 0", len(driver.createRequests))
	}
}

func TestScaleOutMarksRuntimeNodeDeletedWhenProviderCreateFails(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)
	driver := &fakeRuntimeDriver{createErr: errors.New("provider unavailable")}
	service := NewService(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, driver, testConfig(t))

	seedPendingExecution(t, ctx, db.Store, "scale-out-provider-fails", "small")

	if err := service.ReconcileScaleOutOnce(ctx); err == nil {
		t.Fatal("ReconcileScaleOutOnce returned nil, want error")
	}
	items, err := db.Store.ListRuntimeNodesByStatuses(ctx, runtimepool.StatusProvisioning)
	if err != nil {
		t.Fatalf("ListRuntimeNodesByStatuses provisioning returned error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("provisioning runtime nodes = %d, want 0", len(items))
	}
	items, err = db.Store.ListRuntimeNodesByStatuses(ctx, runtimepool.StatusDeleted)
	if err != nil {
		t.Fatalf("ListRuntimeNodesByStatuses deleted returned error: %v", err)
	}
	if len(items) != 1 || items[0].StatusReason == "" {
		t.Fatalf("unexpected deleted runtime nodes: %+v", items)
	}
}

type fakeRuntimeDriver struct {
	createRequests []runtimepool.CreateRequest
	deleteRequests []runtimepool.DeleteRequest
	createErr      error
}

func (f *fakeRuntimeDriver) Create(_ context.Context, request runtimepool.CreateRequest) (runtimepool.CreateResult, error) {
	f.createRequests = append(f.createRequests, request)
	if f.createErr != nil {
		return runtimepool.CreateResult{}, f.createErr
	}
	return runtimepool.CreateResult{
		InstanceID:   "i-created-1",
		InstanceName: request.Name,
		InstanceType: "ecs.demo",
	}, nil
}

func (f *fakeRuntimeDriver) List(context.Context) ([]runtimepool.Node, error) {
	return nil, nil
}

func (f *fakeRuntimeDriver) Delete(_ context.Context, request runtimepool.DeleteRequest) error {
	f.deleteRequests = append(f.deleteRequests, request)
	return nil
}

func testConfig(t *testing.T) cloudplaneconfig.Config {
	t.Helper()
	return cloudplaneconfig.Config{
		Plane: cloudplaneconfig.PlaneConfig{Name: "demo"},
		Infrastructure: cloudplaneconfig.InfrastructureConfig{
			Provider: "aliyun",
			RegionID: "cn-beijing",
		},
		RuntimeProvisioning: cloudplaneconfig.RuntimeProvisioningConfig{
			ProviderSpec: map[string]any{"instanceType": "ecs.demo"},
		},
	}
}

func seedPendingExecution(t *testing.T, ctx context.Context, stores *store.Store, name string, class string) {
	t.Helper()
	if _, err := stores.ApplyExecutionPlan(ctx, cloudmodel.PlanInput{
		PlanID:            name + "-g1",
		ServiceID:         name,
		ServiceName:       name,
		ServiceGeneration: 1,
		Image:             "nginx:latest",
		ContainerPort:     80,
		ReadinessPath:     "/",
		InstanceClass:     class,
		Exposure:          "public",
	}); err != nil {
		t.Fatalf("ApplyExecutionPlan returned error: %v", err)
	}
}

func seedReadyNode(t *testing.T, ctx context.Context, stores *store.Store, name string, cpuMilli int, memoryMi int) {
	t.Helper()
	nodeItem, err := stores.RegisterNode(ctx, cloudmodel.RegisterInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		Name:          name,
		PrivateIP:     "10.0.0.10",
		InstanceID:    "i-" + name,
		InstanceType:  "ecs.demo",
		CPUMilliTotal: cpuMilli,
		MemoryMiTotal: memoryMi,
	})
	if err != nil {
		t.Fatalf("RegisterNode returned error: %v", err)
	}
	_, _, err = stores.RecordNodeHeartbeat(ctx, nodeItem.ID, cloudmodel.HeartbeatInput{
		ReportedAt:          time.Now().UTC(),
		AgentVersion:        "test-agent",
		CPUMilliAllocatable: cpuMilli,
		MemoryMiAllocatable: memoryMi,
		RunningContainers:   0,
		Status:              cloudmodel.StatusReady,
	})
	if err != nil {
		t.Fatalf("RecordNodeHeartbeat returned error: %v", err)
	}
}
