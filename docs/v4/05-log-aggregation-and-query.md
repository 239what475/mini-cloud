# v4/05 Log Aggregation And Query

这一章开始把
`v4/04`
已经统一好的结构化运行日志，
真正收进一个可查询的聚合后端里。

这一章先做成的最小闭环是：

- 本地 `Loki`
  负责存日志
- `Promtail`
  负责采集宿主机日志文件
- control-plane
  新增两个日志查询 API
- 本地模拟环境
  能真实把 `control-plane`
  和 `agent`
  日志打进 `Loki`

也就是说，
`v4/05`
优先解决的是：

- 04 里的结构化日志能不能被真正聚合和检索

而不是：

- retention 策略
- 大规模多租户隔离
- 正式告警规则
- 所有日志来源一次性收全

## 先把这章边界说清楚

这一章当前已经纳入本地 `Loki`
的日志来源是：

- `control-plane.log`
- `sim-worker-a.log`
- `sim-worker-b.log`

这里有两个边界要先记住：

1. gateway 日志当前并不单独落文件
   - 它和 `control-plane`
     共用同一个进程日志流
2. runtime / Docker 相关日志
   - 这一章还没有新增单独的文件采集源
   - 但这类内容仍可能出现在：
     - `control-plane.log`
     - `sim-worker-*.log`
       里

所以这章不是在说：

- 所有来源都已经完整进 `Loki`

而是在说：

- 04 里最关键的进程运行日志，
  先用最小真实链路打通

## 为什么 04 的 `key=value` 文本已经够用了

这是这章最重要的理解点。

`v4/04`
虽然默认输出看起来只是：

- `key=value`
  的文本行

但这些行本质上已经是：

- 稳定字段集合

所以到了 `v4/05`，
我们不需要先把日志改成：

- `JSON`

也能查。

因为这一章走的是：

1. `Promtail`
   先把原始日志行送进 `Loki`
2. 查询时再在 `LogQL`
   里加：
   - `| logfmt`
3. 然后再按：
   - `request_id`
   - `platform_name`
   - `deployment_id`
   - `node_id`
   - `worker_id`
   这些字段过滤

所以：

- 04 里的“结构化字段约定”
- 05 里的“聚合与检索”

是直接接上的。

## 先分清三层过滤语义

这一章如果不先分层，
后面很容易把所有 query 参数都看成一回事。

实际上现在有三层。

### 1. stream selector

这一层只用低基数标签。

当前保留下来的标签只有：

- `job="mini-cloud"`
- `component="control-plane"` 或 `component="agent"`

这样做是故意的，
因为像：

- `request_id`
- `app_id`
- `deployment_id`
- `node_id`

这些字段如果直接做成 label，
基数会很快失控。

### 2. 原始日志行包含匹配

这一层对应：

- `contains`

它会先做：

- `|= "xxx"`

也就是：

- 先按原始文本行做包含过滤

### 3. 结构化字段过滤

这一层才是：

- `| logfmt`
  之后的字段匹配

例如：

- `platform_name`
- `plane_id`
- `project_id`
- `app_id`
- `deployment_id`
- `node_id`
- `worker_id`
- `execution_id`
- `request_id`
- `level`

所以现在的查询思路应该理解成：

- 先用低基数 label 缩范围
- 再按需要做原始文本过滤
- 最后用 `| logfmt`
  精准过滤结构化字段

## 这一章新增了两个 API

control-plane
现在新增了两个 admin-only 日志查询接口：

- `GET /api/v1/platform/logs`
- `GET /api/v1/fleet/logs`

它们都走同一个：

- `Loki query_range`
  后端

但作用域不是一回事。

### `GET /api/v1/platform/logs`

这个接口现在会强制绑定当前平台自己的：

- `platform_name`

也就是说，
它不是“任意传个 `platformName` 就能查”的开放入口，
而是：

- 本地 platform 视角日志

