# v5/04 Managed Gateway Exposure With Caddy

`v5/03`
已经把：

- `service spec`
  的运行输入
- `config / secret / registry credential`
- `gRPC + grpc-gateway`
  这条管理面链路

都补上了。

但那时还有一个关键问题没有收口：

平台虽然能描述服务，
也能把服务调度到 worker，
却还没有把“服务如何真正对外暴露”
做成一条干净、正式、可验证的链路。

这一章要解决的就是这个问题。

## 这一章到底要收什么

这一章只收一条主线：

- 一个 `service`
  在什么条件下可以被公网访问
- 通过什么域名访问
- 谁负责做：
  - `TLS`
  - `HTTP -> HTTPS`
  - 入口转发
  - backend 未就绪时的受控响应

这一章之后，
平台对外暴露的最小语义要稳定下来：

1. `public` 服务
   会被发布到 cell 的公共入口
2. `private` 服务
   不会被发布到公共入口
3. 平台会为 `public` 服务自动维护一个托管默认域名
4. `public` 服务可以额外绑定自定义域名
5. `TLS`
   和 `HTTP -> HTTPS`
   都由 `Caddy`
   负责
6. service backend 没就绪时，
   入口会返回受控 `503`
7. 平台自身域名也走同一套数据面入口

## 这一章明确不做什么

这一章不扩成：

- `CDN`
- 全局统一入口
- 多云流量调度
- 金丝雀 / 权重路由 / 灰度切流
- 完整公网证书自动化平台
- 发布策略
- 扩缩容

也就是说，
这里做的是：

- 单个 `service cell`
  内部的正式入口闭环

不是：

- 全平台的全球入口编排

## 为什么旧最小 gateway 必须删掉

在更早的版本里，
仓库里有一套最小自研 `gateway`：

- 直接在 Go handler 里看 `Host`
- 查数据库
- 反向代理到 backend

它能证明“域名绑定这个想法能工作”，
但到了 `v5`
就不再合适了，
因为它会把两件事混在一起：

1. 管理面
2. 用户流量入口

这一章之后，
这两块必须明确分开：

- `managementserver`
  只负责：
  - management API
  - `grpc-gateway`
  - worker / plane north-south API
  - 静态页面和内部制品分发
- `dataplane + Caddy`
  只负责：
  - 平台域名入口
  - service 域名入口
  - `TLS`
  - `HTTP -> HTTPS`
  - 公网流量转发

所以这一章里，
旧的自研 `gateway`
热路径被彻底删除，
只保留新的正式数据面。

## 现在的流量路径长什么样

现在一个 cloud-plane 里的两条流量路径是：

```text
管理请求
用户/CLI/Web
  -> managementserver
  -> gRPC + grpc-gateway
  -> store / service lifecycle / worker APIs

用户流量
浏览器/HTTP client
  -> Caddy dataplane
  -> dataplane snapshot
  -> 当前 service backend
```

平台域名和服务域名也都进入同一套数据面：

```text
https://console.lab.example.test
  -> Caddy
  -> managementserver

https://hello.edge-team.services.lab.example.test
  -> Caddy
  -> 当前 running execution
```

这点很重要：

- 管理面不再自己兼任公网入口
- 数据面也不直接参与管理 API 的资源决策

## `public / private` 到底是什么意思

这一章把 `service.exposure`
正式收成两种：

### `public`

`public`
表示：

- 这个服务应该被发布到 cell 的公共入口
- 平台要为它维护默认托管域名
- 它也允许绑定额外自定义域名

所以 `public`
不只是一个展示字段，
而是会直接影响：

- 默认域名是否存在
- 自定义域名是否允许创建
- dataplane snapshot 里是否生成站点

### `private`

`private`
表示：

- 这个服务不会被发布到公共入口

它带来的效果是：

1. 不创建托管默认域名
2. 已有托管默认域名会被删除
3. 不允许再创建新的公开自定义域名
4. dataplane snapshot 不会再发布这个服务

这里要特别注意：

- 历史上已经存在的 custom domain 记录可以还留在数据库里
- 但只要 service 切成 `private`
  它就不会再进入公网入口发布

所以：

- 数据保留
- 发布撤回

是两回事。

## 为什么还要区分 `managed` 和 `custom`

这一章把 `domain binding`
也正式分成两类：

### `managed`

`managed`
就是平台自动维护的默认域名，
例如：

```text
hello.edge-team.services.lab.example.test
```

它的生命周期由平台控制：

- `public`
  时自动创建
- 切成 `private`
  时自动删除

### `custom`

`custom`
就是用户自己绑定的域名，
例如：

```text
hello-custom.edge-team.example.test
```

它的创建要走显式 API，
而且只有 `public`
 服务允许创建。

这个区分很关键，
因为否则平台根本没法区分：

- 哪些 host 是平台自己维护的默认暴露面
- 哪些 host 是用户额外声明的发布面

## dataplane snapshot 现在怎么理解

这一章没有让 `Caddy`
自己去查数据库。

