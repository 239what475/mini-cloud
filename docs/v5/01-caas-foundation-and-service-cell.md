# v5/01 CaaS Foundation And Service Cell

做到 `v4`
为止，
`mini-cloud`
已经把下面这些能力做出来了：

- `fleet control-plane`
- 多套独立 `cloud-plane`
- `cloud-worker`
- 多云纳管
- 全局观察和审计
- 基础下发和放置

但从这里继续往下时，
有两个问题已经不能再含糊：

1. 平台边界开始变散
   - workload 类型越来越多
   - 很容易继续摊成一张“大而全能力清单”
2. 基础组件自己搭得太多
   - 入口网关
   - 平台 API
   - 观测入口
   - 都很容易继续长成自研基础设施

所以 `v5/01`
不急着加新功能，
先做一件更重要的事情：

- 把 `v5`
  的基础边界、最终拓扑和组件栈一次冻结

这一章的定位不是：

- 讲兼容迁移细节
- 讲具体代码怎么改
- 讲某个功能接口长什么样

而是：

- 明确 `v5`
  到底是什么产品
- 明确最终平台要搭成什么样
- 明确哪些东西属于主线
- 明确哪些东西现在坚决不做

## 这一章先回答什么问题

这一章只回答下面这些“基础宪法”问题：

- `v5`
  到底是不是多云 `CaaS`
- 它是不是还继续扩 workload 类型
- `service cell`
  到底是什么
- 每个云里的固定入口机要跑什么
- `fleet control-plane`
  和 `cloud-plane`
  的边界是什么
- 用户流量和管理流量怎么分
- `CDN`
  在这里是什么角色
- `Caddy`
  为什么取代当前最小自研 `gateway`
- 为什么 `grpc-gateway`
  只能做平台管理接口
- 为什么 `control-plane`
  不能进入用户流量热路径

这一章不回答：

- `service`
  资源模型的最终字段
- `proto`
  怎么切
- 数据库表怎么改
- `Caddy`
  配置怎么生成
- `CDN`
  API 怎么调用

这些都留给后面具体章节。

## v5 现在到底是什么

从这一章开始，
`mini-cloud`
对 `v5`
的定义正式收成：

- 一个多云 `CaaS`
  平台
- 只支持：
  - 容器
  - 长期在线服务

这里最关键的一刀是：

- `v5`
  不再继续扩：
  - `job`
  - `cron`
  - `oneoff`
  - 更多 workload 类型

这一版只围绕一个核心对象组织自己：

- `service`

也就是说，
`v5`
不是：

- 一个继续做更多 workload 类型的实验平台

而是：

- 一个把长期在线服务这条主链做厚的多云 `CaaS`

## 最终平台要长成什么样

`v5`
最终不是若干零散功能点，
而是要收成一个完整可搭建的平台结果。

这套平台的最终拓扑是：

```text
CLI / Web
  -> grpc-gateway
    -> fleet control-plane
      -> cloud-plane (aliyun cell)
      -> cloud-plane (tencent cell)

用户
  -> CDN
    -> aliyun cell entry host
    -> tencent cell entry host
      -> Caddy
        -> service containers
```

这里面有两类完全不同的路径：

1. 管理路径
2. 用户流量路径

它们必须从这一章开始被明确分开。

## 什么是 service cell

`v5`
不再把“一个云”只理解成：

- 一堆 `worker`

而是把它正式定义成：

- 一个 `service cell`

一个 `service cell`
至少包含：

- 一台固定入口机
  - 跑 `cloud-plane`
  - 跑 `Caddy`
  - 兼一个资源受限的小 `worker`
- 若干按需扩出来的更多 `worker`

这个设计背后的意思是：

- 每个云都应该先具备一个最小可运行面
- 这个最小可运行面本身就能承接少量服务
- 本云资源不足时，
  再由本云 `cloud-plane`
  向本云 provider 申请更多 `worker`

所以一个 `service cell`
不是：

- 只有控制面
- 或只有执行节点

而是：

- 一个最小可自洽的本地服务单元

## 每一层到底负责什么

### `fleet control-plane`

它是全局管理面。

它负责：

- 全局项目
- 全局服务期望状态
- 多个 `cloud-plane`
  的注册、观察、绑定、选址
- 全局审计
- 全局 incident
- 全局观察摘要

它不负责：

- 代理用户请求
- 直接调度容器
- 直接调云厂商 SDK
- 在请求热路径里决定流量去哪

这里必须明确一句：

- `control-plane`
  可以是 `v5`
  的管理面单点
- 但不能是现网流量单点

也就是说，
它挂了以后：

- 发布可能受影响
- 扩容可能受影响
- 统一观察可能受影响

但：

- 现网流量不该跟着一起挂

### `cloud-plane`

它是单云、本地执行面。

它负责：

- 本云服务执行
- 本云 `worker`
  生命周期
- 本云入口机上的 `Caddy`
  配置下发
- 本云 provider API
- 本云 rollout
- 本云实例状态回报

