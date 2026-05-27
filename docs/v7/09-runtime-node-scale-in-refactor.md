# Runtime Node 自动缩容重构

## 背景

v7 之前的 runtime node 只支持按调度缺口自动创建。
一旦 cloud-plane 创建云主机并等 node-agent 注册成功，这台 runtime node 会长期留在云厂商侧。
这和当前架构不一致：

- runtime node 是承载 workload 的弹性容量，不是平台固定资产；
- cloud-plane 已经持久化 accepted desired state，可以在没有运行中 workload 时自行回收 runtime node；
- node-agent 运行在 runtime node 上，节点被回收前必须先关闭调度入口，不能再领取新 work；
- 缩容不是 teardown，不能清理 control-plane / cloud-plane / 数据库 / ingress / egress 等平台组件。

本次重构直接建立完整的 scale-out / scale-in 生命周期，不保留只创建不删除的旧设计。

## 目标

- `RuntimeDriver` 同时支持创建、列出和删除云厂商 runtime node。
- cloud-plane 后台 reconciler 自动删除空闲 runtime node。
- runtime node 可以缩到 `0` 台；后续有 workload 需要容量时再由现有 scale-out 重新创建。
- 不增加 `autoscaling.enabled`、`scaleIn.enabled`、`minReadyNodes`、`idleGrace`、`cooldown`、`maxDeletesPerRun` 等策略配置。
- 不引入隐藏的最小保留数量或每轮删除上限常量。
- 删除语义只依赖当前持久化运行态，不依赖旧的 service/deployment 绑定标签。

## 非目标

- 不做平台 teardown。
- 不删除 platform host。
- 不删除 Caddy、Tinyproxy、PostgreSQL、Prometheus 等平台依赖。
- 不把 runtime node 重新绑定到 service、deployment 或 replica。
- 不实现人工扩缩容命令。

## 状态机

runtime node 生命周期变为：

```text
provisioning -> ready -> draining -> deleting -> deleted
```

含义如下：

- `provisioning`：cloud-plane 已记录创建意图，云厂商实例可能还未创建完成或 node-agent 尚未注册。
- `ready`：node-agent 已注册并上报 ready，节点允许参与 workload 调度。
- `draining`：cloud-plane 已决定回收该 runtime node，并关闭对应 `nodes.schedulable`。
- `deleting`：cloud-plane 已确认该节点没有 active execution，正在调用云厂商删除接口。
- `deleted`：云厂商删除请求已接受，或云厂商返回实例不存在；本地记录保留为审计状态。

删除失败不新增 `delete_failed`。
失败时 runtime node 保持 `deleting`，`status_reason` 记录失败原因；
下一轮 reconciler 继续重试同一条记录。

## 删除判定

一个 runtime node 只有同时满足以下条件才会进入缩容：

```text
不存在 pending/scheduling/assigned/deploying deployment
runtime_nodes.status = ready
runtime_nodes.instance_id 非空
runtime_nodes.node_id 非空
deployment_executions 中没有该 node 的 deploying/running execution
```

active execution 定义为：

```sql
deployment_executions.status IN ('deploying', 'running')
```

这里不使用“节点总数”“保留节点数”“空闲时间”或“每轮最大删除数”。
原因很简单：runtime node 是 workload 容量，不是平台基础组件；
没有 active execution 时，它不应该继续消耗云主机费用。

`pending/scheduling/assigned/deploying`
deployment 是额外的全局保护条件。
它不是缩容策略参数，而是并发正确性条件：
当 cloud-plane 正在为某个 workload 调度或等待 node-agent 领取 work 时，
刚 scale-out 出来的 runtime node 可能已经 ready，
但还没有产生 execution。
这时如果立即按“没有 active execution”删除节点，会和正在进行的调度流程互相打架。
因此只要存在未稳定 deployment，本轮 scale-in 直接跳过；
deployment 进入 `running`、`failed` 或 `superseded` 后，下一轮再按每个 node 的 active execution 判断是否删除。

## 删除流程

后台 reconciler 每轮执行以下流程：

1. 查询当前 ready 且没有 active execution 的 runtime node。
2. 对单个候选 runtime node 开启事务，锁定 `runtime_nodes` 记录。
3. 将 `runtime_nodes.status` 改为 `draining`。
4. 将对应 `nodes.status` 改为 `draining`，并设置 `nodes.schedulable = false`。
5. 事务提交后，再次统计该 node 上的 active execution。
6. 如果 active execution 数量大于 `0`：
   - 把 runtime node 恢复为 `ready`；
   - 把 node 恢复为 `ready` 和 `schedulable = true`；
   - 本轮不删除该实例。
7. 如果 active execution 数量仍为 `0`：
   - 将 runtime node 标记为 `deleting`；
   - 调用 `RuntimeDriver.Delete(instanceID)`；
   - 删除请求被 provider 接受后将 runtime node 标记为 `deleted`；
   - 将 node 标记为 `offline` 且保持不可调度。
8. 如果 provider 删除失败：
   - runtime node 保持 `deleting`；
   - `status_reason` 写入 provider 错误；
   - 下一轮 reconciler 继续重试。

provider 返回实例不存在时按删除成功处理。
这保证了 cloud-plane 重启、provider 请求超时或人为删除实例之后，reconciler 仍能收敛到 `deleted`。

## 调度和 node-agent 领取约束

缩容前先把 node 改为 `draining` 且关闭 `schedulable`。
调度器已经只选择 `ready && schedulable` 的 runtime node。
node-agent 领取 work 时也必须再次检查 node 是否仍然 `ready && schedulable`，
避免已经进入 drain 的节点继续领取新 execution。

这条规则是 scale-in 的安全边界：

- 调度入口关闭，避免产生新的放置；
- execution 二次检查，避免旧放置在 drain 后被领取；
- active execution 统计为 `0` 后才调用 provider 删除实例。

## 云厂商接口边界

`RuntimeDriver` 只暴露通用节点生命周期：

```go
type RuntimeDriver interface {
    Create(ctx context.Context, request CreateRequest) (CreateResult, error)
    List(ctx context.Context) ([]Node, error)
    Delete(ctx context.Context, request DeleteRequest) error
}
```

`DeleteRequest` 只包含云实例 ID。
driver 不接收 service、deployment、replica、tag 或缩容策略参数。
云厂商删除只按本地 `runtime_nodes.instance_id` 调用 provider API。
这里信任 cloud-plane 自己持久化的 runtime node 记录；
标准 ownership tag 仍用于创建和云侧列表诊断，不在 `DeleteRequest` 中重新暴露业务归属。

## 数据保留

本次重构不再物理删除 `runtime_nodes` 行。
`deleted` 记录用于排查：

- 哪台云主机被 cloud-plane 创建过；
- 它何时 ready；
- 它何时进入删除流程；
- provider 删除失败原因是什么。

`nodes` 表保留 node-agent 注册记录，但状态变为 `offline` 且不可调度。
后续如果需要更严格的历史归档，再单独设计 node inventory 的归档模型。

## 验证点

- 无 workload 时，所有 ready runtime node 会被自动删除，最终可以为 `0` 台。
- 有 `deploying/running` execution 的 node 不会被删除。
- 删除失败后 runtime node 保持 `deleting`，下一轮继续重试。
- provider 返回实例不存在时视为成功，并把本地状态收敛为 `deleted`。
- 不存在任何 scale-in 配置项、保留节点常量或每轮删除上限常量。
