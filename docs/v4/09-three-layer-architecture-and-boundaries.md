# v4/09 Three-Layer Architecture And Boundaries

做到 `v4/08` 为止，
`mini-cloud`
已经把这些能力跑起来了：

- fleet 资源模型
- plane 注册与同步
- 全局 inventory
- 全局日志、指标、告警
- manual target deploy
- explainable auto placement

但这时候有一个更根本的问题已经绕不过去了：

- 当前实现已经同时长出了：
  - 全局 fleet / control 能力
  - 单 provider / 单 region 的 plane 能力
  - worker 执行能力
- 但这三类能力还没有被进程、路由、存储、配置真正隔开

所以这一章不继续加功能，
而是先把下一阶段的唯一目标形态定死。

这章的定位不是：

- 讲兼容迁移路线
- 讲临时过渡方案

而是：

- 明确三层是什么
- 明确每层拥有什么
- 明确每层不能碰什么
- 明确 `v4/10`
  重构时必须遵守的边界

## 这一章先回答什么问题

这一章只回答下面这些“架构宪法”问题：

- `control plane`
  到底负责什么
- `cloud plane`
  到底负责什么
- `cloud worker`
  到底负责什么
- northbound / southbound API
  怎么分
- 状态和数据库怎么分
- 调度权归谁
- 云厂商凭证归谁
- 失败语义归谁
- `cmd/`
  和 `internal/`
  最终要长成什么结构

这一章不回答：

- 具体代码怎么迁
- 哪些文件先挪、后挪
- 重构过程中的兼容层怎么写

这些都留给：

- `v4/10`

## 当前实现的问题到底是什么

当前 `mini-cloud`
不是“三层明确分治”，
而更像：

- 一个大进程里混着：
  - 顶层 fleet / control 逻辑
  - 单云 plane 逻辑
  - worker southbound 接口

这个问题在现有代码里非常直接：

- [main.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/cmd/control-plane/main.go)
  只启动了一个 `control-plane`
  进程
- [app.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/app.go)
  却在同一次 `Build`
  里同时做了：
  - 读进程配置
  - 读 provider / runtime 配置
  - 初始化 provider provisioner
  - 初始化数据库
  - 启动 fleet sync
  - 挂整套 HTTP API
- [router.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/router.go)
  同一个 mux
  里同时暴露了：
  - fleet API
  - project / app API
  - node register / heartbeat / work / report
  - runtime worker 回收
  - platform teardown
  - gateway

这说明今天的：

- `cmd/control-plane`

本质上并不是未来的：

- 纯 `control plane`

而是：

- `control plane + cloud plane`
  的单体混合形态

另一方面：

- [main.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/cmd/agent/main.go)

已经比较接近未来的：

- `cloud worker`

但它现在暴露出来的 southbound contract
仍然是匿名信任、结构体复用、边界不清的状态。

## 本章定下来的三层模型

从这一章开始，
`mini-cloud`
正式定义成三层：

### 1. `control plane`

它是全局入口。

它负责：

- 全局项目与全局部署意图
- 多个 `cloud plane`
  的注册、绑定、观察、选址
- 全局 inventory
- 全局 incident / audit / SLO
- 全局运维动作入口

它不负责：

- 直接调云厂商 SDK
- 直接管理 worker
- 直接决定某个 workload
  跑到哪台 worker

### 2. `cloud plane`

它是单 provider、单 region
的本地控制面。

它负责：

- 本地 project / app / revision / deployment / execution
- 本地 node / runtime worker / gateway
- 本地 provider API
- 本地 rollout / retry / reclaim / scale-out
- 对上暴露 plane southbound API
- 对下调度 `cloud worker`

它不负责：

- 全局多 plane 选址
- 全局 incident / audit / SLO
- 跨 provider 统一调度

### 3. `cloud worker`

它是数据面执行节点。

它负责：

- 注册
- 心跳
- 拉 work
- 执行容器
- 回报结果

它不负责：

- 全局资源视图
- plane 级调度决策
- 云厂商资源管理

## 资源归属必须怎么分

这一章先把资源归属定死。

### `control plane` 拥有的权威状态

- global project
- global deployment intent
- plane registry
- project-plane binding
- plane health snapshot
- plane capacity snapshot
- global incident
- global audit event

这里特别强调：

- `control plane`
  拥有的是：
  - global intent
  - global projection
- 它不拥有：
  - node
  - runtime worker
  - deployment execution
  这类 plane-local 运行态

### `cloud plane` 拥有的权威状态

- plane-local project 映射
- app
- revision
- deployment
- execution
- domain / ingress / gateway
- node
- runtime worker
- provider resource ID
- plane-local usage / counters / metrics

