# v5/10 External Front Door And CDN Attachment

`v5/09`
已经把：

- `project`
  级 guardrail
- admission preview
- stable reject reason

这层平台治理边界立住了。

但平台如果还回答不了下面这个问题：

- 一个已经放到某个 `service cell`
  的公开服务，
  怎样再被挂到平台外部统一入口之下

那这一版就还不能算一个完整的多云 `CaaS`。

所以这一章只收一件事：

- fleet 侧的 external front door
  声明模型和同步闭环

## 这一章只做什么

这一章明确只做：

- fleet 侧新增：
  - `fleet service`
  - `service placement`
  - `front door route`
- 把每个 `cloud-plane`
  的 ingress 信息同步进 fleet inventory
- 把：
  - `externalHost`
  - `pathPrefix`
  - `stripPrefix`
  这种外部门面规则，
  声明式地绑定到某个 fleet service
- 把这条绑定最终解析成：
  - 外部入口
    -> 某个 `service cell`
      的 `publicOrigin`
    -> 该 cell
      内部的 `publishedHost`

这一章明确不做：

- 真正的云厂商 `CDN`
  API 对接
- 智能权重切流
- 多活全局入口调度
- 把路径规则直接塞回 `cloud-plane`
  里已有的域名绑定模型

也就是说，
这一章先把：

- 对象模型
- 同步规则
- 校验边界
- 集成测试

收干净，
但暂时不把成功与否绑定到真实 `CDN`
传播时间和供应商差异上。

## 为什么不能复用 cloud-plane 的域名绑定

最容易走偏的一点是：

- 既然 `cloud-plane`
  里已经有：
  - 域名绑定
  - `Caddy`
    暴露

那是不是直接把：

- 外部前门
- 路径前缀

也塞进去就好了。

这条路这章明确不走。

因为两者解决的其实不是同一层问题：

### cloud-plane 内的域名绑定

它解决的是：

- 某个 cell
  里，
  `Caddy`
  怎样把：
  - `Host`
    头
  绑定到本云某个服务

它是：

- cell 内部入口层

### fleet 侧的 front door route

它解决的是：

- 平台外部统一入口
  该怎样把：
  - `externalHost`
  - `pathPrefix`
  映射到某个 cell

它是：

- fleet 外部门面层

如果把这两层混在一起，
马上会出现几个问题：

1. `cloud-plane`
   会被迫知道平台级外部入口规则
2. path-based front door
   和 cell 内 host-based ingress
   会纠缠成同一套对象
3. 以后接真实 `CDN`
   或别的 front door provider
   时边界会很难收

所以这一章最后冻结成两段路由模型：

1. fleet external front door
   - `externalHost + pathPrefix`
     选中目标 cell
2. cell 内 `Caddy`
   - `Host`
     再选中目标服务

## 这一章最后冻结哪些对象

### `fleet service`

这是 fleet 侧对用户服务的声明对象。

它负责表达：

- 服务规格
- 公开性
- 镜像
- 端口
- 健康检查
- 目标 provider / region

它不是 cell 内服务对象的镜像缓存，
而是 fleet 层真正的声明源。

### `service placement`

这是 fleet service
当前落到了哪个 cell
上的解析结果。

它至少要记住：

- `planeID`
- `remoteProjectID`
- `remoteServiceID`
- `publishedHost`
- 远端服务状态摘要

这里的：

- `publishedHost`

非常关键，
因为 external front door
最终不是直接转发到容器，
而是先转发到目标 cell
的公开入口，
再由 cell 内 `Caddy`
根据这个 host
继续分发。

### `plane ingress`

这次 fleet inventory
不再只看：

- provider
- region
- 容量
- health

还要同步每个 cell
的 ingress 摘要：

- `publicOrigin`
- `platformDomain`
- `serviceBaseDomain`

其中真正参与 front door
解析的核心字段是：

- `publicOrigin`

因为它代表：

- 这个 cell
  对外真正可以接流量的入口 origin

### `front door route`

这是 fleet 外部门面的最小声明对象。

这一章把它冻结成：

- `externalHost`
- `pathPrefix`
- `stripPrefix`
- `enabled`

以及同步结果字段：

- `syncStatus`
- `syncMessage`
- `targetPlaneID`
- `targetOrigin`
- `targetHost`
- `lastSyncedAt`

它不保存：

- 云厂商特定 `CDN`
  规则细节
