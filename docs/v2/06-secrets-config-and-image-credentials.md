# 06 Secrets, Config, and Image Credentials

这一章不去重新发明一套“普通配置系统”。

因为当前 `mini-cloud`
其实已经有普通配置入口了：

- `app.env`
- `release.env`

真正缺的，
是另外两层能力：

1. 敏感信息不要继续和普通 `env` 混在一起
2. 私有镜像仓库凭据要能真的进入发布执行链

所以这一章做的事情很明确：

- 保留现有普通配置模型
- 新增 `secret set`
- 新增 `registry credential`
- 让它们真正进入 `release -> deployment -> work item -> agent`
  这条主链

## 这一章解决的核心问题

如果把所有东西都继续塞进：

- `app.env`
- `release.env`

那会立刻出现两个问题：

| 问题 | 后果 |
| --- | --- |
| 密码、token、私钥也会像普通配置一样被公共 API 回显 | 风险太高 |
| 私有镜像仓库凭据没有正式位置 | agent 无法稳定拉私有镜像 |

所以现在明确拆成三类：

| 类别 | 当前放在哪里 | 是否通过公共 API 回显值 |
| --- | --- | --- |
| 普通配置 | `app.env` / `release.env` | 会 |
| 敏感配置 | `project secret set` | 不会 |
| 镜像拉取凭据 | `project registry credential` | 不会 |

这就是这一章最重要的边界。

## 普通配置为什么继续留在 `env`

这里不要过度设计。

当前普通配置本来就已经很好理解：

- 和应用本身绑定的默认配置
  - 放在 `app.env`
- 和某次 release 绑定的覆盖配置
  - 放在 `release.env`

所以这一章不再新增什么：

- config map
- parameter store
- profile system

而是明确规定：

- 只要不是敏感值
  - 就继续走原来的 `env`

这样教程不会被多余抽象打断。

## 新增了哪两类资源

### 1. `project secret set`

这是项目级的一组敏感键值对。

当前公开出来的是：

- `id`
- `projectID`
- `name`
- `keys`

不会回显：

- 真正的值

当前 API：

```bash
POST /api/v1/projects/{projectID}/secret-sets
GET  /api/v1/projects/{projectID}/secret-sets
```

一个最小例子：

```json
{
  "name": "app-secrets",
  "values": {
    "DB_PASSWORD": "top-secret",
    "API_TOKEN": "token-123"
  }
}
```

### 2. `project registry credential`

这是项目级的私有镜像仓库登录信息。

当前公开出来的是：

- `id`
- `projectID`
- `name`
- `server`
- `username`
- `passwordConfigured`

不会回显：

- `password`

当前 API：

```bash
POST /api/v1/projects/{projectID}/registry-credentials
GET  /api/v1/projects/{projectID}/registry-credentials
```

一个最小例子：

```json
{
  "name": "harbor-main",
  "server": "registry.example.com",
  "username": "student",
  "password": "harbor-password"
}
```

## `app` 和 `release` 现在怎么引用它们

当前模型是：

| 对象 | 新字段 | 作用 |
| --- | --- | --- |
| `app` | `secretSetID` | 让这个 app 在运行时拿到一组敏感环境变量 |
| `release` | `imageCredentialID` | 让这次 release 拉镜像时可以用某个仓库凭据 |

也就是说：

- secret 更偏 app 级别
  - 因为它通常描述这个应用长期需要的敏感配置
- 镜像凭据更偏 release 级别
  - 因为不同 release 可能来自不同 registry

## env 现在是怎么合并的

这一章真正把一条之前还不存在的规则落地了。

当前 work item 里的最终环境变量合并顺序是：

1. `app.env`
2. `release.env`
3. `secret set`

也就是说，
如果同一个 key 同时出现在多处：

- secret 的值优先级最高

这样做的理由很直接：

- 普通配置可以被 release 覆盖
- 但敏感值不应该再被一份普通 `env` 意外盖掉

## secrets 现在是怎样下发的

当前实现不是把 secret set 暴露给公共 API 去取值。

而是：

1. control-plane 在 `ClaimExecutionWork`
   时读取：
   - `app.env`
   - `release.env`
   - `secret set`
2. 在 control-plane 内部完成合并
3. 只把最终结果放进 node work item
4. agent 从：
   - `GET /api/v1/nodes/{nodeID}/work`
   拿到这份已解析好的环境变量

所以：

- 普通管理 API
  - 看不到 secret value
- 只有真正要执行 workload 的那条内部链路
  - 才会拿到值

这就是当前 `v2/06`
对“secrets 怎样下发”的最小实现。

## 私有镜像凭据现在是怎样用的

这一章另一条关键主链是：

- `release.imageCredentialID`
  -> `ClaimExecutionWork`
  -> `workItem.imageCredential`
  -> `agent`
  -> `runtime`

当前 agent/runtime 的做法不是：

- 长期把登录态写死在节点上

而是：

1. 为这次执行临时创建一个 `DOCKER_CONFIG` 目录
2. 用：
   - `docker login --password-stdin`
   登录目标 registry
3. 用同一个临时 `DOCKER_CONFIG`
   执行 `docker run`
4. 执行结束后删除这个临时目录

也就是说，
当前镜像登录态是：

- 一次执行级别的临时状态

这比“直接把凭据永远写进节点默认的 `~/.docker/config.json`”
要干净得多。

## 一个需要明确写清楚的现实限制

