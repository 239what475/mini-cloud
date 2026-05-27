# Mini Cloud v5

`v5`
不再继续摊大工作负载类型，
也不再继续自己搭太多基础组件。

这一版明确只做一件事：

- 把 `mini-cloud`
  做成一个多云 `CaaS`
  平台
- 只支持：
  - 容器
  - 长期在线服务

也就是说，
`v5`
不再以：

- `web / background worker / job / cron`
  这种越来越多的 workload 类型

来组织自己，
而是统一收成：

- `service`

这个核心对象。

## 这一版的产品边界

`v5`
明确只解决下面这些问题：

- 怎样定义一个长期在线服务
- 怎样把这个服务部署到多个云
- 怎样让它对外暴露域名和 `TLS`
- 怎样做发布、回滚、扩容
- 怎样在每个云里按需扩更多 `node`
- 怎样从平台视角看这个服务是否健康

这一版不解决：

- 一次性任务
- 定时任务
- 复杂有状态服务
- 双 `control-plane`
  对等同步
- 全局流量编排
- `service mesh`
- 更重的多活控制面

## 这一版的基本形态

`v5`
沿用当前的三层思路，
但重新把职责收得更窄：

- `fleet control-plane`
  - 只做全局管理面
  - 不进入用户流量热路径
- `cloud-plane`
  - 只做本云执行面
- `agent`
  - 负责节点侧接入 `cloud-plane`
  - 负责心跳、拉 work
  - 负责驱动本地容器运行

每个云都被视为一个：

- `service cell`

一个 `service cell`
里至少包含：

- 一台固定入口机
  - 跑 `cloud-plane`
  - 跑 `Caddy`
  - 兼一个资源受限的小 `node`
- 若干按需扩出来的 `runtime node`

当入口机上的资源不够时，
由本云的：

- `cloud-plane`

去向本云申请更多 `runtime node`。

## 两条流量必须分开

### 管理面

本地：

- `CLI`
- `Web UI`

先访问：

- fleet `control-plane`
  的 `HTTP JSON`
  管理接口

在这一版里：

- `cloud-plane`
  的 northbound 管理接口
  收成：
  - `gRPC`
  - `grpc-gateway`
- 平台内部控制链路：
  - `control-plane -> cloud-plane`
  - `cloud-plane -> agent`
  统一走：
  - `gRPC`

也就是说，
`v5`
并不是“整个平台 northbound
都已经统一成 `gRPC + grpc-gateway`”，
而是：

- `cloud-plane`
  先完成这一步
- fleet `control-plane`
  暂时继续保留更轻的
  `HTTP JSON`
  入口

### 用户流量

用户流量不经过：

- `fleet control-plane`

它只走：

- 外部 `front door`
  - 可以是：
    - `CDN`
    - `DNS`
- 每个云内的 `Caddy`
- 该云里的服务实例

这意味着：

- `control-plane`
  挂了，
  现网服务仍然可以继续跑
- 主备切流也不应依赖它

## 这一版的技术取向

`v5`
开始明确多用成熟组件，
少自己搭轮子。

当前建议直接收成：

- 应用入口网关：
  - `Caddy`
- `cloud-plane`
  管理接口：
  - `gRPC`
  - `grpc-gateway`
- `proto`
  和代码生成：
  - `buf`
- 状态库：
  - `Postgres`
- 数据访问：
  - `sqlc`
- 迁移：
  - `goose`
- 运行时控制：
  - `Docker SDK`
- 观测入口：
  - `OTel Collector`
- 指标 / 日志 / 看板：
  - `Prometheus`
  - `Loki`
  - `Grafana`
- 本地客户端：
  - `Cobra`

这里要特别强调：

- `grpc-gateway`
  只用于平台管理接口
- 它不是用户应用流量入口
- 用户应用流量入口由：
  - `Caddy`
    负责

在完整平台形态里，
`v5`
还会明确收一套两段路由模型：

- 第一段：
  - 外部 `front door`
    按路径把请求送到对应云的入口机
- 第二段：
  - 入口机上的 `Caddy`
    再把路径转发到本云对应容器

这里的路径不是固定写死的，
但这一版也不会把它做成一个复杂的全局流量编排系统：

- 它们由独立的
  `front door route`
  资源声明或绑定
- 平台会根据这些 route
  生成或同步最小必需的：
  - `front door`
    路由规则
  - `Caddy`
    路由规则

例如：

- 给服务 `user-api`
  单独创建一条
  `front door route`
  绑定 `/user`
- 给服务 `assets`
  单独创建一条
  `front door route`
  绑定 `/assets`
- 给服务 `admin`
  单独创建一条
  `front door route`
  绑定 `/admin`

那么平台生成的就是：

- `/user`
  -> 对应云入口机
- `/assets`
  -> 对应云入口机
- `/admin`
  -> 对应云入口机

## 当前规则

下面这部分分成两类：

- 已经落地的章节文档
- 后续预留的章节编号和命名

它不表示这些章节文档现在都已经落地。

- `docs/v5/ROADMAP.md`
  - 负责 `v5` 的总规划
- `docs/v5/01-caas-foundation-and-service-cell.md`
  - 负责 `v5/01`
    的产品边界、`service cell`
    拓扑和组件栈冻结
- `docs/v5/02-service-spec-and-management-api.md`
  - 负责 `v5/02`
    的 `service`
    模型、期望状态、
    整条 management API
    的 `gRPC`
    单轨化，以及
    `servicelifecycle`
    分层
- `docs/v5/03-config-secret-and-image-access.md`
  - 负责 `v5/03`
    的配置、密文配置和镜像访问
