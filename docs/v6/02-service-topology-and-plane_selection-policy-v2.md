# 02 Service Topology And Placement Policy v2

`v6/01`
把
`fleet service`
收成了真正的
`reconcile`
对象，
但它的权威模型本身仍然还是：

- `1 service -> 1 placement`

也就是说，
虽然平台已经有了：

- 多个 `cloud-plane`
- front door
- 多云部署

但一个服务到底能不能同时拥有：

- 主 cell
- 备 cell
- 显式绑定的 cell

这件事在对象模型里并没有真正成立。

所以
`v6/02`
做的核心事情只有一件：

- 把 `service`
  正式改成：
  - `1 service -> N cells`

这里的
`cell`
不是“显示层概念”，
而是这次开始真正进入
`control-plane`
权威数据模型的一等对象。

## 这一章改了什么

这一章把原来挂在 `service`
根对象上的这些字段：

- `provider`
- `region`
- `replicas`
- `instanceClass`

全部移到了：

- `service.cells[]`

也就是说，
现在一个服务的结构被明确拆成了两层：

1. 全局服务规格
   - `displayName`
   - `exposure`
   - `image`
   - `command / args`
   - `defaultPort`
   - `readinessPath`
   - `env`
   - `configSetID / secretSetID / registryCredentialID`
2. cell 拓扑规格
   - `key`
   - `role`
   - `provider`
   - `region`
   - `pinnedPlaneID`
   - `replicas`
   - `instanceClass`

对应代码主要在：

- [service.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/fleetservice/service.go)
- [fleet_service_store.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/store/fleet_service_store.go)
- [fleet_service_cell_store.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/store/fleet_service_cell_store.go)
- [fleet_service_placement_store.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/store/fleet_service_placement_store.go)

## 新的权威模型

这次开始，
`control-plane`
真正持久化下面三层数据：

1. 全局 `fleet_services`
   - 只保存全局服务规格和聚合状态
2. `fleet_service_cells`
   - 每个 cell
     一条记录
   - 每个 cell
     自己有：
     - `desiredState`
     - `status`
     - `observedGeneration`
3. `fleet_service_cell_placements`
   - 每个 cell
     自己有一条 placement
     记录

对应迁移在：

- [00028_create_fleet_services.sql](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/store/migrations/00028_create_fleet_services.sql)
- [00029_create_fleet_service_placements.sql](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/store/migrations/00029_create_fleet_service_placements.sql)

也就是说，
从这一章开始：

- service
  不再直接等价于
  “某个 plane
  上的一份远端服务”
- service
  变成全局逻辑对象
- 每个 cell
  才是实际去做 placement /
  apply /
  refresh /
  teardown
  的最小 reconcile 单位

## controller 现在怎么工作

这次 controller
最大的变化是：

- 不再围绕
  `service -> single placement`
  工作
- 而是变成：
  - 先按 service
    聚合
  - 再逐个 reconcile
    它的 cells

对应代码在：

- [controller.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/fleetservicecontroller/controller.go)
- [reconcile.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/fleetservicecontroller/reconcile.go)
- [rollback.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/fleetservicecontroller/rollback.go)

现在每个 cell
都有自己的 reconcile 分支：

1. 没有 placement
   - 先做 placement
2. 有 placement，
   但 service
   有新一代 desired spec
   - 尝试复用当前 plane
   - 不可复用时再重放置
3. 当前代已经观察过
   - 刷远端状态
4. cell
   被移除或 service
   整体删除
   - 走单独的 cell teardown

这使得：

- 一个 service
  可以同时拥有：
  - `primary`
  - `standby`
- 也可以在更新拓扑时：
  - 保留旧 cell
    的 deleting 状态
  - 等 teardown
    完成后再真正删除这条 cell

## 这次的 placement policy

这章没有做复杂流量切换，
但把 placement policy
先收成了能支撑后面章节的样子。

现在 planner
支持两条关键约束：

1. `pinnedPlaneID`
   - 某个 cell
     可以显式钉到指定 plane
2. 同一 service
   的 cell anti-affinity
   - 如果没有显式 pin，
     后面的 cell
     会避开前面已经占用的 plane

对应代码在：

- [placement.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/placement/placement.go)
- [service.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/placement/service.go)

这意味着：

- `primary + standby`
  现在默认不会再被 planner
  收敛回同一台 plane
- 但如果你显式给某个 cell
  配了 `pinnedPlaneID`，
  这个 pin
  仍然拥有更高优先级

## 远端服务命名为什么也要改

如果只是把
`cells[]`
写进 `control-plane`，
但 southbound
仍然拿全局
`service.Name`
去创建远端对象，
那么两个 cell
一旦落到同一个项目里，
远端名字仍然会冲突。

所以这章顺手把远端服务名改成了：

- `service-name--cell-key`

对应代码在：

- [reconcile.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/fleetservicecontroller/reconcile.go)

这一步虽然小，
但它是多 cell
真正成立的前提之一。

## API 现在怎么返回

