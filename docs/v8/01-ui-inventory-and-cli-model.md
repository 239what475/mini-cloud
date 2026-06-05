# UI Inventory and CLI Model

本文记录 v8 的 Web 覆盖面和后续 `minicloud` CLI 模型。

v8 的 breaking change 原则：

```text
mini-cloud 是 single-tenant CaaS control plane，不建 project/tenant abstraction，专注资源治理和运行时控制。
```

因此 operator-facing API、Web 和 CLI 不再暴露 project、project token、project ownership 或 `--project` 作用域。

## 现有入口

当前保留一套 UI：

- Web UI
  - 位置：`web/`
  - 入口：control-plane 通过 `UIDir` 静态托管。
  - 协议：HTTP JSON API，主要是 `/api/v1/**`。

Go TUI 已移除。历史上它位于 `cmd/tui` 和 `internal/tui`，通过 `internal/operatorclient` 调用 `OperatorService` gRPC；当前不再作为 operator-facing 入口维护。

## Web 覆盖面

Web 当前主要由 `web/src/App.tsx` 驱动。

### 只读能力

| 能力 | HTTP API | 当前展示 |
| --- | --- | --- |
| 健康检查 | `GET /api/healthz` | 服务状态、时间、数据库状态字段 |
| 平台概览 | `GET /api/v1/platform/overview` | service / node 计数 |
| config set 列表 | `GET /api/v1/config-sets` | 名称、ID、key 数量 |
| secret set 列表 | `GET /api/v1/secret-sets` | 名称、ID、key 数量 |
| registry credential 列表 | `GET /api/v1/registry-credentials` | 名称、server、username、密码是否配置 |
| 服务列表 | `GET /api/v1/services` | 服务 spec、状态、当前 run |
| 服务详情 | `GET /api/v1/services/{serviceID}` | phase、health、image、runtime inputs、placement、run |

### 写操作

| 能力 | HTTP API | 输入 |
| --- | --- | --- |
| 创建 config set | `POST /api/v1/config-sets` | `name`、`values` |
| 创建 secret set | `POST /api/v1/secret-sets` | `name`、`values` |
| 创建 registry credential | `POST /api/v1/registry-credentials` | `name`、`server`、`username`、`password` |
| 创建服务 | `POST /api/v1/services` | service spec |
| 更新服务 | `PUT /api/v1/services/{serviceID}` | display name 和 service spec |

## 服务端当前 API 面

`minicloud` CLI 应以 control-plane 当前服务端 API 为准。

当前 HTTP API 已覆盖：

- auth / health / metrics
- global config sets
- global secret sets
- global registry credentials
- services
- platform service accounts
- logs
- inventory
- runtime node pools
- planes
- plane registration / sync / operation / capacity snapshots
- direct control deploy
- plane selection preview / apply
- incidents
- control operations

`OperatorService` 当前覆盖：

- overview
- planes list/get/sync
- services list/get/create

早期 CLI 可以直接复用 HTTP API，最快覆盖 Web 能力；如果后续希望全部走 gRPC，应先扩展 `OperatorService`，再把 CLI 迁过去。

## CLI 目标

新增唯一 operator-facing CLI：

```text
cmd/minicloud
```

保留 daemon：

```text
cmd/control-plane
cmd/cloud-plane
cmd/node-agent
```

已删除的 TUI 目标：

```text
cmd/tui
internal/tui
```

Web 继续保留，不作为本阶段删除目标。

## CLI 全局约定

### 连接配置

CLI 需要支持显式参数和环境变量：

```bash
minicloud --addr http://127.0.0.1:8080 status
minicloud --token "$MINICLOUD_ADMIN_TOKEN" service list
```

建议约定：

- `--addr`
  - 默认读取 `MINICLOUD_CONTROL_PLANE_URL`
  - fallback：`http://127.0.0.1:8080`
- `--token`
  - 默认读取 `MINICLOUD_ADMIN_TOKEN`
- `-o, --output`
  - `table`、`json`、`yaml`
  - 默认 `table`
- `--timeout`
  - 单次请求超时
- `--verbose`
  - 输出调试信息到 stderr

不提供 `--project`。资源名称在 single-tenant control plane 内全局唯一。

### 文件优先

资源期望状态统一走 YAML 文件：

