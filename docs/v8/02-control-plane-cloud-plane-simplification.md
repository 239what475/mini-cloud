# Control Plane and Cloud Plane Simplification

本文记录 v8 对 control-plane / cloud-plane 边界的精简方向。

目标不是取消多 cloud-plane。目标是保留“一个 control-plane 管多个云平台 plane”的产品模型，同时避免 control-plane 和 cloud-plane 各自维护一套 service lifecycle 状态机。

## 迁移前判断

迁移前实现已经超过简单 CaaS 平台需要的复杂度。

核心问题是：

```text
control-plane 有 service / placement / remote observed 状态机
cloud-plane 也有 service desired / revision / deployment / execution 状态机
```

这导致一次 service apply 经过两层 service truth：

```text
Web / HTTP API
-> control-plane service controller
-> plane selector
-> deploy service
-> cloud-plane ApplyService
-> cloud-plane service desired store
-> cloud-plane service lifecycle reconciler
-> cloud-plane deployment / execution
-> node-agent
```

对简单 CaaS 来说，这条链路太长，也让失败恢复、状态解释和后续功能演进变难。

## 保留的产品模型

继续支持多个云平台和多个 plane：

```text
control-plane
  管全局 service、resource、调度、状态

aliyun cloud-plane
  管阿里云上的 runtime nodes 和 node-agents

tencent cloud-plane
  管腾讯云上的 runtime nodes 和 node-agents
```

cloud-plane 不是无状态代理。它应该保存 plane-local runtime state。

但 cloud-plane 不应该保存全局 service truth。

## 目标边界

### Control Plane

control-plane 是全局事实来源，负责：

- config set / secret set / registry credential
- service spec
- service generation
- service run
- explicit service-to-plane assignment
- global service status
- operation history and audit

service lifecycle 只应该在 control-plane 存在。

### Cloud Plane

cloud-plane 是某个云平台/地域的执行控制器，负责：

- cloud-plane registration and southbound auth
- node-agent registration
- node heartbeat
- node capacity and runtime inventory
- execution claim / dispatch
- execution result report
- provider runtime node scale out / scale in
- plane-local ingress route publishing

cloud-plane 可以有状态，但状态必须是 plane-local state。

### Node Agent

node-agent 继续保持窄职责：

- register
- heartbeat
- poll one work item
- run container
- report execution result
- cleanup local runtime resources

node-agent 不做本地 checkpoint，不做 server-push stream control protocol。

## 不再保留的方向

### 不做双 service 状态机

cloud-plane 不再维护：

- service desired state
- local service lifecycle
- revision history
- deployment rollout policy
- service rollback state

这些都归 control-plane。

### 不做平台级 rollback

rollback 暂不作为核心能力。

原因是 rollback 不是简单地把 image 改回旧版本。它需要定义：

- 回滚到哪个历史 spec / generation
- config / secret / registry credential 是否仍然有效
- 当前 candidate 部署到一半时如何处理
- 多 plane 部分成功时 service 状态如何表达
- 持久目录如何处理

v8 采用更简单的模型：

```text
每次更新 service spec 都推进 service generation 并创建新的 run
失败后显示 failed
用户要恢复旧版本，就重新 apply 旧 spec
```

后续如果需要“从旧 spec 创建新变更”的体验，可以在 Web 或 CLI 做辅助操作，但底层仍然是一次普通 apply，不引入特殊 rollback 状态机。

### 不把全量 resources 整包同步到 cloud-plane

当前 cloud-plane southbound API 会接收 config set、secret set、registry credential 等全量 resources。

v8 应改为：

```text
control-plane 在生成 execution work 时解析引用
cloud-plane / node-agent 只接收本次 execution 需要的材料
```

这样 cloud-plane 不需要保存 resource truth。

## 新的主流程

### Service Apply

目标流程：

