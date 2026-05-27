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
	"mini-cloud/internal/common/project"
	"mini-cloud/internal/common/projectedfile"
	"mini-cloud/internal/contract/cloudplaneapi"
	"mini-cloud/internal/testutil"
)

// TestIntegrationCreateExecutionClaimUsesRevisionScopedRuntimeInputs 验证 work item 使用 revision 快照中的运行输入。
func TestIntegrationCreateExecutionClaimUsesRevisionScopedRuntimeInputs(t *testing.T) {
	// 使用真实测试数据库验证 CreateExecutionClaim 使用 revision 快照中的 config/secret 引用。
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)
	// 创建 project owner 和 project，作为后续资源归属。
	ownerUserID := "runtime-inputs-owner"

	projectItem, err := db.Store.CreateProject(ctx, project.CreateProjectInput{
		Name:        "runtime-inputs",
		DisplayName: "Runtime Inputs",
		OwnerUserID: ownerUserID,
	})
	if err != nil {
		t.Fatalf("CreateProject returned error: %v", err)
	}

	// 创建 v1 config/secret set，后续 revisionV1 应固定引用这些资源。
	configSetV1 := cloudplaneapi.ProjectConfigSet{
		ID:   "cfg-config-v1",
		Name: "config-v1",
		Values: map[string]string{
			"SERVICE_MODE": "config-v1",
			"API_HOST":     "https://v1.internal.example",
		},
	}
	secretSetV1 := cloudplaneapi.ProjectSecretSet{
		ID:   "sec-secret-v1",
		Name: "secret-v1",
		Values: map[string]string{
			"LOG_LEVEL":   "secret-v1",
			"DB_PASSWORD": "db-pass-v1",
		},
	}
	if _, err := db.Store.ApplyProjectResources(ctx, projectItem.ID, projectResources(configSetV1, secretSetV1)); err != nil {
		t.Fatalf("ApplyProjectResources(v1) returned error: %v", err)
	}

	// 创建 service v1，inline env 会被 config/secret set 中同名 key 覆盖。
	serviceItem, err := db.Store.InsertService(ctx, "svc-demo-web", projectItem.ID, "demo-web", "Demo Web", workload.Spec{
		Region:        "cn-beijing",
		Replicas:      1,
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
	configSetV2 := cloudplaneapi.ProjectConfigSet{
		ID:   "cfg-config-v2",
		Name: "config-v2",
		Values: map[string]string{
			"SERVICE_MODE": "config-v2",
			"API_HOST":     "https://v2.internal.example",
		},
	}
	secretSetV2 := cloudplaneapi.ProjectSecretSet{
		ID:   "sec-secret-v2",
		Name: "secret-v2",
		Values: map[string]string{
			"LOG_LEVEL":   "secret-v2",
			"DB_PASSWORD": "db-pass-v2",
		},
	}
	if _, err := db.Store.ApplyProjectResources(ctx, projectItem.ID, projectResources(configSetV2, secretSetV2)); err != nil {
		t.Fatalf("ApplyProjectResources(v2) returned error: %v", err)
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
		ServiceID:       serviceItem.Metadata.ID,
		RevisionID:      revisionV1.ID,
		DesiredReplicas: 1,
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
		Replicas:        1,
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

// TestIntegrationFailedReplicaCanBeReclaimedWithoutLeakingNodeAllocation 验证失败副本释放资源且重复上报幂等。
func TestIntegrationFailedReplicaCanBeReclaimedWithoutLeakingNodeAllocation(t *testing.T) {
	// 使用真实测试数据库验证失败副本释放资源，重复失败上报不重复释放。
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)
	// 创建 owner/project/service/revision，service 期望两个副本。
	ownerUserID := "failed-replica-owner"

	projectItem, err := db.Store.CreateProject(ctx, project.CreateProjectInput{
		Name:        "failed-replica-retry",
		DisplayName: "Failed Replica Retry",
		OwnerUserID: ownerUserID,
	})
	if err != nil {
		t.Fatalf("CreateProject returned error: %v", err)
	}

	serviceItem, err := db.Store.InsertService(ctx, "svc-retry-web", projectItem.ID, "retry-web", "Retry Web", workload.Spec{
		Region:        "cn-beijing",
		Replicas:      2,
		InstanceClass: workload.InstanceClassSmall,
		Image:         "registry.example.com/demo:v1",
		DefaultPort:   8080,
		ReadinessPath: "/healthz",
	})
	if err != nil {
		t.Fatalf("CreateService returned error: %v", err)
	}

	revisionItem, err := db.Store.InsertRevisionFromService(ctx, serviceItem.Metadata.ID)
	if err != nil {
		t.Fatalf("CreateRevisionFromService returned error: %v", err)
	}

	// 注册一个容量足够承载两个 small 副本的 runtime node。
	runtimeNodeNode, err := db.Store.RegisterNode(ctx, node.RegisterInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		Name:          "runtime-node-retry",
		Role:          node.RoleRuntime,
		PrivateIP:     "10.0.0.20",
		PublicIP:      "203.0.113.20",
		InstanceID:    "i-runtime-node-retry",
		InstanceType:  "ecs.u1-c1m1.large",
		CPUMilliTotal: 4000,
		MemoryMiTotal: 8192,
	})
	if err != nil {
		t.Fatalf("RegisterNode returned error: %v", err)
	}
	markIntegrationNodeReady(t, ctx, db, runtimeNodeNode.ID)

	// 创建 deployment 并把 service 标记为 deploying，模拟发布流程已启动。
	deploymentItem, err := db.Store.InsertDeployment(ctx, deployment.CreateInput{
		ServiceID:       serviceItem.Metadata.ID,
		RevisionID:      revisionItem.ID,
		DesiredReplicas: 2,
	}, "test deployment for failed replica retry")
	if err != nil {
		t.Fatalf("CreateDeployment returned error: %v", err)
	}
	if _, err := db.Store.UpdateServiceRevisionState(ctx, serviceItem.Metadata.ID, revisionItem.ID, "", deployment.StatusDeploying, workload.RolloutPhaseIdle, ""); err != nil {
		t.Fatalf("UpdateServiceRevisionState returned error: %v", err)
	}
	if _, err := db.Store.UpdateDeploymentStatus(ctx, deploymentItem.ID, deployment.StatusScheduling, "scheduler picked up the deployment"); err != nil {
		t.Fatalf("UpdateDeploymentStatus(scheduling) returned error: %v", err)
	}

	// 为两个副本都创建 selection decision，目标是同一个 runtime node。
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
		Replicas:        2,
	}, []scheduler.PlacementDecision{
		{
			DeploymentID: deploymentItem.ID,
			ReplicaIndex: 0,
			NodeID:       runtimeNodeNode.ID,
			Region:       "cn-beijing",
			Score:        100,
			Reason:       "replica 0 on retry runtime node",
		},
		{
			DeploymentID: deploymentItem.ID,
			ReplicaIndex: 1,
			NodeID:       runtimeNodeNode.ID,
			Region:       "cn-beijing",
			Score:        100,
			Reason:       "replica 1 on retry runtime node",
		},
	}); err != nil {
		t.Fatalf("CreatePlacementDecisions returned error: %v", err)
	}
	if _, err := db.Store.UpdateDeploymentStatus(ctx, deploymentItem.ID, deployment.StatusAssigned, "assigned to retry runtime node"); err != nil {
		t.Fatalf("UpdateDeploymentStatus(assigned) returned error: %v", err)
	}

	// 领取第一个副本并上报 running，这会保留该副本的资源占用。
	first, err := db.Store.CreateExecutionClaim(ctx, runtimeNodeNode.ID)
	if err != nil {
		t.Fatalf("CreateExecutionClaim(first) returned error: %v", err)
	}
	if first == nil {
		t.Fatal("CreateExecutionClaim(first) returned nil work item")
	}
	if first.ReplicaIndex != 0 {
		t.Fatalf("first replicaIndex = %d, want 0", first.ReplicaIndex)
	}
	if _, _, _, err := db.Store.UpdateExecutionFromNodeReport(ctx, runtimeNodeNode.ID, first.ExecutionID, execution.ReportInput{
		Status:        deployment.StatusRunning,
		Reason:        "replica 0 is healthy",
		ContainerID:   "ctr-retry-0",
		ContainerName: first.ContainerName,
		HostPort:      18081,
	}); err != nil {
		t.Fatalf("UpdateExecutionFromNodeReport(first running) returned error: %v", err)
	}

	// 领取第二个副本并上报 failed，store 应释放该副本预占的资源。
	second, err := db.Store.CreateExecutionClaim(ctx, runtimeNodeNode.ID)
	if err != nil {
		t.Fatalf("CreateExecutionClaim(second) returned error: %v", err)
	}
	if second == nil {
		t.Fatal("CreateExecutionClaim(second) returned nil work item")
	}
	if second.ReplicaIndex != 1 {
		t.Fatalf("second replicaIndex = %d, want 1", second.ReplicaIndex)
	}
	if _, _, _, err := db.Store.UpdateExecutionFromNodeReport(ctx, runtimeNodeNode.ID, second.ExecutionID, execution.ReportInput{
		Status:        deployment.StatusFailed,
		Reason:        "replica 1 crashed during bootstrap",
		ContainerID:   "ctr-retry-1",
		ContainerName: second.ContainerName,
	}); err != nil {
		t.Fatalf("UpdateExecutionFromNodeReport(second failed) returned error: %v", err)
	}

	// 失败副本释放后，node allocated 应只剩第一个 running 副本的资源请求。
	refreshedNode, err := db.Store.GetNode(ctx, runtimeNodeNode.ID)
	if err != nil {
		t.Fatalf("GetNode returned error: %v", err)
	}
	if refreshedNode.CPUMilliAllocated != cpuMilliRequest {
		t.Fatalf("cpu_milli_allocated = %d, want %d after failed replica revision", refreshedNode.CPUMilliAllocated, cpuMilliRequest)
	}
	if refreshedNode.MemoryMiAllocated != memoryMiRequest {
		t.Fatalf("memory_mi_allocated = %d, want %d after failed replica revision", refreshedNode.MemoryMiAllocated, memoryMiRequest)
	}

	// 对同一个 failed execution 重复上报，应保持幂等，不再次减少 allocated。
	if _, _, _, err := db.Store.UpdateExecutionFromNodeReport(ctx, runtimeNodeNode.ID, second.ExecutionID, execution.ReportInput{
		Status:        deployment.StatusFailed,
		Reason:        "duplicate failed report should be idempotent",
		ContainerID:   "ctr-retry-1",
		ContainerName: second.ContainerName,
	}); err != nil {
		t.Fatalf("UpdateExecutionFromNodeReport(second failed duplicate) returned error: %v", err)
	}

	// 重复失败上报后 allocated 仍应等于一个副本资源。
	refreshedNode, err = db.Store.GetNode(ctx, runtimeNodeNode.ID)
	if err != nil {
		t.Fatalf("GetNode(after duplicate failed report) returned error: %v", err)
	}
	if refreshedNode.CPUMilliAllocated != cpuMilliRequest {
		t.Fatalf("cpu_milli_allocated after duplicate failed report = %d, want %d", refreshedNode.CPUMilliAllocated, cpuMilliRequest)
	}
	if refreshedNode.MemoryMiAllocated != memoryMiRequest {
		t.Fatalf("memory_mi_allocated after duplicate failed report = %d, want %d", refreshedNode.MemoryMiAllocated, memoryMiRequest)
	}

	// 失败副本释放后，CreateExecutionClaim 应能再次为 replica 1 生成重试 work。
	retryWork, err := db.Store.CreateExecutionClaim(ctx, runtimeNodeNode.ID)
	if err != nil {
		t.Fatalf("CreateExecutionClaim(retry) returned error: %v", err)
	}
	if retryWork == nil {
		t.Fatal("CreateExecutionClaim(retry) returned nil work item")
	}
	if retryWork.ReplicaIndex != 1 {
		t.Fatalf("retry replicaIndex = %d, want 1", retryWork.ReplicaIndex)
	}
}

