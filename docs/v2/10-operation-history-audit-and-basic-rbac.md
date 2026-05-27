# 10 Operation History, Audit, and Basic RBAC

`v2/09`
已经把：

- `admin`
- `project_token`

这两种身份接进来了。

但上一章还有一个明显问题：

- 权限边界虽然存在
- 可它主要还是“藏在路由代码里”
- 而且控制面发生了什么变更
  - 也没有一条正式的操作历史可以回看

所以这一章要补的是两件事：

1. 把当前最小 `RBAC`
   - 正式显式化
2. 给 control-plane 增加一条最小可用的操作历史链路

这章仍然不会直接去做：

- 完整用户系统
- 复杂角色继承
- 合规级不可抵赖审计

它先把真正现在就需要的基础能力补上。

## 这一章先记一句话

当前 `mini-cloud`
的权限模型还是很轻：

- `admin`
- `project_token`

但现在它已经不再只是“代码里隐含的判断”。

control-plane
开始同时具备两种可见能力：

1. 当前身份到底拥有哪些权限
2. 最近执行过哪些成功的控制面变更

## 这章新增了什么

### 1. `whoami` 现在会返回权限列表

上一章的：

```text
GET /api/v1/auth/whoami
```

现在除了告诉你“你是谁”，
还会明确返回：

- `permissions`

例如管理员现在会看到这类能力：

- `platform.read`
- `platform.write`
- `project.read`
- `project.write`
- `app.read`
- `app.write`
- `app.release`
- `app.operate`
- `operations.read`

而项目 token 会少掉平台级能力：

- 没有：
  - `platform.read`
  - `platform.write`

但保留自己项目内的：

- `project.read`
- `project.write`
- `app.read`
- `app.write`
- `app.release`
- `app.operate`
- `operations.read`

也就是说，
这一章把“当前这套最小角色权限模型”正式变成了可观察数据。

### 2. 新增操作历史表

control-plane 现在多了一张：

- `operation_events`

它记录的是：

- 成功发生的控制面变更动作

不是所有 HTTP 请求，
也不是 node 的所有内部高频流量。

当前这张表重点保留这些信息：

- `project_id`
- `action`
- `target_type`
- `target_id`
- `target_name`
- `actor_kind`
- `actor_id`
- `actor_label`
- `request_method`
- `request_path`
- `result`
- `details`
- `created_at`

这里要特别注意两点：

### 第一，它是“操作历史”

它更接近：

- 谁做了什么变更

而不是：

- 全量系统日志

### 第二，它现在是 best-effort 审计

当前实现是：

- 业务写成功后
- 再补写一条操作历史

所以它不是严格事务绑定的“强审计”。

这样做是有意的。

因为如果现在强行要求：

- “业务写成功”
  必须和
- “审计写成功”
  同时原子提交

那这章就会立刻把大量 store API 改成统一事务编排，
范围会一下子变大很多。

所以这章先明确做成：

- 足够实用
- 容易理解
- 能支持后续继续增强

## 现在会记录哪些动作

当前这章接入了这些典型控制面变更：

- `project.create`
- `project.secret_set.create`
- `project.registry_credential.create`
- `project.api_token.create`
- `app.create`
- `app.domain.create`
- `app.release.create`
- `app.retry`
- `app.rollback`
- `node.drain`
- `node.activate`
- `node.heartbeats.reconcile`

也就是说，
这一章记录的是：

- 管理员或项目自动化脚本
  对平台状态做的关键修改

而不会把这些内容也塞进来：

- node 注册
- node 心跳
- work poll
- execution report
- gateway 每一次代理请求

因为这些流量要么太高频，
要么本来就属于另一类运行态事件。

它们应该继续走：

- node 状态模型
- gateway request events

而不是混在“操作历史”里。

## 两类查询接口

### 平台级操作历史

```text
GET /api/v1/platform/operations
```

这是管理员视角的全局列表。

只允许：

- `admin`

访问。

### 项目级操作历史

```text
GET /api/v1/projects/{projectID}/operations
```

这是项目视角的列表。

允许：

- `admin`
- 该项目自己的 `project_token`

访问。

所以现在你可以很自然地理解成：

- 平台管理员
  - 看全局