```bash
minicloud config-set apply -f config-set.yaml
minicloud secret-set apply -f secret-set.yaml
minicloud registry-credential apply -f registry-credential.yaml
minicloud service apply -f service.yaml
minicloud runtime-node-pool apply -f runtime-node-pool.yaml
```

设计约定：

- 创建和更新都使用 `apply -f`。
- YAML 是主要用户界面，也是后续 agent 修改和解释资源的主要对象。
- CLI 不提供长 flag 表单来拼装 service spec，例如不设计 `service create --image --port ...` 作为主路径。
- `get -o yaml` 输出应尽量能作为 `apply -f` 的输入基础。
- 复杂资源支持 `apiVersion`、`kind`、`metadata`、`spec` 结构。
- 密钥类输入不在默认 table 输出中回显 secret value。

## CLI 资源模型

### 平台级

- `status`
- `health`
- `auth`
- `logs`
- `operation`

### 运行资源

- `config-set`
- `secret-set`
- `registry-credential`

### 服务级

- `service`

### 控制面运维级

- `plane`
- `runtime-node-pool`
- `inventory`
- `incident`
- `service-account`

## 命令树

### 平台状态

```bash
minicloud status
minicloud health
minicloud auth whoami
```

映射：

- `status`
  - 汇总 health、overview、plane/service 关键计数。
- `health`
  - `GET /api/healthz`
- `auth whoami`
  - `GET /api/v1/auth/whoami`

### Config Set

```bash
minicloud config-set list
minicloud config-set get <config-set>
minicloud config-set apply -f config-set.yaml
```

映射：

- `GET /api/v1/config-sets`
- `POST /api/v1/config-sets`

当前服务端没有单个 config set get 路由；CLI 可先从 list 里筛选，后续再补 API。

### Secret Set

```bash
minicloud secret-set list
minicloud secret-set get <secret-set>
minicloud secret-set apply -f secret-set.yaml
```

映射：

- `GET /api/v1/secret-sets`
- `POST /api/v1/secret-sets`

默认输出只展示 key 名，不展示 value。

### Registry Credential

```bash
minicloud registry-credential list
minicloud registry-credential get <credential>
minicloud registry-credential apply -f registry-credential.yaml
```

映射：

- `GET /api/v1/registry-credentials`
- `POST /api/v1/registry-credentials`

registry credential YAML 不应直接提交真实密码。实现时可以支持 `passwordFrom`，例如从环境变量、stdin 或本地文件读取，避免 shell history 和 Git 泄漏。

### Service

```bash
minicloud service list
minicloud service get demo-web
minicloud service apply -f service.yaml
minicloud service delete demo-web
```

映射：

- `GET /api/v1/services`
- `POST /api/v1/services`
- `GET /api/v1/services/{serviceID}`
- `PUT /api/v1/services/{serviceID}`
- `DELETE /api/v1/services/{serviceID}`

建议 `service apply -f` 支持的 YAML：

```yaml
apiVersion: minicloud.io/v1
kind: Service
metadata:
  name: demo-web
  displayName: Demo Web
spec:
  provider: aliyun
  region: cn-beijing
  instanceClass: small
  exposure: public
  image: nginx:1.27-alpine
  defaultPort: 8080
  readinessPath: /healthz
  env:
    PORT: "8080"
  configSetID: ""
  secretSetID: ""
  registryCredentialID: ""
```

### Logs

```bash
minicloud logs query --since 30m --limit 100
minicloud logs service demo-web --since 30m
minicloud logs plane <plane> --since 30m
```

映射：

- `GET /api/v1/control/logs`

当前 log query handler 支持 query 参数解析，具体字段以 `httpx.ParseLogQueryInput` 为准。CLI 应把常用过滤器转换成 HTTP query。

### Plane

```bash
minicloud plane list
minicloud plane get <plane>
minicloud plane apply -f plane.yaml
minicloud plane register <plane>
minicloud plane sync <plane>
minicloud plane operation apply -f plane-operation.yaml
minicloud plane delete <plane>
minicloud plane capacity-snapshot list <plane>
```

映射：

