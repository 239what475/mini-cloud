# UI Inventory and CLI Model

本文完成两件事：

- 盘点当前 Web 已覆盖的操作。
- 定义 `minicloud` CLI 的资源模型和命令树。

`v8` 的方向是保留 Web，删除已经不再维护的 Go TUI。后续如果新增 `minicloud` CLI，应优先复用当前 HTTP API，并和 Web 保持一致的资源模型。

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
| 平台概览 | `GET /api/v1/platform/overview` | project / service / node / deployment 计数 |
| 项目列表 | `GET /api/v1/projects` | 项目名、显示名、quota、创建时间 |
| 服务列表 | `GET /api/v1/projects/{projectID}/services` | 服务 spec、状态、当前 revision、rollout |
| config set 列表 | `GET /api/v1/projects/{projectID}/config-sets` | 名称、ID、key 数量 |
| secret set 列表 | `GET /api/v1/projects/{projectID}/secret-sets` | 名称、ID、key 数量 |
| registry credential 列表 | `GET /api/v1/projects/{projectID}/registry-credentials` | 名称、server、username、密码是否配置 |
| 服务详情 | `GET /api/v1/services/{serviceID}` | 当前 revision、rollout、phase、health、image、runtime inputs |
| revision 列表 | `GET /api/v1/services/{serviceID}/revisions` | revision number、label、image、port、创建时间 |
| deployment 列表 | `GET /api/v1/services/{serviceID}/deployments` | revision、状态、副本数、创建时间 |

注意：这些 Web 路径和当前 `internal/controlplane/api/router.go` 里的新路径不完全一致。当前 router 已经使用项目作用域服务路径，例如 `GET /api/v1/projects/{projectID}/services/{serviceID}`。v8 CLI 应以服务端当前 router 为准，不以 Web 旧调用路径为准。

### 写操作

| 能力 | HTTP API | 输入 |
| --- | --- | --- |
| 创建项目 | `POST /api/v1/projects` | `name`、`displayName` |
| 创建 config set | `POST /api/v1/projects/{projectID}/config-sets` | `name`、`values` |
| 创建 secret set | `POST /api/v1/projects/{projectID}/secret-sets` | `name`、`values` |
| 创建 registry credential | `POST /api/v1/projects/{projectID}/registry-credentials` | `name`、`server`、`username`、`password` |
| 创建服务 | `POST /api/v1/projects/{projectID}/services` | service spec |
| 更新服务 | 当前 Web 调用旧路径；服务端当前路径是 `PUT /api/v1/projects/{projectID}/services/{serviceID}` | display name 和 service spec |
| 重试失败服务 | 当前 Web 调用旧 action 路径；服务端当前 router 未暴露同名路径 | service ID |

### Web 暗含的资源模型

Web 里实际出现的 operator-facing 资源有：

- `project`
- `config-set`
- `secret-set`
- `registry-credential`
- `service`
- `revision`
- `deployment`
- platform overview / health

Web 没有覆盖当前 control-plane router 中大量 control 侧能力，例如 plane 管理、runtime node pool、operation history、incident、platform service account、project API token、logs query、inventory。

## 服务端当前 API 面

`minicloud` CLI 应以 control-plane 当前服务端 API 为准。

### HTTP API 已有资源

从 `internal/controlplane/api/router.go` 看，当前 HTTP API 已覆盖：

- auth / health / metrics
- projects
- config sets
- secret sets
- registry credentials
- services
- project API tokens
- project operations
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

这说明 CLI 不应该只复制 Web。v8 第一阶段要先覆盖 Web 已有能力，但命令树应为完整 control-plane API 预留位置。

### operator gRPC 已有资源

`OperatorService` 当前覆盖：

- overview
- planes list/get/sync
- projects list/get
- services list/get/create

它适合作为早期 CLI 的只读和少量写操作入口。但如果 CLI 要覆盖 config set、secret set、registry credential、logs、operations、incidents 等能力，需要扩展 operator gRPC，或者让 CLI 暂时直接调用 HTTP API。

v8 推荐策略：

