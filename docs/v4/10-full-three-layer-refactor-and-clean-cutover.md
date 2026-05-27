# v4/10：完整三层重构与干净切换

这一章不是继续加新功能，
而是把 `v4/09`
里定下来的三层边界真正落到代码、脚本、测试和文档上。

做到这章之前，
仓库虽然已经在概念上分出了：

- `control plane`
- `cloud plane`
- `cloud worker`

但真实代码里仍然有几类残留：

- `control-plane`
  仍然直接带着 plane-local runtime 装配逻辑
- `controlplane`
  仍然直接依赖 `cloudplane` 内部包
- `provider`
  和 `config`
  这类运行时归属不清的包还挂在 `internal/`
  顶层
- 文档和脚本里还残留 `httpapi`
  `fleetdeploy`
  `fleetsync`
  这些旧名字

这章的目标很明确：

- 三层不仅目录分开，
  还要把编译依赖方向也收干净
- `control-plane`
  只保留 global / fleet
- `cloud-plane`
  真正独占 plane-local runtime
- `cloud-worker`
  继续只做执行
- 本地验证脚本、
  测试夹具、
  文档术语
  都切到当前真实结构

## 这章最终做完了什么

### 1. 三层目录骨架已经稳定

现在 `internal/`
的主要骨架是：

```text
internal/
  cloudplane/
    api/
    app/
    deployment/
    deployservice/
    execution/
    ingress/
    node/
    observability/
    platformconfig/
    platformteardown/
    processconfig/
    projecttoken/
    provider/
    registrycredential/
    revision/
    runtimeprovision/
    runtimeworker/
    runtimeworkerservice/
    scheduler/
    secretset/
    store/
    usage/
  cloudworker/
    client/
    runtime/
  controlplane/
    api/
    binding/
    deploy/
    incident/
    inventory/
    placement/
    plane/
    planeclient/
    planesync/
    processconfig/
    store/
  common/
    httpx/
  contract/
    planeapi/
    workerapi/
```

也就是说，
这章之后：

- 顶层不再有 `internal/provider`
- 顶层不再有 `internal/config`
- 顶层不再有旧的 `internal/httpapi`
- `controlplane`
  和
  `cloudplane`
  各自把自己的运行时依赖收回层内

### 2. `control-plane` 真正只做 global / fleet

当前的：

- [app.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/app.go)

现在只负责：

- 读取 `controlplane/processconfig`
- 打开 `controlplane/store`
- 跑 `controlplane/store/migrations`
- 启动 `planesync` 后台循环
- 装配 `controlplane/api`

它不再：

- 读取 `platformconfig`
- 初始化 provider runtime
- 初始化 worker provision / reclaim
- 挂 plane-local northbound
- 挂 worker southbound

换句话说，
`cmd/control-plane`
现在真的是一个 global / fleet 进程，
不再是“顺手把 cloud-plane 也装起来”的混合入口。

### 3. `cloud-plane` 真正独占 plane-local runtime

当前的：

- [app.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/app.go)
- [main.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/cmd/cloud-plane/main.go)

现在负责：

- 读取 `cloudplane/processconfig`
- 读取 `platformconfig`
- 初始化 `cloudplane/provider`
- 初始化 runtime worker provisioner
- 初始化 `platformteardown`
- 装配 `cloudplane/api`
- 对外提供：
  - plane-local northbound
  - `control plane -> cloud plane`
    southbound
  - `cloud plane -> cloud worker`
    southbound

也就是说，
`cloud-plane`
已经是一个真实的独立运行体，
不是原来那个“挂在 control-plane 里的第二个名字”。

### 4. `control plane -> cloud plane` 的 southbound 已经回到调用方一侧

这章一个很关键的清扫是：

- southbound contract
  保留在：
  - [planeapi](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/contract/planeapi)
- southbound client
  放在：
  - [planeclient](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/planeclient)

这里要特别注意一件事：

- `planeclient`
  现在属于 `controlplane`
  不是 `cloudplane`

原因很直接：

- southbound client
  是调用方的实现细节
- `control-plane`
  是调用方
- `cloud-plane`
  是被调方

所以把 client
放回：

- `internal/controlplane/planeclient`

才是自然的归属。

这一刀也顺手切掉了：

