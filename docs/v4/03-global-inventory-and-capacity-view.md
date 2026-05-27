# v4/03 Global Inventory And Capacity View

这一章做的事情很直接：

- 不再新增 fleet 动作
- 不再新增同步流程
- 也不再改数据库 schema

而是把 `v4/02` 已经同步回来的这些对象：

- `fleet_planes`
- `fleet_plane_statuses`
- `fleet_plane_capacity_snapshots`

真正整理成一个：

- 全局 inventory 读视图

这就是这一章的核心边界。

## 先说明这章为什么重要

到了 `v4/02`，
fleet 其实已经能知道每套 plane 的：

- provider
- region
- health
- node / app / deployment 摘要
- worker 总 CPU / 内存容量

但这些信息还散在几个地方：

- `GET /api/v1/fleet/planes`
  里有原始 plane 详情
- `status`
  在 plane 详情里
- 最新容量快照
  也挂在 plane 详情里
- 如果想做全局判断，
  调用方还得自己再聚合一遍

这就带来一个问题：

- fleet 虽然已经“拿到了数据”
- 但还没有真正提供一个“全局观察面”

所以 `v4/03`
真正补的是：

- 一个干净的只读聚合层

## 先看这章的数据链路

这章最重要的，
不是先记住某个新接口名字，
而是先看清楚：

- global inventory 到底是怎么长出来的

可以先把它理解成下面这条链路：

```text
remote plane APIs
  /api/healthz
  /api/v1/platform/config
  /api/v1/platform/overview
  /api/v1/platform/reliability
  /api/v1/platform/nodes
        |
        v
fleet sync service
  register / sync / background reconcile
        |
        v
fleet local state
  fleet_planes
  fleet_plane_statuses
  fleet_plane_capacity_snapshots
        |
        v
global inventory view
  summary
  providers
  regions
  planes
```

也就是说：

- `v4/03`
  看的不是远端源数据本身
- 而是 fleet 基于同步结果做出来的投影

## 这章不再新增什么

为了让边界清楚，
这一章明确不做这些事：

- 不新增 plane 注册动作
- 不新增 plane 同步动作
- 不新增 incident 流程
- 不新增调度或下发
- 不新增 fleet 专用表

也就是说：

- `v4/02`
  负责把数据拉回来
- `v4/03`
  负责把这些数据整理成“可看的全局视图”

## 为什么这里不直接继续用 `GET /api/v1/fleet/planes`

这是这章最关键的设计点。

`GET /api/v1/fleet/planes`
返回的是：

- fleet plane 原始资源列表

它适合回答：

- 有哪些 plane
- 每个 plane 的静态资料是什么
- 当前状态对象长什么样
- 最新容量快照有没有挂上来

但它不适合直接回答：

- 所有 plane 加起来一共有多少 node
- 总共有多少 app / deployment
- 哪个 provider 下面的容量最多
- 哪个 region 当前最紧张
- 整个 fleet 一共有多少 ready / degraded / offline plane

因为这些都是：

- 聚合视角

所以这一章新增的不是另一个“资源”，
而是一个：

- read model

## 这章新增了什么 API

这章只加了一个新的只读接口：

- `GET /api/v1/fleet/inventory`

它走的仍然是：

- admin token

不是：

- project token
- fleet bootstrap token

因为这是 fleet 自己的全局管理视图。

## `GET /api/v1/fleet/inventory` 返回什么

这个接口会把当前所有 fleet plane 的最新状态和容量快照，
整理成三个层次：

1. `summary`
2. `providers`
3. `regions`
4. `planes`

也就是说，
同一份返回里同时有：

- 全局总览
- provider 维度汇总
- region 维度汇总
- 每个 plane 的明细行

这就是为什么它叫：

- `inventory`

而不是：

- `planes`

前者是聚合读视图，
后者是原始资源列表。

## 1. `summary` 在回答什么

`summary`
是整个 fleet 的总览。

它至少会回答：

- `planesTotal`
- `planesRegistered`
- `planesRegistering`
- `planesReady`
- `planesDegraded`
- `planesOffline`

以及容量侧的这些字段：

- `nodesTotal`
- `nodesReady`
- `nodesUnavailable`
- `appsTotal`
- `deploymentsTotal`
- `cpuMilliCapacity`
- `cpuMilliAllocated`
- `cpuMilliFree`
- `cpuAllocationRatio`
- `memoryMiCapacity`
- `memoryMiAllocated`
- `memoryMiFree`
- `memoryAllocationRatio`

这里有一个命名细节要注意：

