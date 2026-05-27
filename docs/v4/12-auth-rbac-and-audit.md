# v4/12：认证、RBAC 与审计

这一章不做网页登录页，
也不做完整 `OIDC`。

这一章先做的是：

- `control plane`
  的最小正式权限边界
- project-scoped
  的 `API token`
- 越权访问时的审计闭环

也就是说，
这一章要先回答：

- 调用者是谁
- 它能不能访问这个 `project`
- 如果不能，
  系统会不会明确拒绝并留下记录

## 先回答你最容易疑惑的两个点

### 1. 这一章不是“用户登录系统”

不是。

这一章先不做人类用户登录，
也不做：

- 用户名密码
- `OIDC`
- 组织 / 成员 / 邀请

这一章先做的是：

- 平台管理员 token
- 项目级 token

所以它更像是：

- service identity
- automation identity

而不是：

- browser login
- human user system

后面如果要接：

- `OIDC`
- 单点登录
- 更正式的用户体系

也应该建立在这一章先收好的权限模型之上。

### 2. 这里的 `RBAC` 到底是在干什么

`RBAC`
是：

- `Role-Based Access Control`

中文一般说：

- 基于角色的访问控制

在这一章里，
你可以直接把它理解成：

- 先给调用者一个角色
- 再根据这个角色决定它能做什么

这里先只收两类正式角色：

1. `platform_admin`
2. `project_operator`

它们回答的问题就是：

- 这个 token
  是不是平台管理员
- 如果不是，
  它是不是只允许操作某一个 `project`

所以这一章里的 `RBAC`
不是抽象概念，
而是很具体的接口边界。

## 为什么这一章必须先做

前面几章虽然已经有：

- fleet plane
- placement
- deploy
- incident
- audit event

但 `control plane`
一直只有一把：

- 全局 `admin token`

这会带来一个很实际的问题：

- 路径里虽然写着 `projectID`
- 但权限上仍然是“全局管理员全开”

这意味着系统还不能清楚表达：

- 这个调用者只能操作自己的 `project`
- 不能去看别的 `project`
- 更不能去动整套 fleet

所以这一章的核心价值不是“多一个 token”，
而是让：

- project scope
- platform scope

真正进入系统。

## 这一章的最小身份模型

### principal

这一章先收三种 principal：

1. `anonymous`
2. `admin`
3. `project_token`

这里的：

- `anonymous`

主要只是表示：

- 没开认证
- 或者没有携带 token

真正的正式身份是后两种。

### 1. `admin`

这个 principal
仍然由现有的全局：

- `AdminToken`

来表示。

它对应的角色是：

- `platform_admin`

它可以访问：

- fleet 级接口
- project 管理接口
- project token 管理接口
- fleet 级操作审计

### 2. `project_token`

这一章新增的是：

- `control plane` 自己的 project-scoped `API token`

它对应的角色是：

- `project_operator`

它有一个非常明确的边界：

- 一个 token
  只绑定一个 `projectID`

也就是说，
它不是：

- fleet-global token

它只能在自己绑定的那个 `project`
范围内工作。

## 这一章里的权限边界

### 继续只给 `platform_admin` 的接口

- `GET /metrics/fleet`
- `GET /api/v1/fleet/logs`
- `GET /api/v1/fleet/inventory`
- `GET /api/v1/fleet/planes`
- `POST /api/v1/fleet/planes`
- `GET /api/v1/fleet/planes/{planeID}`
- `DELETE /api/v1/fleet/planes/{planeID}`
- `POST /api/v1/fleet/planes/{planeID}/actions/register`
- `POST /api/v1/fleet/planes/{planeID}/actions/sync`
- `PUT /api/v1/fleet/planes/{planeID}/operation`
- `GET /api/v1/fleet/planes/{planeID}/capacity-snapshots`
- `POST /api/v1/fleet/planes/{planeID}/capacity-snapshots`
- `GET /api/v1/fleet/planes/{planeID}/projects/{projectID}/binding`
- `PUT /api/v1/fleet/planes/{planeID}/projects/{projectID}/binding`
- `POST /api/v1/fleet/planes/{planeID}/projects/{projectID}/actions/apply-app`
- `POST /api/v1/fleet/projects/{projectID}/placements/preview-app`
- `GET /api/v1/fleet/incidents`
- `POST /api/v1/fleet/incidents`
- `PUT /api/v1/fleet/incidents/{incidentID}`
- `POST /api/v1/fleet/incidents/{incidentID}/resolve`
- `GET /api/v1/fleet/operations`
- `GET /api/v1/projects`
- `POST /api/v1/projects`
- `PUT /api/v1/projects/{projectID}`
- `DELETE /api/v1/projects/{projectID}`
- `GET /api/v1/projects/{projectID}/api-tokens`
- `POST /api/v1/projects/{projectID}/api-tokens`
- `DELETE /api/v1/projects/{projectID}/api-tokens/{tokenID}`

这一组接口的共同点是：

- 要么是全局平台面
- 要么会暴露 fleet 拓扑
- 要么会修改 platform 级资源

所以这里先不放给 project token。

### 这一章放给 `project_token` 的接口

- `GET /api/v1/auth/whoami`
- `GET /api/v1/projects/{projectID}`
- `GET /api/v1/projects/{projectID}/operations`
- `POST /api/v1/fleet/projects/{projectID}/actions/apply-app`

注意这里最关键的一点：

- 只有当路径里的 `projectID`
  和 token 绑定的 `projectID`
  一致时，
  才允许通过