- 证书产品细节
- 权重和优先级策略

这一章只保留最小但稳定的抽象。

## 这一章的解析规则到底是什么

`front door route`
不是创建出来就一定能下发。

它必须先经过下面这些解析条件：

1. route
   必须是启用状态
2. 对应的 fleet service
   必须还存在
3. fleet service
   必须是：
   - `public`
4. 这个服务必须已经完成 placement
5. placement
   必须带出非空的：
   - `publishedHost`
6. 目标 plane
   必须还在 fleet inventory
   里
7. 目标 plane ingress
   必须有非空的：
   - `publicOrigin`

只有这些条件都满足，
route
才会被解析成一条真正可下发的：

- resolved route

也就是：

- `externalHost`
- `pathPrefix`
- `stripPrefix`
- `targetPlaneID`
- `targetOrigin`
- `targetHost`

否则它只会停留在：

- `planned`
- `blocked`
- `error`

这些状态里。

## 这一章最后怎样同步

这一章没有直接写死某一家：

- `CDN`
- `gateway`
- front door provider

而是先引入一个很窄的 adapter：

- 输入：
  - `frontdoor.Snapshot`
- 输出：
  - 由具体实现决定怎样下发

这样做的意义是：

1. 这章可以先把：
   - 声明模型
   - 规则规划
   - store
   - controller
   收干净
2. 集成测试可以用：
   - fake / recording adapter
   去验证真正下发前的快照
3. 后面接真实：
   - 阿里云 `CDN`
   - 腾讯云 `CDN`
   - 或别的 front door
   时，
   不需要重新改对象模型

这里还有一个很重要的实现边界：

- adapter
  接收的是整份 fleet front door
  快照

不是：

- 某一个 `project`
  的局部路由片段

因为外部门面本质上就是平台级共享入口，
只要把它做成“按项目分别覆盖”，
不同项目之间就会互相抹掉彼此的路由结果。

## 这章最后怎样收敛

这章现在不再把：

- front door reconcile

直接塞进：

- `service`
  或 route
  的写请求里同步完成

而是改成真正的后台 controller
模型：

1. 写路径先落声明对象
2. 然后只触发一次：
   - front door sync
3. 后台 reconciler
   异步读取全量 snapshot
4. adapter
   再按这份 snapshot
   做实际下发

这个后台 reconciler
同时具备两种触发源：

- 写路径触发
  - 降低正常变更时的收敛延迟
- 周期全量 reconcile
  - 即使某次触发失败，
    或者外部状态漂了，
    也能在后续重新收敛

所以这一章现在的语义已经不是：

- 请求内同步完成下发

而是：

- 请求负责写入和触发
- 后台 loop
  负责最终收敛

默认情况下，
如果 adapter
没有配置，
route
会被标成：

- `planned`

并保留解析结果。

如果 adapter
已配置并且成功应用，
route
会进入：

- `synced`

如果解析条件不满足，
则进入：

- `blocked`

如果 adapter
已经配置，
但这次下发失败，
那么只有：

- 原本已经解析成功、
  本该被下发的 route

会进入：

- `error`

而那些本来就应该停在：

- `planned`
- `blocked`

的 route
仍然保留原有语义，
不会被统一抹成失败态。

## 这章里的同步语义不是原子写

这一章还要刻意说明一件事：

- service CRUD
- front door route CRUD

和：

- front door adapter
  下发

不是同一个原子事务。

这里的稳定语义是：

1. 先落声明对象
2. 再触发后台 front door reconcile
3. 最终以 route
   上的：
   - `syncStatus`
   - `syncMessage`
   反映当前下发结果

也就是说，
调用方不能把：

- HTTP 写入成功

理解成：

- 外部门面已经传播完成

真正的同步结果，
要回看 route
自己的状态字段。

这也意味着：

- `syncStatus`
  反映的是后台 controller
  最近一次收敛结果

而不是：

- 当前这个 HTTP 请求
  自己同步跑完了什么

## 这章最后新增了什么 API 入口

这章对外增加的是 fleet 侧项目服务入口：

- `GET /api/v1/projects/{projectID}/services`
- `POST /api/v1/projects/{projectID}/services`
- `GET /api/v1/projects/{projectID}/services/{serviceID}`
- `PUT /api/v1/projects/{projectID}/services/{serviceID}`
- `DELETE /api/v1/projects/{projectID}/services/{serviceID}`