它不负责：

- 全局写入口
- 全局流量编排
- 对等同步另一个 `cloud-plane`

### `cloud-worker`

它只负责：

- 拉取工作
- 运行容器
- 回报状态

它不负责：

- 全局视图
- 全局调度
- 云厂商资源管理

## 两条流量为什么必须分开

### 1. 管理流量

管理流量是：

- `CLI`
- `Web UI`
- 平台 northbound API
- 平台 southbound 控制链路

这里统一收成：

- `gRPC`
  作为内部 RPC
- `grpc-gateway`
  作为 HTTP/JSON 管理入口

### 2. 用户流量

用户流量是：

- 最终访问服务的公网流量

这条路径不应该经过：

- `fleet control-plane`

它应该只走：

- `CDN`
- 云内固定入口机
- `Caddy`
- 本云 service container

这就是为什么：

- `grpc-gateway`
  不能拿来做通用应用入口

因为它解决的是：

- 平台 API 协议转换

而不是：

- 通用业务流量网关

## CDN 在 v5 里到底是什么

`CDN`
在 `v5`
里被明确收成：

- 外部统一入口
- 平台前门

它不是：

- 全局流量编排系统
- 动态权重调度系统
- 多活全局入口控制平面

`v5`
里只把它当成：

- 外部请求先打到哪里
- 路径规则怎样落到不同云入口机

但这一章先只把这个角色钉死，
不展开到具体 API 或配置。

## 为什么这一版要换基础组件

这一章直接把下面这套组件栈冻结下来。

### 应用入口

- `Caddy`

它用来取代当前最小自研 `gateway`。

从这一章开始，
我们默认的方向是：

- `v5`
  用成熟 gateway
  替代当前平台内嵌最小入口代理

也就是说，
后面的目标不是：

- 两套入口长期并存

而是：

- 把入口能力收敛到 `Caddy`

### 平台 API

- `gRPC`
- `grpc-gateway`
- `buf`

这里的判断是：

- 平台管理接口适合收敛成 `gRPC`
- 需要 `HTTP/JSON`
  的地方由 `grpc-gateway`
  暴露
- `buf`
  负责 schema 和生成流程

### 数据库和访问层

- `Postgres`
- `sqlc`
- `goose`

这里的目标是：

- 少手写数据库胶水
- 把状态表和访问层收得更规整

### 运行时和观测

- `Docker SDK`
- `OTel Collector`
- `Prometheus`
- `Loki`
- `Grafana`

这里的意思不是：

- `v5`
  要做一个全新的观测平台

而是：

- 不再自己拼太多入口层
- 优先复用成熟组件

## 这一章明确不做什么

从这一章开始，
下面这些东西都视为：

- 不属于 `v5`
  主线

1. `job / cron / oneoff`
2. 对等双 `control-plane`
   同步
3. 正式多活 `control-plane`
4. 全局流量编排
5. 复杂 `CDN / DNS`
   策略系统
6. 有状态服务主线
7. `service mesh`
8. 第三家 provider
9. `Kubernetes`
   运行底座迁移

这些方向不是没有价值，
而是：

- 它们现在会明显拉散
  `v5`
  的主线

## 本章结束时必须冻结什么

这一章结束时，
下面这些东西都必须被视为已经冻结：

1. `v5`
   只做长期在线 `service`
2. 每个云是一个 `service cell`
3. 固定入口机上跑：
   - `cloud-plane`
   - `Caddy`
   - 小 `worker`
4. 本云资源不足时，
   由本云 `cloud-plane`
   扩更多 `worker`
5. 用户流量不经过：
   - `fleet control-plane`
6. `CDN`
   只是平台前门，
   不是全局流量编排系统
7. 平台组件栈冻结为：
   - `Caddy`
   - `gRPC`
   - `grpc-gateway`
   - `buf`
   - `Postgres`
   - `sqlc`
   - `goose`
   - `Docker SDK`
   - `OTel Collector`
   - `Prometheus`
   - `Loki`
   - `Grafana`

## 这一章之后再做什么

这一章之后，
后面的第一件正事才是：

- 定义正式的：
  - `service`
    资源模型
  - 期望状态
  - 管理接口

也就是：

- `v5/02`

## 本章检查点

如果这一章做对了，
你现在应该已经能比较自然地回答下面这些问题：

1. `v5`
   到底是不是一个多云 `CaaS`
2. 为什么它只做长期在线 `service`
3. 什么是 `service cell`
4. 为什么固定入口机上同时跑：
   - `cloud-plane`
   - `Caddy`
   - 小 `worker`
5. 为什么 `control-plane`
   不能进入用户流量热路径
6. 为什么 `grpc-gateway`
   只能做管理接口，
   不能做通用业务入口
7. 为什么 `CDN`
   在 `v5`
   里只能先被视为外部统一入口

如果这些问题都已经能稳定回答，
那 `v5`
的基础边界就算真正定下来了。

对应提交：

- `b39ca3bd292129d18c62cdedf0133713627c93bd`
