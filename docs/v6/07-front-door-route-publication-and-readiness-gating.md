# 07 Front Door Route Publication And Readiness Gating

这一章不做“服务切换”。

原因很简单：

- 当前平台没有正式的多 backend /
  主备 /
  权重 /
  回切
  设计
- `service`
  在 `06`
  之后已经重新收回成：
  - 单 provider
  - 单 region
  - 单长期运行服务
- 所以这一章最自然的目标，
  不是假装平台已经具备 failover，
  而是把 front door
  先收成一个干净的入口层对象

也就是说，
这一章只解决三个问题：

1. route
   什么时候应该被真正发布
2. route
   为什么当前还不能被发布
3. 用户应该去哪里看这些状态

## 这一章要收什么能力

这一章把 front door
明确收成：

- 路由声明对象
- readiness /
  exposure /
  placement
  门控器
- route
  发布状态的排障入口

具体说，
这一章完成后：

- 用户仍然按
  `service`
  维度创建 route
- control-plane
  会根据当前 service
  的真实状态，
  决定这条 route
  是：
  - `pending`
  - `planned`
  - `blocked`
  - `synced`
  - `error`
- 用户可以在
  `project`
  级直接查看整个项目下所有 route
  的发布状态，
  而不是逐个 service
  翻

## 这一章明确不做什么

这一章明确不做：

- 自动主备切换
- 多 backend
  故障转移
- 权重流量分配
- 回切策略
- 会话保持
- 连接排空
- 跨地域入口调度

如果后面要做这些能力，
必须先单独引入新的流量模型，
而不是把“切换”直接塞进当前
`front door route -> single service`
这套语义里。

## 当前 front door 的真实模型

这章之后，
front door
的最小模型应该这样理解：

1. `service`
   - 是部署对象
   - 决定：
     - 运行在哪个 provider / region
     - 是否 public
     - 当前是否 ready
2. `front door route`
   - 是入口声明对象
   - 决定：
     - `externalHost`
     - `pathPrefix`
     - `stripPrefix`
     - `enabled`
3. `resolved route`
   - 是 controller
     根据 service /
     placement /
     plane ingress
     算出来的当前发布目标
   - 决定：
     - `targetPlaneID`
     - `targetOrigin`
     - `targetHost`

也就是说，
route
不是直接写死指向一个公网地址，
而是始终引用某个 service，
再由 controller
在当前真实状态下解析。

## 这一章的门控规则

当前 front door
只在下面这些条件都满足时，
才会把 route
推进到真正可同步状态：

1. route
   自己是 `enabled`
2. service
   仍然存在
3. service
   是 `public`
4. service
   没在删除中
5. service
   已经 `ready`
   且
   `healthy`
6. service
   已经有 placement
7. placement
   已经拿到了
   `publishedHost`
8. 对应 plane
   仍然存在
9. plane ingress
   里有可用的
   `publicOrigin`

只要其中任何一步不满足，
route
就不会被“切到别的 service”，
而是直接停在：

- `blocked`
  或
- `planned`

并把原因写到
`syncMessage`
里。

这就是这一章的核心边界：

- 有门控
- 有状态
- 有原因
- 没有切换

## 新增的排障入口

这一章新增一个 project
级 route
视图：

- `GET /api/v1/projects/{projectID}/frontdoor-routes`

它返回：

- route
  自己的声明
- 归属的 service
  基本信息
- 当前 `syncStatus`
- 当前 `syncMessage`
- 当前解析出的：
  - `targetPlaneID`
  - `targetOrigin`
  - `targetHost`

这意味着现在排障不需要再：

- 先列 service
- 再逐个 service
  看 route

也就是说，
它的定位不是新的
route desired state
写入口，
也不是替代
`GET /api/v1/projects/{projectID}/services`
里的内联 route
信息，
而是一个：

- project
  级
- route-centric
- 扁平化
  排障投影

而是可以直接从项目视角看到：

- 哪条 route
  已发布
- 哪条 route
  还在 blocked
- 它到底是卡在：
  - service private
  - service not ready
  - 没 placement
  - 没 published host
  - plane ingress
    缺失

## 这一章的 northbound 契约

这章之后，
front door
相关 northbound API
应该这样理解：

### service 级 route 管理

- `GET /api/v1/projects/{projectID}/services/{serviceID}/frontdoor-routes`
- `POST /api/v1/projects/{projectID}/services/{serviceID}/frontdoor-routes`
- `PUT /api/v1/projects/{projectID}/services/{serviceID}/frontdoor-routes/{routeID}`
- `DELETE /api/v1/projects/{projectID}/services/{serviceID}/frontdoor-routes/{routeID}`

这一组接口负责：

- 写 route desired state
- 看某个 service
  自己挂了哪些 route

### project 级 route 排障视图

- `GET /api/v1/projects/{projectID}/frontdoor-routes`

这一组接口负责：

- 从整个 project
  视角看 route
  是否已经发布
- 看 route
  当前阻塞原因
- 看 route
  当前解析出来的目标
- 做 route
  级的聚合扫描与排障

## 这一章影响的代码面

- [frontdoor.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/frontdoor/frontdoor.go)
- [controller.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/frontdoorcontroller/controller.go)
- [reconciler.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/frontdoorcontroller/reconciler.go)
- [service_handler.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/api/service_handler.go)
- [router.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/api/router.go)
- [frontdoor_store.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/store/frontdoor_store.go)
- [integration_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/api/integration_test.go)
- [controller_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/frontdoorcontroller/controller_test.go)
- [reconciler_integration_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/frontdoorcontroller/reconciler_integration_test.go)

## 这一章的测试重点

这一章重点验证：

1. public +
   ready +
   placed
   的 service
   route
   可以进入 `synced`
2. private
   service
   route
   会被 `blocked`
3. disabled
   route
   会停在 `planned`
4. adapter
   短暂失败后，
   periodic reconcile
   能把 route
   重新推进到 `synced`
5. project
   级 route
   视图能直接返回：
   - service 引用
   - 当前 sync 状态
   - 当前解析目标

## 这一章结束后的平台理解方式

做到这里以后，
front door
应该被理解成：

- 一个“按 service
  发布入口”的对象
- 一个“用 service
  readiness / exposure / placement
  做门控”的对象
- 一个“给 route
  发布失败提供直接诊断视图”的对象

而不是：

- 一个已经具备多 backend
  自动切换能力的流量系统

先把这层边界收干净，
后面如果真要做更复杂的入口控制，
才不会把 failover
和当前单 service
route
语义搅在一起。

## 检查点

- 待本章提交时回填 commit hash。
