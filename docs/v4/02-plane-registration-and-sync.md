# v4/02 Plane Registration And Sync

这一章把 `v4/01` 里的静态 `fleet plane` 资源，
真正变成了“可以连上远端 plane 并周期同步”的对象。

做完这一章之后，
`fleet`
已经具备下面这条最小闭环：

1. 在 fleet 里登记一套 plane 的静态资料
2. 给这套 plane 配置专用的 `bootstrap token`
3. fleet 主动去远端 plane 拉：
   - `healthz`
   - `platform/config`
   - `platform/overview`
   - `platform/reliability`
   - `platform/nodes`
4. fleet 根据拉回来的结果：
   - 更新 `plane_status`
   - 记录 `capacity_snapshot`
   - 产生日志和操作审计
5. control-plane 进程后台周期性重复这件事

## 这章先解决什么问题

到 `v4/01` 为止，
fleet 里虽然已经有了：

- `plane`
- `plane_status`
- `capacity_snapshot`
- `incident`

但它们还只是本地对象。

也就是说：

- 远端 plane 到底活没活着
- provider / region 是不是真的对得上
- 远端现在有多少 node / app / deployment
- worker 总 CPU / 内存容量是多少

这些都还没有真正从远端拉回来。

所以 `v4/02`
真正补的是：

- 注册凭证
- 主动探活
- 同步摘要
- 周期 reconcile

## 为什么这里要单独引入 `fleet bootstrap token`

这章最重要的边界之一是：

- fleet 不应该直接拿远端 plane 的 `admin token` 当长期同步凭证

所以这里新增了一个单独的进程级配置：

- `MINICLOUD_FLEET_BOOTSTRAP_TOKEN`

它的作用是：

- 允许 fleet 只读访问远端 plane 的少数平台接口

当前远端 plane 上，
`fleet bootstrap token`
可以访问这些只读接口：

- `GET /api/v1/platform/config`
- `GET /api/v1/platform/overview`
- `GET /api/v1/platform/reliability`
- `GET /api/v1/platform/nodes`

而这些接口原本只接受：

- admin token

现在改成：

- admin token
- 或 fleet bootstrap token

这样做的好处是：

- fleet 不再必须持有远端 plane 的最高权限 token
- 远端 plane 可以单独轮换这条同步凭证
- 认证边界更清楚

这里还要注意一个现实取舍：

- 这章为了先把闭环做通，
  fleet 端会把远端 bootstrap token 存进自己的数据库
- 但它被单独放在专用表里，
  不会混进 `fleet_planes`
- HTTP API 也不会把 token 明文回显出来

这仍然只是教学版最小实现。

更完整的做法通常会继续往后演进到：

- 外部 secret store
- KMS / envelope encryption
- 凭证轮换和吊销

## `apiBaseURL` 现在到底表示什么

这一章里，
`plane.apiBaseURL`
明确表示：

- 远端 plane 的 API 根路径

也就是：

- 结尾应该停在 `/api`

例如：

```json
{
  "apiBaseURL": "https://plane-a.example.com/api"
}
```

这样 fleet 在拼接远端路径时就很直接：

- `healthz` -> `apiBaseURL + "/healthz"`
- `platform/config` -> `apiBaseURL + "/v1/platform/config"`
- `platform/overview` -> `apiBaseURL + "/v1/platform/overview"`

这一点很重要，
因为根路径如果填成：

- `https://plane-a.example.com`

那 fleet 拼出来的地址就会错。

## 同步时实际拉哪些接口

这章的同步实现不会让 plane 直接写 fleet DB。

真正的方向是：

- fleet 主动去远端拉取

当前最小同步链路如下：

### 1. `GET /api/healthz`

这一步只回答：

- 这套 plane 现在连不连得上
- 数据库是不是至少还能被 control-plane 访问

注意：

- `healthz`
  只是连通性和最小存活探针
- 它不是完整平台健康判断

