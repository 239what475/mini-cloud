# compose

这里放 `mini-cloud` 的本地开发环境文件。

当前这套 `compose` 主要服务三类场景：

- 本地 `cloud-plane`
  开发和单元/集成实验需要的基础依赖
- `v4/05`
  的日志聚合实验
- `v4/06`
  的指标和告警
  实验
- `v5/07`
  的应用侧观测实验
  - `OTel Collector`
  - `Tempo`
  - `Grafana`

目前包含：

- `postgres`
  - 本地数据库
- `loki`
  - 日志聚合后端
- `promtail`
  - 把宿主机日志文件采集进 `Loki`
- `grafana`
  - 统一查看 `Loki` 和 `Prometheus`
- `prometheus`
  - 抓取 control-plane 的 `/metrics/control`
    和 OTel Collector 的 `/metrics`，并评估告警规则
- `alertmanager`
  - 展示当前 firing / resolved 告警
- `tempo`
  - 本地 trace 后端
- `otel-collector`
  - 应用 `metrics / traces`
    的统一接收入口

这里要特别强调：

- 这套 `compose`
  只是本地实验辅助栈
- 它不是应用运行模型
- 应用容器仍然是：
  - `cloud-worker`
    通过 Docker API
    启动
- `compose`
  里放的是：
  - 数据库
  - 日志后端
  - 指标后端
  - trace 后端
  - 看板

## 当前采集范围

这套本地日志栈当前只采集两类文件：

- `cloud-plane.log`
- `sim-worker-*.log`

这里要明确两个边界：

- workload 运行日志当前和 `cloud-plane`
  在同一个进程日志流里
- runtime / Docker 相关日志
  这一章还没有单独采进来

也就是说，
`v4/05`
先解决的是：

- 04 里已经统一好的结构化运行日志
  能不能真实进 `Loki`
  并被 API / Grafana 查出来

不是一次性把所有来源都收全。

## 日志目录挂载

`promtail`
会把宿主机的日志目录挂到容器里的：

- `/var/log/mini-cloud`

默认挂载来源是：

- `/tmp/mini-cloud-v2-simulated-env/logs`

这里保留
`v2`
后缀只是历史目录名，
当前仓库不再维护
`simulated-env.sh`
脚本。
如果需要接日志，
请让本地进程直接把日志写入这个目录，
或者通过
`MINICLOUD_COMPOSE_LOG_DIR`
改成自己的目录。

如果你想改成别的目录，
可以在执行 `docker compose` 前设置：

- `MINICLOUD_COMPOSE_LOG_DIR`
- `MINICLOUD_IMAGE_PREFIX`

例如：

```bash
MINICLOUD_COMPOSE_LOG_DIR=/tmp/mini-cloud-v2-simulated-env/logs \
MINICLOUD_IMAGE_PREFIX=docker.m.daocloud.io/ \
docker compose -f deploy/compose/docker-compose.yml up -d loki promtail grafana
```

## v4/05 与 v4/06 最小启动方式

只起数据库：

```bash
docker compose -f deploy/compose/docker-compose.yml up -d postgres
```

起日志聚合栈：

```bash
MINICLOUD_COMPOSE_LOG_DIR=/tmp/mini-cloud-v2-simulated-env/logs \
MINICLOUD_IMAGE_PREFIX=docker.m.daocloud.io/ \
docker compose -f deploy/compose/docker-compose.yml up -d loki promtail grafana
```

起完整观测栈：

```bash
MINICLOUD_COMPOSE_LOG_DIR=/tmp/mini-cloud-v2-simulated-env/logs \
MINICLOUD_IMAGE_PREFIX=docker.m.daocloud.io/ \
docker compose -f deploy/compose/docker-compose.yml up -d \
  loki promtail prometheus alertmanager grafana tempo otel-collector
```

如果你只想手动补起指标与告警栈，
可以单独执行：

```bash
MINICLOUD_IMAGE_PREFIX=docker.m.daocloud.io/ \
docker compose -f deploy/compose/docker-compose.yml up -d prometheus alertmanager grafana
```

停止本地栈时手动执行：

- `docker compose down -v`

Grafana 默认地址：

- `http://127.0.0.1:3000`

默认账号密码都是：

- `admin`

这套 `compose`
按设计就是单机单实例实验环境，
默认会占用宿主机上的：

- `5432`
- `3100`
- `3000`
- `9090`
- `9093`
- `3200`
- `4318`
- `9464`

## 默认抓取目标

`Prometheus` 默认不再抓 cloud-plane `/metrics`；cloud-plane 已经是内部 gRPC 执行面。当前本地栈只抓 OTel Collector，并通过 file_sd 抓明确登记的 control-plane 指标端点。

这个 `planes.json`
里的每个 target
都应该直接指向 control-plane 指标端点：

- `/metrics/control`

例如：

```json
[
  {
    "targets": [
      "198.51.100.20:18080",
      "198.51.100.21:18080"
    ],
    "labels": {
      "plane_scope": "remote"
    }
  }
]
```

## v4/06 当前默认告警

当前这套规则先落这 4 类：

- `MiniCloudPlaneOffline`
- `MiniCloudDeploymentRolloutStuck`
- `MiniCloudNodeBootstrapStuck`

其中要注意一件事：

- `provider API failure`
  这一类还没有直接进入默认规则

原因不是忘了做，
而是当前 provider 层还没有统一的失败计数模型，
现在强行接进来只会变成字符串匹配，
边界不干净。

## v4/06/2 新增的长期 counter

`06/2`
开始把三类事件信号切到长期 counter：

- `minicloud_deployment_rollout_outcomes_total`
- `minicloud_node_bootstrap_events_total`

这样 `Prometheus`
就可以直接用：

- `increase()`
- `rate()`

自己算窗口，
不再只能依赖 cloud-plane 现算好的固定窗口 gauge。

这里要注意一个实现边界：

- 这些 counter 不是直接对业务表做 `COUNT(*)`

而是 cloud-plane 在写业务事件时，
同步递增独立的持久化 counter / mark 表。

这样即使后面清理 service / deployment，
也不会把给 `Prometheus`
看的长期 counter 直接删掉。

## 怎么查 04 里的结构化字段

这一章故意不把：

- `request_id`
- `platform_name`
- `deployment_id`
- `node_id`

做成 `Loki label`。

它们仍然保留在日志行里的 `key=value` 字段中，
查询时走：

- `| logfmt`

例如：

```logql
{job="mini-cloud",component="cloud-plane"} | logfmt | request_id="req_demo"
```

```logql
{job="mini-cloud",component="agent"} | logfmt | node_id="node_demo"
```

## v4/06/2 可以直接试的 PromQL

最近 24 小时 rollout 成功率：

```promql
sum(increase(minicloud_deployment_rollout_outcomes_total{result="success"}[24h]))
/
clamp_min(sum(increase(minicloud_deployment_rollout_outcomes_total{result="total"}[24h])), 1)
```

最近 30 分钟 node bootstrap 成功率：

```promql
sum(increase(minicloud_node_bootstrap_events_total{result="ready"}[30m]))
/
clamp_min(sum(increase(minicloud_node_bootstrap_events_total{result="started"}[30m])), 1)
```