- 这里没有叫 `nodesNotReady`
- 而是叫：
  - `nodesUnavailable`

原因是这章聚合用的来源是：

- 每个 plane 最新快照里的：
  - `nodesTotal`
  - `nodesReady`

所以只能可靠地算出：

- `nodesTotal - nodesReady`

但这部分剩余节点里，
实际可能包括：

- `not_ready`
- `offline`
- `draining`
- 甚至别的临时状态

所以：

- `nodesUnavailable`

会比：

- `nodesNotReady`

更准确。

这里还要再补一句很重要的话：

- `nodesTotal / nodesReady`
  是按远端 plane 里的所有 node 总数统计出来的
- 但 CPU / 内存容量汇总，
  当前只会统计：
  - `role == worker`

所以：

- “节点数量总览”
- “worker 容量总览”

在这一章里是两条并排的观察线，
它们不是同一个统计口径。

## 2. `providers` 在回答什么

`providers`
是按 provider 聚合的汇总列表。

第一版里它主要让你回答：

- 阿里云这一侧现在一共有多少 plane
- 腾讯云这一侧有多少 ready / degraded plane
- 每家 provider 当前总容量是多少
- 哪家 provider 当前分配更满

这一层是后面做：

- placement policy
- provider 层容量观察

的最小基础。

## 3. `regions` 在回答什么

`regions`
和 `providers`
类似，
只是切换到 region 维度。

第一版里它主要回答：

- 哪些 region 已经接入 fleet
- 每个 region 有多少 plane
- 每个 region 当前有多少 node / app / deployment
- 每个 region 的 worker 容量和分配量

这也是后面做：

- region-aware placement
- 维护窗口
- 容量预警

的基础。

## 4. `planes` 在回答什么

`planes`
是 inventory 视图里的“行级明细”。

这一层不是直接把原始 `fleetplane.Detail`
原封不动塞回来，
而是整理成一行 operator 更容易看的字段：

- `id`
- `name`
- `displayName`
- `provider`
- `region`
- `apiBaseURL`
- `registered`
- `status`
- `statusMessage`
- `lastHeartbeatAt`
- `lastSyncAt`
- `capacityCapturedAt`
- `nodesTotal`
- `nodesReady`
- `nodesUnavailable`
- `appsTotal`
- `deploymentsTotal`
- `cpuMilliCapacity`
- `cpuMilliAllocated`
- `cpuMilliFree`
- `memoryMiCapacity`
- `memoryMiAllocated`
- `memoryMiFree`

也就是说，
这层是：

- 面向操作视角的行模型

而不是：

- 原始数据库对象直出

这里有三个特别容易混淆的字段，
要单独记一下：

### `lastHeartbeatAt`

这里说的不是：

- 某个 worker node 的 heartbeat

而是：

- fleet 最近一次成功探活远端 plane 的时间

### `registered`

这里说的不是：

- plane 当前一定健康

它回答的是：

- 这套 plane 的 bootstrap token 是否已经被成功验证并保存过

所以：

- `registered == true`

不等于：

- `status == ready`

### `capacityCapturedAt`

这个时间表示的是：

- 最新容量快照对应的观测时间

而：

- `lastSyncAt`

表示的是：

- fleet 本地最近一次成功把同步结果写回数据库的时间

所以这两个时间接近很正常，
但它们表达的不是同一件事。

## 这个视图是怎么构建出来的

这章没有新增 SQL 聚合表，
也没有新建 materialized view。

当前做法很简单：

1. 先通过现有 store 读取：

- `ListFleetPlanes`

2. 这个方法已经能带出每个 plane 的：

- 当前 `status`
- `registration`
- 最新 `capacity snapshot`

3. 再在内存里聚合成：

- `summary`
- `providers`
- `regions`
- `planes`

这样做的原因是：

- 现在数据量还很小
- 聚合逻辑还在快速演进
- 先把读模型语义做清楚，
  比过早把聚合写死到 SQL 更重要

所以这一章新增了一个纯 Go 聚合包：

- `internal/fleetinventory`

它的职责非常单一：

- 输入 `[]fleetplane.Detail`
- 输出一个 inventory 视图

这使得：

- 聚合逻辑可以独立单测
- handler 也能保持很薄

这也是为什么这一章没有再去建：

- `global_nodes`
- `global_apps`
- `global_deployments`

这类表。

因为当前 fleet 还没有把远端每个对象逐条同步回来，
现在最合适的边界仍然是：

- plane 粒度的全局汇总

## 为什么这章不改数据库

因为 `v4/02`
已经把这章需要的核心信息都同步回来了：

