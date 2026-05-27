package store_test

import (
	"context"
	"errors"
	"testing"

	"mini-cloud/internal/cloudplane/domain/deployment"
	"mini-cloud/internal/cloudplane/domain/desired"
	"mini-cloud/internal/cloudplane/domain/usage"
	"mini-cloud/internal/cloudplane/domain/workload"
	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/project"
	"mini-cloud/internal/testutil"
)

// TestIntegrationCreateProjectPersistsQuota 验证 project quota 持久化和 usage summary 计算。
func TestIntegrationCreateProjectPersistsQuota(t *testing.T) {
	// 使用真实测试数据库验证 project quota 的持久化和 usage summary 读取。
	db := testutil.OpenCloudPlaneTestDatabase(t)
	ownerUserID := "quota-owner"

	// 创建 project 时显式写入 quota。
	created, err := db.Store.CreateProject(context.Background(), project.CreateProjectInput{
		Name:        "quota-demo",
		DisplayName: "Quota Demo",
		OwnerUserID: ownerUserID,
		Quota: project.Quota{
			MaxServices: 2,
			CPUMilli:    1200,
			MemoryMi:    1500,
		},
	})
	if err != nil {
		t.Fatalf("CreateProject returned error: %v", err)
	}

	// 重新读取 project，确认 quota 原样持久化。
	got, err := db.Store.GetProject(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetProject returned error: %v", err)
	}

	if got.Quota.MaxServices != 2 || got.Quota.CPUMilli != 1200 || got.Quota.MemoryMi != 1500 {
		t.Fatalf("unexpected persisted quota: %+v", got.Quota)
	}

	// usage summary 应把未使用额度计算为完整 quota。
	summary, err := usage.BuildProjectUsageSummary(got, nil)
	if err != nil {
		t.Fatalf("BuildProjectUsageSummary returned error: %v", err)
	}
	if summary.Remaining.CPUMilli != 1200 || summary.Remaining.MemoryMi != 1500 {
		t.Fatalf("unexpected remaining quota: %+v", summary.Remaining)
	}
}

// TestIntegrationCreateServiceRejectsQuotaOverflow 验证创建 service 会受 project quota admission 约束。
func TestIntegrationCreateServiceRejectsQuotaOverflow(t *testing.T) {
	// 使用真实测试数据库验证 service 创建会被 project quota admission 拒绝。
	db := testutil.OpenCloudPlaneTestDatabase(t)
	ownerUserID := "small-quota-owner"

	prj, err := db.Store.CreateProject(context.Background(), project.CreateProjectInput{
		Name:        "small-quota",
		DisplayName: "Small Quota",
		OwnerUserID: ownerUserID,
		Quota: project.Quota{
			MaxServices: 1,
			CPUMilli:    500,
			MemoryMi:    512,
		},
	})
	if err != nil {
		t.Fatalf("CreateProject returned error: %v", err)
	}

	// 第一个 service 消耗掉项目允许的唯一 service 名额。
	_, err = db.Store.InsertService(context.Background(), "svc-demo-one", prj.ID, "demo-one", "Demo One", workload.Spec{
		Region:        "cn-beijing",
		Replicas:      1,
		InstanceClass: workload.InstanceClassSmall,
		Image:         "nginx:1.27-alpine",
		DefaultPort:   8080,
		ReadinessPath: "/",
	})
	if err != nil {
		t.Fatalf("first CreateService returned error: %v", err)
	}

	// 第二个 service 会超过 max services。
	_, err = db.Store.InsertService(context.Background(), "svc-demo-two", prj.ID, "demo-two", "Demo Two", workload.Spec{
		Region:        "cn-beijing",
		Replicas:      1,
		InstanceClass: workload.InstanceClassSmall,
		Image:         "nginx:1.27-alpine",
		DefaultPort:   8080,
		ReadinessPath: "/",
	})
	if err == nil {
		t.Fatalf("expected second CreateService to be rejected")
	}

	// 错误应保留结构化 quota rejection，供 API 层生成 details。
	var quotaErr *usage.QuotaExceededError
	if !errors.As(err, &quotaErr) {
		t.Fatalf("expected QuotaExceededError, got %T: %v", err, err)
	}
	if len(quotaErr.RejectReasons) == 0 {
		t.Fatalf("expected quota rejection reasons, got none")
	}
	if quotaErr.RejectReasons[0].Code != usage.RejectReasonServicesQuotaExceeded {
		t.Fatalf("expected services quota rejection, got %+v", quotaErr.RejectReasons)
	}
}