### 2. `GET /api/v1/platform/config`

这一步主要拿：

- `provider.name`
- `provider.regionId`

这一章不会直接把远端值覆盖回本地 `fleet_planes`。

原因是：

- `fleet_planes.provider`
- `fleet_planes.region`

更像“注册时声明的静态身份”。

所以当前实现做的是：

- 同步时校验它们是否与远端一致
- 如果不一致，把 plane 标成 `degraded`

这样不会把静态元数据悄悄改掉，
也更容易排查“指错了 plane”或“配置漂移”。

### 3. `GET /api/v1/platform/overview`

这一步拿的是总览摘要：

- `nodesTotal`
- `nodesReady`
- `appsTotal`
- `deploymentsTotal`

### 4. `GET /api/v1/platform/reliability`

这一步主要看：

- 当前有没有 `firing` 告警

它会参与 plane 状态判定。

### 5. `GET /api/v1/platform/nodes`

这一章必须额外拉这一步。

原因是：

- `platform/overview`
  只有数量摘要
- 它没有：
  - `cpuMilliCapacity`
  - `cpuMilliAllocated`
  - `memoryMiCapacity`
  - `memoryMiAllocated`

而 `fleet_plane_capacity_snapshots`
已经需要这组容量字段。

所以当前实现会从远端节点列表里，
只对：

- `role == worker`

的节点做聚合，
得到整套 plane 的 worker 总容量。

## plane 状态现在怎么判定

这一章没有额外引入复杂状态机。

当前规则非常直接：

### `offline`

当 fleet 连 `healthz` 都失败时：

- 网络不可达
- 请求超时
- 返回不是正常的成功响应

就把 plane 记成：

- `offline`

### `ready`

当下面这些条件都成立时：

- `healthz` 成功
- 远端 `platform/config` 是可读的
- provider / region 与本地登记一致
- `platform/reliability` 没有 `firing` 告警
- 没有 `not_ready / offline / draining` 节点

就记成：

- `ready`

### `degraded`

只要远端还能连通，
但下面任何一项出现问题：

- `platform/config` 可读但 provider / region 不一致
- 远端 reliability 有 `firing` 告警
- 远端存在 `not_ready / offline / draining` 节点
- 远端平台配置本身还没完全就绪

就记成：

- `degraded`

## 这章新增了哪些 API

### 1. 注册 plane 同步凭证并立即做首次同步

```bash
curl -sS -X POST http://127.0.0.1:8080/api/v1/fleet/planes/<plane-id>/actions/register \
  -H 'Authorization: Bearer <admin-token>' \
  -H 'Content-Type: application/json' \
  -d '{
    "bootstrapToken": "remote-plane-fleet-bootstrap-token"
  }'
```

这一步做两件事：

1. 校验 `bootstrapToken` 非空
2. 用它去拉远端 plane

只有首次同步成功后，
这条 token 才会真正存到 fleet DB 里。

也就是说：

- 注册失败时，
  不会留下一个“其实不可用”的脏凭证

### 2. 手动触发一次同步

```bash
curl -sS -X POST http://127.0.0.1:8080/api/v1/fleet/planes/<plane-id>/actions/sync \
  -H 'Authorization: Bearer <admin-token>'
```

如果这套 plane 还没注册过 bootstrap token，
会返回：

- `409`

因为 fleet 还不知道该用哪条凭证去访问远端。

### 3. 查看同步后的 plane 详情

```bash
curl -sS http://127.0.0.1:8080/api/v1/fleet/planes/<plane-id> \
  -H 'Authorization: Bearer <admin-token>'
```

现在返回里除了原来的 `status` 和 `latestCapacitySnapshot`，
还会多一个：

- `registration`

它至少回答三件事：

- 这套 plane 有没有完成注册
- 最近一次成功验证 bootstrap token 是什么时候
- 这条 token 最近一次被设置 / 替换是什么时候