这里要明确一件事：

- 当前仓库里的：
  - `project`
  - `app`
  - `revision`
  - `deployment`
  这套模型
- 在未来默认都先视为：
  - `cloud plane`
    的本地模型

也就是说：

- 后面的 `control plane`
  如果要有自己的全局部署模型，
  应该定义自己的 global model
- 不能继续直接复用 plane-local model

### `cloud worker` 拥有的本机短期状态

- 本机注册状态
- 当前 work lease
- 本地容器 / 进程状态
- 尚未上报的执行结果

`cloud worker`
不拥有任何集群级权威状态。

## 调度边界必须怎么分

这一章把调度权也定死。

### `control plane` 只选 `cloud plane`

它的输入可以包括：

- provider
- region
- capacity snapshot
- policy
- SLO
- maintenance / incident 状态

它不能做：

- 直接选 `cloud worker`
- 直接触发 provider scale-out
- 直接决定某个 execution
  落在哪个 node

### `cloud plane` 只选 `cloud worker`

它负责：

- 本地 placement
- rollout
- retry
- scale-out
- reclaim

也就是说：

- 顶层选 plane
- plane 内部再选 worker

不能再出现：

- `control plane`
  直接越过 `cloud plane`
  去挑 worker

### `cloud worker` 没有调度权

它只能：

- 执行
- 心跳
- 上报

不能自己决定：

- 何时扩容
- 何时迁移
- 何时重试

## API 边界必须怎么分

这是这章最关键的边界之一。

未来必须同时存在三类 API，
而且三类 API
不能再混在一个包里。

### 一类：用户面 northbound API

这类 API
服务对象是：

- 用户
- 管理员
- Terraform provider
- Web UI

它们属于：

- `control plane`
  的全局 northbound API
- `cloud plane`
  的 plane-local admin / ops API

这里要明确收紧一条默认规则：

- 用户
- Web UI
- Terraform provider

默认都只打：

- `control plane`

`cloud plane`
如果保留 northbound API，
也只允许作为：

- plane-local admin / ops

入口，
不再承担全局统一入口角色。

### 二类：`control plane -> cloud plane` southbound API

这是一套独立 contract。

它只应该暴露：

- plane register / heartbeat / status
- plane health / overview / capacity snapshot
- project binding apply
- global intent 下发到 plane
- 必要的 plane admin 动作

它不应该继续复用：

- `/projects/*`
- `/apps/*`
- `/platform/config`

这类面向人类或 plane 内部的 northbound API。

### 三类：`cloud plane -> cloud worker` southbound API

这也是一套独立 contract。

它只应该暴露：

- worker join
- worker heartbeat
- poll work
- report result

它不应该继续复用：

- plane 自己的 northbound project / app API

它也不应该继续保持：

- 匿名信任
- 无独立 worker auth

## southbound contract 不允许再直接复用哪些内部模型

从这章开始，
下面这些结构体都不应继续直接穿过层边界：

- `internal/app.App`
- `internal/node.Node`
- `internal/node.PlatformNode`
- `internal/platformconfig.Config`
- `internal/runtimeworker.Record`
- `internal/observability.Overview`

原因很简单：

- 它们都是层内业务模型
- 不是跨层 contract

所以未来需要单独收口成：

- `control plane <-> cloud plane`
  contract DTO
- `cloud plane <-> cloud worker`
  contract DTO

## 全局对象和 plane-local 对象怎么映射

这一章再把对象映射关系写死，
避免下一章又把 global model
和 plane-local model
重新混在一起。

| 全局对象 | plane-local 投影 / 落点 | 关联键 |
| --- | --- | --- |
| `global_project` | `plane_local_project` | `global_project_id` + `plane_id` + `remote_project_id` |
| `global_deployment_intent` | `plane_local_app` + `plane_local_deployment` | `global_deployment_intent_id` + `plane_id` + `app_id` + `deployment_id` |
| `plane_registry` | `cloud plane` 自身注册记录 | `plane_id` |
| `plane_capacity_snapshot` | `cloud plane` 汇总出来的本地容量视图 | `plane_id` + `snapshot_id` |
| `global_incident` | `cloud plane` 上报的本地故障投影 | `incident_id` + `plane_id` |

这张表真正要表达的是：

- `control plane`
  不直接拥有 plane-local app / deployment / execution
- 它只拥有：
  - global 对象
  - 到 plane-local 对象的映射关系
- `cloud plane`
  也不能假装自己拥有 global intent

## 数据库边界必须怎么分

这一章也明确要求：

- `control plane`
  和 `cloud plane`
  必须分库存放