如果不一致，
就直接：

- `403 Forbidden`

并写入审计事件。

## 为什么 `preview-app` 这一章不先放给 project token

因为当前的 placement 预览结果里，
会直接暴露：

- `planeID`
- `planeName`
- `remoteProjectID`
- 全部 candidate plane

这更像管理员视角的调度细节。

所以这一章先收一个更干净的边界：

- project token
  可以调用“项目级自动部署”
- 但不直接读取完整 fleet placement 明细

## 审计这一章到底做了什么

这一章没有新建第二套审计表。

继续复用的是：

- `operation_events`

但它不再只是“成功操作历史”，
而是开始承担最小正式审计能力。

### 1. actor 信息补全了

现在 `operation_events`
里会明确记录：

- `actorKind`
- `actorID`
- `actorLabel`
- `actorProjectID`

这里新增的关键 actor 是：

- `project_token`

它会把：

- token id
- token name
- token 所属 project

一起带进审计事件。

同时，
管理员 actor
也不再是空 `actorID`，
而是收成固定值：

- `platform-admin`

### 2. 审计结果不再只有 succeeded

这一章开始，
`operation_events.result`
除了：

- `succeeded`

还正式进入：

- `denied`
- `failed`

也就是说，
现在不仅会记录：

- 某个接口成功做了什么

还会记录：

- 某个 token
  因为权限不够被拒绝了什么

同时也能区分：

- `denied`
  - 权限或认证要求不满足
- `failed`
  - 认证链路自己出错，
    例如后端存储不可用

### 3. 授权信息会写进 details.authorization

这一章会把最小授权上下文写进：

- `details.authorization`

里面至少会带：

- `requiredPermission`
- `scopeType`
- `scopeID`
- `decision`
- `reason`
- `matchedRoles`
- `httpStatus`

所以以后看到一条审计事件时，
就不只是知道：

- 调了哪个接口

还能知道：

- 这次访问要求什么权限
- 是在哪个 scope 上判断的
- 最后是允许还是拒绝

## project token 的接口返回为什么要收口

这一章要特别避免一个坏味道：

- 虽然鉴权已经是 project-scoped 了
- 但响应体里还在把 fleet 内部细节原样返回出去

所以：

- project token
  调用项目级 `apply-app`

时，
返回体会做收口，
只保留项目视角真正需要的结果，
不会直接把：

- `remote`
- placement decision 里的 plane 细节

全部原样暴露出去。

同样，
project-scoped 的：

- `GET /api/v1/projects/{projectID}/operations`

也会对明显的敏感字段做基础裁剪，
例如：

- `planeID`
- `selectedPlane`
- `remoteProjectID`
- `remoteAppID`
- `tokenPrefix`
- `requestPath`

这样这一章的 project-facing API
才算真的有边界。

## 一个最小使用流程

### 1. 管理员给某个 project 发 token

```bash
curl -sS \
  -H 'Authorization: Bearer fleet-admin' \
  -H 'Content-Type: application/json' \
  -d '{"name":"demo-operator"}' \
  http://127.0.0.1:18080/api/v1/projects/prj_xxx/api-tokens
```

返回里会看到两部分：

- `token`
  - 可保存的元数据
- `secret`
  - 只会在签发时返回一次

### 2. 用这个 token 看自己是谁

```bash
curl -sS \
  -H 'Authorization: Bearer mcpt_xxx' \
  http://127.0.0.1:18080/api/v1/auth/whoami
```

这里可以直接看到：

- principal 是不是 `project_token`
- 它绑定的是哪个 `project`
- 它当前有哪些权限

### 2.5 如果 token 泄露了，可以撤销

```bash
curl -sS \
  -X DELETE \
  -H 'Authorization: Bearer fleet-admin' \
  http://127.0.0.1:18080/api/v1/projects/prj_xxx/api-tokens/ptk_xxx
```

这一点很重要，
因为这章既然引入的是长期有效 token，
就不能只有：

- issue
- list

还必须有：

- revoke

### 3. 用这个 token 访问自己的 project

```bash
curl -sS \
  -H 'Authorization: Bearer mcpt_xxx' \
  http://127.0.0.1:18080/api/v1/projects/prj_xxx
```

如果换成别的 `projectID`，
就会得到：

- `403`

并且后台会留下：

- `project.auth.denied`

审计事件。

## 这一章刻意没做什么

这一章刻意先不做：

- `OIDC`
- 人类用户体系
- project member / viewer / editor / owner
- group / org / invitation
- control-plane 到 cloud-plane 的调用者身份透传
- 更复杂的审计查询和报表

原因很直接：

如果前面这些重系统先上，
但最基础的：

- project-scoped token
- northbound 权限边界
- denied audit

还没站稳，
系统只会继续变得更混。

所以这一章要先把最小正式模型立住。

## 这章结束后，应该记住什么

1. 这一章不是登录页，
   而是 service identity。
2. `RBAC`
   在这里就是：
   不同 token
   能访问不同 scope。
3. `platform_admin`
   负责 fleet / project 管理面。
4. `project_token`
   只能操作自己绑定的 `project`。
5. 越权不再只是返回 `403`，
   而是会写入：
   - `operation_events`
   - `result = denied`

## 本章检查点

- `control plane`
  已支持 project-scoped `API token`
- 项目级接口已经不再全部是 admin-only
- 越权访问会留下 `denied` 审计事件
- `operation_events`
  已能区分：
  - `admin`
  - `project_token`