这也是为什么这一章专门把
`agent`
本地模拟环境的启动参数补上了：

- `--platform-name`

否则：

- `control-plane`
  日志能查到
- `agent`
  日志却会因为缺少 `platform_name`
  被当前平台作用域排除掉

如果当前进程根本没有拿到本地平台名，
这个接口现在会直接 fail-closed，
返回：

- `503`

而不是退化成一个“谁都能查”的宽查询入口。

### `GET /api/v1/fleet/logs`

这个接口当前不强制绑定本地：

- `platform_name`

它更接近：

- fleet 视角聚合查询入口

所以这两个路径虽然都查同一个后端，
但现在已经不是简单别名了。

## 这两个 API 支持哪些过滤参数

当前支持：

- `component`
- `platformName`
- `planeID`
- `projectID`
- `appID`
- `deploymentID`
- `nodeID`
- `workerID`
- `executionID`
- `requestID`
- `level`
- `contains`
- `start`
- `end`
- `since`
- `limit`
- `direction`

这里再记两个规则：

1. `since`
   只在没显式给 `start`
   时生效
2. `limit`
   当前公开上限是：
   - `2000`

如果时间参数明显不合法，
例如：

- `start > end`

现在会直接返回：

- `400`

而不是再误报成：

- `502`

## 本地日志栈怎么起

### 方式 1：手动起本地日志栈

如果你只想看
`Loki/Promtail/Grafana`
本身，
可以直接：

```bash
MINICLOUD_COMPOSE_LOG_DIR=/tmp/mini-cloud-v2-simulated-env/logs \
MINICLOUD_IMAGE_PREFIX=docker.m.daocloud.io/ \
docker compose -f deploy/compose/docker-compose.yml up -d loki promtail
```

如果还想开：

- `Grafana`

再手动补一条：

```bash
MINICLOUD_IMAGE_PREFIX=docker.m.daocloud.io/ \
docker compose -f deploy/compose/docker-compose.yml up -d grafana
```

这次实验里我实际遇到过一个现象：

- 当前环境直连 `docker.io`
  拉镜像超时

所以这套 `compose`
支持通过：

- `MINICLOUD_IMAGE_PREFIX`

显式切到镜像代理。

### 方式 2：让 `simulated-env.sh` 自动接线

如果你想直接把本地模拟环境和日志采集链路一起拉起来，
可以用：

```bash
MINICLOUD_SIM_START_LOG_STACK=1 \
MINICLOUD_IMAGE_PREFIX=docker.m.daocloud.io/ \
bash ./scripts/simulated-env.sh start
```

这里再分清两个变量：

- `MINICLOUD_SIM_START_LOG_STACK=1`
  - 脚本自动起本地：
    - `Loki`
    - `Promtail`
- `MINICLOUD_SIM_LOKI_URL`
  - control-plane
    用哪个 `Loki`
    做查询后端
- `MINICLOUD_SIM_LOKI_TENANT_ID`
  - 如果你接的是多租户 `Loki`
    可以额外带上租户头

如果只设置了：

- `MINICLOUD_SIM_START_LOG_STACK=1`

脚本会默认把 control-plane 接到：

- `http://127.0.0.1:3100`

如果两个都设置了，
则：

- 本地采集链路照样会起
- control-plane
  查询日志时优先使用你显式传入的：
  - `MINICLOUD_SIM_LOKI_URL`

这里再补一个清理规则：

- 如果是脚本自己起的：
  - `Loki`
  - `Promtail`
  那么：
  - `bash ./scripts/simulated-env.sh reset`
    会一起清掉
- 如果是你手动：
  - `docker compose up -d ...`
    起的
  那么：
  - `reset`
    只会清模拟环境本身
  - 手动起的日志栈需要你自己再：
    - `docker compose down -v`

## 这一章的最小真实验证

这次我实际验证通过的最小闭环是：

