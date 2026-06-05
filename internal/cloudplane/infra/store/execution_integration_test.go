package store_test

import (
	"context"
	"testing"
	"time"

	"mini-cloud/internal/cloudplane/domain/deployment"
	"mini-cloud/internal/cloudplane/domain/execution"
	"mini-cloud/internal/cloudplane/domain/node"
	"mini-cloud/internal/cloudplane/domain/scheduler"
	"mini-cloud/internal/cloudplane/domain/workload"
	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/projectedfile"
	"mini-cloud/internal/contract/cloudplaneapi"
	"mini-cloud/internal/testutil"
)

// TestIntegrationCreateExecutionClaimUsesRevisionScopedRuntimeInputs 验证 work item 使用 revision 快照中的运行输入。
func TestIntegrationCreateExecutionClaimUsesRevisionScopedRuntimeInputs(t *testing.T) {
	// 使用真实测试数据库验证 CreateExecutionClaim 使用 revision 快照中的 config/secret 引用。
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)
	// 创建 v1 config/secret set，后续 revisionV1 应固定引用这些资源。
	configSetV1 := cloudplaneapi.ConfigSet{
		ID:   "cfg-config-v1",
		Name: "config-v1",
		Values: map[string]string{
			"SERVICE_MODE": "config-v1",
			"API_HOST":     "https://v1.internal.example",
		},
	}
	secretSetV1 := cloudplaneapi.SecretSet{
		ID:   "sec-secret-v1",
		Name: "secret-v1",
		Values: map[string]string{
			"LOG_LEVEL":   "secret-v1",
			"DB_PASSWORD": "db-pass-v1",
		},
	}
	if _, err := db.Store.ApplyResources(ctx, resourceBundle(configSetV1, secretSetV1)); err != nil {
		t.Fatalf("ApplyResources(v1) returned error: %v", err)
	}

	// 创建 service v1，inline env 会被 config/secret set 中同名 key 覆盖。
	serviceItem, err := db.Store.InsertService(ctx, "svc-demo-web", "demo-web", "Demo Web", workload.Spec{
		Region:        "cn-beijing",
		InstanceClass: workload.InstanceClassSmall,
		Image:         "registry.example.com/demo:v1",
		DefaultPort:   8080,
		ReadinessPath: "/healthz",
		Env: map[string]string{
			"SERVICE_MODE": "inline-v1",
			"LOG_LEVEL":    "inline-v1",
		},
		ConfigSetID: configSetV1.ID,
		SecretSetID: secretSetV1.ID,
	})
	if err != nil {
		t.Fatalf("CreateService returned error: %v", err)
	}

	// 从 service 创建 revisionV1，确认 revision 固定保存 v1 resource 引用。
	revisionV1, err := db.Store.InsertRevisionFromService(ctx, serviceItem.Metadata.ID)
	if err != nil {
		t.Fatalf("InsertRevisionFromService(v1) returned error: %v", err)
	}
	if revisionV1.ConfigSetID != configSetV1.ID {
		t.Fatalf("revisionV1.ConfigSetID = %s, want %s", revisionV1.ConfigSetID, configSetV1.ID)
	}
	if revisionV1.SecretSetID != secretSetV1.ID {
		t.Fatalf("revisionV1.SecretSetID = %s, want %s", revisionV1.SecretSetID, secretSetV1.ID)
	}

	// 再创建 v2 config/secret set，模拟 service 后续更新到新运行输入。
	configSetV2 := cloudplaneapi.ConfigSet{
		ID:   "cfg-config-v2",
		Name: "config-v2",
		Values: map[string]string{
			"SERVICE_MODE": "config-v2",
			"API_HOST":     "https://v2.internal.example",
		},
	}
	secretSetV2 := cloudplaneapi.SecretSet{
		ID:   "sec-secret-v2",
		Name: "secret-v2",
		Values: map[string]string{
			"LOG_LEVEL":   "secret-v2",
			"DB_PASSWORD": "db-pass-v2",
		},
	}
	if _, err := db.Store.ApplyResources(ctx, resourceBundle(configSetV2, secretSetV2)); err != nil {
		t.Fatalf("ApplyResources(v2) returned error: %v", err)
	}

	// 更新当前 service 指向 v2 资源；这不应影响已经创建的 revisionV1。
	updatedSpec := workload.SpecFromService(serviceItem)
	updatedSpec.Image = "registry.example.com/demo:v2"
	updatedSpec.Env = map[string]string{
		"SERVICE_MODE": "inline-v2",
		"LOG_LEVEL":    "inline-v2",
	}
	updatedSpec.ConfigSetID = configSetV2.ID
	updatedSpec.SecretSetID = secretSetV2.ID
	updatedService, impact, err := db.Store.UpdateServiceSpec(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.DisplayName, updatedSpec)
	if err != nil {
		t.Fatalf("UpdateService returned error: %v", err)
	}
	if !impact.RevisionChanged {
		t.Fatalf("UpdateService should require a new revision when runtime inputs change")
	}
	if updatedService.Spec.ConfigSetID != configSetV2.ID {
		t.Fatalf("updated service configSetID = %s, want %s", updatedService.Spec.ConfigSetID, configSetV2.ID)
	}
	if updatedService.Spec.SecretSetID != secretSetV2.ID {
		t.Fatalf("updated service secretSetID = %s, want %s", updatedService.Spec.SecretSetID, secretSetV2.ID)
	}

	// 注册 runtime node，作为 deployment selection 和 work claim 的目标。
	runtimeNodeNode, err := db.Store.RegisterNode(ctx, node.RegisterInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		Name:          "runtime-node-a",
		Role:          node.RoleRuntime,
		PrivateIP:     "10.0.0.10",
		PublicIP:      "203.0.113.10",
		InstanceID:    "i-runtime-node-a",
		InstanceType:  "ecs.u1-c1m1.large",
		CPUMilliTotal: 2000,
		MemoryMiTotal: 4096,
	})
	if err != nil {
		t.Fatalf("RegisterNode returned error: %v", err)
	}
	markIntegrationNodeReady(t, ctx, db, runtimeNodeNode.ID)

	// 创建指向 revisionV1 的 deployment，并推进到 scheduling。
	deploymentItem, err := db.Store.InsertDeployment(ctx, deployment.CreateInput{
		ServiceID:  serviceItem.Metadata.ID,
		RevisionID: revisionV1.ID,
	}, "test deployment for revision v1")
	if err != nil {
		t.Fatalf("CreateDeployment returned error: %v", err)
	}
	if _, err := db.Store.UpdateDeploymentStatus(ctx, deploymentItem.ID, deployment.StatusScheduling, "scheduler picked up the deployment"); err != nil {
		t.Fatalf("UpdateDeploymentStatus(scheduling) returned error: %v", err)
	}

	// 按 service 规格计算资源请求，并创建指向 runtime node 的 selection decision。
	cpuMilliRequest, memoryMiRequest, err := workload.ResourceRequest(serviceItem.Spec.InstanceClass)
	if err != nil {
		t.Fatalf("ResourceRequest returned error: %v", err)
	}
	if _, err := db.Store.CreatePlacementDecisions(ctx, scheduler.PlacementRequest{
		DeploymentID:    deploymentItem.ID,
		Provider:        "aliyun",
		Region:          "cn-beijing",
		CPUMilliRequest: cpuMilliRequest,
		MemoryMiRequest: memoryMiRequest,
	}, []scheduler.PlacementDecision{{
		DeploymentID: deploymentItem.ID,
		NodeID:       runtimeNodeNode.ID,
		Region:       "cn-beijing",
		Score:        100,
		Reason:       "test pinned to runtime-node-a",
	}}); err != nil {
		t.Fatalf("CreatePlacementDecisions returned error: %v", err)
	}
	if _, err := db.Store.UpdateDeploymentStatus(ctx, deploymentItem.ID, deployment.StatusAssigned, "assigned to runtime-node-a"); err != nil {
		t.Fatalf("UpdateDeploymentStatus(assigned) returned error: %v", err)
	}

	// node-agent 拉取 work item 时，store 会把 revisionV1 的运行输入物化进 work.Env。
	work, err := db.Store.CreateExecutionClaim(ctx, runtimeNodeNode.ID)
	if err != nil {
		t.Fatalf("CreateExecutionClaim returned error: %v", err)
	}
	if work == nil {
		t.Fatalf("CreateExecutionClaim returned nil work item")
	}

	// 断言 env 来自 revisionV1 引用的 config/secret，而不是 service 当前的 v2 引用。
	if work.Env["SERVICE_MODE"] != "config-v1" {
		t.Fatalf("SERVICE_MODE = %q, want config-v1 from revision v1 config set", work.Env["SERVICE_MODE"])
	}
	if work.Env["LOG_LEVEL"] != "secret-v1" {
		t.Fatalf("LOG_LEVEL = %q, want secret-v1 from revision v1 secret set", work.Env["LOG_LEVEL"])
	}
	if work.Env["API_HOST"] != "https://v1.internal.example" {
		t.Fatalf("API_HOST = %q, want v1 config set value", work.Env["API_HOST"])
	}
	if work.Env["DB_PASSWORD"] != "db-pass-v1" {
		t.Fatalf("DB_PASSWORD = %q, want secret from revision v1 secret set", work.Env["DB_PASSWORD"])
	}
	if work.Env["SERVICE_MODE"] == "config-v2" || work.Env["LOG_LEVEL"] == "secret-v2" || work.Env["DB_PASSWORD"] == "db-pass-v2" {
		t.Fatalf("work env leaked current service runtime inputs instead of revision-scoped ones: %+v", work.Env)
	}
}