这一章之后，
服务 API
不再返回单个：

- `placement`

而是返回：

- 全局 `spec`
- 聚合 `status`
- `cells[]`
  - 每个 cell
    自己的 spec
  - 每个 cell
    自己的 status
  - 每个 cell
    自己的 placement

对应代码在：

- [service_handler.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/api/service_handler.go)

这让 API
层终于能准确表达：

- 一个服务有多个 cell
- 每个 cell
  分别在哪个 plane
  上
- 哪个 cell
  还在 pending /
  deleting /
  ready

## front door 这章只做了最小适配

这一章没有做自动故障切换，
也没有做多 backend
切流。

front door
这次只做了最小适配：

- 它不再读
  `service -> one placement`
- 而是改成读：
  - `service.cells`
  - `cell placements`

但这章仍然只消费：

- 首选 cell
  也就是当前排序后的第一个 active cell

如果首选 cell
没有 placement，
front door
会直接阻塞，
而不是自动切到 standby。

这是刻意保留的章节边界：

- `v6/02`
  先把拓扑模型收干净
- `v6/06`
  会先把
  `service -> cell`
  语义整体回收
- `v6/07`
  再做基于单地域
  `service`
  的路由发布、
  readiness gating
  和排障入口

对应代码在：

- [controller.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/frontdoorcontroller/controller.go)

为了让
`multi-cell`
下的 route
不再只显示
“落在哪个 plane”，
这一章还把
`frontDoorRoutes`
补成会显式返回：

- `targetCellKey`
- `targetPlaneID`
- `targetOrigin`
- `targetHost`

这里要特别区分两层语义：

- `targetCellKey`
  解决的是
  “这条 route
  当前是按哪个 cell
  解析出来的”
- `syncStatus`
  和
  `lastSyncedAt`
  解决的是
  “这次解析结果
  是否已经真正同步出去”

所以当 route
已经解析出目标，
但还没有稳定进入
`synced`
时，
这些 target
字段表示的是 controller
最近一次解析出的目标，
不是强语义上的
“外部流量一定已经切过去了”。

## 这章的测试重点

这章新增和重写的测试，
重点验证六件事：

1. `create`
   仍然只是写 desired state
   - 不同步 placement
2. 单 cell
   服务能够正常 reconcile
   到 ready
3. `primary + standby`
   会默认分散到不同 plane
4. 移除某个 cell
   时，
   controller
   会先把它标成 deleting，
   再在 reconcile
   后真正清掉
5. 整个 service
   删除时，
   controller
   会先把 service /
   cells
   一起标成 deleting，
   等 cell
   teardown
   完成后再硬删除 service
6. rollback
   仍然只以
   “首选 active cell”
   为回滚源，
   并把回滚得到的全局规格重新写回 service

另外，
这章还把
store
层的几个关键写路径
补成了真正的
`generation`
保护：

- `UpdateFleetService`
- `MarkFleetServiceDeletionRequested`
- 最终 `DeleteFleetService`

它们不再只是
“按 id 覆盖”，
而是显式要求
当前代际
仍然匹配，
避免旧一代写把新一代 service
覆盖掉。

相关测试在：

- [controller_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/fleetservicecontroller/controller_test.go)
- [controller_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/frontdoorcontroller/controller_test.go)
- [reconciler_integration_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/frontdoorcontroller/reconciler_integration_test.go)
- [store_integration_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/store/store_integration_test.go)

## 这章仍然刻意没做什么

虽然
`1 service -> N cells`
已经正式成立，
但这一章仍然保留两个明确边界：

1. front door
   还不会自动在多个 cell
   之间做健康切流
   - 现在仍然只认首选 cell
2. rollback
   也还是
   “首选 active cell
   authoritative”
   - 不是按 serving cell
   - 不是按最新成功 cell
   - 也不是让用户显式选 cell

这不是遗漏，
而是章节边界：

- `v6/02`
  先把 topology /
  placement /
  generation fence
  收干净
- 后面章节
  不会继续沿着
  `service -> cell`
  做产品化
- `06`
  会先把这套用户可见语义整体回收
- `07`
  再做基于单地域
  `service`
  的 front door
  路由发布与最小门控

## 这一章之后的平台语义

做到这里，
`mini-cloud`
先经历了一次重要但过渡性的对象建模：

- `service`
  暂时被抬成全局对象
- `cell`
  暂时承担了部署语义

这条线在当时的目标下有学习价值，
但它不会成为
`v6`
的最终资源模型。

后面几章的关系应该这样理解：

- `03`
  继续沿着这个中间模型，
  先把容量治理和供给侧约束说明白
- `05`
  继续沿着这个中间模型，
  先把 candidate release
  和 rollout
  语义说明白
- `06`
  再把
  `service -> cell`
  这套用户可见语义整体回收
- `07`
  才开始基于单地域
  `service`
  做 front door
  的路由发布、
  readiness gating
  和排障入口

## 检查点

- 提交：`11002424c632d298da819f0bab1dd2efffa0a513`
