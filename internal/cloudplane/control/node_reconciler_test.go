package control

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	"mini-cloud/internal/cloudplane/infra/nodeprovider"
	"mini-cloud/internal/cloudplane/infra/store"
	cloudmodel "mini-cloud/internal/cloudplane/model"
	"mini-cloud/internal/testutil"
)

func TestReconcileCreatesProvisioningNodeWhenPendingServiceRunHasNoCapacity(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)
	driver := &fakeDriver{}
	service := newNodeReconciler(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, driver, testConfig(t))

	seedPendingServiceRun(t, ctx, db.Store, "needs-new-node", "medium")

	if err := service.reconcileOnce(ctx); err != nil {
		t.Fatalf("reconcileOnce returned error: %v", err)
	}
	if len(driver.createRequests) != 1 {
		t.Fatalf("create requests = %d, want 1", len(driver.createRequests))
	}
	if driver.createRequests[0].CPUMilli != 1000 || driver.createRequests[0].MemoryMi != 1024 {
		t.Fatalf("unexpected create resource request: %+v", driver.createRequests[0])
	}
	items, err := db.Store.ListElasticNodesByStatuses(ctx, cloudmodel.StatusProvisioning)
	if err != nil {
		t.Fatalf("ListElasticNodesByStatuses returned error: %v", err)
	}
	if len(items) != 1 || items[0].InstanceID != "i-created-1" || items[0].InstanceType != "ecs.demo" {
		t.Fatalf("unexpected nodes: %+v", items)
	}
}

func TestReconcileSkipsProvisioningWhenReadyNodeHasCapacity(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)
	driver := &fakeDriver{}
	service := newNodeReconciler(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, driver, testConfig(t))

	seedReadyNode(t, ctx, db.Store, "ready-capacity", 2000, 2048)
	seedPendingServiceRun(t, ctx, db.Store, "has-capacity", "small")

	if err := service.reconcileOnce(ctx); err != nil {
		t.Fatalf("reconcileOnce returned error: %v", err)
	}
	if len(driver.createRequests) != 0 {
		t.Fatalf("create requests = %d, want 0", len(driver.createRequests))
	}
}

func TestReconcileSkipsProvisioningWhenNodeAlreadyProvisioning(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)
	driver := &fakeDriver{}
	service := newNodeReconciler(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, driver, testConfig(t))

	if _, err := db.Store.CreateProvisioningNode(ctx, cloudmodel.ProvisioningInput{
		Provider:     "aliyun",
		Region:       "cn-beijing",
		Name:         "demo-existing",
		InstanceType: "ecs.demo",
		StatusReason: "already provisioning",
	}); err != nil {
		t.Fatalf("CreateProvisioningNode returned error: %v", err)
	}
	seedPendingServiceRun(t, ctx, db.Store, "existing-provisioning", "small")

	if err := service.reconcileOnce(ctx); err != nil {
		t.Fatalf("reconcileOnce returned error: %v", err)
	}
	if len(driver.createRequests) != 0 {
		t.Fatalf("create requests = %d, want 0", len(driver.createRequests))
	}
}

func TestReconcileCreatesNodeWhenExistingProvisioningNodeUsesDifferentInstanceType(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)
	driver := &fakeDriver{}
	service := newNodeReconciler(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, driver, testConfig(t))

	if _, err := db.Store.CreateProvisioningNode(ctx, cloudmodel.ProvisioningInput{
		Provider:     "aliyun",
		Region:       "cn-beijing",
		Name:         "demo-node-old-type",
		InstanceType: "ecs.old",
		StatusReason: "stale provisioning node from previous node provisioning config",
	}); err != nil {
		t.Fatalf("CreateProvisioningNode returned error: %v", err)
	}
	seedPendingServiceRun(t, ctx, db.Store, "new-node-config", "small")

	if err := service.reconcileOnce(ctx); err != nil {
		t.Fatalf("reconcileOnce returned error: %v", err)
	}
	if len(driver.createRequests) != 1 {
		t.Fatalf("create requests = %d, want 1", len(driver.createRequests))
	}
}

