# Cloud Plane Store 边界重构

## 背景

v7 前期重构已经把 cloud-plane 收敛成内部 gRPC 进程，并引入本地 desired state、后台 reconciler、外置 ingress/egress 和 runtime node 自动缩放。
这些变化让 `internal/cloudplane/infra/store` 的问题变得明显：它不再只是数据库访问层，而是混合了承载业务决策的应用层逻辑。

当前 `Store` 同时承担：

- SQL repository；
- control-plane desired accept 等上层业务语义；
- identity/token 签发；
- service admission；
- revision/deployment/rollout 状态机；
- node-agent work item 构造；
- execution report 编排；
- node health reconciler；
- runtime node scale-in 策略；
- 查询默认值和测试便利逻辑。

这会带来三个直接问题：

1. 调用方看见 `store.Store` 时无法判断方法只是读写数据库，还是会推进业务状态机。
2. 业务规则藏在 SQL 事务脚本里，难以单独测试和复用。
3. store 被迫知道 control-plane、node-agent、runtime pool、rollout、identity 等上层语义，包边界反向污染。

本次重构直接 breaking change，不保留旧 store API。

## 目标

- `infra/store` 只保留 repository 和必要的事务 primitive。
- control-plane 协议适配移出 store。
- token/secret 生成、hash、prefix 规则移出 store。
- service create/update admission 移出 store。
- node-agent claim/report work usecase 移出 store。
- node health、runtime node scale-in 候选策略移出 store。
- store 中不再暴露测试 seed helper 或 synthetic 业务 helper。
- 旧的 usecase 型 store 方法直接删除或降级为内部 primitive，不保留兼容 wrapper。

## 非目标

- 不改数据库表结构，除非清理边界必须修改。
- 不重写业务状态机语义。
- 不引入新兼容层。
- 不为旧 store 方法保留 deprecated wrapper。
- 不把所有 SQL 拆成微小 DAO；需要原子性的事务 primitive 可以留在 store，但必须命名清楚。

## 新边界

### `infra/store`

只负责：

- SQL 读写；
- scan / insert / update / delete；
- 数据库唯一约束、外键约束到领域错误的转换；
- 必要的事务原子更新；
- 必要的领域输入校验；
- 必要的事务内最终约束校验，例如避免并发写入绕过 quota 上限；
- 只读 read model 查询。

不负责：

- control-plane owner 映射；
- token 明文生成；
- secret hash 策略；
- API 查询默认 limit；
- service admission 策略；
- quota 策略决策；
- rollout 自动转正、失败处理等发布决策；
- node-agent work item 组装；
- runtime node scale-in 候选策略；
- stale heartbeat 影响 workload 的 reconciler 逻辑；
- 测试 seed helper。

### `control/identity`

负责：

- 平台 service account 签发；
- project API token 签发；
- node-agent session token 签发；
- bearer secret 解析；
- control-plane owner 到本地 human user 的映射。

store 只保存 hash 和元数据。

### `control/project` binding

负责：

- control-plane project 到 cloud-plane 本地 project 的幂等绑定；
- owner user 映射后的 project create/update/owner transfer 编排。

store 只负责 project 行的创建、更新、查询和 owner 转移。

### `control/project` resources

负责：

- config set 幂等 ensure；
- secret set 幂等 ensure；
- registry credential 幂等 ensure；
- 并发唯一键冲突后的重读和 noop 判定。

store 只负责 project resource 行的创建和列表查询。

### `control/lifecycle`

保留并强化职责：

- service create/update admission；
- quota preview 和 admission 编排；
- config/secret/registry/projected file 引用检查；
- service spec diff；
- 是否需要新 revision 的判断；
- revision 创建；
- deployment 调度；
- candidate revision 发布、自动转正和失败处理；
- scale-out 和 service replica 扩容。

store 只提供原子状态更新和查询；涉及并发一致性的 quota 最终检查可以留在 store primitive 内，但不再作为 API/usecase 入口暴露。

### `control/serviceintent`

负责：

- 接受 control-plane 下发的 service desired state；
- 接受 rollback intent，并从目标 revision 生成新的 desired state；
- 校验 service desired 引用的 project/config/secret/registry credential；
- 将通过校验的 desired 写入本地事务边界方法。

API 只负责 proto 转换、鉴权和错误码映射。

### `control/serviceview`

负责：

- 构造 accepted service 读模型；
- 构造 observed service status；
- 补齐 stable/candidate rollout 副本计数；
- 在 desired 已绑定本地 service 时尝试返回真实运行态，否则返回 accepted-but-not-observed 降级视图。

API 不直接读取 service、deployment 或 revision。

### `control/snapshot`

负责：

- 数据库健康探测；
- overview / reliability / runtime inventory / runtime capacity 聚合；
- cloud-plane 配置摘要聚合；
- 构造 control-plane 同步使用的 snapshot contract。

API 只负责鉴权、错误码映射和 protobuf 转换。

### `control/nodeagent`

负责：

- runtime node-agent 注册；
- 注册后 session token 签发；
- node-agent heartbeat 写入；
- work claim；
- execution report。

claim/report 仍复用 store 的事务边界方法，原因是它们必须在同一事务内维护 execution、deployment、service 和 node allocation 一致性。