- 早期 CLI 可以先复用 HTTP API，最快覆盖 Web 能力。
- `internal/operatorclient` 可以继续作为 gRPC 客户端基础，但不要让 CLI 同时出现两套不一致的输出模型。
- 如果后续希望全部走 gRPC，应先扩展 `OperatorService`，再把 CLI 迁过去。

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
minicloud --token "$MINICLOUD_ADMIN_TOKEN" project list
```

建议约定：

- `--addr`
  - 默认读取 `MINICLOUD_CONTROL_PLANE_URL`
  - fallback：`http://127.0.0.1:8080`
- `--token`
  - 默认读取 `MINICLOUD_ADMIN_TOKEN`
- `--project`
  - 设置项目作用域
- `-o, --output`
  - `table`、`json`、`yaml`
  - 默认 `table`
- `--timeout`
  - 单次请求超时
- `--verbose`
  - 输出调试信息到 stderr

### 输出

默认输出给人读：

```bash
minicloud service list --project team-a
```

示例：

```text
NAME      IMAGE             PHASE    HEALTHY  REPLICAS  REVISION
demo-web  nginx:1.27-alpine ready    true     2         rev_123
```

JSON 输出给脚本和后续 agent：

```bash
minicloud service list --project team-a -o json
```

要求：

- JSON 必须稳定。
- 错误写 stderr。
- 成功结果写 stdout。
- 退出码表达机器可读状态。

### 文件优先

资源期望状态统一走 YAML 文件：

```bash
minicloud project apply -f project.yaml
minicloud config-set apply -f config-set.yaml
minicloud secret-set apply -f secret-set.yaml
minicloud registry-credential apply -f registry-credential.yaml
minicloud service apply -f service.yaml
minicloud runtime-node-pool apply -f runtime-node-pool.yaml
```

设计约定：

- 创建和更新都使用 `apply -f`。
- YAML 是主要用户界面，也是后续 agent 修改和解释资源的主要对象。
- CLI 不提供长 flag 表单来拼装 service spec，例如不设计 `service create --image --replicas --port ...` 作为主路径。
- `get -o yaml` 输出应尽量能作为 `apply -f` 的输入基础。
- 复杂资源支持 `apiVersion`、`kind`、`metadata`、`spec` 结构。

命令行 flag 只保留操作上下文：

- 连接配置：`--addr`、`--token`
- 作用域：`--project`
- 输出：`-o json|yaml|table`
- 查询过滤：`--since`、`--limit`、`--selector`
- 安全确认：`--dry-run`、`--yes`

### 写操作安全

写操作约定：

- `apply -f` 支持 `--dry-run` 时，只做输入校验和预览，不提交变更。
- 危险操作支持 `--yes` 跳过交互确认。
- 删除、回滚、drain 必须打印目标资源摘要。
- 密钥类输入不在默认 table 输出中回显 secret value。

## CLI 资源模型

### 平台级

- `status`
- `health`
- `auth`
- `logs`
- `operation`

### 项目级

- `project`
- `config-set`
- `secret-set`
- `registry-credential`
- `api-token`

### 服务级

- `service`
- `revision`
- `deployment`

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

### 项目

```bash
minicloud project list
minicloud project get <project>
minicloud project apply -f project.yaml
minicloud project transfer <project> --owner-user-id <user>
minicloud project delete <project>
```

映射：

- `GET /api/v1/projects`
- `POST /api/v1/projects`
- `GET /api/v1/projects/{projectID}`
- `PUT /api/v1/projects/{projectID}`
- `POST /api/v1/projects/{projectID}/actions/transfer-ownership`
- `DELETE /api/v1/projects/{projectID}`

### Config Set

```bash
minicloud config-set list --project team-a
minicloud config-set get --project team-a <config-set>
minicloud config-set apply -f config-set.yaml
```

映射：

- `GET /api/v1/projects/{projectID}/config-sets`
- `POST /api/v1/projects/{projectID}/config-sets`

当前服务端没有单个 config set get 路由；CLI 可先从 list 里筛选，后续再补 API。

### Secret Set

```bash
minicloud secret-set list --project team-a
minicloud secret-set get --project team-a <secret-set>
minicloud secret-set apply -f secret-set.yaml
```

