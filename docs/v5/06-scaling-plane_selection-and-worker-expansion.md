# v5/06 Scaling Placement And Worker Expansion

`v5/05`
收的是：

- 单副本 `service`
  的健康、发布、回滚

但平台要成为真正可用的 `CaaS`，
还差一个很现实的问题：

- 一个服务不可能永远只有 1 个副本

所以这一章开始，
我们不再只问：

- 新版本能不能安全上线

而是开始问：

- 一个长期在线服务怎样稳定跑多个副本
- 副本不够时该放到哪些 `worker`
- 当前云里不够放时能不能继续扩 `worker`
- 某个副本失败后，平台怎么重试，而不是把整个部署直接写死

这一章收的不是：

- 多副本滚动发布
- 多批次灰度
- 缩容和 `worker`
  回收

这一章只先把：

- 多副本运行
- 当前 deployment 的 scale-up
- 副本级放置
- 本云 `worker`
  扩容

收成正式模型。

## 这一章先回答什么问题

这里最容易混乱的点有 4 个：

1. 为什么改 `replicas`
   不应该自动变成一次新 revision 发布
2. 为什么多副本后，
   `deployment`
   不能再只看“一条 execution”
3. 为什么 `gateway`
   不能再只指向一个 backend
4. 为什么失败副本不能把自己的槽位永久占死

如果这 4 个问题不讲清楚，
平台虽然表面上有了：

- `replicas`

这个字段，

但实际上仍然只是：

- 单副本心智下套了一个多副本参数

那是不成立的。

## 为什么副本变化不等于新 revision

`revision`
描述的是：

- 镜像
- 命令
- 参数
- 环境变量
- 健康检查
- 配置和密文引用

这些会改变运行内容的东西。

而：

- `replicas`

改变的是：

- 这个 revision
  需要跑几份

所以从产品语义看：

- 改镜像
  是发布
- 改副本数
  是运行容量调整

这两件事不能混在一起。

现在代码里已经把这条边界明确下来：

- `service_store.UpdateService`
  会区分：
  - `RevisionChanged`
  - `ReplicasChanged`
- `servicelifecycle.Update`
  对 revision 变化继续走：
  - `LaunchRevision`
- 对副本变化改走：
  - `ScaleCurrentDeployment`

也就是说：

- `replicas`
  变化不再创建新 revision
- 只会在当前 deployment 上补新的副本槽位

## 为什么需要 `replica_index`

单副本时，
平台里很多地方都默认：

- 一个 deployment
  只有一条 placement
- 一个 deployment
  最多只有一个当前 execution

多副本后，
这个前提彻底失效。

所以这一章把两个核心表都补上了：

- `placement_decisions.replica_index`
- `deployment_executions.replica_index`

这样平台才能说清楚：

- 第 0 个副本放哪
- 第 1 个副本放哪
- 第 1 个副本当前是不是失败了
- 第 1 个副本失败后下一次重试还应该领哪一个槽位

可以把它理解成：

```text
deployment
  ├─ replica 0 -> placement -> execution
  ├─ replica 1 -> placement -> execution
  └─ replica 2 -> placement -> execution
```

不是：

```text
deployment -> 一个 placement -> 一个 execution
```

这就是多副本和单副本最本质的模型差异。

## 这章的数据面为什么也必须跟着改

一旦一个服务有多个 ready 副本，
`gateway`
就不能再只知道一个 backend。

所以这章把数据面快照从：

- 单 `Upstream`

收成了：

- `Upstreams []string`

整体链路现在是：

```text
service
  -> current deployment
    -> running executions
      -> ResolveCurrentServiceRoutes
        -> dataplane snapshot
          -> Caddy reverse_proxy backend1 backend2 ...
```

这里还有一个容易忽略的细节：

当一个站点后面已经有多个 backend 时，
`gateway`
不能再伪造一个固定的：

- `X-MiniCloud-Execution-ID`

因为真正处理请求的是哪一个副本，
要等 `Caddy`
做完负载分发才知道。

所以现在只在单 backend 场景下保留这个头，
多 backend 时只保留：

- `service`
  级别信息
- backend 数量

避免 header 语义失真。

## 当前 scale-up 的正式路径

这一章把“当前 deployment 扩副本”收成下面这条链：

```text
UpdateService(replicas changed)
  -> servicelifecycle.Update
    -> deploymentService.ScaleCurrentDeployment
      -> scheduler.Plan(additional replicas)
      -> tryScaleOutForPlacement(if local workers are not enough)
      -> store.ApplyDeploymentScaleUp
         - update deployments.desired_replicas
         - insert placement_decisions for new replica_index
```

这里有两个关键点。

### 1. 先规划，再一次性落库

之前最危险的问题是：

- deployment 的 `desired_replicas`
  先写进数据库
- 后面调度失败才返回错误

这样用户看到的是：

- 接口失败了

但数据库里已经像是扩容成功了一半。

这章把它改成了：

- 先做调度和必要的 `worker`
  扩容
- 确认新增副本确实能放下
- 最后再通过：
  - `store.ApplyDeploymentScaleUp`
  一次性更新 deployment 和 placement

这样失败时不会先把 deployment 写脏。