func TestReconcileMarksNodeDeletedWhenProviderCreateFails(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)
	driver := &fakeDriver{createErr: errors.New("provider unavailable")}
	service := newNodeReconciler(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, driver, testConfig(t))

	seedPendingServiceRun(t, ctx, db.Store, "provider-create-fails", "small")

	if err := service.reconcileOnce(ctx); err == nil {
		t.Fatal("reconcileOnce returned nil, want error")
	}
	items, err := db.Store.ListElasticNodesByStatuses(ctx, cloudmodel.StatusProvisioning)
	if err != nil {
		t.Fatalf("ListElasticNodesByStatuses provisioning returned error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("provisioning nodes = %d, want 0", len(items))
	}
	items, err = db.Store.ListElasticNodesByStatuses(ctx, cloudmodel.StatusDeleted)
	if err != nil {
		t.Fatalf("ListElasticNodesByStatuses deleted returned error: %v", err)
	}
	if len(items) != 1 || items[0].StatusReason == "" {
		t.Fatalf("unexpected deleted nodes: %+v", items)
	}
	snapshots, err := db.Store.ListExecutionSnapshots(ctx)
	if err != nil {
		t.Fatalf("ListExecutionSnapshots returned error: %v", err)
	}
	if len(snapshots) != 1 || snapshots[0].Status != cloudmodel.StatusFailed {
		t.Fatalf("execution snapshots = %+v, want failed", snapshots)
	}
	if len(driver.createRequests) != 1 {
		t.Fatalf("create requests after failed reconcile = %d, want 1", len(driver.createRequests))
	}
	if err := service.reconcileOnce(ctx); err != nil {
		t.Fatalf("second reconcileOnce returned error: %v", err)
	}
	if len(driver.createRequests) != 1 {
		t.Fatalf("create requests after second reconcile = %d, want no retry", len(driver.createRequests))
	}
}

func TestReconcileDeletesProviderInstanceWhenBindFails(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)
	driver := &fakeDriver{}
	service := newNodeReconciler(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, driver, testConfig(t))

	existing, err := db.Store.RegisterNode(ctx, cloudmodel.RegisterInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		Name:          "existing-instance",
		PrivateIP:     "10.0.0.9",
		InstanceID:    "i-created-1",
		InstanceType:  "ecs.demo",
		CPUMilliTotal: 1,
		MemoryMiTotal: 1,
	})
	if err != nil {
		t.Fatalf("RegisterNode returned error: %v", err)
	}
	if _, err := db.Store.RecordNodeHeartbeat(ctx, existing.ID, cloudmodel.HeartbeatInput{
		CPUMilliAllocatable: 1,
		MemoryMiAllocatable: 1,
	}); err != nil {
		t.Fatalf("RecordNodeHeartbeat returned error: %v", err)
	}
	seedPendingServiceRun(t, ctx, db.Store, "bind-fails", "small")

	if err := service.reconcileOnce(ctx); err == nil {
		t.Fatal("reconcileOnce returned nil, want bind error")
	}
	if len(driver.createRequests) != 1 {
		t.Fatalf("create requests = %d, want 1", len(driver.createRequests))
	}
	if len(driver.deleteRequests) != 1 || driver.deleteRequests[0].InstanceID != "i-created-1" {
		t.Fatalf("delete requests = %+v, want rollback of created instance", driver.deleteRequests)
	}
	nodes, err := db.Store.ListElasticNodesByStatuses(ctx, cloudmodel.StatusDeleted)
	if err != nil {
		t.Fatalf("ListElasticNodesByStatuses returned error: %v", err)
	}
	if len(nodes) != 1 || nodes[0].StatusReason == "" {
		t.Fatalf("deleted elastic nodes = %+v, want failed provisioning node marked deleted", nodes)
	}
	snapshots, err := db.Store.ListExecutionSnapshots(ctx)
	if err != nil {
		t.Fatalf("ListExecutionSnapshots returned error: %v", err)
	}
	if len(snapshots) != 1 || snapshots[0].Status != cloudmodel.StatusFailed {
		t.Fatalf("execution snapshots = %+v, want failed", snapshots)
	}
}