// TestIntegrationCreateExecutionClaimMaterializesRevisionScopedProjectedFiles 验证 projected files 按 revision 快照渲染。
func TestIntegrationCreateExecutionClaimMaterializesRevisionScopedProjectedFiles(t *testing.T) {
	// 使用真实测试数据库验证 projected files 按 revision 快照物化，而不是读取 service 当前 spec。
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)
	// 创建 config/secret 资源，作为 service 输入。

	// 创建 v1 config/secret，projected file 会分别读取非敏感配置和敏感 token。
	configSetV1 := cloudplaneapi.ConfigSet{
		ID:   "cfg-cliproxy-v1",
		Name: "cliproxy-config-v1",
		Values: map[string]string{
			"config.yaml": "listen: :8317\nupstream: https://v1.internal.example\n",
		},
	}
	secretSetV1 := cloudplaneapi.SecretSet{
		ID:   "sec-cliproxy-v1",
		Name: "cliproxy-secret-v1",
		Values: map[string]string{
			"token": "token-v1",
		},
	}
	if _, err := db.Store.ApplyResources(ctx, resourceBundle(configSetV1, secretSetV1)); err != nil {
		t.Fatalf("ApplyResources(v1) returned error: %v", err)
	}

	// 创建带 projected files 的 service v1。
	serviceItem, err := db.Store.InsertService(ctx, "svc-cliproxyapi", "cliproxyapi", "CLI Proxy API", workload.Spec{
		Region:        "cn-beijing",
		InstanceClass: workload.InstanceClassSmall,
		Image:         "ghcr.io/example/cliproxyapi:v1",
		DefaultPort:   8317,
		ReadinessPath: "/healthz",
		ProjectedFiles: []projectedfile.Spec{
			{
				MountPath:  "/etc/cliproxy/config.yaml",
				SourceKind: projectedfile.SourceKindConfigSet,
				SourceID:   configSetV1.ID,
				SourceKey:  "config.yaml",
			},
			{
				MountPath:  "/etc/cliproxy/auth/token",
				SourceKind: projectedfile.SourceKindSecretSet,
				SourceID:   secretSetV1.ID,
				SourceKey:  "token",
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateService returned error: %v", err)
	}

	// 创建 revisionV1，确认 projected file spec 已写入 revision 快照。
	revisionV1, err := db.Store.InsertRevisionFromService(ctx, serviceItem.Metadata.ID)
	if err != nil {
		t.Fatalf("InsertRevisionFromService(v1) returned error: %v", err)
	}
	if len(revisionV1.ProjectedFiles) != 2 {
		t.Fatalf("revisionV1 projected files len = %d, want 2", len(revisionV1.ProjectedFiles))
	}

	// 创建 v2 config/secret，后续更新 service 当前 spec 指向 v2。
	configSetV2 := cloudplaneapi.ConfigSet{
		ID:   "cfg-cliproxy-v2",
		Name: "cliproxy-config-v2",
		Values: map[string]string{
			"config.yaml": "listen: :8317\nupstream: https://v2.internal.example\n",
		},
	}
	secretSetV2 := cloudplaneapi.SecretSet{
		ID:   "sec-cliproxy-v2",
		Name: "cliproxy-secret-v2",
		Values: map[string]string{
			"token": "token-v2",
		},
	}
	if _, err := db.Store.ApplyResources(ctx, resourceBundle(configSetV2, secretSetV2)); err != nil {
		t.Fatalf("ApplyResources(v2) returned error: %v", err)
	}

	// 更新 service 当前 projected file refs；revisionV1 应继续保持 v1 refs。
	updatedSpec := workload.SpecFromService(serviceItem)
	updatedSpec.Image = "ghcr.io/example/cliproxyapi:v2"
	updatedSpec.ProjectedFiles = []projectedfile.Spec{
		{
			MountPath:  "/etc/cliproxy/config.yaml",
			SourceKind: projectedfile.SourceKindConfigSet,
			SourceID:   configSetV2.ID,
			SourceKey:  "config.yaml",
		},
		{
			MountPath:  "/etc/cliproxy/auth/token",
			SourceKind: projectedfile.SourceKindSecretSet,
			SourceID:   secretSetV2.ID,
			SourceKey:  "token",
		},
	}
	updatedService, impact, err := db.Store.UpdateServiceSpec(ctx, serviceItem.Metadata.ID, serviceItem.Metadata.DisplayName, updatedSpec)
	if err != nil {
		t.Fatalf("UpdateService returned error: %v", err)
	}
	if !impact.RevisionChanged {
		t.Fatalf("UpdateService should require a new revision when projected files change")
	}
	if updatedService.Spec.ProjectedFiles[0].SourceID == revisionV1.ProjectedFiles[0].SourceID {
		t.Fatalf("updated service projected files should point at v2 resources")
	}

	// 注册 runtime node，作为 revisionV1 deployment 的执行目标。
	runtimeNodeNode, err := db.Store.RegisterNode(ctx, node.RegisterInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		Name:          "runtime-node-projected",
		Role:          node.RoleRuntime,
		PrivateIP:     "10.0.0.30",
		PublicIP:      "203.0.113.30",
		InstanceID:    "i-runtime-node-projected",
		InstanceType:  "ecs.u1-c1m1.large",
		CPUMilliTotal: 2000,
		MemoryMiTotal: 4096,
	})
	if err != nil {
		t.Fatalf("RegisterNode returned error: %v", err)
	}
	markIntegrationNodeReady(t, ctx, db, runtimeNodeNode.ID)

	// 创建指向 revisionV1 的 deployment，并推进到 assigned。
	deploymentItem, err := db.Store.InsertDeployment(ctx, deployment.CreateInput{
		ServiceID:  serviceItem.Metadata.ID,
		RevisionID: revisionV1.ID,
	}, "test deployment for revision v1 projected files")
	if err != nil {
		t.Fatalf("CreateDeployment returned error: %v", err)
	}
	if _, err := db.Store.UpdateDeploymentStatus(ctx, deploymentItem.ID, deployment.StatusScheduling, "scheduler picked up projected-files deployment"); err != nil {
		t.Fatalf("UpdateDeploymentStatus(scheduling) returned error: %v", err)
	}

	// 创建单副本 selection decision，让 node-agent 可以领取 work。
	cpuMilliRequest, memoryMiRequest, err := workload.ResourceRequest(serviceItem.Spec.InstanceClass)
	if err != nil {
		t.Fatalf("ResourceRequest returned error: %v", err)
	}
	if _, err := db.Store.CreatePlacementDecisions(ctx, scheduler.PlacementRequest{
		DeploymentID:    deploymentItem.ID,
		Provider:        "aliyun",
		Region:          "cn-beijing",
		CPUMilliRequest: cpuMilliRequest,
		MemoryMiRequest: memoryMiRequest,
	}, []scheduler.PlacementDecision{{
		DeploymentID: deploymentItem.ID,
		NodeID:       runtimeNodeNode.ID,
		Region:       "cn-beijing",
		Score:        100,
		Reason:       "test pinned to projected runtime node",
	}}); err != nil {
		t.Fatalf("CreatePlacementDecisions returned error: %v", err)
	}
	if _, err := db.Store.UpdateDeploymentStatus(ctx, deploymentItem.ID, deployment.StatusAssigned, "assigned to projected runtime node"); err != nil {
		t.Fatalf("UpdateDeploymentStatus(assigned) returned error: %v", err)
	}

	// 领取 work item，store 会把 revisionV1 的 projected files 渲染为具体文件内容。
	work, err := db.Store.CreateExecutionClaim(ctx, runtimeNodeNode.ID)
	if err != nil {
		t.Fatalf("CreateExecutionClaim returned error: %v", err)
	}
	if work == nil {
		t.Fatalf("CreateExecutionClaim returned nil work item")
	}
	if len(work.ProjectedFiles) != 2 {
		t.Fatalf("work projected files len = %d, want 2", len(work.ProjectedFiles))
	}
	// CloneFiles 会按挂载路径排序；secret 文件应标记 Sensitive 并使用 v1 token。
	if work.ProjectedFiles[0].MountPath != "/etc/cliproxy/auth/token" || work.ProjectedFiles[0].Content != "token-v1" || !work.ProjectedFiles[0].Sensitive {
		t.Fatalf("unexpected secret projected file: %+v", work.ProjectedFiles[0])
	}
	if work.ProjectedFiles[1].MountPath != "/etc/cliproxy/config.yaml" || work.ProjectedFiles[1].Content != "listen: :8317\nupstream: https://v1.internal.example\n" {
		t.Fatalf("unexpected config projected file: %+v", work.ProjectedFiles[1])
	}
}

// TestIntegrationCreateExecutionClaimMaterializesPersistentDirs 验证 persistent dirs 在 work item 中补全宿主机路径。
func TestIntegrationCreateExecutionClaimMaterializesPersistentDirs(t *testing.T) {
	// 使用真实测试数据库验证 persistent dirs 会在 work item 中物化为 node 本地 source path。
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)
	// 创建带 persistent dir 的 service。

	serviceItem, err := db.Store.InsertService(ctx, "svc-cliproxyapi", "cliproxyapi", "CLI Proxy API", workload.Spec{
		Region:        "cn-beijing",
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

	// 创建 revisionV1，固定 persistent dir spec。
	revisionV1, err := db.Store.InsertRevisionFromService(ctx, serviceItem.Metadata.ID)
	if err != nil {
		t.Fatalf("CreateRevisionFromService returned error: %v", err)
	}
	if len(revisionV1.PersistentDirs) != 1 {
		t.Fatalf("revision persistent dirs len = %d, want 1", len(revisionV1.PersistentDirs))
	}

	// 注册 runtime node，作为持久目录挂载所在节点。
	runtimeNodeNode, err := db.Store.RegisterNode(ctx, node.RegisterInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		Name:          "runtime-node-persistent",
		Role:          node.RoleRuntime,
		PrivateIP:     "10.0.0.31",
		PublicIP:      "203.0.113.31",
		InstanceID:    "i-runtime-node-persistent",
		InstanceType:  "ecs.u1-c1m1.large",
		CPUMilliTotal: 2000,
		MemoryMiTotal: 4096,
	})
	if err != nil {
		t.Fatalf("RegisterNode returned error: %v", err)
	}
	markIntegrationNodeReady(t, ctx, db, runtimeNodeNode.ID)

	// 创建 deployment 并推进到 assigned，使该 node 能领取 execution work。
	deploymentItem, err := db.Store.InsertDeployment(ctx, deployment.CreateInput{
		ServiceID:  serviceItem.Metadata.ID,
		RevisionID: revisionV1.ID,
	}, "test deployment for revision persistent dirs")
	if err != nil {
		t.Fatalf("CreateDeployment returned error: %v", err)
	}
	if _, err := db.Store.UpdateDeploymentStatus(ctx, deploymentItem.ID, deployment.StatusScheduling, "scheduler picked up persistent-dirs deployment"); err != nil {
		t.Fatalf("UpdateDeploymentStatus(scheduling) returned error: %v", err)
	}

	// 创建 selection decision，把副本固定到 runtime node。
	cpuMilliRequest, memoryMiRequest, err := workload.ResourceRequest(serviceItem.Spec.InstanceClass)
	if err != nil {
		t.Fatalf("ResourceRequest returned error: %v", err)
	}
	if _, err := db.Store.CreatePlacementDecisions(ctx, scheduler.PlacementRequest{
		DeploymentID:    deploymentItem.ID,
		Provider:        "aliyun",
		Region:          "cn-beijing",
		CPUMilliRequest: cpuMilliRequest,
		MemoryMiRequest: memoryMiRequest,
	}, []scheduler.PlacementDecision{{
		DeploymentID: deploymentItem.ID,
		NodeID:       runtimeNodeNode.ID,
		Region:       "cn-beijing",
		Score:        100,
		Reason:       "test pinned to persistent runtime node",
	}}); err != nil {
		t.Fatalf("CreatePlacementDecisions returned error: %v", err)
	}
	if _, err := db.Store.UpdateDeploymentStatus(ctx, deploymentItem.ID, deployment.StatusAssigned, "assigned to persistent runtime node"); err != nil {
		t.Fatalf("UpdateDeploymentStatus(assigned) returned error: %v", err)
	}

	// 领取 work item 后，persistent dir spec 应被补全为 node 宿主机 source path。
	work, err := db.Store.CreateExecutionClaim(ctx, runtimeNodeNode.ID)
	if err != nil {
		t.Fatalf("CreateExecutionClaim returned error: %v", err)
	}
	if work == nil {
		t.Fatalf("CreateExecutionClaim returned nil work item")
	}
	if len(work.PersistentDirs) != 1 {
		t.Fatalf("work persistent dirs len = %d, want 1", len(work.PersistentDirs))
	}
	// 名称和挂载路径来自 revision spec。
	if work.PersistentDirs[0].Name != "auth-dir" {
		t.Fatalf("persistent dir name = %q, want auth-dir", work.PersistentDirs[0].Name)
	}
	if work.PersistentDirs[0].MountPath != "/var/lib/cliproxy/auth" {
		t.Fatalf("persistent dir mountPath = %q, want /var/lib/cliproxy/auth", work.PersistentDirs[0].MountPath)
	}
	// source path 由 store 根据 service/deployment/node 信息计算，不能为空。
	if work.PersistentDirs[0].SourcePath == "" {
		t.Fatalf("persistent dir sourcePath should not be empty")
	}
}

// TestIntegrationDeleteExecutionPlanClaimsRunningIntentAndReportsSnapshot 验证 v8 delete plan 会转成原节点 delete work，并在清理上报后形成可完成删除的 snapshot。
func TestIntegrationDeleteExecutionPlanClaimsRunningIntentAndReportsSnapshot(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)

	runtimeNodeNode, err := db.Store.RegisterNode(ctx, node.RegisterInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		Name:          "runtime-node-delete",
		Role:          node.RoleRuntime,
		PrivateIP:     "10.0.0.40",
		PublicIP:      "203.0.113.40",
		InstanceID:    "i-runtime-node-delete",
		InstanceType:  "ecs.u1-c1m1.large",
		CPUMilliTotal: 2000,
		MemoryMiTotal: 4096,
	})
	if err != nil {
		t.Fatalf("RegisterNode returned error: %v", err)
	}
	markIntegrationNodeReady(t, ctx, db, runtimeNodeNode.ID)

	if _, err := db.Store.ApplyExecutionPlan(ctx, execution.PlanInput{
		PlanID:            "svc-delete-g1",
		ServiceID:         "svc-delete",
		ServiceName:       "delete-web",
		ServiceGeneration: 1,
		Image:             "nginx:1.27-alpine",
		ContainerPort:     8080,
		ReadinessPath:     "/healthz",
		InstanceClass:     workload.InstanceClassSmall,
		Exposure:          "public",
	}); err != nil {
		t.Fatalf("ApplyExecutionPlan returned error: %v", err)
	}

	runWork, err := db.Store.CreateExecutionClaim(ctx, runtimeNodeNode.ID)
	if err != nil {
		t.Fatalf("CreateExecutionClaim(run) returned error: %v", err)
	}
	if runWork == nil {
		t.Fatal("CreateExecutionClaim(run) returned nil work item")
	}
	if runWork.Action != execution.WorkActionRun {
		t.Fatalf("run action = %q, want %q", runWork.Action, execution.WorkActionRun)
	}
	if _, _, _, err := db.Store.UpdateExecutionFromNodeReport(ctx, runtimeNodeNode.ID, runWork.ExecutionID, execution.ReportInput{
		Status:        execution.StatusRunning,
		Reason:        "execution is healthy",
		ContainerID:   "ctr-delete-0",
		ContainerName: runWork.ContainerName,
		HostPort:      18080,
	}); err != nil {
		t.Fatalf("UpdateExecutionFromNodeReport(running) returned error: %v", err)
	}

	deleted, err := db.Store.DeleteExecutionPlansForService(ctx, execution.DeletePlanInput{
		ServiceID:         "svc-delete",
		ServiceGeneration: 2,
		PlanID:            "svc-delete-delete-g2",
	})
	if err != nil {
		t.Fatalf("DeleteExecutionPlansForService returned error: %v", err)
	}
	if !deleted {
		t.Fatalf("DeleteExecutionPlansForService deleted = false, want true")
	}

	deleteWork, err := db.Store.CreateExecutionClaim(ctx, runtimeNodeNode.ID)
	if err != nil {
		t.Fatalf("CreateExecutionClaim(delete) returned error: %v", err)
	}
	if deleteWork == nil {
		t.Fatal("CreateExecutionClaim(delete) returned nil work item")
	}
	if deleteWork.Action != execution.WorkActionDelete {
		t.Fatalf("delete action = %q, want %q", deleteWork.Action, execution.WorkActionDelete)
	}
	if deleteWork.NodeID != runtimeNodeNode.ID {
		t.Fatalf("delete nodeID = %q, want original node %q", deleteWork.NodeID, runtimeNodeNode.ID)
	}
	if deleteWork.ContainerID != "ctr-delete-0" || deleteWork.ContainerName != runWork.ContainerName || deleteWork.HostPort != 18080 {
		t.Fatalf("delete work runtime fields = containerID %q containerName %q hostPort %d", deleteWork.ContainerID, deleteWork.ContainerName, deleteWork.HostPort)
	}

	if _, _, _, err := db.Store.UpdateExecutionFromNodeReport(ctx, runtimeNodeNode.ID, deleteWork.ExecutionID, execution.ReportInput{
		Status:        execution.StatusSuperseded,
		Reason:        "service deletion stopped container",
		ContainerID:   deleteWork.ContainerID,
		ContainerName: deleteWork.ContainerName,
		HostPort:      deleteWork.HostPort,
	}); err != nil {
		t.Fatalf("UpdateExecutionFromNodeReport(delete superseded) returned error: %v", err)
	}

	snapshots, err := db.Store.ListExecutionSnapshots(ctx)
	if err != nil {
		t.Fatalf("ListExecutionSnapshots returned error: %v", err)
	}
	deleteSnapshot := findExecutionSnapshot(snapshots, "svc-delete-delete-g2")
	if deleteSnapshot == nil {
		t.Fatalf("delete execution snapshot not found in %+v", snapshots)
	}
	if deleteSnapshot.Status != execution.StatusSuperseded {
		t.Fatalf("delete snapshot = %+v, want superseded status", *deleteSnapshot)
	}
}