// TestIntegrationRevisionOnlyUpdateSkipsAdmission 验证只改变 revision spec 时跳过容量 admission。
func TestIntegrationRevisionOnlyUpdateSkipsAdmission(t *testing.T) {
	// 使用真实测试数据库验证只改变 revision spec 时不重新做容量 admission。
	db := testutil.OpenCloudPlaneTestDatabase(t)
	ownerUserID := "revision-only-owner"

	prj, err := db.Store.CreateProject(context.Background(), project.CreateProjectInput{
		Name:        "revision-only",
		DisplayName: "Revision Only",
		OwnerUserID: ownerUserID,
		Quota: project.Quota{
			MaxServices: 1,
			CPUMilli:    500,
			MemoryMi:    512,
		},
	})
	if err != nil {
		t.Fatalf("CreateProject returned error: %v", err)
	}

	created, err := db.Store.InsertService(context.Background(), "svc-demo", prj.ID, "demo", "Demo", workload.Spec{
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

	// 将 project quota 收紧到低于当前 service 占用，模拟历史服务已存在但新 quota 更小。
	if _, err := db.Store.UpdateProject(context.Background(), prj.ID, project.UpdateProjectInput{
		DisplayName: prj.DisplayName,
		OwnerUserID: prj.OwnerUserID,
		Quota: project.Quota{
			MaxServices: 1,
			CPUMilli:    250,
			MemoryMi:    256,
		},
	}); err != nil {
		t.Fatalf("UpdateProject returned error: %v", err)
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

	ownerUserID := "desired-identity-owner"
	projectItem, err := db.Store.CreateProject(ctx, project.CreateProjectInput{
		Name:        "desired-identity",
		DisplayName: "Desired Identity",
		OwnerUserID: ownerUserID,
	})
	if err != nil {
		t.Fatalf("CreateProject returned error: %v", err)
	}

	spec := workload.Spec{
		Region:        "cn-beijing",
		Replicas:      1,
		InstanceClass: workload.InstanceClassSmall,
		Image:         "nginx:1.27-alpine",
		DefaultPort:   8080,
		ReadinessPath: "/",
	}
	if _, err := db.Store.UpsertServiceDesired(ctx, desired.AcceptInput{
		ProjectID:   projectItem.ID,
		ServiceID:   "svc-desired-identity",
		Name:        "demo",
		DisplayName: "Demo",
		Spec:        spec,
	}); err != nil {
		t.Fatalf("first UpsertServiceDesired returned error: %v", err)
	}

	// 同一个 serviceID 再次提交不同 name，必须显式拒绝，不能静默改绑 desired 身份。
	_, err = db.Store.UpsertServiceDesired(ctx, desired.AcceptInput{
		ProjectID:   projectItem.ID,
		ServiceID:   "svc-desired-identity",
		Name:        "other-demo",
		DisplayName: "Demo",
		Spec:        spec,
	})
	if !errors.Is(err, workload.ErrServiceIdentityConflict) {
		t.Fatalf("UpsertServiceDesired error = %v, want %v", err, workload.ErrServiceIdentityConflict)
	}

	otherProject, err := db.Store.CreateProject(ctx, project.CreateProjectInput{
		Name:        "desired-identity-other",
		DisplayName: "Desired Identity Other",
		OwnerUserID: ownerUserID,
	})
	if err != nil {
		t.Fatalf("CreateProject(other) returned error: %v", err)
	}
	// 同一个 serviceID 再次提交不同 projectID，即使 name 不变，也必须拒绝改绑。
	_, err = db.Store.UpsertServiceDesired(ctx, desired.AcceptInput{
		ProjectID:   otherProject.ID,
		ServiceID:   "svc-desired-identity",
		Name:        "demo",
		DisplayName: "Demo",
		Spec:        spec,
	})
	if !errors.Is(err, workload.ErrServiceIdentityConflict) {
		t.Fatalf("UpsertServiceDesired project rewrite error = %v, want %v", err, workload.ErrServiceIdentityConflict)
	}
}

// TestIntegrationUpdateServiceRejectsRevisionChangeWhenPersistentDirsAndRevisionExists 验证 persistent dir service 暂不支持已有 revision 后滚动。
func TestIntegrationUpdateServiceRejectsRevisionChangeWhenPersistentDirsAndRevisionExists(t *testing.T) {
	// 使用真实测试数据库验证已有 revision 的 persistent dir service 暂不支持 revision rollout。
	db := testutil.OpenCloudPlaneTestDatabase(t)
	ctx := context.Background()

	ownerUserID := "persistent-rollout-owner"

	prj, err := db.Store.CreateProject(ctx, project.CreateProjectInput{
		Name:        "persistent-rollout",
		DisplayName: "Persistent Rollout",
		OwnerUserID: ownerUserID,
	})
	if err != nil {
		t.Fatalf("CreateProject returned error: %v", err)
	}

	created, err := db.Store.InsertService(ctx, "svc-cliproxyapi", prj.ID, "cliproxyapi", "CLI Proxy API", workload.Spec{
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
