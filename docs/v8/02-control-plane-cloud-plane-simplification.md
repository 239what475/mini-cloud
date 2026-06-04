# Control Plane and Cloud Plane Simplification

本文记录 v8 对 control-plane / cloud-plane 边界的精简方向。

目标不是取消多 cloud-plane。目标是保留“一个 control-plane 管多个云平台 plane”的产品模型，同时避免 control-plane 和 cloud-plane 各自维护一套 service lifecycle 状态机。

## 当前判断

当前实现已经超过简单 CaaS 平台需要的复杂度。

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
  管全局项目、服务、调度、状态

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

- project
- config set / secret set / registry credential
- service spec
- revision
- deployment
- placement
- desired replicas
- global service status
- plane selection
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

- 回滚到哪个 revision
- config / secret / registry credential 是否仍然有效
- 当前 candidate 部署到一半时如何处理
- 多 plane 部分成功时 service 状态如何表达
- 持久目录如何处理

v8 采用更简单的模型：

```text
每次更新 service spec 都生成新的 revision / deployment
失败后显示 failed
用户要恢复旧版本，就重新 apply 旧 spec
```

后续如果需要“从旧 revision 创建新变更”的体验，可以在 Web 或 CLI 做辅助操作，但底层仍然是一次普通 apply，不引入特殊 rollback 状态机。

### 不把 project resources 整包同步到 cloud-plane

当前 cloud-plane southbound API 会接收 project、config set、secret set、registry credential 等全量 project resources。

v8 应改为：

```text
control-plane 在生成 execution work 时解析引用
cloud-plane / node-agent 只接收本次 execution 需要的材料
```

这样 cloud-plane 不需要保存 project resource truth。

## 新的主流程

### Service Apply

目标流程：

```text
1. 用户创建或更新 service
2. control-plane 写入 service / revision / deployment
3. control-plane 选择目标 cloud-plane
4. control-plane 创建属于该 plane 的 execution intents
5. cloud-plane 拉取或接收属于自己的 execution intents
6. cloud-plane 分配给 node-agent
7. node-agent 执行容器并上报结果
8. cloud-plane 记录 plane-local execution fact
9. cloud-plane 把 execution result 同步回 control-plane
10. control-plane 汇总 deployment / service status
```

重点是：

```text
service / revision / deployment truth 在 control-plane
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

control-plane 负责汇总全局 front door / CDN 需要的 service-level view。

## 需要废弃或替换的当前模块

### Cloud Plane

优先废弃：

- `internal/cloudplane/domain/desired`
- `internal/cloudplane/domain/workload`
- `internal/cloudplane/domain/revision`
- `internal/cloudplane/domain/deployment`
- `internal/cloudplane/control/lifecycle`
- cloud-plane service desired reconcile loop
- cloud-plane service rollback API

需要保留并收缩：

- `internal/cloudplane/api/nodeagent`
- `internal/cloudplane/domain/node`
- `internal/cloudplane/domain/execution`
- `internal/cloudplane/infra/store/node.go`
- `internal/cloudplane/infra/store/execution.go`
- provider runtime pool driver
- ingress route sink, but its input should come from execution facts

### Control Plane

需要改造：

- `internal/controlplane/servicecontroller`
- `internal/controlplane/deploy`
- `internal/controlplane/planeclient`
- `internal/controlplane/planeselector`
- service status aggregation

control-plane should no longer call cloud-plane `ApplyService`, `GetService`, `RollbackService`.

It should dispatch execution intent and consume execution result.

### Proto

需要替换 `ControlPlaneWorkloadService` 的 service-oriented API：

```text
ApplyService
DeleteService
GetService
RollbackService
```

目标 API 应围绕 plane-local execution 和 inventory，例如：

```text
PullPlaneExecutions
AckPlaneExecution
ReportPlaneExecution
GetPlaneSnapshot
DeletePlaneExecution
```

具体是 pull 还是 push 可以后续决定。为保持简单，优先考虑 pull 或 unary RPC，避免引入双向 stream。

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
-> control-plane 创建 service / revision / deployment / execution
-> cloud-plane 拉取 execution
-> node-agent 执行
-> execution result 回 control-plane
-> Web 看到 running / failed
```

这是最重要的切换点。完成后，新的 execution-oriented southbound API 有第一条可验证链路。

### Slice 2：service update 链路

```text
更新 image/spec
-> control-plane 创建新 revision / deployment
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
-> control-plane 标记 affected executions / deployments
-> Web 展示 degraded / failed
```

node health 是 plane-local 事实，但 service/deployment 状态仍由 control-plane 聚合。

### Slice 5：scale out / scale in 链路

```text
control-plane 期望 replicas 变化
-> 生成新增或删除 execution
-> cloud-plane 调度到 node-agent
-> control-plane 汇总 replica 状态
```

scale 链路完成后，再考虑 provider runtime node scale out / scale in 的保留和收缩。

## 迁移策略

不要一次性删除所有旧表和旧模块。

建议分阶段：

### 阶段 1：固化边界

- 新增文档和测试目标。
- 停止新增 cloud-plane service lifecycle 功能。
- 删除 rollback 入口。

### 阶段 2：新增 execution-oriented southbound API

- control-plane 创建 execution intents。
- cloud-plane 拉取属于自己的 execution intents。
- cloud-plane 分发给 node-agent。
- cloud-plane 上报 execution result。

旧 `ApplyService` 路径暂时保留但不继续扩展。

### 阶段 3：control-plane status 改为 execution 聚合

- service status 不再依赖 cloud-plane `GetService`。
- deployment ready / running / failed 来自 execution reports。
- Web 只读 control-plane 聚合状态。

### 阶段 4：切掉 cloud-plane service lifecycle

- 停止写入 cloud-plane service desired / revision / deployment 表。
- cloud-plane 只保留 node、execution、runtime node、ingress route、provider state。
- 删除或废弃对应 Go 包和 proto 方法。

### 阶段 5：清理 schema

- 先保留旧表，避免迁移风险。
- 等新链路稳定后，再新增 migration 删除废弃表或标记不再使用。

## 验收标准

精简完成后，应满足：

- control-plane 是 service / revision / deployment 的唯一事实来源。
- cloud-plane 不再有 service desired reconciler。
- cloud-plane 不再有 rollback API。
- cloud-plane 不再保存完整 project resources。
- node-agent API 仍保持 unary pull/report 模型。
- Web 可以只读 control-plane 得到完整 service 状态。
- 多 cloud-plane 调度仍可工作。
- 阿里云和腾讯云 plane 的差异限制在 provider driver、runtime node inventory 和 plane-local execution dispatch。