// TestIntegrationCreateExecutionClaimMaterializesRevisionScopedProjectedFiles 验证 projected files 按 revision 快照渲染。
func TestIntegrationCreateExecutionClaimMaterializesRevisionScopedProjectedFiles(t *testing.T) {
	// 使用真实测试数据库验证 projected files 按 revision 快照物化，而不是读取 service 当前 spec。
	ctx := context.Background()
	db := testutil.OpenCloudPlaneTestDatabase(t)
	// 创建 owner/project，作为 config/secret 和 service 归属。
	ownerUserID := "projected-files-owner"

	projectItem, err := db.Store.CreateProject(ctx, project.CreateProjectInput{
		Name:        "projected-files",
		DisplayName: "Projected Files",
		OwnerUserID: ownerUserID,
	})
	if err != nil {
		t.Fatalf("CreateProject returned error: %v", err)
	}

	// 创建 v1 config/secret，projected file 会分别读取非敏感配置和敏感 token。
	configSetV1 := cloudplaneapi.ProjectConfigSet{
		ID:   "cfg-cliproxy-v1",
		Name: "cliproxy-config-v1",
		Values: map[string]string{
			"config.yaml": "listen: :8317\nupstream: https://v1.internal.example\n",
		},
	}
	secretSetV1 := cloudplaneapi.ProjectSecretSet{
		ID:   "sec-cliproxy-v1",
		Name: "cliproxy-secret-v1",
		Values: map[string]string{
			"token": "token-v1",
		},
	}
	if _, err := db.Store.ApplyProjectResources(ctx, projectItem.ID, projectResources(configSetV1, secretSetV1)); err != nil {
		t.Fatalf("ApplyProjectResources(v1) returned error: %v", err)
	}

	// 创建带 projected files 的 service v1。
	serviceItem, err := db.Store.InsertService(ctx, "svc-cliproxyapi", projectItem.ID, "cliproxyapi", "CLI Proxy API", workload.Spec{
		Region:        "cn-beijing",
		Replicas:      1,
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
	configSetV2 := cloudplaneapi.ProjectConfigSet{
		ID:   "cfg-cliproxy-v2",
		Name: "cliproxy-config-v2",
		Values: map[string]string{
			"config.yaml": "listen: :8317\nupstream: https://v2.internal.example\n",
		},
	}
	secretSetV2 := cloudplaneapi.ProjectSecretSet{
		ID:   "sec-cliproxy-v2",
		Name: "cliproxy-secret-v2",
		Values: map[string]string{
			"token": "token-v2",
		},
	}
	if _, err := db.Store.ApplyProjectResources(ctx, projectItem.ID, projectResources(configSetV2, secretSetV2)); err != nil {
		t.Fatalf("ApplyProjectResources(v2) returned error: %v", err)
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
		ServiceID:       serviceItem.Metadata.ID,
		RevisionID:      revisionV1.ID,
		DesiredReplicas: 1,
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
		Replicas:        1,
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
	// 创建 owner/project 和带 persistent dir 的 service。
	ownerUserID := "persistent-dirs-owner"

	projectItem, err := db.Store.CreateProject(ctx, project.CreateProjectInput{
		Name:        "persistent-dirs",
		DisplayName: "Persistent Dirs",
		OwnerUserID: ownerUserID,
	})
	if err != nil {
		t.Fatalf("CreateProject returned error: %v", err)
	}

	serviceItem, err := db.Store.InsertService(ctx, "svc-cliproxyapi", projectItem.ID, "cliproxyapi", "CLI Proxy API", workload.Spec{
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
		ServiceID:       serviceItem.Metadata.ID,
		RevisionID:      revisionV1.ID,
		DesiredReplicas: 1,
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
		Replicas:        1,
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

// projectResources 把测试使用的 project resource 组装成 store apply 所需的 project 聚合。
func projectResources(configSet cloudplaneapi.ProjectConfigSet, secretSet cloudplaneapi.ProjectSecretSet) cloudplaneapi.Project {
	return cloudplaneapi.Project{
		ConfigSets: []cloudplaneapi.ProjectConfigSet{configSet},
		SecretSets: []cloudplaneapi.ProjectSecretSet{secretSet},
	}
}