- `docs/v5/04-managed-gateway-exposure-with-caddy.md`
  - 负责 `v5/04`
    的域名、`TLS`
    和 `Caddy`
    网关接入
- `docs/v5/05-health-release-and-rollback.md`
  - 负责 `v5/05`
    的健康检查、发布和回滚
- `docs/v5/06-scaling-placement-and-worker-expansion.md`
  - 负责 `v5/06`
    的扩缩容、放置和 `runtime node`
    扩展
- `docs/v5/07-app-observability-with-otel.md`
  - 负责 `v5/07`
    的应用侧观测
- `docs/v5/08-identity-service-account-and-authorization.md`
  - 负责 `v5/08`
    的身份主体、
    外部登录、
    多管理员模型、
    项目归属关系和授权边界
  - 这一章明确区分：
    - `human_user`
    - `service_account`
    - `break_glass`
    - 平台级角色
    - `project_token -> service_account(scope=project)`
      的收编关系

## 项目归属简化模型

为了让 `v5`
保持成一个简单的 `CaaS`
平台，
后面的模型先收成下面这套：

- 一个 `project`
  固定只属于一个 `human_user`
- 一个 `project`
  下面可以有多个 `service`
- `service`
  仍然属于 `project`
  不直接属于 `user`
- 项目级自动化身份继续保留：
  - `service_account(scope=project)`
- 当前不做：
  - 多人共享同一个 `project`
  - `project membership`
  - 人类用户的项目内多档角色

这里也要特别说明：

- 这套关系更接近
  `cloud-plane`
  身份侧和
  `v5`
  的项目语义
- fleet `control-plane`
  当前仍然保留：
  - `ownerUserID`
  - `admin token / project token`
    这套更轻的管理面模型

也就是说，
`v5`
还没有把 fleet
和 `cloud-plane`
的身份模型完全收成同一套实现。

可以先把关系理解成：

```text
human_user --< project --< service

project --< service_account(scope=project)
platform --< service_account(scope=platform)

actor = human_user | service_account
owner = project.owner_user
```

也就是说：

- `actor`
  是谁发起了这次操作
- `owner`
  是这个 `project`
  的固定拥有者

这套模型是 `v5`
的简化版。

如果以后真的要支持：

- 多人协作同一个项目
- 更复杂的共享主体

再引入：

- `project_membership`

也不迟。

下面这些是：

- 当前这一版的后续章节编号
- `docs/v5/09-project-guardrails-and-capacity-admission.md`
  - 负责 `v5/09`
    的项目级配额、usage、preview
    和 admission reject reasons
- `docs/v5/10-external-front-door-and-cdn-attachment.md`
  - 负责 `v5/10`
    的外部统一入口和前门接入
- `docs/v5/11-full-platform-bootstrap-and-e2e.md`
  - 负责 `v5/11`
    的完整平台搭建和端到端验收
- `docs/v5/12-v5-hardening-and-boundary-review.md`
  - 负责 `v5/12`
    的收尾、加固和边界复盘
- 每一章结束时仍然单独做一个 `commit`
  - 作为章节检查点

## 章节规划

当前已经落地到：

- `12`

完整章节编号规划如下：

- `01-caas-foundation-and-service-cell`
- `02-service-spec-and-management-api`
- `03-config-secret-and-image-access`
- `04-managed-gateway-exposure-with-caddy`
- `05-health-release-and-rollback`
- `06-scaling-placement-and-worker-expansion`
- `07-app-observability-with-otel`
- `08-identity-service-account-and-authorization`
- `09-project-guardrails-and-capacity-admission`
- `10-external-front-door-and-cdn-attachment`
- `11-full-platform-bootstrap-and-e2e`
- `12-v5-hardening-and-boundary-review`

## 这一版怎么理解

如果说：

- `v4`
  解决的是：
  - 多个 `cloud-plane`
    能不能被统一纳管
  - 三层边界能不能收清楚
  - 统一观察和运维面能不能成立

那么：

- `v5`
  解决的就是：
  - 能不能把这套底座做成一个真正可用的多云 `CaaS`
  - 用户能不能围绕：
    - 服务定义
    - 暴露
    - 发布
    - 回滚
    - 扩容
    - 观测
    来使用平台

## 当前对技术方向的判断

### 高概率会在 `v5` 正式进入主线

- 长期在线 `service`
  资源模型
- 每云 `service cell`
  拓扑
- `Caddy`
  网关接入
- `cloud-plane`
  northbound 的
  `gRPC + grpc-gateway`
  管理接口
- 健康检查、发布、回滚
- 本云 `runtime node`
  扩容与回收
- `OTel Collector`
  驱动的应用观测
- 项目级配额、usage
  与 admission 护栏
- 外部统一入口
  与 `front door`
  前门接入
- 一条真实的双云 `CaaS`
  平台端到端主链

### 更可能留到 `v5` 之后

- 双 `control-plane`
  同步
- 正式多活 `control-plane`
  高可用
- 更强的 controller / reconcile
  收敛模型
- front door
  健康路由和跨 cell
  故障切换
- 控制链路安全、
  token / secret / 证书
  轮转
- 更正式的 `day-2`
  运维和 operator `SLO`
- 全局流量编排
- `DNS`
  级主备切换自动化
- 第三家 provider
- 有状态服务主线
- 多人共享项目
- 更复杂的租户体系
- 余额 / 账单 / 支付系统

### 这版明确不急着做

- `job / cron / oneoff`
- `service mesh`
- `Kubernetes`
  运行底座迁移
- 更复杂的消息队列体系
- 更复杂的数据库 / 对象存储产品线

更细的章节规划以：

- [projects/mini-cloud/docs/v5/ROADMAP.md](./ROADMAP.md)

为准。