func TestReconcileDeletesStaleProvisioningNodeAndFailsPendingServiceRun(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)
	driver := &fakeDriver{}
	service := newNodeReconciler(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, driver, testConfig(t))
	seedPendingServiceRun(t, ctx, db.Store, "stale-provisioning", "small")
	nodeName := "demo-node-" + serviceRunHashSuffix("stale-provisioning", 1)
	node, err := db.Store.CreateProvisioningNode(ctx, cloudmodel.ProvisioningInput{
		Provider:     "aliyun",
		Region:       "cn-beijing",
		Name:         nodeName,
		InstanceType: "ecs.demo",
		StatusReason: "test stale provisioning node",
	})
	if err != nil {
		t.Fatalf("CreateProvisioningNode returned error: %v", err)
	}
	if _, err := db.Store.BindProvisionedNode(ctx, node.ID, "i-stale-provisioning", nodeName, "ecs.demo", "test provisioned", time.Now().UTC().Add(-provisioningNodeTimeout-time.Minute)); err != nil {
		t.Fatalf("BindProvisionedNode returned error: %v", err)
	}
	if _, err := db.DB.ExecContext(ctx, `
		UPDATE nodes
		SET updated_at = $2
		WHERE id = $1
	`, node.ID, time.Now().UTC().Add(-provisioningNodeTimeout-time.Minute)); err != nil {
		t.Fatalf("seed stale provisioning updated_at: %v", err)
	}

	if err := service.reconcileOnce(ctx); err != nil {
		t.Fatalf("reconcileOnce returned error: %v", err)
	}
	if len(driver.deleteRequests) != 1 || driver.deleteRequests[0].InstanceID != "i-stale-provisioning" {
		t.Fatalf("delete requests = %+v, want stale provisioning node deletion", driver.deleteRequests)
	}
	deleted, err := db.Store.GetNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("GetNode returned error: %v", err)
	}
	if deleted.Status != cloudmodel.StatusDeleted {
		t.Fatalf("node status = %q, want deleted", deleted.Status)
	}
	snapshots, err := db.Store.ListExecutionSnapshots(ctx)
	if err != nil {
		t.Fatalf("ListExecutionSnapshots returned error: %v", err)
	}
	if len(snapshots) != 1 || snapshots[0].Status != cloudmodel.StatusFailed {
		t.Fatalf("execution snapshots = %+v, want failed", snapshots)
	}
}

func TestReconcileDeletesIdleNode(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)
	driver := &fakeDriver{}
	service := newNodeReconciler(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, driver, testConfig(t))

	node := seedReadyElasticNode(t, ctx, db.Store, "idle-node", "i-idle-node")

	if err := service.reconcileOnce(ctx); err != nil {
		t.Fatalf("reconcileOnce returned error: %v", err)
	}
	if len(driver.deleteRequests) != 1 || driver.deleteRequests[0].InstanceID != "i-idle-node" {
		t.Fatalf("delete requests = %+v, want idle node deletion", driver.deleteRequests)
	}
	items, err := db.Store.ListElasticNodesByStatuses(ctx, cloudmodel.StatusDeleted)
	if err != nil {
		t.Fatalf("ListElasticNodesByStatuses deleted returned error: %v", err)
	}
	if len(items) != 1 || items[0].ID != node.ID {
		t.Fatalf("deleted nodes = %+v, want %s", items, node.ID)
	}
}

func serviceRunHashSuffix(serviceID string, generation int64) string {
	sum := md5.Sum([]byte(fmt.Sprintf("%s-%d", serviceID, generation)))
	return hex.EncodeToString(sum[:])[:10]
}

func TestReconcileKeepsIdleFixedNode(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)
	driver := &fakeDriver{}
	service := newNodeReconciler(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, driver, testConfig(t))

	seedReadyNode(t, ctx, db.Store, "fixed-node", 2000, 2048)

	if err := service.reconcileOnce(ctx); err != nil {
		t.Fatalf("reconcileOnce returned error: %v", err)
	}
	if len(driver.deleteRequests) != 0 {
		t.Fatalf("delete requests = %+v, want no fixed node deletion", driver.deleteRequests)
	}
}

func TestReconcileDoesNotDeleteIdleNodeInNodeCreationRound(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)
	driver := &fakeDriver{}
	service := newNodeReconciler(slog.New(slog.NewTextHandler(io.Discard, nil)), db.Store, driver, testConfig(t))

	seedReadyElasticNode(t, ctx, db.Store, "idle-node", "i-idle-node")
	seedPendingServiceRun(t, ctx, db.Store, "new-node-and-idle-node", "large")

	if err := service.reconcileOnce(ctx); err != nil {
		t.Fatalf("reconcileOnce returned error: %v", err)
	}
	if len(driver.createRequests) != 1 {
		t.Fatalf("create requests = %d, want 1", len(driver.createRequests))
	}
	if len(driver.deleteRequests) != 0 {
		t.Fatalf("delete requests = %+v, want no deletion while creating a node", driver.deleteRequests)
	}
}

