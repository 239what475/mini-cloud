# 09 Auth, API Token, and Project Access

这一章不去直接上完整 `IAM`。

`mini-cloud v2/09`
先把真正最容易立刻需要的两件事补上：

1. 管理员怎么登录 control-plane
2. 某个项目怎么拿一枚自己的自动化 token

也就是说，
这章解决的是：

- 平台管理员入口
- 项目级 `API token`
- 平台面和项目面的最小访问边界

而不是：

- 完整用户体系
- 细粒度角色权限
- 复杂组织模型

## 先记结论

现在的认证模型非常刻意地只保留两层：

1. `Admin bearer token`
   - 平台管理员使用
2. `Project API token`
   - 某个项目自己的自动化脚本使用

并且还有一个很重要的默认行为：

- 如果没有设置：
  - `MINICLOUD_ADMIN_TOKEN`
- 那么整套认证直接关闭

这是故意这样设计的。

原因很现实：

- 我们前面已经写了很多本地实验和集成测试
- 如果一上来把所有接口都强制鉴权
  - 本地开发会立刻变重
  - 教学主链也会突然多出很多和当前主题无关的噪音

所以现在的策略是：

- 本地默认：
  - 可以继续像以前一样直接跑
- 一旦你明确配置了：
  - `MINICLOUD_ADMIN_TOKEN`
- control-plane
  - 才开始真正启用 bearer token 认证

## 这章新增了什么

### 1. `MINICLOUD_ADMIN_TOKEN`

control-plane 现在多了一个环境变量：

```bash
export MINICLOUD_ADMIN_TOKEN='replace-with-a-long-random-token'
```

当它非空时：

- 平台面接口开始要求认证
- 项目面接口开始区分：
  - 管理员
  - 项目 token

当它为空时：

- 认证关闭
- 保持和前面章节相同的无认证开发体验

### 2. `GET /api/v1/auth/whoami`

这个接口是这章里最直观的“你现在是谁”检查点。

如果认证关闭：

- 返回：
  - `authenticationEnabled=false`

如果认证开启：

- 需要带 `Authorization: Bearer ...`
- 然后返回当前身份

当前只会出现两种已认证身份：

- `admin`
- `project_token`

### 3. 项目级 `API token`

现在管理员可以给某个项目签发 token：

```text
POST /api/v1/projects/{projectID}/api-tokens
```

返回里会带两部分：

1. `token`
   - 可长期保存的元数据
2. `secret`
   - 只在签发这一刻返回一次

这里的设计和很多真实平台类似：

- 列表接口不会再把明文 secret 返回给你
- 所以后面如果丢了
  - 只能重新签一枚新的

## 现在的访问边界怎么理解

这一章最重要的不是“多了一个 token 表”。

而是把边界正式立起来了。

| 接口类型 | 现在谁能访问 |
| --- | --- |
| `/api/healthz` | 公开 |
| `/metrics` | 公开 |
| gateway 转发到 app 的流量 | 公开 |
| node 注册 / 心跳 / work / report | 公开 |
| 平台面 `/api/v1/platform/...` | 仅管理员 |
| 创建项目 | 仅管理员 |
| 项目 token 管理 | 仅管理员 |
| 项目自己的 secret set / registry credential / usage preview | 管理员或该项目 token |
| app 的创建、查看、release、重试、回滚、探测、域名绑定 | 管理员或该 app 所属项目 token |

注意这里有两个“故意没上锁”的地方：

### node 内部执行通道

现在仍然保持公开：

- `POST /api/v1/nodes/register`
- `POST /api/v1/nodes/{nodeID}/heartbeat`
- `GET /api/v1/nodes/{nodeID}/work`
- `POST /api/v1/nodes/{nodeID}/executions/{executionID}/report`

原因不是“它永远不需要认证”，
而是这章先不把 node 身份体系一起做进来。

不然这一章会一下子同时混进：

- agent bootstrap
- 节点身份证书
- 执行通道签名

会把主题冲散。

### gateway 对外流量

app 对外入口依然不要求 bearer token。

因为这里处理的是：

- 真实用户访问业务应用

而不是：

- 平台管理员调用 control-plane API

## 项目 token 到底能做什么

可以把项目 token 理解成：

- “这不是平台管理员”
- “它只是某一个项目的自动化身份”

所以它能做的事情非常聚焦：

- 看到自己的项目
- 看到自己的 app
- 在自己的项目里创建 app
- 给自己的 app 提交 release
- 对自己的 app 做 retry / rollback / probe
- 访问自己项目下的 secret set、registry credential、usage preview

但它不能做的事情也很明确：

- 不能创建项目
- 不能看平台所有 node
- 不能看平台总览
- 不能管理别的项目
- 不能签发新的项目 token

## 一个最小操作流

### 1. 管理员先创建项目

```bash
curl -s \
  -H "Authorization: Bearer $MINICLOUD_ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "team-a",
    "displayName": "Team A"
  }' \
  http://127.0.0.1:8080/api/v1/projects
```

### 2. 管理员给项目签发 token

```bash
curl -s \
  -H "Authorization: Bearer $MINICLOUD_ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "ci-bot"
  }' \
  http://127.0.0.1:8080/api/v1/projects/<project-id>/api-tokens
```

返回里最重要的是：

- `secret`

它就是后面项目自动化脚本真正拿去用的 bearer token。

### 3. 项目自己的自动化脚本用 project token 调用接口

```bash
export PROJECT_TOKEN='the-secret-returned-once'
```

先确认当前身份：

```bash
curl -s \
  -H "Authorization: Bearer $PROJECT_TOKEN" \
  http://127.0.0.1:8080/api/v1/auth/whoami
```

再创建自己项目里的 app：

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

## 这一章里一个容易忽略的细节

项目 token 每次被成功解析时，
control-plane 都会更新它的：

- `lastUsedAt`

这有什么用？

最直接的用处就是后面做运维时，
你至少能知道：

- 这枚 token 最近有没有被真正使用过

虽然这离完整审计还很远，
但已经比“完全没有使用痕迹”更像一个正式平台了。

## 为什么这还不叫完整 RBAC

因为当前只有非常粗的两级：

- admin
- project token

它还没有这些东西：

- 用户
- 组织
- 成员关系
- 角色继承
- 细粒度 action 级权限
- 审计策略

所以更准确地说，
这章做的是：

- `mini-cloud` 的最小认证与最小授权边界

而不是完整权限系统。

## 这一章跑了哪些检查

当前已经跑过：

```bash
go test ./...
./scripts/check.sh
./scripts/test-integration.sh
```

其中这章新增的检查重点是：

- project token 的模型校验
- store 层的 token 签发 / 解析 / `lastUsedAt` 更新
- HTTP 层的：
  - admin token
  - project token
  - 平台面拒绝
  - 项目面放行
  - node 公共通道保持可用

## 这章的结论

到了 `v2/09`，
`mini-cloud`
终于不再是“谁都能直接调用所有 control-plane API”。

它现在已经有了一个非常明确、也非常务实的最小模型：

- 本地开发时
  - 可以继续不开认证快速推进
- 正式环境里
  - 可以打开管理员 token
- 项目自动化侧
  - 可以拿项目级 token 只操作自己的资源

这套边界虽然还很轻，
但已经足够支撑下一步继续做：

- 审计
- 基础 `RBAC`
- 运维事件和平台操作记录

## 本章检查点

- 提交：
  - `7568b05a556a5cddd0abc3433dcd04cd560fcd2b`
- 状态：
  - 管理员 token、项目 token 和 node 公共通道这三条认证边界已经正式进入 control-plane