以及 front door route
入口：

- `GET /api/v1/projects/{projectID}/services/{serviceID}/frontdoor-routes`
- `POST /api/v1/projects/{projectID}/services/{serviceID}/frontdoor-routes`
- `PUT /api/v1/projects/{projectID}/services/{serviceID}/frontdoor-routes/{routeID}`
- `DELETE /api/v1/projects/{projectID}/services/{serviceID}/frontdoor-routes/{routeID}`

这里的关键不是：

- 又多了几条 CRUD API

而是 fleet
第一次有了：

- 自己的 service
  声明面
- 自己的 placement
  结果面
- 自己的 external front door
  声明面

这三层一旦分开，
后面再接真实 provider
就不会重新把模型搅乱。

## 这一章最后怎样验证

这章最后补了三类测试：

### 规则层单测

直接验证：

- `frontdoorcontroller.Plan`

会不会正确处理：

- longest path prefix
  排序
- `public`
  服务解析
- `private`
  服务阻断
- disabled route
  保留但不下发

### 控制平面集成测试

用本地内存测试链路验证：

1. remote plane
   注册并同步 ingress
2. fleet project
   绑定到 remote plane
3. 通过 fleet service API
   创建公开服务
4. 创建 front door route
5. 先验证写请求只返回：
   - `pending`
6. 再等待后台 reconciler
   把 route
   收敛到：
   - `synced`
7. 验证 adapter
   收到的快照里，
   目标是：
   - remote plane 的 `publicOrigin`
   - 以及 remote service 的 `publishedHost`
8. 再创建第二个项目和第二条 route，
   验证它不会覆盖第一条 route
9. 再把服务改成：
   - `private`
10. 验证 route
   变成：
   - `blocked`
    而其他项目的 route
    仍然保留
11. 最后删除第一个 fleet service，
    验证 remote cloud-plane
    上对应 service
    也被一起删除

也就是说，
这章已经把：

- plane ingress
  -> placement
  -> front door route
  -> adapter snapshot

这一条核心链路跑通了。

### 后台 reconciler 测试

另外还单独补了：

- `frontdoorcontroller.Reconciler`
  的后台 loop 测试

这里专门验证两件事：

1. 写路径 trigger
   会唤醒后台 reconcile
2. 即使某次 adapter apply
   失败，
   后续周期 reconcile
   也能在没有新写入的情况下，
   把原本进入：
   - `error`
   的 resolved route
   自动收敛回：
   - `synced`

同时也确认：

- 原本就应该停在：
  - `blocked`
  - `planned`

的 route，
不会因为一次 apply 失败
被错误抹成：

- `error`

## 这一章之后，平台边界怎样变化

做完这一章以后，
平台就不再只是：

- 能部署服务
- 能做项目级准入
- 能在 cell
  里暴露域名

而是第一次具备了真正的平台外部门面：

- fleet external front door

虽然它这时还没有绑定真实云厂商：

- `CDN`

但对象模型和同步边界已经先收干净了。

这意味着后面的：

- 完整平台搭建
- 真实 `CDN`
  或 front door
  provider
  接入

都可以直接建立在这套声明模型上，
而不用再回头改：

- fleet service
- placement
- cell ingress
- route sync state

## 本章检查点

如果这一章做对了，
你现在应该已经能稳定回答下面这些问题：

1. 为什么 external front door
   不能直接复用 `cloud-plane`
   里的域名绑定模型
2. 为什么 fleet
   侧必须显式拥有：
   - `fleet service`
   - `service placement`
   - `front door route`
   这三层对象
3. 为什么：
   - `publicOrigin`
   和：
   - `publishedHost`
   缺一不可
4. 为什么服务一旦改成：
   - `private`
   已有 front door route
   必须自动转成：
   - `blocked`
5. 为什么这章先做：
   - adapter + snapshot
   而不是直接绑死某一家 `CDN`
6. 为什么 external front door
   解决的是：
   - 平台外部门面
   而不是：
   - cell 内部 `Caddy`
     分发
7. 为什么这章必须放在：
   - `v5/09`
   后面，
   以及：
   - `v5/11`
   前面

如果这些问题都已经能稳定回答，
那 `v5/10`
的 external front door
边界就算真正立住了。

对应提交：

- `ec9121d1b3cf98a6094a7b23b699d158ace493c4`