这在 `v4/10`
里要彻底结束。

这里的“分库”
最少要同时满足：

- 独立连接
- 独立 migration history
- 独立 schema ownership
- 独立 store 实现

目标边界应该是：

### `control plane` 数据库

只放：

- global project
- plane registry
- project-plane binding
- plane snapshot
- global incident
- global audit
- global deployment intent

### `cloud plane` 数据库

只放：

- app / revision / deployment / execution
- node
- runtime worker
- ingress / gateway
- plane-local usage / counters
- provider runtime bookkeeping

### `cloud worker`

不直接写数据库。

它只通过：

- worker southbound API

把状态回报给 `cloud plane`。

## 凭证边界必须怎么分

这也是一个必须先钉死的边界。

### `control plane`

只持有：

- 全局用户认证信息
- `control plane -> cloud plane`
  的 southbound 凭证

这里的 southbound 凭证
应该是：

- plane-scoped service credential

不应该继续是：

- remote project token

### `cloud plane`

才允许持有：

- provider 凭证
- provider instance role
- worker bootstrap / join credential
- artifact 分发权限
- plane-local admin token

也就是说：

- 阿里云 / 腾讯云 SDK
  只能出现在 `cloud plane`

### `cloud worker`

只允许持有：

- 短期 join credential
- workload 运行所需的最小 secret

这里也再补一条原则：

- worker credential
  必须由 `cloud plane`
  签发或分发
- 它必须是短期有效的
- 它只能访问：
  - worker southbound API

绝不允许持有：

- provider credential
- plane admin token
- global admin token

## 失败语义必须怎么分

这一章还要把“谁有资格判定什么失败”定死。

### `control plane`

只判定：

- plane 级故障

例如：

- plane register 失败
- plane snapshot 超时
- plane health 长时间异常

它的动作应该是：

- 把 plane 标成：
  - `registering`
  - `ready`
  - `degraded`
  - `offline`
- 停止对这个 plane
  下发新的 placement

它不应该做：

- 直接把某个 worker 判死
- 直接把某个 deployment 判失败

### `cloud plane`

它是下面这些状态迁移的唯一裁决层：

- worker offline
- deployment failed
- runtime worker reclaim failed
- gateway unhealthy
- provider API failure

也就是说：

- 所有 plane-local 运行态迁移
  都只能由 `cloud plane`
  自己写入

### `cloud worker`

只负责报告：

- bootstrap 失败
- 容器启动失败
- healthcheck 失败
- work 执行失败

最终的：

- offline
- reclaim
- retry
- failover

都不由它裁决。

## ID 边界也要定清楚

这一章再补一个容易忽略、但后面一定会撞墙的点：

- global ID
  和 plane-local ID
  不能继续混着理解

至少先定这条规则：

- `plane_id`
  是全局唯一
- `global_project_id`
  是全局唯一
- `global_deployment_intent_id`
  是全局唯一
- `worker_id`
  只要求在某个 plane 内唯一
- `node_id`
  只要求在某个 plane 内唯一
- `app_id`
  默认先视为某个 plane 内唯一

所以未来所有跨层事件和日志，
至少都必须能带上：

- `plane_id`
- `global_project_id`
- `global_deployment_intent_id`
- `worker_id`

如果事件已经进入 plane-local 运行阶段，
则还应该能继续补齐：

- `app_id`
- `deployment_id`
- `node_id`

否则三层拆开以后，
日志和事件就会串不起来。

## 配置边界必须怎么分

当前配置也还是单体平台思路：

- [config.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/config/config.go)
- [platformconfig.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/platformconfig/platformconfig.go)

`v4/10`
以后要改成按进程拆：

### `control plane` 配置

- HTTP listen
- DB
- 全局 auth
- fleet / plane southbound
- global observability

### `cloud plane` 配置

- HTTP listen
- DB
- provider runtime
- gateway
- artifact source
- worker join / bootstrap
- plane-local observability

### `cloud worker` 配置

- plane API endpoint
- worker credential
- heartbeat / work interval
- runtime engine

## 目标进程结构

从这一章开始，
目标二进制结构也正式定下来。

至少应该有：

```text
cmd/
  control-plane/
  cloud-plane/
  cloud-worker/
  terraform-provider-mini-cloud/
```

这里特别说明：

- `terraform-provider-mini-cloud`
  不属于三层本体
- 它只是 northbound adapter

## 目标包结构

这一章不要求现在就完成代码迁移，
但要把目标结构先定死。

一个可接受的目标结构应该至少接近这样：

