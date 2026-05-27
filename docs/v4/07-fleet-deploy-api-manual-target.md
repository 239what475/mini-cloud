# v4/07 Fleet Deploy API Manual Target

这一章开始把 `fleet`
从“只会看”
推进到“能下发动作”。

但这里先不急着做：

- 自动选 plane
- fleet 自己维护一套全局 app desired state
- 远端多 plane 统一回滚编排

这一章只先收稳一条最小可写链路：

- 先明确指定目标 plane
- 再把本地 fleet project 绑定到远端 plane project
- 最后用远端 project 级 token 去创建或更新 app

也就是：

- manual target
- action based apply

先跑通。

## 这一章到底新增了什么

到 `v4/06` 为止，
fleet 对远端 plane 的关系还是：

- 只读注册
- 只读同步
- 只读观察

这一章新增的是两层东西：

### 1. fleet -> plane 的写入凭证边界

之前的：

- `fleet bootstrap token`

只适合做：

- `healthz`
- `platform/config`
- `platform/overview`
- `platform/reliability`
- `platform/nodes`

这些只读同步。

它不应该继续直接承担：

- 远端 app create/update

所以这一章新增第二类凭证：

- 远端 plane 上某个 project 的 API token

它的作用是：

- 只允许 fleet 在那个远端 project 里查 project
- 查 app 列表
- create app
- update app

这样边界就变成：

- `bootstrap token`
  负责：
  - plane 注册后的只读同步
- `deploy token`
  负责：
  - 某个远端 project 内的写入动作

这里最重要的一点是：

- fleet 不再默认拿远端 `admin token` 当部署凭证

因为那会把权限边界重新打平，
和 `v4/02`
刚刚建立起来的注册边界相冲突。

### 2. local project -> remote project 的显式 binding

fleet 里本地 project，
和远端 plane 里的 project，
不是天然同一个对象。

所以这一章没有偷懒去假设：

- 本地 project 名字
  一定等于
  远端 project 名字

而是单独新增一张 binding 表：

- `fleet_plane_project_bindings`

它表达的是：

- 本地 `project_id`
- 在某个 `plane_id`
- 绑定到哪个 `remote_project_id`
- 使用哪一条 `deploy_token`

这样以后同一个本地 project，
就可以按 plane 分别绑定不同的远端 project。

当前实现里，
`deploy_token`
虽然不会通过 HTTP API 明文回显，
但仍然保存在 fleet 自己的专用 binding 表里。

也就是说这一章先收的是：

- 权限边界
- 资源边界

不是：

- 凭证加密存储

更正式的加密和轮换，
后面再继续补。

## 先把这四个对象分清楚

这一章最容易看晕的地方，
就是“project 到底有几个”。

这里固定只看四个对象：

1. fleet 本地 project
   - 例如：
     - `fleet-manual-target`
2. fleet 纳管的 plane
   - 例如：
     - `aliyun-bj-remote-write`
3. 远端 plane 里的 project
   - 例如：
     - `remote-write-demo`
4. 远端 plane 给这个 project 签发的 API token
   - 例如：
     - `fleet-writer`

真正的关系是：

- 本地 project 先和某个 plane 建 binding
- binding 里再指向那个 plane 上的远端 project
- 后续 apply app 时，
  fleet 就用这条 binding 里保存的 deploy token 去访问远端 plane

所以这一章不是：

- 先把整个 fleet 全局应用模型做出来

而是：

- 先把“写到哪个 plane、写到那个 plane 的哪个 project、拿什么 token 写”
  这三个问题说清楚

## 新增了哪两个 API

### 1. project binding

读取 binding：

```text
GET /api/v1/fleet/planes/{planeID}/projects/{projectID}/binding
```

写入 binding：

```text
PUT /api/v1/fleet/planes/{planeID}/projects/{projectID}/binding
```

请求体最小只需要两项：

```json
{
  "remoteProjectID": "prj_remote_123",
  "deployToken": "mcpt_xxx"
}
```

这里还要额外注意：

- `deployToken`
  不能是远端 plane 的 `admin token`
- 也不能是 `fleet bootstrap token`

当前 binding 过程会先去远端调：

- `GET /api/v1/auth/whoami`

只有当它确实是：

- `project_token`

并且这个 token 的 `projectID`
正好等于：

- `remoteProjectID`

时，
binding 才会成功。

这里有两个前置条件：

1. 本地 `projectID`
   必须已经存在
2. 目标 `planeID`
   必须已经完成 `v4/02`
   的 bootstrap 注册

第二点很重要。

因为：

- plane 还没完成注册时，
  fleet 连远端 plane 的只读身份都还没建立好
- 这时去配置写凭证，
  边界会很乱

所以当前实现会直接拒绝这种情况，
返回：

- `plane write credentials cannot be configured until the plane bootstrap registration is complete`

### 2. manual target apply-app

```text
POST /api/v1/fleet/planes/{planeID}/projects/{projectID}/actions/apply-app
```

当前输入里必须给出：

- app 名称和镜像等参数

例如：

```json
{
  "name": "fleet-demo-app",
  "displayName": "Fleet Demo App",
  "replicas": 1,
  "instanceClass": "small",
  "image": "nginx:1.27-alpine",
  "defaultPort": 80,
  "readinessPath": "/"
}
```

这个接口现在只做一件事：

- 在指定 plane 上，
  根据这个本地 project 对应的 binding，
  到远端 project 里 create/update 一个同名 app

## apply-app 现在到底怎么工作

当前逻辑很直接：

1. 先检查 `planeID`
   对应的 plane 是否存在且已经注册
2. 再检查路径里的本地 `projectID`
   是否存在
3. 读出：
   - `planeID + projectID`
     对应的 binding
4. 从 binding 拿到：
   - `remoteProjectID`
   - `deployToken`