- `controlplane`
  直接依赖 `cloudplane/client`
  的旧关系

当前使用点主要是：

- [service.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/deploy/service.go)
- [http_fetcher.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/planesync/http_fetcher.go)

### 5. `controlplane` 已经不再直接依赖 `cloudplane/app`

这章最重要的一刀之一，
是把 `controlplane`
从 `cloudplane` 的内部领域模型里摘出来。

现在：

- [apply.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/deploy/apply.go)

已经在 `controlplane/deploy`
里自己定义：

- app apply 输入
- `InstanceClass`
- 资源请求换算
- 参数校验错误

并直接产出：

- `planeapi.ApplyAppRequest`

这意味着：

- `controlplane`
  不再去 import
  `cloudplane/app`
- `controlplane`
  不再借 `cloudplane/app`
  的 `CreateInput` / `UpdateInput`
  做校验

这样做会有一点点重复，
但这正是这里应该接受的代价：

- 要的是所有权清晰，
  不是跨层共用一个“方便的内部包”

### 6. `provider` 和 `processconfig` 已经按层收回

这章以前，
下面这两类东西都还挂在 `internal/`
顶层：

- `provider`
- `config`

现在已经明确改成：

- `controlplane/processconfig`
- `cloudplane/processconfig`
- `cloudplane/provider`

对应代码在：

- [config.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/processconfig/config.go)
- [config.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/processconfig/config.go)
- [provider.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/provider/provider.go)
- [builtins.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/provider/builtins.go)

这一步的意义不是换个目录，
而是把运行时装配关系说清楚：

- `controlplane/processconfig`
  只服务 `control-plane`
- `cloudplane/processconfig`
  只服务 `cloud-plane`
- `cloudplane/provider`
  只服务 `cloud-plane`
  的 runtime worker provision / reclaim

这样后面继续做 provider 扩展时，
不会再把“共享基础能力”
和
“cloud-plane 运行时插件”
混在一起。

### 7. `cloud-plane` 内部保留 `deployservice` / `runtimeworkerservice`，但它们不是第四层

这一点必须说清楚，
否则读者很容易误会。

当前 `cloud-plane`
的内部结构不是：

- `api + store + model`

而是：

- `api`
  ->
  `application service`
  ->
  `store / model`

这里的 application service
就是：

- [deployservice](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/deployservice/service.go)
- [runtimeworkerservice](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/runtimeworkerservice/service.go)

要强调两点：

1. 这两个 `service`
   不是新的网络服务

2. 它们也不是“第四层”

它们只是 `cloud-plane`
这一层内部的编排服务，
用来承接：

- handler 不该直接写进去的长流程
- 多 store / 多模型联动
- 调度、发布、scale-out、teardown、reclaim
  这类应用编排逻辑

具体来说：

- `deployservice`
  负责：
  - deployment 创建
  - scheduler 调用
  - placement 持久化
  - 必要时 runtime scale-out
  - teardown 活跃时的阻断

- `runtimeworkerservice`
  负责：
  - runtime worker 视图
  - 手动 reclaim
  - 平台 teardown 时的批量回收
  - linked node 的 drain / offline 协调

之所以保留这层，
是因为前面已经实际证明：

- 如果把这些逻辑硬塞回 `deployment`
  或
  `runtimeworker`
  模型包，
  很容易把 store / model / orchestration
  再揉回一起，
  甚至引入 import cycle

所以这一步不是“多长出一层”，
而是把同层内部的责任切开。

### 8. `common/httpx` 仍然只保留 HTTP 机械胶水

这章没有把权限、项目语义、审计语义
重新抽成一个脏公共层。

真正被抽出去的，
只有：

- [httpx](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/common/httpx)

里这些不懂业务的 HTTP 机械件，
例如：

- `WriteJSON`
- `RecoverPanics`
- request logger
- bearer header parser
- bounded query parser
- log query parser
- response capture helper

这保证了：

- `controlplane/api`
  和
  `cloudplane/api`
  可以去掉重复胶水
- 但不会因为“复用”
  又把两层的资源语义重新揉回一起

### 9. 脚本和测试布局已经跟上当前结构

本章不仅改了生产代码，
也把本地验证链同步收口了。

当前：

