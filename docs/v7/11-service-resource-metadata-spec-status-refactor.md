# v7/11 Service Resource Metadata/Spec/Status Refactor

## 背景

v7 已经把 cloud-plane 的 service apply 链路从多套 `CreateInput` / `UpdateInput` 收敛为 `workload.Spec`，但资源模型仍然不干净：

- Go 领域对象 `Service` 仍把身份、展示元数据、运行规格和运行状态平铺在同一个结构里；
- cloud-plane 数据库 `services` / `service_desired` 表也把 spec 与 status 字段混在一起；
- cloud-plane southbound proto 返回扁平 `Service`；
- control-plane 消费 cloud-plane service 时继续依赖扁平字段；
- control-plane 自己的 service domain / store / operator API 也保留了同样的扁平资源模型。

这会让后续实现继续复制字段、制造薄转换函数，并把 metadata/spec/status 的边界模糊到所有调用方。

v7 不保留旧设计兼容，直接做 breaking change。

## 目标

全链路把 service 资源模型统一为：

```text
Service
├── Metadata：资源身份与用户可读元数据
├── Spec：用户声明的期望运行规格
└── Status：系统观测或控制得到的当前状态
```

具体目标：

1. `Service` 领域对象不再平铺 spec/status 字段。
2. `Spec` 不包含 `id`、`projectID`、`name` 等身份字段。
3. `Status` 不混入用户期望字段。
4. cloud-plane DB 字段使用 `spec_` / `status_` 前缀表达边界。
5. cloud-plane southbound proto / contract 返回嵌套 `metadata/spec/status`。
6. control-plane 消费 cloud-plane 的 client/controller 同步改为嵌套模型。
7. control-plane 自身 service domain/store/operator API 也同步切到 `metadata/spec/status`，避免旧 flat 模型继续污染实现。

## 非目标

- 不做旧 proto 字段兼容。
- 不做旧数据库 migration 兼容。v7 仍是开发期 baseline，重建数据库。
- 不拆 `services` 为 `service_specs` / `service_statuses` 三张表。SQL 表保持单表，字段用前缀表达语义边界。

## 资源模型

### cloud-plane service

```go
type Service struct {
    Metadata ServiceMetadata
    Spec     Spec
    Status   ServiceStatus
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

`ServiceMetadata`：

- `ID`
- `ProjectID`
- `Name`
- `DisplayName`

`Spec`：

- `Region`
- `Replicas`
- `InstanceClass`
- `Exposure`
- `Image`
- `Command`
- `Args`
- `DefaultPort`
- `ReadinessPath`
- `Env`
- `ConfigSetID`
- `SecretSetID`
- `RegistryCredentialID`
- `ProjectedFiles`
- `PersistentDirs`

`ServiceStatus`：

- `Phase`
- `CurrentRevisionID`
- `CandidateRevisionID`
- `RolloutPhase`
- `RolloutMessage`

### control-plane service

control-plane service 也使用同样边界，但 spec 包含全局调度字段：

`ServiceMetadata`：

- `ID`
- `ProjectID`
- `Name`
- `DisplayName`
- `Generation`

`ServiceSpec`：

- `Provider`
- `Region`
- `PinnedPlaneID`
- `Replicas`
- `InstanceClass`
- `Exposure`
- `Image`
- `Command`
- `Args`
- `DefaultPort`
- `ReadinessPath`
- `Env`
- `ConfigSetID`
- `SecretSetID`
- `RegistryCredentialID`
- `RevisionPolicy`
- `ProjectedFiles`
- `PersistentDirs`

`ServiceStatus`：

- `DesiredState`
- `ObservedGeneration`
- `Phase`
- `Healthy`
- `Message`
- `LastReconciledAt`
- `CurrentRevision`
- `Rollout`
- `Placement`

## 数据库设计

### cloud-plane `services`

保留单表，字段改为：

- metadata：`id`、`project_id`、`name`、`display_name`
- spec：`spec_region`、`spec_replicas`、`spec_instance_class`、`spec_exposure`、`spec_image`、`spec_command_json`、`spec_args_json`、`spec_default_port`、`spec_readiness_path`、`spec_env_json`、`spec_config_set_id`、`spec_secret_set_id`、`spec_registry_credential_id`、`spec_projected_files_json`、`spec_persistent_dirs_json`
- status：`status_phase`、`status_current_revision_id`、`status_candidate_revision_id`、`status_rollout_phase`、`status_rollout_message`

### cloud-plane `service_desired`

`service_desired` 是 control-plane 下发的期望状态缓存，字段改为：

- identity：`service_id`、`project_id`、`name`、`display_name`
- spec：同 `spec_` 前缀
- reconcile：`generation`、`observed_generation`、`spec_hash`、`reconcile_phase`、`reconcile_message`

### control-plane `services`

保留单表，字段改为：

- metadata：`id`、`project_id`、`name`、`display_name`、`generation`
- spec：`spec_provider`、`spec_region`、`spec_pinned_plane_id`、`spec_replicas`、`spec_instance_class`、`spec_exposure`、`spec_image`、`spec_command_json`、`spec_args_json`、`spec_default_port`、`spec_readiness_path`、`spec_env_json`、`spec_config_set_id`、`spec_secret_set_id`、`spec_registry_credential_id`、`spec_revision_policy_json`、`spec_projected_files_json`、`spec_persistent_dirs_json`
- status：`status_desired_state`、`status_observed_generation`、`status_phase`、`status_healthy`、`status_message`、`status_last_reconciled_at`、`status_current_revision_json`、`status_rollout_json`

## Proto / contract

### cloud-plane southbound

`cloudplane.v1.Service` 改为：

```proto
message Service {
  ServiceMetadata metadata = 1;
  ServiceSpec spec = 2;
  ServiceStatus status = 3;
}
```

`GetServiceResponse` 保持：

```proto
message GetServiceResponse {
  Service service = 1;
  ObservedServiceStatus status = 2;
}
```

这里 `Service.status` 是 cloud-plane 本地 lifecycle 状态，`GetServiceResponse.status` 是 runtime observed status。

### control-plane northbound

`controlplane.v1.Service` 也改为：

```proto
message Service {
  ServiceMetadata metadata = 1;
  ServiceSpec spec = 2;
  ServiceStatus status = 3;
  google.protobuf.Timestamp created_at = 4;
  google.protobuf.Timestamp updated_at = 5;
}
```

`CreateServiceRequest` 继续使用 `metadata` 输入字段和 `ServiceSpecInput`，但响应不再 flat。

## 实现原则

1. 不再新增 `CreateInput` / `UpdateInput` 复制整套 spec 字段。
2. 创建和更新路径接收身份字段 + `Spec`。
3. store scan 负责从列式 DB 组装 `Service{Metadata,Spec,Status}`。
4. API conversion 只做边界转换，不承载业务默认值。
5. 控制器逻辑访问字段必须通过 `service.Metadata`、`service.Spec`、`service.Status`。
6. 测试和 fake 也必须使用新模型，不保留旧 flat helper。

## 验证

完成后必须通过：

```bash
go test ./cmd/... ./internal/...
staticcheck ./cmd/... ./internal/...
go vet ./cmd/... ./internal/...
```