type fakeDriver struct {
	createRequests []nodeprovider.CreateRequest
	deleteRequests []nodeprovider.DeleteRequest
	createErr      error
}

func (f *fakeDriver) Create(_ context.Context, request nodeprovider.CreateRequest) (nodeprovider.CreateResult, error) {
	f.createRequests = append(f.createRequests, request)
	if f.createErr != nil {
		return nodeprovider.CreateResult{}, f.createErr
	}
	return nodeprovider.CreateResult{
		InstanceID:   "i-created-1",
		InstanceName: request.Name,
		InstanceType: "ecs.demo",
	}, nil
}

func (f *fakeDriver) Delete(_ context.Context, request nodeprovider.DeleteRequest) error {
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
		NodeProvisioning: cloudplaneconfig.NodeProvisioningConfig{
			InstanceType: "ecs.demo",
			Aliyun: cloudplaneconfig.AliyunNodeConfig{
				ImageID:         "m-test",
				VSwitchID:       "vsw-test",
				SecurityGroupID: "sg-test",
			},
		},
	}
}

func seedPendingServiceRun(t *testing.T, ctx context.Context, stores *store.Store, name string, class string) {
	t.Helper()

	if _, err := stores.UpsertService(ctx, cloudmodel.UpsertServiceInput{
		ID:          name,
		Name:        name,
		DisplayName: name,
		Host:        name + ".apps.example.test",
		Generation:  1,
		Spec: cloudmodel.ServiceSpec{
			InstanceClass: class,
			Exposure:      cloudmodel.ExposurePublic,
			Image:         "nginx:latest",
			ContainerPort: 80,
			ReadinessPath: "/",
		},
	}); err != nil {
		t.Fatalf("UpsertService returned error: %v", err)
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
	_, err = stores.RecordNodeHeartbeat(ctx, nodeItem.ID, cloudmodel.HeartbeatInput{
		CPUMilliAllocatable: cpuMilli,
		MemoryMiAllocatable: memoryMi,
	})
	if err != nil {
		t.Fatalf("RecordNodeHeartbeat returned error: %v", err)
	}
}

func seedReadyElasticNode(t *testing.T, ctx context.Context, stores *store.Store, name string, instanceID string) cloudmodel.Node {
	t.Helper()

	provisioning, err := stores.CreateProvisioningNode(ctx, cloudmodel.ProvisioningInput{
		Provider:     "aliyun",
		Region:       "cn-beijing",
		Name:         name,
		InstanceType: "ecs.demo",
		StatusReason: "test node",
	})
	if err != nil {
		t.Fatalf("CreateProvisioningNode returned error: %v", err)
	}
	if _, err := stores.BindProvisionedNode(ctx, provisioning.ID, instanceID, name, "ecs.demo", "test node provisioned", time.Now().UTC()); err != nil {
		t.Fatalf("BindProvisionedNode returned error: %v", err)
	}

	node, err := stores.RegisterNode(ctx, cloudmodel.RegisterInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		Name:          name,
		PrivateIP:     "10.0.0.10",
		InstanceID:    instanceID,
		InstanceType:  "ecs.demo",
		CPUMilliTotal: 1000,
		MemoryMiTotal: 1024,
	})
	if err != nil {
		t.Fatalf("RegisterNode returned error: %v", err)
	}
	if node.ID != provisioning.ID {
		t.Fatalf("registered node ID = %s, want provisioning node ID %s", node.ID, provisioning.ID)
	}
	if _, err := stores.RecordNodeHeartbeat(ctx, node.ID, cloudmodel.HeartbeatInput{
		CPUMilliAllocatable: 1000,
		MemoryMiAllocatable: 1024,
	}); err != nil {
		t.Fatalf("RecordNodeHeartbeat returned error: %v", err)
	}

	ready, err := stores.GetNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("GetNode returned error: %v", err)
	}
	if ready.Status != cloudmodel.StatusReady {
		t.Fatalf("node %s did not become ready: %+v", node.ID, ready)
	}
	return ready
}