// markIntegrationNodeReady 将测试节点推进到 ready/schedulable，确保 selection 写入前的节点状态符合生产调度约束。
func markIntegrationNodeReady(t *testing.T, ctx context.Context, db testutil.TestDatabase, nodeID string) {
	t.Helper()

	if _, _, err := db.Store.RecordNodeHeartbeat(ctx, nodeID, node.HeartbeatInput{
		ReportedAt:          time.Now().UTC(),
		AgentVersion:        "test-agent",
		CPUMilliAllocatable: 1500,
		MemoryMiAllocatable: 3584,
		RunningContainers:   0,
		Status:              node.StatusReady,
	}); err != nil {
		t.Fatalf("RecordNodeHeartbeat returned error: %v", err)
	}
}

func findExecutionSnapshot(items []cloudplaneapi.ExecutionSnapshot, planID string) *cloudplaneapi.ExecutionSnapshot {
	for i := range items {
		if items[i].PlanID == planID {
			return &items[i]
		}
	}
	return nil
}

// resourceBundle 把测试使用的资源组装成全局 resource apply 所需的聚合。
func resourceBundle(configSet cloudplaneapi.ConfigSet, secretSet cloudplaneapi.SecretSet) cloudplaneapi.ResourceBundle {
	return cloudplaneapi.ResourceBundle{
		ConfigSets: []cloudplaneapi.ConfigSet{configSet},
		SecretSets: []cloudplaneapi.SecretSet{secretSet},
	}
}