- [test-integration.sh](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/scripts/test-integration.sh)
  已经跑这些真实 Postgres 集成包：
  - `./internal/controlplane/store`
  - `./internal/cloudplane/store`
  - `./internal/cloudplane/api/worker`
  - `./internal/controlplane/placement`
  - `./internal/controlplane/api`
  - `./internal/cloudplane/api`

另外，
当前测试布局也已经比较清楚：

- `fleet / global`
  的集成测试在：
  - [integration_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/api/integration_test.go)
- `cloud-plane`
  自己的 northbound / southbound
  集成测试在：
  - [integration_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/integration_test.go)

## 这章做完后，三层各自负责什么

### `control plane`

负责：

- global project
- fleet plane registry
- fleet binding
- fleet placement
- fleet incident
- fleet operations
- plane sync

不再负责：

- provider runtime
- runtime worker provision / reclaim
- plane-local northbound
- worker southbound

### `cloud plane`

负责：

- plane-local project / app / deployment / execution
- node / runtime worker
- ingress / gateway
- provider runtime
- platform teardown
- plane southbound
- worker southbound

### `cloud worker`

继续只负责：

- register
- heartbeat
- poll work
- report execution

## 这一章验证了什么

这章当前至少验证过：

```bash
go test ./cmd/... ./internal/...
```

以及：

```bash
./scripts/check.sh
```

以及真实 Postgres 集成验证：

```bash
bash ./scripts/test-integration.sh
```

这说明：

- 三层最新目录和依赖关系可以完整编译
- `provider/processconfig/planeclient`
  这些归属调整没有打断主链
- `test-integration.sh`
  已经跟上当前包布局
- `controlplane/api`
  与
  `cloudplane/api`
  都还能通过当前集成测试

## 这章结束时的状态判断

做到这里，
`v4/10`
可以认为已经完成：

- 三层运行体已经真正切开
- `controlplane`
  不再直接依赖 `cloudplane`
  内部包
- southbound client
  已经回到 `controlplane`
  一侧
- `provider`
  和
  `processconfig`
  都已经按层收回
- `cloud-plane`
  内部的应用编排服务
  已经明确留在本层内部，
  而不是继续和 model / store 混写
- 脚本、
  测试、
  文档术语
  已经对齐到当前真实结构

目前仍然可以继续打磨的低优先级项只有：

- `controlplane`
  的 northbound 响应里，
  还有少量 southbound `planeapi`
  DTO 复用

这不是本章的 blocker，
但如果后面 southbound contract
要独立演进，
可以再单独开一章把这层映射拆出来。

## 本章涉及的主要文件

- [app.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/app.go)
- [app.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/app.go)
- [config.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/processconfig/config.go)
- [config.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/processconfig/config.go)
- [router.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/api/router.go)
- [router.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/router.go)
- [types.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/contract/planeapi/types.go)
- [client.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/planeclient/client.go)
- [service.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/deploy/service.go)
- [apply.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/deploy/apply.go)
- [service.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/planesync/service.go)
- [http_fetcher.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/planesync/http_fetcher.go)
- [provider.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/provider/provider.go)
- [builtins.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/provider/builtins.go)
- [service.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/deployservice/service.go)
- [service.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/runtimeworkerservice/service.go)
- [test-integration.sh](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/scripts/test-integration.sh)

## 本章检查点

- `cmd/control-plane`
  和
  `cmd/cloud-plane`
  已经是两个真实运行体
- `controlplane`
  的生产代码
  不再直接 import
  `cloudplane`
  内部包
- `controlplane`
  通过
  `controlplane/planeclient`
  +
  `contract/planeapi`
  调用 `cloud-plane`
- `controlplane/processconfig`
  和
  `cloudplane/processconfig`
  已分开
- `cloudplane/provider`
  已经收回 `cloud-plane`
  层内
- `cloud-plane`
  里保留 `deployservice`
  和
  `runtimeworkerservice`
  作为同层应用编排服务，
  不是第四层
- `common/httpx`
  仍然只承载 HTTP 机械胶水
- `./scripts/test-integration.sh`
  已经跟上当前包布局
- `go test ./cmd/... ./internal/...`
  已通过
- `./scripts/check.sh`
  已通过
- `bash ./scripts/test-integration.sh`
  已通过
- 提交：
  - 待提交后补充