当前 `v2/06`
还不是完整成熟的 secret management。

现在做到的是：

- secret values / registry password
  - 会存进 control-plane 数据库
- 公共 API
  - 不会把这些值回显出来
- 真正执行时
  - 才会把需要的值下发到 agent
- 私有 registry 登录态
  - 只保留在一次执行的临时 `DOCKER_CONFIG` 目录里

但现在还没做到：

- 数据库静态加密
- KMS / Vault 之类的外部密钥托管
- 更细粒度的 secret 审计
- secret 轮换和版本管理

所以这一章要把它理解成：

- `v2`
  阶段一个“足够走通真实发布链”的最小 secret / credential 方案

而不是最终形态。

## 这一章最值得看的代码落点

### 资源模型

- `internal/secretset`
- `internal/registrycredential`

### 数据库存储与查询

- `internal/store/secret_store.go`
- `internal/store/migrations/00010_create_project_secret_sets.sql`
- `internal/store/migrations/00011_create_project_registry_credentials.sql`
- `internal/store/migrations/00012_add_secret_set_and_image_credential_refs.sql`

### app / release 引用关系

- `internal/app/app.go`
- `internal/release/release.go`
- `internal/store/app_store.go`

### 执行链里的解析与下发

- `internal/store/execution_store.go`
- `internal/execution/execution.go`

### agent / runtime 怎样临时登录私有 registry

- `cmd/agent/managed.go`
- `internal/runtime/docker_cli.go`

## 本地最小实验顺序

### 1. 创建 project

```bash
curl -fsS -X POST http://127.0.0.1:18080/api/v1/projects \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "secret-demo",
    "displayName": "Secret Demo"
  }' | jq
```

### 2. 创建 secret set

```bash
curl -fsS -X POST http://127.0.0.1:18080/api/v1/projects/<project-id>/secret-sets \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "app-secrets",
    "values": {
      "DB_PASSWORD": "top-secret",
      "API_TOKEN": "token-123"
    }
  }' | jq
```

### 3. 创建 registry credential

```bash
curl -fsS -X POST http://127.0.0.1:18080/api/v1/projects/<project-id>/registry-credentials \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "harbor-main",
    "server": "registry.example.com",
    "username": "student",
    "password": "harbor-password"
  }' | jq
```

### 4. 创建 app，并绑定 `secretSetID`

```bash
curl -fsS -X POST http://127.0.0.1:18080/api/v1/apps \
  -H 'Content-Type: application/json' \
  -d '{
    "projectID": "<project-id>",
    "name": "secret-app",
    "displayName": "Secret App",
    "region": "cn-beijing",
    "replicas": 1,
    "instanceClass": "small",
    "defaultPort": 8080,
    "readinessPath": "/healthz",
    "env": {
      "APP_MODE": "demo"
    },
    "secretSetID": "<secret-set-id>"
  }' | jq
```

### 5. 创建 release，并绑定 `imageCredentialID`

```bash
curl -fsS -X POST http://127.0.0.1:18080/api/v1/apps/<app-id>/releases \
  -H 'Content-Type: application/json' \
  -d '{
    "version": "v1",
    "image": "registry.example.com/demo/app:v1",
    "env": {
      "LOG_LEVEL": "debug"
    },
    "imageCredentialID": "<registry-credential-id>",
    "port": 8080,
    "readinessPath": "/healthz"
  }' | jq
```

### 6. 让 worker 取一次 work

```bash
curl -fsS http://127.0.0.1:18080/api/v1/nodes/<worker-node-id>/work | jq
```

这时你会看到：

- `env`
  里已经同时有：
  - `APP_MODE`
  - `LOG_LEVEL`
  - `DB_PASSWORD`
  - `API_TOKEN`
- `imageCredential`
  里带着：
  - `server`
  - `username`
  - `password`

注意这里是内部 agent work 接口，
所以它看到值是正常的；
而项目级 list API 不会回显这些敏感值。

## 这一章跑了哪些检查

当前我已经跑过：

```bash
go test ./...
./scripts/test-integration.sh
./scripts/check.sh
```

其中：

- API 集成测试已经覆盖：
  - secret set 创建 / 列表
  - registry credential 创建 / 列表
  - app / release 引用关系
  - work item 里能看到合并后的 env 和 image credential
- 普通 list API
  - 也验证了不会直接回显 secret values / registry password

## 这一章的结论

`06`
真正补上的不是“几个新表”。

它补上的是一条以前缺失的真实发布能力：

- 普通配置
  - 继续保持简单
- 敏感配置
  - 有了单独位置
- 私有镜像凭据
  - 有了正式进入执行链的路径
- agent
  - 已经能在一次执行里临时登录私有 registry

也就是说，
从这一章开始，
`mini-cloud`
已经不只能跑公共镜像和明文 `env`，
而是开始具备：

- 真正托管更像生产环境应用的基础条件

## 本章检查点

- 提交：
  - `a850917e28e74cd17f3cde772d5dc8724efe46d0`
- 状态：
  - `project secret set`
    - 已经进入数据模型、数据库和 API
  - `project registry credential`
    - 已经进入数据模型、数据库和 API
  - 公共项目 API
    - 已经不会回显 secret value 和 registry password
  - `ClaimExecutionWork`
    - 已经会把 `app.env`、`release.env`、`secret set` 合并后下发
  - agent / runtime
    - 已经能在一次执行里临时登录私有 registry