### 2. service spec 失败时要恢复

`service.spec.replicas`
本身也会更新。

所以现在 `servicelifecycle.Update`
在扩容或 revision 发布失败时，
会把 `service`
行恢复回旧 spec，
避免出现：

- API 报错
- 但 `service.spec`
  已经偷偷改掉

这种最难排查的状态漂移。

## 失败副本为什么不能永久占死槽位

多副本最容易出问题的地方，
不是“怎么把多个副本跑起来”，
而是：

- 某一个副本失败后，
  剩下副本还活着，
  这时系统怎么办

如果平台只是简单地说：

- 这个 deployment
  已经有过 `replica 1`
  的 execution 记录了

那下一次就再也不会有人重领这个槽位。

结果会变成：

- `desiredReplicas = 2`
- 真实长期只剩 1 个副本在跑
- 平台还会一直以为第 2 个副本的历史记录“占着位置”

这一章把这条链收正了：

1. `deployment_executions`
   对 `(deployment_id, replica_index)`
   的唯一性，
   只约束活跃状态：
   - `deploying`
   - `running`
2. `ClaimExecutionWork`
   也只会把活跃 execution
   当成“这个槽位已被占用”
3. 某个副本上报 `failed`
   时，
   会先释放这次 execution
   占用的节点 CPU / 内存

这样结果就是：

- 失败副本不会泄漏节点容量
- 同一个 `replica_index`
  后面还能再次被调度和领取

这才是多副本服务真正需要的“副本槽位”语义。

## 平台展示层为什么也要改

之前平台里的 deployment 摘要接口，
实际上还是：

- 一个 deployment
  只显示一个 node

这在多副本下是错误的。

因为一个 deployment
完全可能同时分布在多个节点上。

所以这一章把平台 deployment 摘要改成了：

- `placementCount`
- `nodeIDs`
- `nodeNames`

完整 placement 细节仍然在：

- `/api/v1/platform/placements`

而 deployment 列表现在至少不会再假装：

- 这个 deployment
  只落在一个节点上

## 这一章明确还不做什么

为了把边界收干净，
这章明确不做 3 件事：

### 1. 不做 `scale-down`

缩容不是把 `replicas`
改小这么简单。

它至少还需要回答：

- 要停哪一个 `replica_index`
- 什么时候删 placement
- 什么时候释放 allocation
- 什么时候真正下发 stop work
- 如果这个副本还在接流量怎么办

当前 southbound 只有“启动 execution”和“替换旧 execution”的正式链路，
还没有独立的 stop work 模型，
所以这章明确拒绝：

- `scale-down`

### 2. 不做多副本 rollout / rollback

这一章只允许：

- 多副本运行
- 当前 deployment 扩容

但不允许：

- 多副本 revision 变更
- 多副本 rollback
- 多副本 pause / resume promote

原因很直接：

- `v5/05`
  的发布状态机本质上还是单副本安全切换心智

如果现在把多副本发布也混进来，
会立刻把：

- rollout
- scale
- safe cutover
- supersede

几条链全搅在一起。

所以现在代码会直接拒绝：

- 多副本 revision 更新
- 多副本 rollback
- rollout 过程中改副本数

### 3. 不做 `worker` 回收

这章已经把：

- 本云 `worker`
  扩容

接进来了，

但：

- 扩出来的 `worker`
  什么时候安全回收

仍然是另一条独立问题。

它需要和：

- 缩容
- drain
- execution 停止
- allocation 回收

一起设计，
所以也留到后面章节。

## 这一章结束后，平台新增了什么正式能力

做到这里，
`mini-cloud`
第一次真正具备了：

1. 创建多副本 `service`
2. 当前 deployment 上做副本扩容
3. 按副本粒度放置和执行
4. 数据面把多个 ready backend
   一起挂到 `Caddy`
5. 本云 ready `worker`
   不够时尝试继续扩容
6. 某个副本失败后释放容量并允许重试
7. 平台 deployment 摘要正确表达多节点落点

这意味着从这一章开始，
平台的运行心智终于从：

- “一个服务就是一个容器”

提升成了：

- “一个服务是一个 deployment，
  deployment 下面有多个副本槽位，
  每个槽位各自被放置、执行、失败、重试”

这才是后面继续做：

- 应用观测
- 项目约束
- 外部统一入口

这些能力的真实基础。

## 本章检查点

如果这一章做对了，
你现在应该已经能稳定回答下面这些问题：

1. 为什么改：
   - `replicas`
   不应该触发新 revision
2. 为什么多副本后必须引入：
   - `replica_index`
3. 为什么 deployment 不能再只看一条 execution
4. 为什么数据面必须从单 backend
   变成多 backend
5. 为什么失败副本不能永久占死自己的槽位和节点容量
6. 为什么这章只做：
   - 多副本运行
   - scale-up
   而不做：
   - `scale-down`
   - 多副本 rollout
7. 为什么平台摘要接口也必须改成能表达多节点落点

如果这些问题都已经能稳定回答，
那 `v5/06`
的多副本运行边界就算真正立住了。

对应提交：

- `01d8a20ba9400f91a6b6f255313244b95425f906`
