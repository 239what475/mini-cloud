# 01 Controller Reconcile Model v2

到 `v5`
结束时，
`mini-cloud`
已经能把全局 `service`
放到某个 `cloud-plane`
上，
也已经有：

- placement
- front door route
- 回滚
- 项目资源绑定

但这里面一直有一个核心问题没有真正收干净：

- `fleet control-plane`
  里的 `service`
  写路径，
  还是太像“请求触发动作”

也就是说，
之前的 `create / update / delete / rollback`
更像是：

- API 请求一进来
- 立刻做 placement
- 立刻调远端 apply / delete / rollback
- 本地写和远端副作用强绑定

这会直接带来三个问题：

1. 请求语义太重
   - 一次普通写请求，
     实际上在做跨层、跨网络、跨系统的同步动作
2. 状态语义不清楚
   - 本地对象里没有稳定的
     `generation / observedGeneration / conditions`
     语义
3. 删除和失败处理不干净
   - 删除更像“同步清理命令”
   - 失败时只能在请求里补偿，
     很难自然形成后台收敛模型

所以 `v6/01`
先不碰大规模目录清扫，
只做这一件事：

- 把 `fleet service`
  真正改成一个
  “写 desired state，
  后台 reconcile observed state”
  的控制器对象

## 这一章做了什么

这一章把 `fleet service`
补成了真正有控制器语义的资源。

现在这个对象除了原来的服务规格字段，
还正式有了：

- `generation`
- `desiredState`
- `status`
  - `observedGeneration`
  - `phase`
  - `healthy`
  - `message`
  - `conditions`
  - `lastReconciledAt`

对应代码主要在：

- [fleetservice/service.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/fleetservice/service.go)
- [fleet_service_store.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/store/fleet_service_store.go)
- [controller.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/fleetservicecontroller/controller.go)
- [reconcile.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/fleetservicecontroller/reconcile.go)
- [rollback.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/fleetservicecontroller/rollback.go)

## 新的请求语义

### create

现在 `create`
只做两件事：

1. 写入本地 `fleet_service`
   记录
2. 触发后台 reconcile

它不再在请求里同步做：

- placement
- 远端 apply

所以创建后的第一份对象，
天然就是：

- `desiredState=active`
- `generation=1`
- `status.phase=pending`
- `status.observedGeneration=0`

这表示：

- 期望已经写下来了
- 但 controller
  还没把这一代真正观察并收敛完

### update

`update`
现在也只写期望状态。

它会：

- 更新服务规格
- `generation + 1`
- 保留上一次已经观察到的
  `observedGeneration`
- 把状态打回新的 pending
  语义

也就是说，
`update`
成功不再等价于：

- 新版本已经下发到远端

它只等价于：

- 新的 desired spec
  已经被接受

### delete

`delete`
现在改成了两阶段语义。

请求本身只做：

- `desiredState=deleted`
- `generation + 1`
- `status.phase=deleting`
- 保留删除前最后一次已经观察到的
  `observedGeneration`

真正的远端删除和本地最终硬删除，
由后台 reconcile 完成。

所以 `DELETE /services/{id}`
现在返回的是：

- 删除请求已接受

而不是：

- 一切已经同步清理完成

### rollback

`rollback`
也不再直接在请求里执行远端回滚。

它现在会：

1. 读取远端当前 release
   和 revision 列表
2. 找到上一个 revision
3. 把那个 revision
   重新翻译回本地 desired spec
4. 写回本地对象并触发后台 reconcile

所以回滚也正式纳入了：

- “改期望状态，
  而不是直接发命令”

这一套统一模型

## 新的后台 reconcile 语义

这一章新增了 `fleetservicecontroller.Controller.Run`。

`control-plane`
启动时，
它会像其他后台 loop 一样常驻运行：

- 周期性扫描所有 `fleet service`
- 也支持被写路径主动 `Trigger`

对应启动装配在：

- [app.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/app.go)

reconcile 的主逻辑现在是：

1. 如果 `desiredState=deleted`
   - 先删远端
   - 成功后再删本地
2. 如果还没有 placement
   - 做 placement 预览
   - 成功后 apply 到目标 plane
   - 没有可用 plane 时，
     把失败写进 `status`
     而不是把失败只留在请求返回里
3. 如果 `generation > observedGeneration`
   - 说明有新的 desired spec
   - 优先尝试在当前 placement
     上重下发
   - 当前 placement
     不再可复用时，
     再做重新放置
4. 如果当前代已经观察过
   - 走远端 `GetService`
     刷新 observed status

另外这一章还补了一条很重要的并发保护：

- status 和 placement
  的回写都带
  `expected generation`
  比较
- 过期 reconcile
  结果不会再覆盖更新后的新一代状态

这意味着：

- 请求路径负责“写期望”
- 后台 controller
  负责“看现在实际怎么样，
  再一点点收敛过去”

## 这一章定义的状态语义

目前 `phase`
先收成下面几种：

- `pending`
  - 期望已写入，
    但还没有成功放置/下发
- `progressing`
  - 远端对象已存在，
    但还没健康
- `ready`
  - 当前代已经被观察，
    且远端健康
- `degraded`
  - 当前代已经被 controller
    观察到，
    但下发失败、
    远端丢失、
    或远端不健康
- `deleting`
  - 已收到删除意图，
    正在后台回收

`observedGeneration`
的语义也定下来了：

- 它表示 controller
  已经处理到哪一代 desired spec

所以即使某一代处理失败，
也仍然会把失败结果写进：

- `phase`
- `message`
- `conditions`
- `observedGeneration`

而不是把失败只留在某次请求返回里。

## front door 在删除中的新语义

以前如果服务对象还在，
front door
有机会继续看到旧 placement。

现在补了一条明确规则：

- `desiredState=deleted`
  的服务，
  `front door`
  必须直接阻断

对应逻辑在：

- [frontdoorcontroller/controller.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/frontdoorcontroller/controller.go)

这样即使本地对象还没被后台最终删除，
也不会再继续把旧 target
暴露出去。

## 这一章的测试

这一章新增了一组不依赖外部 Postgres
的 controller 单元测试：

- [controller_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/fleetservicecontroller/controller_test.go)

它们覆盖了这章最关键的行为：

- create 只写 desired state，
  不同步 placement/apply
- reconcile 成功后补 placement
  和 observed status
- update 不会把 apply 失败直接塞回主写路径
  而是由后台 reconcile
  把失败写进 status
- delete 先记删除意图，
  后台 reconcile
  再做最终删除
- rollback 改成“重写 desired spec”
  而不是立即远端回滚

此外还跑过整套项目检查：

- `./scripts/check.sh`

## 这一章结束后，平台有什么变化

到这里为止，
`fleet service`
终于不再只是一个：

- 会在请求里顺手调远端系统的资源包装

而是正式变成了：

- 有 desired state
- 有 observed state
- 有 generation
- 有 conditions
- 有后台 controller
  收敛的对象

这一步做完以后，
后面的章节才有稳定基础去继续做：

- 服务拓扑
- 放置策略
- 渐进式发布
- front door
  健康切换

## 本章检查点

本章代码检查点会在提交 `v6/01`
后回填。