```text
internal/
  controlplane/
    api/
    application/
    domain/
    store/
  cloudplane/
    api/
      northbound/
      worker/
      controlsouthbound/
    application/
    domain/
    store/
    provider/
    gateway/
  cloudworker/
    bootstrap/
    runner/
    client/
  contract/
    planeapi/
    workerapi/
  common/
    logctx/
    httpx/
    idgen/
```

这里真正要表达的是：

- 按层拆
- 再按职责拆

而不是继续把所有东西塞进：

- `httpapi`
- `store`
- `config`

这种跨层大包里。

## import 方向也必须限制

只给目录树还不够，
这一章再把依赖方向定死。

允许的方向是：

- `controlplane/*`
  可以依赖：
  - `contract/*`
  - `common/*`
  - 自己层内包
- `cloudplane/*`
  可以依赖：
  - `contract/*`
  - `common/*`
  - 自己层内包
- `cloudworker/*`
  可以依赖：
  - `contract/*`
  - `common/*`
  - 自己层内包

禁止的方向是：

- `controlplane/*`
  直接依赖：
  - `cloudplane/provider/*`
  - `cloudplane/domain/*`
  - `cloudplane/store/*`
- `cloudplane/*`
  直接依赖：
  - `controlplane/*`
- `cloudworker/*`
  直接依赖：
  - `controlplane/*`
  - `cloudplane/domain/*`
  - `cloudplane/store/*`
- `contract/*`
  反向依赖任何业务层

也就是说：

- `contract`
  只能是纯 DTO / contract
- `common`
  只能是无业务归属的基础能力
- 任何层内 model
  都不能再伪装成“共享模型”

## `v4/10` 必须遵守的禁止事项

这一章最后把下一章的硬约束列出来。

`v4/10`
必须做到：

- 不再保留“旧 API + 新 API”
  长期并存的兼容层
- 不再让 `control plane`
  直接链接 provider 实现
- 不再让 `control plane`
  直接使用 plane-local model
- 不再让 `cloud plane`
  承载 fleet/global 职责
- 不再让 `cloud worker`
  直接理解全局项目 / plane / fleet 布局
- 不再保留一套“一库通吃”的 `store.Store`
- 不再保留一个“什么都挂”的 `httpapi`
- 不再保留单体配置模型
- 不再复用 northbound API
  充当 southbound contract

如果做完 `v4/10`
以后，
这些问题还存在，
那就说明重构没有真正完成。

## `v4/10` 的完成判定

除了上面的禁止事项，
`v4/10`
还必须满足下面这些正向判定：

- `control plane`
  可以独立启动
- `cloud plane`
  可以独立启动
- `cloud worker`
  可以独立启动
- `control plane -> cloud plane`
  和 `cloud plane -> cloud worker`
  已经有独立的：
  - listener
  - router
  - auth
- `control plane`
  与 `cloud plane`
  已经有独立的：
  - config
  - store
  - migrations
  - database connection
- `control plane`
  已经不再 import
  provider 实现
- 跨层传输
  已经只使用：
  - `contract/*`
    DTO
  不再直接穿透层内 domain model
- worker southbound
  已经不再匿名
- 旧的单体：
  - `httpapi`
  - `store`
  - `config`
  不再作为长期兼容壳保留

## 这一章没有做什么

这一章故意没有做：

- 代码迁移
- 包移动
- 路由迁移
- 存储拆分
- southbound contract 落地

因为这章的目标不是“开始改”，
而是：

- 先把后面所有重构必须遵守的边界定死

## 本章涉及的主要文件

- [main.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/cmd/control-plane/main.go)
- [main.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/cmd/agent/main.go)
- [app.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/app.go)
- [router.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/router.go)
- [config.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/config/config.go)
- [platformconfig.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/platformconfig/platformconfig.go)
- [service.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/deploy/service.go)
- [client.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/planeclient/client.go)
- [http_fetcher.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/planesync/http_fetcher.go)
- [service.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/runtimeworker/service.go)
- [migrations.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/store/migrations/migrations.go)

## 本章检查点

- 三层模型已经正式定成：
  - `control plane`
  - `cloud plane`
  - `cloud worker`
- `control plane`
  只选 `cloud plane`
  不直接选 worker
- `cloud plane`
  独占 plane-local 运行态、
  provider SDK
  和 worker 调度权
- `cloud worker`
  只执行、心跳、上报，
  不拥有全局视角
- northbound API
  和两套 southbound contract
  已明确要求分离
- `control plane`
  和 `cloud plane`
  已明确要求分库存放
- provider credential
  已明确只允许出现在 `cloud plane`
- `v4/10`
  已明确必须做一次不留兼容包袱的彻底重构