```text
1. 用户创建或更新 service
2. control-plane 写入 service generation / run
3. control-plane 按 service.spec.planeID 确认目标 cloud-plane 可用
4. control-plane 创建属于该 plane 的 execution intents
5. cloud-plane 拉取或接收属于自己的 execution intents
6. cloud-plane 分配给 node-agent
7. node-agent 执行容器并上报结果
8. cloud-plane 记录 plane-local execution fact
9. cloud-plane 把 execution result 同步回 control-plane
10. control-plane 汇总 run / service status
```

重点是：

```text
service generation / run truth 在 control-plane
node / execution runtime fact 在 cloud-plane
container runtime fact 在 node-agent
```

### Status Read

Web 和后续 CLI 只读 control-plane。

control-plane 的 service status 来自：

- control-plane 自己的 desired state
- placement
- cloud-plane execution reports
- plane runtime inventory

不再通过 `GetService` 询问 cloud-plane 的 local service view。

### Ingress

cloud-plane 可以继续发布本 plane 的 Caddy route。

但 route 输入应来自 plane-local running executions，而不是 cloud-plane local service rollout state。

public service 只暴露一个平台托管三级域名：

```text
<service-name>.<ingress.baseDomain>
```

service 不携带自定义域名、多域名或 path route 配置。不同 service 可以使用相同 container port；
node-agent 自动分配 hostPort，cloud-plane 只把托管 host 反代到 ready node 的 privateIP:hostPort。

control-plane 负责汇总全局 front door / CDN 需要的 service-level view。

## 已废弃或替换的模块

### Cloud Plane

保留并收缩后的核心模块：

- `internal/cloudplane/api/nodeagent`
- `internal/cloudplane/domain/node`
- `internal/cloudplane/domain/execution`
- `internal/cloudplane/infra/store/node.go`
- `internal/cloudplane/infra/store/execution_intent.go`
- provider runtime pool driver
- ingress route sink, but its input should come from execution facts

旧 `deployment` / `revision` / `desired` / `scheduler` / `usage` / `workload` 领域包以及旧 cloud-plane service/resource store 已删除。

### Control Plane

已改造为通过 execution plan 驱动 cloud-plane：

- `internal/controlplane/domain`
- `internal/controlplane/serviceops`
- `internal/controlplane/planeclient`
- service status aggregation

control-plane 不再调用 cloud-plane `ApplyService`、`GetService`、`RollbackService`。

control-plane 会物化本次 execution 需要的 config / secret / registry credential，并通过 execution plan 下发给 cloud-plane。

### Proto

旧 `ControlPlaneWorkloadService` 的 service-oriented API 已删除：

```text
ApplyService
DeleteService
GetService
RollbackService
```

当前 cloud-plane southbound API 只保留：

```text
ControlPlaneSnapshotService.GetSnapshot
ControlPlaneExecutionService.ApplyExecutionPlan
ControlPlaneExecutionService.DeleteExecutionPlan
```

node-agent 侧仍使用 unary pull/report：`PollWork` 和 `ReportExecution`。

## 执行方式：按完整链路纵切

这次精简不按模块横向推进。

避免这样的顺序：

```text
先改全部 proto
再改全部 store
再改全部 controller
再改全部 API
```

这种方式会让系统长期处于半迁移状态，也容易再次形成两套并行状态机。

v8 的重构必须按一条完整业务链路纵切。每次修改都要覆盖：

```text
用户动作
-> control-plane 状态写入和调度
-> cloud-plane 接收或拉取执行意图
-> node-agent 执行
-> 执行结果回流
-> control-plane 聚合状态
-> Web 或测试能看到结果
```

每个 slice 的验收标准：

- 能独立编译和测试。
- 不留下只写一半的新状态。
- 不新增第二套 service lifecycle。
- 不依赖平台级 rollback。
- 能说明旧路径是否仍保留、被旁路还是被删除。

建议的 slice 顺序：

### Slice 1：service create / apply 最小链路

```text
Web/API create service
-> control-plane 创建 service generation / run / execution plan
-> cloud-plane 拉取 execution
-> node-agent 执行
-> execution result 回 control-plane
-> Web 看到 running / failed
```