1. 起本地模拟环境和：
   - `Loki`
   - `Promtail`
2. 确认：
   - `control-plane`
     healthz 正常
3. 直接查：
   - `Loki query_range`
4. 再查：
   - `GET /api/v1/platform/logs`

### 1. control-plane healthz

实际返回示例：

```json
{
  "database": "ok",
  "service": "mini-cloud-control-plane",
  "status": "ok",
  "time": "2026-04-14T05:49:48Z"
}
```

### 2. 直接查 Loki 里的 agent 日志

实际查询条件就是：

```logql
{component="agent",job="mini-cloud"} | logfmt | platform_name="mini-cloud-simulated-v2"
```

实际能查到：

- `sim-worker-a.log`
- `sim-worker-b.log`

并且日志行里已经带上：

- `platform_name=mini-cloud-simulated-v2`
- `node_id=...`
- `request_id=...`

这说明本地模拟环境里的：

- `agent -> file -> promtail -> loki`

链路已经打通。

### 3. 通过 `/api/v1/platform/logs` 查 agent 日志

实际请求示例：

```bash
curl -H 'Authorization: Bearer sim-admin-token' \
  'http://127.0.0.1:18080/api/v1/platform/logs?component=agent&limit=5'
```

实际返回里可以看到：

- `backend: "loki"`
- `direction: "backward"`
- `query: {component="agent",job="mini-cloud"} | logfmt | platform_name="mini-cloud-simulated-v2"`

以及真实日志项。

这说明：

- `platform/logs`
  当前平台作用域
- `agent`
  日志
- `platform_name`
  强制过滤

这三件事已经在 API 层真正连起来了。

## 现在怎么查 04 里的字段

到了这一章，
你应该开始习惯这样查：

按请求链路查：

```logql
{job="mini-cloud",component="control-plane"} | logfmt | request_id="req_demo"
```

按部署查：

```logql
{job="mini-cloud",component="control-plane"} | logfmt | deployment_id="dep_demo"
```

按节点查 agent：

```logql
{job="mini-cloud",component="agent"} | logfmt | node_id="node_demo"
```

按当前平台范围查 agent：

```logql
{job="mini-cloud",component="agent"} | logfmt | platform_name="mini-cloud-simulated-v2"
```

所以这一章最关键的学习结果不是：

- 记住 `Loki`
  某个配置文件长什么样

而是：

- 04 里定下来的字段
  到 05 里已经真的可以拿来查

## 本章涉及的主要文件

- [logquery.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/common/logquery/logquery.go)
- [logquery_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/common/logquery/logquery_test.go)
- [log_query_handler.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/log_query_handler.go)
- [log_query_handler_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/log_query_handler_test.go)
- [router.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/router.go)
- [app.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/app.go)
- [config.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/config/config.go)
- [docker-compose.yml](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/compose/docker-compose.yml)
- [config.yaml](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/compose/loki/config.yaml)
- [config.yaml](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/compose/promtail/config.yaml)
- [loki.yaml](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/compose/grafana/provisioning/datasources/loki.yaml)
- [README.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/compose/README.md)
- [simulated-env.sh](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/scripts/simulated-env.sh)
- [managed.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/cmd/agent/managed.go)

## 本章检查点

- 本地 `Loki + Promtail`
  已经能真实收进：
  - `control-plane`
  - `agent`
    日志
- 当前只保留低基数 label：
  - `job`
  - `component`
- `request_id`、`platform_name`、`node_id` 等字段继续留在日志行里，
  查询时走：
  - `| logfmt`
- `GET /api/v1/platform/logs`
  已经具备当前平台作用域，
  会强制绑定：
  - `platform_name`
- `GET /api/v1/fleet/logs`
  当前仍保留为更宽的聚合查询入口
- 本地模拟环境里的 agent
  现在也会带上：
  - `platform_name`
  所以平台作用域下可以真正查到 agent 日志
