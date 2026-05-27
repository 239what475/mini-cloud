# 11 Mini Cloud Terraform 入口

这一章没有直接去写真正的 Terraform provider 代码。

`10`
做完以后，
平台底座入口已经统一到：

- `deploy/terraform/lab`

但如果要继续往上做“把 mini-cloud 当成 Terraform 资源入口来使用”，
真正的前置条件不是再加一层 Terraform 目录，
而是先把 mini-cloud 自己暴露出来的资源 API 收成 Terraform 友好的形状。

所以这一章真正完成的是：

- 资源模型收口
- HTTP API 收口
- `release -> revision`
  语义收口

也就是说，
`11`
本质上是一章：

- “为了后续 Terraform 入口而做的服务端资源 API 重构”

不是：

- “Terraform provider 已经写完”

## 上一章检查点

- `v3/10`
  - `f59cd87df8253481a327d5c1f3126cf13482086f`

## 这一章现在已经落地了什么

当前实现已经明确把 mini-cloud 对外对象分成三层。

### 1. 声明式资源

第一版真正适合被 Terraform 直接管理的资源是：

- `project`
- `app`

它们现在都已经有比较完整的生命周期接口。

### 2. 服务端自动生成的只读对象

这些对象可以回读，
但不应该作为 Terraform 第一版直接管理的主资源：

- `revision`
- `deployment`
- `execution`

这也是这一章最核心的改名：

- 旧的 `release`
  语义已经收口成：
  - `revision`

因为它真正表达的是：

- 某次 app 运行规格的不可变快照

### 3. 动作接口

这些能力保留为动作，
而不是声明式资源：

- `rollback`
- `retry`
- `probe`
- `issue-token`

动作接口会改变系统状态，
但它们本身不是长期托管的资源对象。

## 为什么这一章必须先做 API 重构

如果不先改服务端 API，
后面即使勉强写出 Terraform provider，
本质上也只是在包装一组命令式发布接口，
而不是管理一组稳定资源。

旧模型的问题主要有三个。

### 1. `app` 不是完整期望资源

以前很多真正影响运行效果的字段放在：

- `release`

里，
导致：

- `app`
  只表达了一半的期望状态
- `release`
  又同时像资源、又像动作输入

这不适合 Terraform。

### 2. `release -> deployment`
本质上是命令式流程

旧模型更像：

1. 提交一条发布记录
2. 服务端立刻创建一次 deployment
3. 然后进入 rollout / 调度 / 执行

这更像“发布操作 API”，
不是“资源 CRUD API”。

### 3. `deployment`
不适合做 Terraform 直接资源

`deployment`
有非常明显的过程状态：

- `pending`
- `scheduling`
- `assigned`
- `deploying`
- `running`
- `failed`

它本质上是：

- rollout 过程记录

而不是：

- 用户长期声明的目标资源

所以这一章把边界彻底收正了：

- Terraform 第一版管理：
  - `project`
  - `app`
- 服务端自动生成并回读：
  - `revision`
  - `deployment`
  - `execution`

## 这一章改完后的资源模型

### `project`

`project`
现在就是一个正常资源，
支持：

- 列表
- 创建
- 单资源读取
- 更新
- 删除

对应入口在：

- [router.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/router.go)

### `app`

`app`
现在自己承载完整的期望运行规格，
例如：

- `region`
- `replicas`
- `instanceClass`
- `image`
- `command`
- `args`
- `env`
- `defaultPort`
- `readinessPath`
- `secretSetID`
- `registryCredentialID`

也就是说，
现在用户声明的是：

- “这个 app 现在想跑成什么样”

而不是：

- “先创建 app，再额外提一条 release”

对应代码在：

- [app_handler.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/app_handler.go)
- [00016_refactor_releases_to_revisions_and_app_spec.sql](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/store/migrations/00016_refactor_releases_to_revisions_and_app_spec.sql)

### `revision`

`revision`
是服务端根据 app 当前规格自动生成的不可变快照。

它不是用户单独提交的资源，
而是：

- app 创建时自动生成
- app 更新且需要 rollout 时自动生成
- rollback / retry 时被引用

### `deployment`

`deployment`
继续保留为 rollout 过程记录。

它负责表达：

- 当前调度推进到了哪里
- 当前 revision 是否已经上线
- 当前运行状态是否健康

它属于状态回读对象，
不是 Terraform 第一版直接管理资源。

## 现在的 API 已经收成什么样

### 1. `project`

```text
GET    /api/v1/projects
POST   /api/v1/projects
GET    /api/v1/projects/{projectID}
PUT    /api/v1/projects/{projectID}
DELETE /api/v1/projects/{projectID}
```

### 2. `app`

```text
GET    /api/v1/projects/{projectID}/apps
POST   /api/v1/projects/{projectID}/apps
GET    /api/v1/apps/{appID}
PUT    /api/v1/apps/{appID}
DELETE /api/v1/apps/{appID}
```

### 3. 只读状态对象

```text
GET /api/v1/apps/{appID}/revisions
GET /api/v1/apps/{appID}/deployments
```

### 4. 动作接口

```text
POST /api/v1/apps/{appID}/actions/retry
POST /api/v1/apps/{appID}/actions/rollback
POST /api/v1/apps/{appID}/actions/probe
POST /api/v1/projects/{projectID}/actions/issue-token
```

这里要特别注意两点：

1. 已经不再保留：
   - `POST /api/v1/apps/{appID}/releases`
2. `project api token`
   现在明确是：
   - “签发动作”
   不是资源创建型接口