而是由 cloud-plane 里的 `dataplane source`
先从 store 里装配出一个快照，
再由 `Caddy engine`
把它应用成正式入口配置。

这条链是：

```text
store
  -> dataplane/source
  -> snapshot
  -> dataplane/caddy
  -> caddy.Load(...)
```

当前 snapshot 的规则很直接：

1. 平台域名
   总是生成一个站点
   反代回 managementserver
2. 只加载 `services.exposure = public`
   的域名绑定
3. 如果服务当前 backend 已 ready
   就生成 proxy site
4. 如果域名存在但 backend 没 ready
   就生成 static `503` site

也就是说，
数据面现在不负责“判断应不应该发布”这类业务规则。

这些规则已经在：

- service lifecycle
- store 查询边界

里先收好了。

数据面只负责把当前期望状态变成真正入口行为。

## `Caddy` 在这一章里到底负责什么

这里用的是 embedded `Caddy`，
不是外部单独进程。

当前 `Caddy`
负责：

- 监听 HTTP / HTTPS
- 本地测试证书
- `HTTP -> HTTPS`
  跳转
- host-based 站点匹配
- 反向代理到当前 backend
- backend 未就绪时返回静态 `503`

当前做法是：

- 先渲染 `Caddyfile`
- 再通过 adapter 转成 JSON config
- 最后调用 `caddy.Load(...)`

这里的重点不是“`Caddyfile` 本身”，
而是：

- 平台的用户入口能力
  不再由自研 proxy handler 承担
- 入口层现在由成熟 gateway 组件承担

## 平台域名为什么也要走同一套入口

这一章里，
平台域名：

```text
console.lab.example.test
```

也进入 `Caddy` 数据面，
再回源到 managementserver。

这样做的目的有两个：

1. 让平台自身入口和 service 入口共享同一套：
   - `TLS`
   - 监听端口
   - host-based routing
2. 让之后的外部 front door / `CDN`
   只需要面对一类源站入口

所以后面 `v5/09`
做外部 front door 时，
不会再出现：

- 平台域名走一套路
- service 域名走另一套路

这种分裂。

## 这一章现在是怎么验证的

这一章的验证分成两层：

### 1. API + dataplane 集成测试

核心集成测试现在不再用：

- 一个 management handler
  既做管理面又做入口面

而是改成真正的双入口模型：

1. 启一个 management server
2. 启一个 embedded `Caddy` dataplane
3. 通过管理 API 创建项目、服务、域名绑定
4. 通过 worker report 把 execution 切到 running
5. 直接请求 `Caddy` 入口验证行为

覆盖的关键场景包括：

- `public`
  服务自动获得 `managed` 默认域名
- `private`
  服务没有默认域名
- `private`
  服务不能创建自定义域名
- 平台域名经 `Caddy`
  仍能访问 `/api/healthz`
- backend 未 ready 时返回受控 `503`
- HTTP 请求被重定向到 HTTPS
- HTTPS 成功回源到 backend
- `public -> private`
  后原有 host 不再公开可达

### 2. `Caddy` 配置渲染单测

除了黑盒集成测试，
这一章还补了 `dataplane/caddy`
的最小单测，
直接检查：

- proxy site
- static `503` site
- `HTTP/HTTPS`
  端口
- `local_certs`
- redirect 配置

这些关键配置片段有没有被正确渲染出来。

## 本章落地后的边界

到这里，
`mini-cloud`
里关于“服务怎么对外发布”的最小正式边界已经确定了：

1. 管理面是 `managementserver`
2. 用户流量入口是 `Caddy dataplane`
3. `public/private`
   是真正影响发布行为的 service 语义
4. `managed/custom`
   是真正影响域名生命周期的 domain 语义
5. 平台域名和服务域名都走同一套数据面

这意味着从 `v5/05`
开始，
我们终于可以在一个稳定入口模型之上继续做：

- `readiness`
- `liveness`
- rolling update
- rollback

而不用再回头争论：

- 请求到底先进管理面还是先进入口面
- 默认域名到底算不算正式发布对象
- `TLS`
  到底应该由谁负责

## 本章检查点

如果这一章做对了，
你现在应该已经能稳定回答下面这些问题：

1. 为什么 `managementserver`
   和用户流量入口必须分开
2. 为什么旧最小自研 `gateway`
   在这一章必须被删除，
   不能和 `Caddy`
   长期并存
3. `public / private`
   为什么不是展示字段，
   而是直接影响发布行为的 service 语义
4. 为什么：
   - `managed`
   - `custom`
   两类域名绑定必须区分
5. 为什么 service 切成 `private`
   后，
   历史 custom domain 记录可以保留，
   但不能继续发布到公网入口
6. 为什么 backend 未 ready 时，
   入口应该返回受控 `503`，
   而不是直接让请求失败成一团噪音
7. 为什么平台域名也应该走同一套 `Caddy`
   数据面，
   再回源到 managementserver

如果这些问题都已经能稳定回答，
那 `v5/04`
的入口边界就算真正立住了。

对应提交：

- `336fd2f4acad38f406cf87b223a7f56a658c9bc2`