映射：

- `GET /api/v1/projects/{projectID}/secret-sets`
- `POST /api/v1/projects/{projectID}/secret-sets`

默认输出只展示 key 名，不展示 value。

### Registry Credential

```bash
minicloud registry-credential list --project team-a
minicloud registry-credential get --project team-a <credential>
minicloud registry-credential apply -f registry-credential.yaml
```

映射：

- `GET /api/v1/projects/{projectID}/registry-credentials`
- `POST /api/v1/projects/{projectID}/registry-credentials`

registry credential YAML 不应直接提交真实密码。实现时可以支持 `passwordFrom`，例如从环境变量、stdin 或本地文件读取，避免 shell history 和 Git 泄漏。

### Service

```bash
minicloud service list --project team-a
minicloud service get --project team-a demo-web
minicloud service apply -f service.yaml
minicloud service delete --project team-a demo-web
minicloud service retry --project team-a demo-web
```

映射：

- `GET /api/v1/projects/{projectID}/services`
- `POST /api/v1/projects/{projectID}/services`
- `GET /api/v1/projects/{projectID}/services/{serviceID}`
- `PUT /api/v1/projects/{projectID}/services/{serviceID}`
- `DELETE /api/v1/projects/{projectID}/services/{serviceID}`

`retry` 当前 Web 有入口，但当前 router 未暴露同名 service action。实现 CLI 前需要先确认是补 HTTP route、改成 service controller 现有操作，还是删除这个语义。

建议 `service apply -f` 支持的 YAML：

```yaml
apiVersion: minicloud.io/v1
kind: Service
metadata:
  name: demo-web
  displayName: Demo Web
  project: team-a
spec:
  provider: aliyun
  region: cn-beijing
  replicas: 1
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

### Revision

```bash
minicloud revision list --project team-a --service demo-web
minicloud revision get --project team-a --service demo-web <revision>
```

映射：

- Web 当前调用 `GET /api/v1/services/{serviceID}/revisions`。
- 当前 router 片段没有对应新路径。实现 CLI 前需要确认 revision API 是否已经迁移到别处，或补齐项目作用域路径。

### Deployment

```bash
minicloud deployment list --project team-a --service demo-web
minicloud deployment get --project team-a --service demo-web <deployment>
```

映射：

- Web 当前调用 `GET /api/v1/services/{serviceID}/deployments`。
- 当前 router 片段没有对应新路径。实现 CLI 前需要确认 deployment API 是否已经迁移到别处，或补齐项目作用域路径。

### Logs

```bash
minicloud logs query --since 30m --limit 100
minicloud logs service --project team-a demo-web --since 30m
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
minicloud operation list --project team-a
```

映射：

- `GET /api/v1/control/operations`
- `GET /api/v1/projects/{projectID}/operations`

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

### Tokens and Service Accounts

```bash
minicloud project-token list --project team-a
minicloud project-token apply -f project-token.yaml
minicloud project-token delete --project team-a <token>

minicloud service-account list
minicloud service-account apply -f service-account.yaml
minicloud service-account delete <account>
```

映射：

- `GET /api/v1/projects/{projectID}/api-tokens`
- `POST /api/v1/projects/{projectID}/api-tokens`
- `DELETE /api/v1/projects/{projectID}/api-tokens/{tokenID}`
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
minicloud project list
minicloud project get
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
minicloud project apply -f project.yaml
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
minicloud service retry
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

- Web 使用的 `/api/v1/platform/overview` 和当前 router 是否仍一致。
- Web 使用的非项目作用域 service detail / revisions / deployments 路径是否已经废弃。
- `service retry` 是否应该保留；当前 router 片段没有 `/actions/retry`。
- `service rollback` 不进入 v8 核心能力；需要恢复旧版本时重新 apply 旧 spec。
- `node drain` 是否指 runtime node、plane operation，还是 node-agent 侧节点维护；当前 router 明确支持 plane operation，但没有独立 node drain 命令。

这些缺口不阻塞 v8 文档，但会影响 CLI 写操作实现顺序。
