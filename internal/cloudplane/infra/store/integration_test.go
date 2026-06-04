package store_test

import (
	"context"
	"errors"
	"testing"

	"mini-cloud/internal/cloudplane/domain/deployment"
	"mini-cloud/internal/cloudplane/domain/desired"
	"mini-cloud/internal/cloudplane/domain/workload"
	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/testutil"
)

// TestIntegrationRevisionOnlyUpdatePersistsSpec 验证只改变 revision spec 时正常持久化。
func TestIntegrationRevisionOnlyUpdatePersistsSpec(t *testing.T) {
	// 使用真实测试数据库验证只改变 revision spec 时正常更新 service。
	db := testutil.OpenCloudPlaneTestDatabase(t)

	created, err := db.Store.InsertService(context.Background(), "svc-demo", "demo", "Demo", workload.Spec{
		Region:        "cn-beijing",
		Replicas:      1,
		InstanceClass: workload.InstanceClassSmall,
		Image:         "nginx:1.27-alpine",
		DefaultPort:   8080,
		ReadinessPath: "/",
	})
	if err != nil {
		t.Fatalf("CreateService returned error: %v", err)
	}

	// 更新镜像和 readiness path 会触发新 revision，但不改变 service 数量/副本/规格。
	updateSpec := workload.SpecFromService(created)
	updateSpec.Image = "nginx:1.28-alpine"
	updateSpec.ReadinessPath = "/readyz"
	updated, _, err := db.Store.UpdateServiceSpec(context.Background(), created.Metadata.ID, created.Metadata.DisplayName, updateSpec)
	if err != nil {
		t.Fatalf("expected revision-only update to bypass admission, got %v", err)
	}
	// 更新后的 service 应保存新镜像。
	if updated.Spec.Image != "nginx:1.28-alpine" {
		t.Fatalf("updated image = %q, want nginx:1.28-alpine", updated.Spec.Image)
	}
}

// TestIntegrationServiceDesiredRejectsIdentityRewrite 验证 serviceID 对应的 desired 身份创建后不可改绑。
func TestIntegrationServiceDesiredRejectsIdentityRewrite(t *testing.T) {
	// 使用真实测试数据库确认同一 serviceID 不能被后续 apply 改成另一个 service name。
	db := testutil.OpenCloudPlaneTestDatabase(t)
	ctx := context.Background()

	spec := workload.Spec{
		Region:        "cn-beijing",
		Replicas:      1,
		InstanceClass: workload.InstanceClassSmall,
		Image:         "nginx:1.27-alpine",
		DefaultPort:   8080,
		ReadinessPath: "/",
	}
	if _, err := db.Store.UpsertServiceDesired(ctx, desired.AcceptInput{
		ServiceID:   "svc-desired-identity",
		Name:        "demo",
		DisplayName: "Demo",
		Spec:        spec,
	}); err != nil {
		t.Fatalf("first UpsertServiceDesired returned error: %v", err)
	}

	// 同一个 serviceID 再次提交不同 name，必须显式拒绝，不能静默改绑 desired 身份。
	_, err := db.Store.UpsertServiceDesired(ctx, desired.AcceptInput{
		ServiceID:   "svc-desired-identity",
		Name:        "other-demo",
		DisplayName: "Demo",
		Spec:        spec,
	})
	if !errors.Is(err, workload.ErrServiceIdentityConflict) {
		t.Fatalf("UpsertServiceDesired error = %v, want %v", err, workload.ErrServiceIdentityConflict)
	}
}

// TestIntegrationUpdateServiceRejectsRevisionChangeWhenPersistentDirsAndRevisionExists 验证 persistent dir service 暂不支持已有 revision 后滚动。
func TestIntegrationUpdateServiceRejectsRevisionChangeWhenPersistentDirsAndRevisionExists(t *testing.T) {
	// 使用真实测试数据库验证已有 revision 的 persistent dir service 暂不支持 revision rollout。
	db := testutil.OpenCloudPlaneTestDatabase(t)
	ctx := context.Background()

	created, err := db.Store.InsertService(ctx, "svc-cliproxyapi", "cliproxyapi", "CLI Proxy API", workload.Spec{
		Region:        "cn-beijing",
		Replicas:      1,
		InstanceClass: workload.InstanceClassSmall,
		Image:         "ghcr.io/example/cliproxyapi:v1",
		DefaultPort:   8317,
		ReadinessPath: "/healthz",
		PersistentDirs: []persistentdir.Spec{
			{Name: "auth-dir", MountPath: "/var/lib/cliproxy/auth"},
		},
	})
	if err != nil {
		t.Fatalf("CreateService returned error: %v", err)
	}

	// 先创建并提升一个 revision，让 service 进入已有持久目录运行态。
	rev, err := db.Store.InsertRevisionFromService(ctx, created.Metadata.ID)
	if err != nil {
		t.Fatalf("CreateRevisionFromService returned error: %v", err)
	}
	if _, err := db.Store.UpdateServiceRevisionState(ctx, created.Metadata.ID, rev.ID, "", deployment.StatusRunning, workload.RolloutPhaseIdle, ""); err != nil {
		t.Fatalf("UpdateServiceRevisionState returned error: %v", err)
	}

	// 变更镜像会触发 revision 变化；由于 persistent dir rollout 暂不支持，应被拒绝。
	blockedSpec := workload.SpecFromService(created)
	blockedSpec.Image = "ghcr.io/example/cliproxyapi:v2"
	_, _, err = db.Store.UpdateServiceSpec(ctx, created.Metadata.ID, created.Metadata.DisplayName, blockedSpec)
	if !errors.Is(err, workload.ErrPersistentDirsRolloutUnsupported) {
		t.Fatalf("UpdateService error = %v, want %v", err, workload.ErrPersistentDirsRolloutUnsupported)
	}
}