- 项目自动化脚本或项目运维
  - 看自己项目

## 这里的 basic RBAC 到底指什么

它不是完整的“用户-角色-绑定-策略引擎”。

当前更准确的说法是：

- 角色很少
- 权限集合很小
- 但它们已经开始变成一套正式、可读、可检查的数据视图

所以这里的：

- `RBAC`

现在只做到：

1. 有明确角色
2. 角色有明确权限集合
3. 接口访问边界和权限集合是一致的
4. `whoami`
   可以直接把这套信息读出来

这就足够支撑后面继续往：

- 审计
- 运维事件
- 更正式的授权模型

迭代了。

## 一个最小体验流

### 1. 管理员先看自己是谁

```bash
curl -s \
  -H "Authorization: Bearer $MINICLOUD_ADMIN_TOKEN" \
  http://127.0.0.1:8080/api/v1/auth/whoami
```

现在除了身份本身，
你还能直接看到：

- `permissions`

### 2. 管理员创建项目并签发项目 token

```bash
curl -s \
  -H "Authorization: Bearer $MINICLOUD_ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "history-demo",
    "displayName": "History Demo"
  }' \
  http://127.0.0.1:8080/api/v1/projects
```

```bash
curl -s \
  -H "Authorization: Bearer $MINICLOUD_ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "deploy-bot"
  }' \
  http://127.0.0.1:8080/api/v1/projects/<project-id>/api-tokens
```

### 3. 项目 token 去创建自己的 app

```bash
curl -s \
  -H "Authorization: Bearer $PROJECT_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "projectID": "<project-id>",
    "name": "hello",
    "displayName": "Hello",
    "region": "cn-beijing",
    "replicas": 1,
    "instanceClass": "small",
    "defaultPort": 8080,
    "readinessPath": "/healthz"
  }' \
  http://127.0.0.1:8080/api/v1/apps
```

### 4. 项目维度回看操作历史

```bash
curl -s \
  -H "Authorization: Bearer $PROJECT_TOKEN" \
  http://127.0.0.1:8080/api/v1/projects/<project-id>/operations?limit=20
```

这时你会看到类似动作：

- `project.create`
- `project.api_token.create`
- `app.create`

注意这里有一个很有意思的点：

- 虽然 `project.api_token.create`
  是管理员发起的
- 但它依然属于这个项目的操作历史

所以项目范围查询里也会看到它。

## 这一章还做了一个很重要的安全细节

操作历史会尽量保留“足够有用的上下文”，
但不会把敏感明文直接塞进去。

例如：

- `project.api_token.create`
  - 会记录：
    - `tokenPrefix`
  - 不会记录：
    - token 明文 secret
- `project.secret_set.create`
  - 只记录：
    - `keyCount`
  - 不会把 secret values 直接写进去
- `project.registry_credential.create`
  - 不会把 password 写进操作历史

这点很关键，
因为如果审计表本身开始泄露敏感信息，
那它就会从“帮助排查问题”
变成“扩大攻击面”。

## 这章对后面的意义

从这章开始，
`mini-cloud`
终于开始有一点“正式平台”的味道了。

因为你不再只是能：

- 调接口改状态

你还开始能够：

- 解释当前身份能做什么
- 回看刚才是谁改了什么

这会直接给后面的章节打底：

- 告警与 runbook
- 备份恢复
- 更正式的运维事件模型

## 这一章跑了哪些检查

当前已经跑过：

```bash
go test ./...
./scripts/check.sh
./scripts/test-integration.sh
```

这章新增覆盖的重点包括：

- 操作历史表的 store 读写
- `whoami` 权限视图
- 平台级 / 项目级操作历史查询
- 项目 token 只能看项目级历史，不能看平台级历史
- 审计记录里不暴露 token 明文 secret

## 本章检查点

- 提交：
  - `29840d72fad761bbe51fbe8291f1dd42987f8ee2`
- 状态：
  - `whoami`
    - 现在不仅返回身份，也会返回权限列表
  - control-plane
    - 已经开始记录关键成功变更的操作历史
  - 平台管理员
    - 已经能看全局操作历史
  - 项目 token
    - 已经能看自己项目范围的操作历史
  - 当前审计模型
    - 已经明确是 best-effort，而不是强事务绑定审计
