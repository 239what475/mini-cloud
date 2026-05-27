# 07. Cloud Plane 本地 Reconciler 重构

## 背景

旧 cloud-plane 的 `ApplyService` 曾经混合 desired accept、显式放置计划和同步执行语义：control-plane 调用一次 RPC，cloud-plane 会在同一个请求里创建或更新 service、创建 revision、创建 deployment、调度 placement，容量不足时还会调用 provider 创建 runtime node 并等待 node-agent 注册。

这个模型的问题是职责混在一起：

- control-plane 下发的是 desired state，但 cloud-plane API 路径却直接执行 orchestration。
- cloud-plane 有本地 PostgreSQL 状态库，却没有以状态库为中心的后台控制循环。
- provider 扩容、node ready 等待和 rollout 推进被绑定到 gRPC 请求生命周期，进程重启或请求断开后不能可靠恢复。
- `services` 同时承载 desired spec、observed status、rollout state 和 plan metadata，状态语义不干净。

v7 直接切到 breaking change：不保留旧同步 orchestration 语义。

## 目标架构

```text
control-plane
  - 全局 desired state 作者
  - 全局用户、项目、权限、策略源

cloud-plane
  - plane-local accepted desired state store
  - plane-local reconciler
  - runtime node controller
  - deployment / execution state machine
  - node-agent southbound API

node-agent
  - 单节点 executor
  - heartbeat / poll / report
```

cloud-plane 只承诺“已接受 desired state”，不再承诺“RPC 返回时已经开始或完成部署”。部署推进由 cloud-plane 本地 reconciler 异步完成。

## 新语义

### control-plane -> cloud-plane

- `ApplyService`：接受 service desired state，持久化为 plane-local generation。
- control-plane 只选择目标 cloud-plane，不再向 cloud-plane 下发 `replica -> runtime node` 放置计划。
- 查询类 RPC 返回当前 observed 状态，observed 允许落后于 desired。

### cloud-plane reconciler

后台循环读取本地库，把 accepted desired state 推向 observed state：

1. service desired -> service identity / revision / deployment intent。
2. deployment pending -> placement assigned。
3. 容量不足 -> runtime node intent。
4. runtime node intent -> provider create / bootstrap waiting / ready。
5. node-agent poll/report -> execution observed。
6. execution observed -> deployment status。
7. deployment status -> service rollout / observed status。
8. heartbeat stale -> node offline -> affected execution/deployment/service degraded or failed。

### node-agent -> cloud-plane

node-agent 仍然使用 poll/report 模型：

- `RegisterNode`
- `RecordHeartbeat`
- `PollWork`
- `ReportExecution`

这个边界保留，因为它适合断连、重试和教学场景。

## 状态边界

### Desired

`service_desired` 是权威 desired state：

- `generation` 是 cloud-plane 本地单调版本。
- `spec_hash` 用于判断 revision-changing update。
- `reconcile_phase` 表示 desired 是否 accepted / reconciling / observed / blocked / rejected / deleting。
- `observed_generation` 表示实际状态已经追上的 generation。

### Observed

`services` 只作为 service identity 和 observed summary 使用：

- `current_revision_id`
- `candidate_revision_id`
- `status`
- `rollout_phase`
- `rollout_message`

当前实现仍保留 service spec 字段作为最新 accepted cache，避免查询路径反复反序列化 desired；但 reconciler 以 `service_desired` 为权威输入。

### Plane Selection vs Placement Decision

- plane selection：control-plane 根据 provider、region、plane ready 状态和粗略容量选择目标 cloud-plane。
- placement decision：cloud-plane 本地 scheduler 绑定到 deployment/runtime node、node-agent 可以领取的执行放置结果。

`ApplyService` 只写 desired state，不直接写 placement decision。

## Reconciler 列表

### ServiceDesiredReconciler

读取未观察到的 `service_desired`，确保 service identity、revision 和 deployment intent 存在。

### PlacementReconciler

读取 pending / scheduling deployment，始终使用 cloud-plane 本地 scheduler。容量不足时创建 runtime node intent；成功后写 placement decision 并将 deployment 推进到 assigned。

### RuntimeNodeReconciler

读取 runtime node intent，调用 provider 幂等创建 runtime node，等待 node-agent 注册并把 runtime node 标记 ready。

### DeploymentStatusReconciler

聚合 execution 状态，维护 deployment ready / available / status。

### RolloutReconciler

根据 deployment 状态推进 service current/candidate revision、rollout phase 和 observed status。

### NodeHealthReconciler

周期性检查 stale heartbeat，把失联 node 标记 offline，并将受影响 execution / deployment / service 标记失败或降级。

## 实施约束

- API 请求路径不得再调用 provider。
- API 请求路径不得等待 node-agent ready。
- API 请求路径不得直接完成 deployment placement binding。
- 所有后台写入必须以 generation / object status 做条件，避免旧 reconcile 覆盖新 desired。
- provider 调用必须使用 runtime node intent 作为幂等键。
- 不保留旧同步 lifecycle 兼容开关。

## 验证点

- `go test ./internal/cloudplane/...`
- `go vet ./internal/cloudplane/...`
- 真实部署时，control-plane 断开后，cloud-plane 仍能：
  - 继续让 node-agent poll 已接受任务。
  - 根据 node-agent report 推进本地 observed 状态。
  - 周期性处理 heartbeat stale。
  - 进程重启后从本地 desired / deployment / runtime node intent 恢复推进。