这是最重要的切换点。完成后，新的 execution-oriented southbound API 有第一条可验证链路。

### Slice 2：service update 链路

```text
更新 image/spec
-> control-plane 推进 generation 并创建新 run
-> cloud-plane 执行新 execution
-> control-plane 聚合新状态
```

更新失败只进入 failed 状态。恢复旧版本通过重新 apply 旧 spec 表达，不引入 rollback。

### Slice 3：service delete 链路

```text
control-plane 标记 deleted
-> 生成 stop/delete execution
-> cloud-plane / node-agent 清理容器
-> control-plane 标记完成
```

delete 必须先做成明确的端到端链路，避免遗留容器和 control-plane 状态不一致。

### Slice 4：node offline 链路

```text
cloud-plane 检测 node heartbeat 过期
-> 上报 control-plane
-> control-plane 标记 affected executions / runs
-> Web 展示 degraded / failed
```

node health 是 plane-local 事实，但 service/run 状态仍由 control-plane 聚合。

### Slice 5：移除 replica 模型

```text
service spec 只描述一个 workload/container
-> execution plan 只下发一个运行实例
-> cloud-plane / node-agent 只传递 execution 身份
-> control-plane 按单个 execution 状态聚合 service run
```

mini-cloud v8 是 CaaS demo，不提供多副本 scaling 语义。一个 service 表示一个运行中的 workload/container。

运行态只表达单个 run / execution 的状态，例如 `pending`、`deploying`、`running`、`failed`、`superseded`。

## 当前落地状态

截至 v8 slice 5 后，迁移不再保留 cloud-plane 旧 service lifecycle 兼容层。

### 阶段 1：固化边界

- 已新增文档和测试目标。
- 已停止新增 cloud-plane service lifecycle 功能。
- 已删除 cloud-plane rollback 入口。

### 阶段 2：新增 execution-oriented southbound API

- control-plane 创建 execution plan。
- cloud-plane 接收 execution plan 并持久化 plane-local execution intent。
- cloud-plane 分发给 node-agent。
- cloud-plane 接收 node-agent execution result。

旧 southbound `ApplyService` / `GetService` / `ApplyResources` API 已删除。旧 `service.proto` / `resources.proto` 以及对应生成代码也已删除，cloud-plane southbound 契约只保留 snapshot 和 execution plan。

### 阶段 3：control-plane status 改为 execution 聚合

- service status 不再依赖 cloud-plane `GetService`。
- service run ready / running / failed 来自 plane execution reports。
- Web 只读 control-plane 聚合状态。

### 阶段 4：切掉 cloud-plane service lifecycle

- cloud-plane 不再写入 service desired / revision / deployment 表。
- cloud-plane baseline schema 只保留 node、execution intent、runtime node、ingress route、provider-local state。
- 旧 cloud-plane service lifecycle Go 包和 store 文件已删除。

### 阶段 5：清理 schema

- cloud-plane 初始 schema 已移除旧 `services`、`revisions`、`deployments`、`service_desired`、`placement_decisions`、`deployment_executions`、`config_sets`、`secret_sets`、`registry_credentials` 等旧表。
- runtime node scale-in / node offline / ingress 均基于 execution intent 和 node/runtime-node state。

## 验收标准

精简完成后，应满足：

- control-plane 是 service generation / run 的唯一事实来源。
- cloud-plane 不再有 service desired reconciler。
- cloud-plane 不再有 rollback API。
- cloud-plane 不再保存完整 resources。
- node-agent API 仍保持 unary pull/report 模型。
- Web 可以只读 control-plane 得到完整 service 状态。
- 多 cloud-plane 调度仍可工作。
- service spec、execution plan、cloud-plane 和 node-agent API 不暴露 replicas。
- 阿里云和腾讯云 plane 的差异限制在 provider driver、runtime node inventory 和 plane-local execution dispatch。
