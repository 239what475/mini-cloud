# 03 Runtime Node Pools And Capacity Headroom

`v6/02`
把
`service`
正式收成了：

- `1 service -> N cells`

但做到这里，
平台仍然还只知道：

- 一个服务想落到哪些 `cell`
- 每个 `cell`
  想被放到哪个 `plane`

它还不知道另外一半同样重要的事情：

- 这些 `plane`
  背后应该保有多少运行时节点容量
- 正常放置时应该预留多少余量
- 什么情况下应该把某个 `plane`
  视为“还能放”
  或
  “应该先保留 headroom”

如果这一层不显式建模，
那平台实际上仍然还是靠：

- “有容量就继续放”
- “放不下了再临时扩一台 runtime node”

来表达供给侧。

这对真正的长期运行平台来说太松了。

所以
`v6/03`
要做的事情是：

- 把 `plane`
  后面的运行时供给侧
  正式收成
  `runtime node pool`
  和
  `capacity headroom`

## 这一章的核心决定

这一章先明确做一个最小但干净的版本：

- `runtime node pool`
  放在 fleet `control-plane`
  侧
- 每个 `plane`
  先只允许有一个 pool
- `pool`
  只表达这个 `plane`
  的运行时节点供给策略
- 真正的 runtime node
  创建、回收和 ready
  生命周期，
  仍然留在 `cloud-plane`
  侧执行

也就是说，
这一章不是把 runtime node
  管理链路搬到 fleet，
而是先让 fleet
有能力正式声明：

- 这个 `plane`
  至少要有多少 ready runtime nodes
- 最多允许多少 ready runtime nodes
- 正常放置后
  至少还要保留多少
  `cpu / memory`
  headroom

## 为什么先做成“每个 plane 一个 pool”

当前代码里，
每个 `cloud-plane`
本身就只有一套：

- provider
- region
- runtime node
  规格与 provisioner

也就是说，
今天的 `plane`
  本来就更像：

- 一个单一运行时池

而不是：

- 一个内部还有多种 node class
  和多条供给队列的大集群

所以
`v6/03`
故意不做：

- 一个 `plane`
  下多个 pools
- pool 级别的 node class
- pool 级别的独立 scheduler

因为这些都会把这一章带进过度设计。

这一章只先做：

- fleet 里有很多 `plane`
- 每个 `plane`
  可以单独配置一个 runtime node pool

从 fleet
视角看，
这已经是：

- 多个 pools

了。

## 这一章里的 node 命名怎么理解

这里有三个很容易混在一起的概念：

- `node`
  - 指已经注册回来的真实节点身份
  - 在 `cloud-plane`
    里它还有
    `platform`
    和
    `runtime`
    两种 role
- `runtime node record`
  - 指某次 runtime 节点扩缩的生命周期记录
  - 比如：
    provisioning / ready / reclaiming
- fleet 里的 `capacity snapshot`
  - 仍然沿用
    `NodesTotal / NodesReady`
    这组名字
  - 但当前语义是：
    它统计的是远端 plane
    `role=runtime`
    的 node 供给侧汇总

所以，
这里没有把所有名字都改成
`runtimeNode*`，
是刻意的：

- `node`
  这一层实体本身没有问题
- 真正需要单独区分的，
  是 runtime 节点的扩缩生命周期记录
- 而 fleet 侧的 pool / headroom
  只关心供给侧容量视图，
  不直接管理单个 runtime node record

## 这章的对象模型

这一章会新增一个正式对象：

- `fleet runtime node pool`

它直接挂在某个 `plane`
下面，
最小规格只有四个字段：

1. `minReady`
   - 至少希望这个 `plane`
     背后维持多少
     ready runtime nodes
2. `maxReady`
   - 这个 `plane`
     背后最多允许多少
     ready runtime nodes
3. `headroomCPUMilli`
   - 正常放置后
     仍希望保留的最小 CPU 余量
4. `headroomMemoryMi`
   - 正常放置后
     仍希望保留的最小内存余量

对应地，
这个对象会有一份派生状态，
但这章先不把它做成会触发远端扩缩的 controller，
而是先收成：

- 基于当前 `plane`
  最新 capacity snapshot
  计算出的 pool view

它至少会回答这些问题：

- 当前 ready nodes
  是否低于 `minReady`
- 当前 ready nodes
  是否高于 `maxReady`
- 当前 free cpu / memory
  是否已经低于 headroom