5. 用 `deployToken`
   去远端 plane 验证：
   - 这个远端 project 是否可读
6. 读取远端 project 下的 app 列表
7. 如果找到同名 app
   - 调 `PUT /api/v1/apps/{appID}`
8. 如果没找到同名 app
   - 调 `POST /api/v1/projects/{remoteProjectID}/apps`

所以这一章的 apply 是：

- 按名字幂等地 create-or-update

而不是：

- fleet 自己维护一个远端 app ID 映射表

这是当前阶段的一个刻意取舍：

- 先把最小链路做通
- 不提前引入第二张 mapping 表

## `region` 这里怎么处理

这一章还有一个小但很重要的默认规则：

- 如果 apply 输入里没写 `region`
- 就默认使用目标 plane 的 `region`

例如：

- 目标 plane 是：
  - `cn-beijing`
- 请求里没写 `region`

那么远端 create/update 时，
最终会落成：

- `region = cn-beijing`

这样做的原因是：

- manual target 已经明确指定了目标 plane
- 这时如果还强制每次手填 region，
  只是重复信息

但如果你显式传了 `region`，
当前实现仍然会按你给的值走校验。

## `secretSetID` 和 `registryCredentialID` 要怎么理解

这两个字段在这一章里很容易误解。

它们不是 fleet 全局资源 ID，
而是：

- 目标 plane 上
- 目标 remote project 里的本地资源 ID

也就是说：

- `secretSetID`
  必须是远端 plane 中那个 `remoteProjectID`
  下面真实存在的 secret set
- `registryCredentialID`
  也必须是远端 plane 中那个 `remoteProjectID`
  下面真实存在的 registry credential

fleet 当前不会替你做跨 plane 的资源复制，
也不会帮你把本地 ID 自动翻译成远端 ID。

所以这章先只保证：

- app create/update 能被下发

不保证：

- 依赖的 secret / registry 资源自动同步

## 为什么这一章还是 action-based，而不是声明式 fleet app

这一章故意没有新增一套：

- `fleet_apps`
- `fleet_app_revisions`
- `fleet_deployments`

原因不是做不到，
而是现在太早。

如果在：

- 写凭证边界
- project binding
- 远端写入 client

都还没收稳之前，
就提前上全局声明式模型，
你会同时面对：

- 本地 desired state
- 远端 actual state
- 每个 plane 自己的 app / revision / deployment
- 失败重试和回读一致性

复杂度会一下跳太大。

所以这一章先收成：

- 一个明确的 admin action API

等 `08`
再去补：

- placement policy

再往后才更适合做：

- 更正式的 fleet app 模型

## 当前权限边界要怎么记

这一章先记三句话就够了：

1. `bootstrap token`
   只负责注册后的只读同步
2. `deploy token`
   只负责某个远端 project 的 app 写入
3. 现在这些 fleet deploy API
   在 fleet 自己这一侧仍然先收成：
   - `admin only`

第三点也很重要。

因为这章的重点是：

- 把写入边界建立清楚

不是立刻做：

- 更复杂的 `RBAC`

更正式的多角色权限控制，
留到 `v4/10`
再统一收。

## 一次最小实验应该怎么看

当前集成测试走的就是下面这条链：

1. 先起一个远端 plane HTTP server
2. 在远端 plane 创建一个 project
3. 给那个远端 project 签发一条 API token
4. 再起一个 fleet server
5. 在 fleet 里创建一个本地 project
6. 在 fleet 里登记一个 plane
7. 在 plane 还没注册前尝试写 binding
   - 预期失败
8. 完成 plane bootstrap 注册
9. 再写 project binding
   - 预期成功
10. 调一次 `apply-app`
    - 远端应创建 app
11. 再调一次同名 `apply-app`
    - 远端应更新原 app，
      而不是创建第二个

这条测试的意义是：

- 不只是证明 HTTP handler 能返回 200
- 而是把：
  - 凭证边界
  - binding
  - remote create/update
  - operation event
  一次串起来

## 当前实现还没做什么

这章刻意还没做下面这些事：

- 自动选 plane
- 一个请求同时下发多个 plane
- 远端 secret set / registry credential 自动复制
- 更正式的 fleet app desired state
- 非 admin 角色的 deploy 权限

这不是遗漏，
而是当前阶段的边界控制。

先把：

- credential boundary
- project binding
- remote app apply

这三件事收稳，
后面的 placement 和更正式编排才有基础。

## 本章涉及的主要文件

- [fleetbinding.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/binding/fleetbinding.go)
- [fleetdeploy.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/deploy/fleetdeploy.go)
- [service.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/deploy/service.go)
- [client.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/planeclient/client.go)
- [fleet_deploy_store.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/store/fleet_deploy_store.go)
- [00020_create_fleet_plane_project_bindings.sql](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/store/migrations/00020_create_fleet_plane_project_bindings.sql)
- [fleet_deploy_handler.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/fleet_deploy_handler.go)
- [router.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/router.go)
- [httpapi_integration_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/httpapi_integration_test.go)
- [store_integration_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/store/store_integration_test.go)

## 本章检查点

- fleet 现在已经区分两类凭证：
  - `bootstrap token`
    只读同步
  - `deploy token`
    远端 project 写入
- 本地 project 和远端 project
  已经通过：
  - `fleet_plane_project_bindings`
    显式绑定
- `POST /api/v1/fleet/planes/{planeID}/projects/{projectID}/actions/apply-app`
  已经能在指定 plane 上：
  - 同名更新
  - 不存在时创建
- apply 输入未显式给 `region`
  时，
  会默认落到目标 plane 的 region
- 当前 fleet deploy 路径
  仍然先保持：
  - `admin only`
  - manual target
  不提前扩成全局声明式编排