## 后台周期同步是怎么工作的

这章没有单独引入复杂调度器。

当前实现采用最小后台循环：

- control-plane 启动时创建 `fleetsync.Service`
- 再启动一个 ticker
- 每隔一段时间同步所有“已经注册过 bootstrap token”的 plane

周期由环境变量控制：

- `MINICLOUD_FLEET_SYNC_INTERVAL_SECONDS`

默认值是：

- `30`

所以：

- 首次注册成功后会立刻同步一次
- 后面后台会按周期继续 reconcile

## 这章的数据模型变化

这一章新增了：

- `fleet_plane_bootstrap_tokens`

它和 `fleet_planes`
分开存的原因很明确：

- `fleet_planes`
  是静态元数据
- `fleet_plane_bootstrap_tokens`
  是敏感凭证

两者不应该混在一行里。

当前这张表至少保存：

- `plane_id`
- `bootstrap_token`
- `last_verified_at`
- `created_at`
- `updated_at`

## 这章代码里最值得看的位置

### 认证边界

- `internal/httpapi/auth.go`

这里新增了：

- `fleet_bootstrap` principal
- `platformReadOnlyFunc`

所以远端 plane 才能接受更窄权限的同步 token。

### 同步服务

- `internal/fleetsync/service.go`
- `internal/fleetsync/http_fetcher.go`

这里是真正的核心：

- 怎么拉远端 plane
- 怎么把远端返回映射成 `ready / degraded / offline`
- 怎么写回本地 `status` 和 `capacity snapshot`

### store

- `internal/store/fleet_store.go`
- `internal/store/migrations/00018_create_fleet_plane_bootstrap_tokens.sql`

这里补上了：

- bootstrap token 的持久化
- `registration` 元数据读取

### HTTP API

- `internal/httpapi/fleet_handler.go`
- `internal/httpapi/router.go`

这里新增了：

- `register`
- `sync`

两个入口。

## 这一章怎么验证

当前集成测试做的是一个很重要的真实模拟：

- 起一套“remote plane” `httptest` server
- 给它配置：
  - `admin token`
  - `fleet bootstrap token`
- 在远端 plane 里注册一个 ready worker
- 再创建一个 project 和 app
- 然后起一套“fleet plane” server
- 通过 `register` 把远端 plane 纳管进来
- 检查同步后的：
  - `status`
  - `registration`
  - `latestCapacitySnapshot`
- 再把远端节点故意打成 stale
- 触发远端 reconcile
- 再次执行 `sync`
- 检查 fleet 里的 plane 是否从 `ready` 变成 `degraded`

这组测试的意义是：

- 它不是只测单个 handler
- 而是在本地把“plane 纳管到 fleet”整条链路真的跑了一遍

## 这一章的取舍

这章有几个故意的取舍：

### 1. 不做 plane 反向写 fleet DB

因为那会把边界搅乱。

这章坚持：

- fleet 主动拉
- plane 被动提供只读接口

### 2. 不自动改写本地 `provider / region`

因为它们更像注册资料。

这章只做：

- 一致性校验

而不是：

- 把远端结果直接覆盖本地声明

### 3. 先不引入 KMS / 外部 secret store

因为这一章的重点是：

- 注册链路
- 探活链路
- 同步链路

不是完整凭证治理。

## 本章涉及的主要文件

- `internal/config/config.go`
- `internal/httpapi/auth.go`
- `internal/httpapi/router.go`
- `internal/httpapi/fleet_handler.go`
- `internal/fleetsync/service.go`
- `internal/fleetsync/http_fetcher.go`
- `internal/store/fleet_store.go`
- `internal/store/migrations/00018_create_fleet_plane_bootstrap_tokens.sql`
- `internal/httpapi/httpapi_integration_test.go`
- `internal/fleetsync/service_test.go`
- `internal/store/store_integration_test.go`

## 本章检查点

- 待本章完成后补充