- 如果某个新的 `cell`
  再落到这个 `plane`
  上，
  会不会把 headroom
  打穿

## 这章怎么接到现有放置链路

这一章最关键的实现点不是
“多一个表”，
而是：

- 要把 headroom
  真正接进
  `fleet placement`
  的 admission 过程

也就是：

1. 先按原来的规则过滤：
   - 绑定
   - 注册
   - ready
   - operation
   - provider
   - region
   - 反亲和
   - 原始可用容量
2. 如果某个 `plane`
   配了 runtime node pool，
   再继续判断：
   - 现在的 ready nodes
     是否已经低于 `minReady`
   - 这次 placement
     之后剩余容量
     是否仍满足 headroom
3. 只有两层都满足，
   才允许成为最终候选

这意味着，
从这一章开始：

- `plane`
  “还有空闲容量”
  不再等价于
  “允许继续放”

平台会第一次正式区分：

- 原始剩余容量
- 保留余量之后
  还能不能继续接新的 `cell`

## 这一章刻意不做什么

这一章有几个明确的边界。

### 1. 不从 fleet 直接触发真实扩缩

当前真实 runtime node
  供给链路在
`cloud-plane`
里已经存在：

- 本地调度放不下
- provider provisioner
  去创建 runtime node
- node-agent
  回连后进入 ready

`v6/03`
不会把这条链路搬到 fleet，
也不会让 fleet
直接调 provider API
做扩缩。

这一章先只做：

- 声明式 pool 策略
- admission
- 可观察性

这样边界是干净的：

- fleet
  管策略和准入
- `cloud-plane`
  继续管节点生命周期执行

### 2. 不做 pool 级别的独立调度器

这一章不会引入：

- pool selector
- pool affinity
- pool 级 rollout
- pool 级 front door

因为现在真正被调度的对象仍然是：

- `service cell -> plane`

而不是：

- `service cell -> plane -> pool`

这一章先把 pool
作为
`plane`
的供给侧约束，
而不是新的放置维度。

### 3. 不做自动 scale-down 策略执行

这一章会识别：

- ready nodes
  高于 `maxReady`
- 当前余量显著高于 headroom

但它不会进一步自动选择：

- 该删哪一个 runtime node
- 什么时候安全回收

这些属于后续真正的
day-2
和自动化治理能力。

## 这章结束后平台会多什么能力

做完这一章之后，
平台会第一次正式具备下面这些能力：

1. 可以给某个 `plane`
   显式声明运行时供给策略
2. placement preview
   会把
   “headroom 不满足”
   和
   “原始容量不足”
   区分开
3. operator
   可以明确看到：
   - 哪些 `plane`
     配了 pool
   - 哪些 pool
     正在低于 `minReady`
   - 哪些 pool
     已经低于 headroom
   - 这些状态现在会直接出现在
     `/api/v1/fleet/inventory`
     和
     `runtime-node-pools`
     相关接口里
4. 后面的发布与切流
   可以开始建立在更可信的供给侧约束上

这也是为什么
`03`
要先于：

- `04-centralized-control-plane-and-thin-cloud-plane-refactor`
- `05-progressive-delivery-and-release-policy`
- `06-service-resource-model-single-region-refactor`
- `07-front-door-route-publication-and-readiness-gating`

因为如果没有这一层，
后面的发布和切流都只能建立在：

- “当前看起来还能放”

这样不稳定的前提上。

## 相关代码位置

这一章预计会主要落在：

- `internal/controlplane/runtimepool/`
- `internal/controlplane/store/`
- `internal/controlplane/placement/`
- `internal/controlplane/api/`

## 这一章新增的接口

- `GET /api/v1/fleet/runtime-node-pools`
- `GET /api/v1/fleet/planes/{planeID}/runtime-node-pool`
- `PUT /api/v1/fleet/planes/{planeID}/runtime-node-pool`
- `DELETE /api/v1/fleet/planes/{planeID}/runtime-node-pool`

同时，
`fleet`
侧记录的
`capacity snapshot`
从这章开始会明确把：

- `NodesTotal`
- `NodesReady`

视为 runtime node
供给侧计数，
同步时优先取远端
`Capacity.RuntimeNodesTotal`
和
`Capacity.RuntimeNodesReady`，
而不是更泛化的
`Overview.Nodes*`。

## 检查点

- 提交：`e87709365cfd7cf61433d1f4a35ad11aeade48cc`