## `app` 创建和更新现在怎么工作

这一章落地以后，
`app` 的创建和更新已经变成下面这条主链。

### 创建 app

`POST /api/v1/projects/{projectID}/apps`
现在会做三件事：

1. 创建 `app`
2. 立刻基于当前 app 规格生成首个 `revision`
3. 推进首个 `deployment`

也就是说，
创建 app 以后，
响应里已经可以直接看到：

- `app`
- `status`
- `domains`
- `revision`
- `deployment`
- `placementDecision`

### 更新 app

`PUT /api/v1/apps/{appID}`
现在更新的是：

- app 的期望状态

如果这次更新会影响 rollout，
服务端会自动：

1. 生成新的 `revision`
2. 创建新的 `deployment`

并在响应里明确返回：

- `rolloutTriggered`
- `revision`
- `deployment`
- `placementDecision`

如果只是一些不需要 rollout 的修改，
例如纯元数据字段，
那就不会强行生成新的 revision。

## `GET /api/v1/apps/{appID}`
现在回什么

这一章之后，
`GET /api/v1/apps/{appID}`
已经收成了比较稳定的资源 + 状态模型：

```json
{
  "app": { "...": "声明式期望资源字段" },
  "status": {
    "currentRevisionID": "rev_xxx",
    "currentDeploymentID": "dep_xxx",
    "currentRevision": { "...": "当前 revision" },
    "currentDeployment": { "...": "当前 deployment" },
    "currentExecution": { "...": "当前 execution" },
    "healthy": true,
    "message": "current revision is healthy"
  },
  "domains": [
    { "...": "已绑定域名" }
  ]
}
```

这样就把两层语义分开了：

- `app`
  负责表达期望状态
- `status`
  负责表达系统当前观察到的实际状态

而完整历史对象：

- `revision`
- `deployment`

则继续通过独立列表接口回读。

## 数据层这一章做了什么

这一章不只是改了 handler，
还把数据模型一起迁过去了。

迁移文件在：

- [00016_refactor_releases_to_revisions_and_app_spec.sql](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/store/migrations/00016_refactor_releases_to_revisions_and_app_spec.sql)

它做了几件关键事情：

1. 给 `apps` 表补上 app 自己承载的运行规格字段
   - `image`
   - `command_json`
   - `args_json`
   - `registry_credential_id`
2. 把：
   - `current_release_id`
   重命名成：
   - `current_revision_id`
3. 把：
   - `releases`
   表重命名成：
   - `revisions`
4. 把 deployment / execution 里引用的：
   - `release_id`
   改成：
   - `revision_id`
5. 把旧的：
   - `version`
   收口成：
   - `label`
6. 给 revision 增加：
   - `revision_number`

也就是说，
这一章不是只换了 API 名字，
而是把：

- 数据表
- 服务端对象
- 接口语义

一起改到了新的模型上。

## 为什么这更适合未来的 Terraform 入口

这一章做完以后，
后续真正的 Terraform 入口边界已经清楚了。

最自然的资源映射会是：

- `mini_cloud_project`
- `mini_cloud_app`

而像：

- `revision`
- `deployment`

更适合作为：

- 只读输出字段
- 或后续 data source

动作接口则继续保持为动作：

- `rollback`
- `retry`
- `issue-token`
- `probe`

这样 Terraform 管的是：

- 用户声明的长期目标资源

而不是：

- 一次次 rollout 过程对象

## 这一章没有做什么

这里也要把边界说清楚。

`11`
虽然把 mini-cloud 资源 API 收正了，
但它还没有直接做下面这些事：

1. 还没有实现真正的 Terraform provider
2. 还没有实现 `mini_cloud_project`
   / `mini_cloud_app`
   这些 Terraform 资源
3. 还没有把：
   - `revision`
   - `deployment`
   做成 Terraform data source

所以这一章的结论不是：

- “Terraform 入口已经完整可用”

而是：

- “mini-cloud 自己的服务端 API 已经被收成适合后续 Terraform 入口接入的形状”

## 这一章的代码落点

- [router.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/router.go)
- [app_handler.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/app_handler.go)
- [project_token_handler.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/project_token_handler.go)
- [00016_refactor_releases_to_revisions_and_app_spec.sql](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/store/migrations/00016_refactor_releases_to_revisions_and_app_spec.sql)
- [httpapi_integration_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/httpapi_integration_test.go)

## 当前验证

这轮修改后，
已经确认：

- `go test ./...`
  通过
- `projects/mini-cloud/tests`
  下的独立测试项目可以正常：
  - `go build ./...`
- 前端：
  - `npm run build`
  通过

并且接口、脚本、前端、当前文档里都已经不再残留旧的：

- `release`
- `/operations/probe`
- 旧 POST `/api-tokens`

这说明这一章的 API 收口已经真正落到了代码上。

## 本章结论

从这一章开始，
`mini-cloud Terraform entry`
这条线终于有了稳定边界：

- Terraform 第一版直接管理：
  - `project`
  - `app`
- 服务端内部自动生成并推进：
  - `revision`
  - `deployment`
- `retry`、`rollback`、`probe`、`issue-token`
  保持为动作接口

所以：

- `11`
  已经完成的是：
  - 面向 Terraform 入口的服务端资源 API 重构
- `12`
  才该继续往前做：
  - provider / tests / hardening

## 本章检查点

- `v3/11`
  - `a663ae8b0f86cc96b1b248fe52e913b18331fc80`