### `control/inventory`

负责：

- 从本地 node 记录构造 runtime inventory；
- 构造 runtime capacity 汇总；
- 统一 runtime node 过滤口径。

### `control/runtimepool`

负责：

- scale-in 候选策略；
- runtime node 删除状态机；
- provider delete 错误重试策略。

scale-out 的 runtime node create intent / bind provisioned 仍属于 service deployment 调度路径，由 `control/lifecycle` 编排；node-agent ready 同步发生在心跳写入事务内，不再为它单独拆薄 wrapper。

store 只提供 scale-in 所需的 runtime node 状态 CAS、node 状态原子切换和查询 primitive。

## 具体重构规则

### 1. control-plane synthetic owner

删除：

```go
store.EnsureSyntheticControlPlaneOwner
syntheticControlPlaneOwnerIssuer
```

新增：

```go
control/identity.ControlPlaneOwnerInput(ownerUserID string)
(*control/identity.Service).EnsureControlPlaneOwner(ctx, ownerUserID)
```

`api/controlplane` 不再调用 store 的 synthetic helper。

### 2. token 生成

删除 store 中：

```go
newPlatformServiceAccountSecret
newProjectTokenSecret
newNodeAgentSessionTokenSecret
hashProjectTokenSecret
hashAuthSecret
authSecretPrefix
tokenSecretPrefix
```

新增通用 token 包：

```text
internal/common/token
```

store 接收：

```go
SecretHash
SecretPrefix
ExpiresAt
```

不生成明文。

### 3. desired accept

删除旧 store 对外 API：

```go
ApplyService
```

新增 `control/serviceintent` 作为 desired intent 用例边界；API 只做 proto 转换、鉴权和错误码映射。

store 保留：

```go
GetServiceDesiredByProjectName
UpsertServiceDesired
ListServiceDesiredByPhases
UpdateServiceDesiredPhase
```

control-plane 不再下发 runtime node 放置计划；cloud-plane 本地 scheduler 负责生成 placement decision。

### 4. query limit

store 方法不再内置默认 limit。
调用方必须显式传入 limit。

涉及：

```go
ListServiceDesiredByPhases
ListPlatformOperationEvents
ListProjectOperationEvents
```

### 5. runtime node scale-in

删除 store 对外策略 API：

```go
ListRuntimeNodesForDeletion
HasUnsettledDeployments
```

新增 control/runtimepool 缩容策略，store 提供：

```go
ListRuntimeNodesByStatuses
HasDeploymentsWithStatuses
CountActiveExecutionsByNode
```

`MarkRuntimeNodeDraining/Deleting/Deleted` 保留为事务 primitive，因为它们必须原子更新 runtime node 和 backing node。

### 6. stale heartbeat

删除 store 对外 reconciler 命名 API：

```go
ReconcileStaleHeartbeats
```

迁移到 `control.Manager` 启动的 node-health loop；store 可保留一个明确命名的事务 primitive：

```go
UpdateStaleNodeHeartbeatState
```

该 primitive 的存在只为保证 node、deployment、service、execution 的同事务更新；对外语义留在 control manager 的 node-health loop 中，不单独抽薄 wrapper 包。

### 7. node-agent claim/report

node-agent gRPC handler 调用 `control/nodeagent`，由 control 层统一承载注册、心跳、claim 和 report 入口。`control/nodeagent` 内部调用明确的 store 事务边界方法：

```go
CreateExecutionClaim
UpdateExecutionFromNodeReport
```

不再保留只包含 claim/report 两个转发方法的薄 wrapper。claim/report 的复杂度在于事务内的 execution、deployment、service、node allocation 一致性；这部分应由 store 事务边界方法保证原子性，handler 只负责鉴权、协议转换和错误码映射。

rollout 相关入口迁移到 `control/lifecycle`。store 保留命名为事务更新的边界方法，例如 `PromoteServiceCandidateRevisionState`、`AbortServiceCandidateRevisionState`。

如果某个事务必须跨多张表保持原子性，优先保留为命名明确的 store 事务边界方法，避免为了“usecase 层”再抽只转发 store 的 control wrapper。

## 检查点

完成后必须满足：

- `grep -R "EnsureSyntheticControlPlaneOwner" internal/cloudplane/infra/store` 无结果。
- `infra/store` 不再 import `crypto/rand`。
- `infra/store` 不再生成 token 明文。
- `infra/store` 不再有 `ApplyService` 或显式放置计划写入方法，只保留 `UpsertServiceDesired` 等 desired repository primitive。
- `infra/store` 不再有 `ListRuntimeNodesForDeletion` / `HasUnsettledDeployments` 策略方法。
- `infra/store` 不再有 `ReconcileStaleHeartbeats` reconciler 方法。
- API handler 不再直接访问 store；API 构造器仍接收 store 用于装配 control 服务，control 层和 reconciler 只调用命名明确、承担事务一致性的 store primitive。
- 测试 seed helper 放在 `_test.go` 或 `internal/testutil`，不放 production store。

## 验证

- `go test ./internal/cloudplane/...`
- `go vet ./internal/cloudplane/...`
- `staticcheck ./internal/cloudplane/...`
- `grep` 检查旧 store API 残留。