- `GET /api/v1/control/planes`
- `POST /api/v1/control/planes`
- `GET /api/v1/control/planes/{planeID}`
- `DELETE /api/v1/control/planes/{planeID}`
- `POST /api/v1/control/planes/{planeID}/actions/register`
- `POST /api/v1/control/planes/{planeID}/actions/sync`
- `PUT /api/v1/control/planes/{planeID}/operation`
- `GET /api/v1/control/planes/{planeID}/capacity-snapshots`

`drain` 可以作为语义化别名：

```bash
minicloud plane drain <plane> --reason "maintenance"
```

它映射到 operation state `draining`。

### Runtime Node Pool

```bash
minicloud runtime-node-pool list
minicloud runtime-node-pool get <plane>
minicloud runtime-node-pool apply -f runtime-node-pool.yaml
minicloud runtime-node-pool delete <plane>
```

映射：

- `GET /api/v1/control/runtime-node-pools`
- `GET /api/v1/control/planes/{planeID}/runtime-node-pool`
- `PUT /api/v1/control/planes/{planeID}/runtime-node-pool`
- `DELETE /api/v1/control/planes/{planeID}/runtime-node-pool`

### Inventory

```bash
minicloud inventory get
```

映射：

- `GET /api/v1/control/inventory`

### Operation History

```bash
minicloud operation list
```

映射：

- `GET /api/v1/control/operations`

### Incident

```bash
minicloud incident list
minicloud incident apply -f incident.yaml
minicloud incident resolve <incident> --message "fixed"
```

映射：

- `GET /api/v1/control/incidents`
- `POST /api/v1/control/incidents`
- `PUT /api/v1/control/incidents/{incidentID}`
- `POST /api/v1/control/incidents/{incidentID}/resolve`

### Service Accounts

```bash
minicloud service-account list
minicloud service-account apply -f service-account.yaml
minicloud service-account delete <account>
```

映射：

- `GET /api/v1/control/service-accounts`
- `POST /api/v1/control/service-accounts`
- `DELETE /api/v1/control/service-accounts/{accountID}`

token secret 只在创建时打印一次；table 输出必须明确标记 sensitive。

## 迁移计划

### 阶段 1：只读 CLI

先实现：

```bash
minicloud status
minicloud health
minicloud service list
minicloud service get
minicloud plane list
minicloud plane get
minicloud logs query
```

验收标准：

- 默认 table 输出可读。
- `-o json` 可被脚本消费。
- 能覆盖 Web 的核心查看路径。

### 阶段 2：基础写操作

实现：

```bash
minicloud config-set apply -f config-set.yaml
minicloud secret-set apply -f secret-set.yaml
minicloud registry-credential apply -f registry-credential.yaml
minicloud service apply -f service.yaml
minicloud service delete
minicloud plane sync
```

验收标准：

- 覆盖 Web 的创建 / 更新操作。
- plane sync 和 service create 走当前服务端 API，不再依赖 TUI。
- 危险操作具备确认和 `--yes`。

### 阶段 3：运维写操作

实现：

```bash
minicloud plane drain
minicloud plane operation apply -f plane-operation.yaml
minicloud runtime-node-pool apply -f runtime-node-pool.yaml
minicloud incident apply -f incident.yaml
minicloud incident resolve
```

验收标准：

- 能完成常见运维操作。
- 对尚未有服务端 API 的语义先补 API，再实现 CLI。

### 阶段 4：CLI 完善和文档收敛

已删除：

- `cmd/tui`
- `internal/tui`

保留：

- `web/`
- `Makefile` 中的 web check / build 入口
- control-plane 静态 UI serving 配置

后续如果实现 CLI，验收标准是：

- CLI 覆盖 Web 中适合脚本化的查看和变更操作。
- `make check` 可以继续同时覆盖 Go 和 Web 工具链。
- 文档同时保留 Web 和 CLI 入口说明。

## 需要先澄清的 API 缺口

以下能力在 UI 或目标命令树中出现，但当前服务端 API 状态需要确认：

- `service retry` 是否应该保留；当前 router 没有 `/actions/retry`。
- `service rollback` 不进入 v8 核心能力；需要恢复旧版本时重新 apply 旧 spec。
- `node drain` 是否指 runtime node、plane operation，还是 node-agent 侧节点维护；当前 router 明确支持 plane operation，但没有独立 node drain 命令。

这些缺口不阻塞 v8 文档，但会影响 CLI 写操作实现顺序。