- plane 当前状态
- 最新容量快照
- 注册状态

所以：

- `v4/03`
  缺的不是“数据还没存”
- 缺的是“怎么把它整理成全局看板”

如果这时候再去加新表，
很容易把事情做重。

这一章更好的做法就是：

- 先做纯读聚合

## 这一章最值得看的代码

### 1. 聚合逻辑

- [fleetinventory.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/inventory/fleetinventory.go)

这是这章真正的核心。

建议重点看：

- `Build`
- `buildPlane`
- `accumulateSummary`
- `finalizeSummary`

这里你会看到：

- 原始 plane detail
  是怎么被整理成 operator 友好的行模型
- 全局 summary / provider / region
  是怎么被聚出来的

### 2. HTTP handler

- [fleet_handler.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/fleet_handler.go)

这里新增了：

- `inventory`

它本身很薄，
只做三件事：

1. 读取 plane 列表
2. 调用聚合器
3. 返回 JSON

这就是这章想保持的结构：

- 聚合规则放进纯逻辑层
- handler 只负责 HTTP

### 3. 路由

- [router.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/router.go)

这里接上了：

- `GET /api/v1/fleet/inventory`

而且仍然保持：

- `adminOnly`

这点要和 `v4/02` 区分开：

- 远端 plane 的只读平台接口
  可以接受：
  - admin token
  - fleet bootstrap token
- fleet 自己的：
  - `fleet/inventory`
  - `fleet/planes`
  - `fleet/incidents`

仍然都是：

- admin only

## 这一章怎么验证

这章做了两层验证。

### 第一层：纯聚合单测

- [fleetinventory_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/inventory/fleetinventory_test.go)

这里直接构造了几套不同状态的 plane：

- `ready`
- `degraded`
- `registering`

然后验证：

- 全局汇总
- provider 分组
- region 分组
- plane 行排序

是不是都符合预期。

### 第二层：HTTP 集成测试

- [httpapi_integration_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/httpapi_integration_test.go)

这里会：

- 往测试数据库里造几套不同状态的 plane
- 再请求：
  - `GET /api/v1/fleet/inventory`

检查返回里的：

- `summary`
- `providers`
- `regions`
- `planes`

是不是都对。

另外还顺手补了一个权限边界：

- `fleet bootstrap token`
  不能访问 `fleet/inventory`

因为它只是用来读远端 plane 平台接口的，
不是 fleet 自己的全局管理 token。

如果想把这条链路看得更完整，
可以把下面这组调用连起来看：

1. `POST /api/v1/fleet/planes/{planeID}/actions/register`
2. `POST /api/v1/fleet/planes/{planeID}/actions/sync`
3. `GET /api/v1/fleet/planes/{planeID}`
4. `GET /api/v1/fleet/inventory`
5. `GET /api/v1/fleet/operations`

这样你能同时看到：

- 同步动作
- 单 plane 当前状态
- 全局聚合结果
- 以及这份视图是什么时候被哪次 fleet 动作更新出来的

## 这一章最容易混淆的点

### 1. `fleet/planes` 和 `fleet/inventory` 不是一回事

- `fleet/planes`
  更像资源原表视图
- `fleet/inventory`
  更像面向 operator 的聚合视图

两者都要保留，
因为回答的问题不同。

### 2. `nodesUnavailable` 不是某个原始节点状态

它只是：

- `nodesTotal - nodesReady`

也就是说，
它是聚合读模型里的“不可用节点数”，
不是底层数据库某个单独状态列。

### 3. 这章还是只读

虽然它已经非常像“看板”了，
但这一章仍然不做：

- fleet deploy
- fleet placement
- fleet incident action

这些都留给后面章节。

## 手动试一下

先看全局 inventory：

```bash
curl -sS http://127.0.0.1:8080/api/v1/fleet/inventory \
  -H 'Authorization: Bearer <admin-token>'
```

再对比原始 plane 列表：

```bash
curl -sS http://127.0.0.1:8080/api/v1/fleet/planes \
  -H 'Authorization: Bearer <admin-token>'
```

你会很明显看到差别：

- 前者是全局聚合视图
- 后者是原始资源列表

## 本章涉及的主要文件

- [fleetinventory.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/inventory/fleetinventory.go)
- [fleetinventory_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/inventory/fleetinventory_test.go)
- [fleet_handler.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/fleet_handler.go)
- [router.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/router.go)
- [httpapi_integration_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/httpapi_integration_test.go)

## 本章检查点

- 待本章完成后补充

- 待本章完成后补充
