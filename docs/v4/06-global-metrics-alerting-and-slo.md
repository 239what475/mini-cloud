# v4/06 Global Metrics Alerting And SLO

这一章不再继续扩充：

- `03`
  的全局 inventory
- `05`
  的日志聚合

它要解决的是另一件事：

- 现在是否已经异常
- 影响面大不大
- 有没有打穿目标
- 是否应该立刻处理

也就是：

- `03`
  回答“现在有哪些 plane、节点和容量”
- `05`
  回答“具体发生了什么”
- `06`
  回答“现在是不是已经值得报警，是否已经碰到了 `SLO`”

## 先把这一章的主路径说清楚

这一章没有新增一套：

- `fleet reliability json store`
- 自己维护的时序数据库

当前的取舍是：

1. 每个 plane 继续暴露自己的：
   - `GET /metrics`
   - `GET /api/v1/platform/reliability`
2. fleet manager 自己的：
   - `GET /metrics`
   继续暴露本地 control-plane 指标，
   同时把 fleet 已登记 plane 的同步状态也导出出来
3. 真正的全局聚合与规则评估交给：
   - `Prometheus`
   - `Alertmanager`
   - `Grafana`

所以这一章的核心不是：

- 再做一个新的 fleet 只读接口

而是：

- 把 Prometheus 真正需要的原始信号补出来
- 让全局告警与全局 `SLO`
  可以直接建立在 `/metrics` 上

## 这一章新增了哪些可靠性信号

前面已有的单 plane 可靠性里，
已经有这三条：

- 最近 24 小时发布成功率
- 当前 worker 就绪率
- 最近 5 分钟 gateway 成功率

但如果只停在这些上面，
`ROADMAP`
里 06 想做的几类告警还不够落地。

所以这一章继续补了两类直接来自现有状态机的数据：

### 1. deployment stuck

这里的意思不是：

- deployment 已经 `failed`

而是：

- deployment 长时间停留在：
  - `pending`
  - `scheduling`
  - `assigned`
  - `deploying`

当前阈值先定成：

- `600s`

也就是：

- 超过 10 分钟还停在非终态，
  就认为这次 rollout 卡住了

### 2. worker bootstrap stuck

这里也不是：

- worker 已经 `offline`

而是：

- runtime worker 已经被 provision 出来
- 但它长时间还停在：
  - `provisioning`

当前阈值同样先定成：

- `600s`

也就是：

- 云侧实例可能已经创建出来了
- 但 agent 没启动、bootstrap 没完成，
  或者 ready 链路没有走完

这两个信号都直接来自当前已经存在的：

- `deployments.updated_at`
- `runtime_workers.provisioned_at`

所以它们不是“猜测性告警”，
而是直接建立在现有状态机和时间戳上的。

## 为什么 06 不能只暴露 ratio

这是这一章最关键的实现点。

如果 `/metrics`
里只有：

- `minicloud_slo_ratio`

那么 Prometheus 只能看到：

- 每个 plane 自己算完后的结果

这会带来一个问题：

- 全局 `SLO`
  不能直接把多个 plane 的 ratio 再平均

因为：

- 一个 plane 的样本量可能是 10
- 另一个 plane 的样本量可能是 1000

所以这一章新增了：

- `minicloud_slo_window_events{id="...",result="good"}`
- `minicloud_slo_window_events{id="...",result="total"}`

这里要特别注意：

- 它是“当前固定窗口里的样本数”
- 不是单调递增 counter

也就是说，
它的职责是：

- 让 Prometheus 能按同一口径做多 plane 聚合

不是：

- 替代后续更正式的长期 counter / burn-rate 设计

真正的全局 `SLO`
应该由 Prometheus 去做：

- 先把所有 plane 的 `good`
  相加
- 再把所有 plane 的 `total`
  相加
- 最后再求比值

这才是同口径聚合。

## 06/2：把事件类信号切到长期 counter

上面这套：

- `minicloud_slo_window_events`

已经能解决：

- 多 plane 不再错误地平均 ratio

但它仍然是：

- control-plane 预先算好的固定窗口 gauge

所以 `06/2`
再往前走一步：

- 把事件流改成长期 counter
- 让 `Prometheus`
  自己用：
  - `increase()`
  - `rate()`
  算窗口

当前新增的三组长期 counter 是：

- `minicloud_gateway_request_events_total`
- `minicloud_deployment_rollout_outcomes_total`
- `minicloud_runtime_worker_bootstrap_events_total`

这里最关键的一点是：

- 这次不是简单对现有业务表做 `COUNT(*)`

因为：

- `gateway_request_events`
  会跟着 app / execution 级联删除
- `deployment_transitions`
  会跟着 deployment 级联删除

如果直接拿这些表现算“累计值”，
那么一旦业务资源被清理，
counter 就会回退，
Prometheus 看到的更像一次：

- reset

所以 `06/2`
实际落地的是：

- 独立的持久化 counter 表
- 配套的去重 mark 表

写入路径分别是：

- 写 gateway event 时递增 gateway counter
- deployment 第一次进入终态时写 rollout outcome mark，
  再递增 rollout counter
- runtime worker bootstrap 开始 / ready 时写 mark，
  再递增 bootstrap counter

这样做完之后，
`Prometheus`
就能稳定地对这些事件类信号使用：

- `increase(counter[5m])`
- `increase(counter[24h])`

而不是只能吃 control-plane 已经算好的窗口值。

这里还有一个迁移边界要明确：

- `06/2`
  migration 会把当前数据库里还存在的：
  - gateway event
  - rollout outcome
  - runtime worker bootstrap mark
  回填成初始 counter 值

所以这些 counter 从这次版本开始，
会成为后续长期统计的权威来源；
但对更早之前、而且已经被业务级联删除掉的历史，
它当然不可能凭空补回来。

## 哪些规则已经切到 counter 路径，哪些继续保留 gauge

`06/2`
不是把所有规则都一刀切成 counter，
而是按信号类型拆开：

### 已经切到 counter 路径的

- gateway 成功率
- rollout 成功率
- runtime worker bootstrap 成功率记录规则

这些本质上都是：

- 一段时间里的事件成功 / 失败比

所以默认 recording rule
现在改成直接基于：

- `increase(counter[window])`

来算。

### 继续保留 gauge 路径的

- `MiniCloudDeploymentRolloutStuck`
- `MiniCloudRuntimeWorkerBootstrapStuck`
- `MiniCloudPlaneOffline`

原因很简单：

- 这些不是“最近发生了多少次”
- 而是“当前是不是已经卡住 / 已经离线”

所以继续使用：

- 当前状态 gauge

反而更准确。

## 这一章现在导出了哪些关键指标

### 本地 plane 指标

当前 `/metrics`
里最关键的新指标是：

- `minicloud_slo_window_events`
  - 单 plane `SLO`
    的原始 `good/total`
- `minicloud_deployments_stuck`
  - 按状态拆分的卡住 deployment 数量
- `minicloud_deployments_stuck_oldest_age_seconds`
  - 当前最老的非终态 deployment 年龄
- `minicloud_runtime_workers_bootstrap_stuck`
  - 超过阈值仍未完成 bootstrap 的 runtime worker 数量
- `minicloud_runtime_workers_oldest_provisioning_age_seconds`
  - 当前最老 provisioning worker 的年龄
- `minicloud_gateway_request_events_total`
  - 给 `Prometheus`
    用 `increase()`
    算窗口的长期 gateway counter
- `minicloud_deployment_rollout_outcomes_total`
  - rollout 首次终态结果的长期 counter
- `minicloud_runtime_worker_bootstrap_events_total`
  - runtime worker bootstrap 的长期 counter

### fleet manager 指标

fleet manager 自己的 `/metrics/fleet`
还会额外导出：

- `minicloud_fleet_planes`
- `minicloud_fleet_plane_registered`
- `minicloud_fleet_plane_status{status="..."}`
- `minicloud_fleet_plane_last_sync_age_seconds`
- `minicloud_fleet_plane_last_heartbeat_age_seconds`

这里要注意：

- 这些指标反映的是：
  - fleet 对 plane 的纳管与同步视角

不是：

- 远端 plane 内部所有运行时明细

所以：

- `plane offline`
  这种 fleet-native 告警
  适合直接用这些指标
- 而：
  - gateway 错误率
  - rollout 成功率
  - worker readiness
  这类更像 plane 自己的可靠性，
  还是应该直接抓每个 plane 的 `/metrics`

这也是为什么这一章显式把 fleet 指标放在：

- `/metrics/fleet`

而不是继续混进：

- `/metrics`

这样 plane 自己的指标和 fleet 纳管指标边界更清楚。

## 当前默认规则先落哪四类

这一章先把下面四类告警接进默认规则：

- `MiniCloudPlaneOffline`
- `MiniCloudDeploymentRolloutStuck`
- `MiniCloudRuntimeWorkerBootstrapStuck`
- `MiniCloudGatewayErrorRatioHigh`

这里故意先不把：

- `provider API failure`

直接做成默认规则。

原因很简单：

- 现在 provider 层还没有统一的失败计数模型

如果现在硬接，
最后只能靠：

- 错误字符串匹配

这条边界不干净，
所以这一项留到后续章节再补。

## 本地监控栈怎么启动

先确保本地 control-plane 已经跑在：

- `127.0.0.1:18080`

如果你直接用当前模拟环境，
可以先启动：

```bash
cd projects/mini-cloud

MINICLOUD_SIM_START_LOG_STACK=1 \
MINICLOUD_IMAGE_PREFIX=docker.m.daocloud.io/ \
bash ./scripts/simulated-env.sh start
```

然后再启动完整观测栈：

```bash
cd projects/mini-cloud

MINICLOUD_COMPOSE_LOG_DIR=/tmp/mini-cloud-v2-simulated-env/logs \
MINICLOUD_IMAGE_PREFIX=docker.m.daocloud.io/ \
docker compose -f deploy/compose/docker-compose.yml up -d \
  loki promtail prometheus alertmanager grafana
```

这时本地会有这些入口：

- `http://127.0.0.1:9090`
  - `Prometheus`
- `http://127.0.0.1:9093`
  - `Alertmanager`
- `http://127.0.0.1:3000`
  - `Grafana`

## 为什么 Prometheus 能抓到宿主机上的 control-plane

当前 `compose`
里的 `prometheus`
默认抓的是：

- `host.docker.internal:18080/metrics`
- `host.docker.internal:18080/metrics/fleet`

并且已经显式加了：

- `host.docker.internal:host-gateway`

所以在 Linux 上，
Prometheus 容器也能访问宿主机的：

- `18080`

这件事的意义是：

- 你不用把 control-plane 再塞进 compose
- 也不用给本地实验额外改网络结构

## 如果要再抓远端 plane，该怎么加

这一章不让你直接改：

- `deploy/compose/prometheus/config.yml`

而是通过：

- `file_sd`

追加远端 targets。

本地实际文件路径是：

- `deploy/compose/prometheus/file_sd/planes.json`

仓库里提交的是示例：

- `deploy/compose/prometheus/file_sd/planes.json.example`

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

只要这些 plane 自己也在暴露：

- `/metrics`

Prometheus 就会把它们一起抓进来。

## 这章怎么验证

### 1. 先看本地指标里有没有新信号

```bash
curl -s http://127.0.0.1:18080/metrics | grep minicloud_gateway_request_events_total
curl -s http://127.0.0.1:18080/metrics | grep minicloud_deployment_rollout_outcomes_total
curl -s http://127.0.0.1:18080/metrics | grep minicloud_runtime_worker_bootstrap_events_total
curl -s http://127.0.0.1:18080/metrics | grep minicloud_deployments_stuck
curl -s http://127.0.0.1:18080/metrics | grep minicloud_runtime_workers_bootstrap_stuck
curl -s http://127.0.0.1:18080/metrics/fleet | grep minicloud_fleet_plane_status
```

你至少应该能看到：

- `minicloud_gateway_request_events_total`
- `minicloud_deployment_rollout_outcomes_total`
- `minicloud_runtime_worker_bootstrap_events_total`
- `minicloud_deployments_stuck`
- `minicloud_runtime_workers_bootstrap_stuck`

如果当前已经有 fleet plane，
还会看到：

- `minicloud_fleet_plane_status`

### 2. 在 Prometheus 里直接看全局 SLO 聚合

打开：

- `http://127.0.0.1:9090`

先试这一条：

```promql
sum(increase(minicloud_gateway_request_events_total{result="good"}[5m]))
/
clamp_min(sum(increase(minicloud_gateway_request_events_total{result="total"}[5m])), 1)
```

这条表达式的意义就是：

- 不去平均各 plane 的 ratio
- 而是先合并所有 plane 的样本
- 再算全局 gateway 成功率

再试这一条：

```promql
minicloud_fleet_plane_status{status="offline"} == 1
```

如果某个 plane 被 fleet 标成：

- `offline`

这里就会直接出现结果。

### 3. 再验证 counter 规则已经切到 `increase()`

再试这一条：

```promql
sum(increase(minicloud_deployment_rollout_outcomes_total{result="success"}[24h]))
/
clamp_min(sum(increase(minicloud_deployment_rollout_outcomes_total{result="total"}[24h])), 1)
```

还有这一条：

```promql
sum(increase(minicloud_runtime_worker_bootstrap_events_total{result="ready"}[30m]))
/
clamp_min(sum(increase(minicloud_runtime_worker_bootstrap_events_total{result="started"}[30m])), 1)
```

如果这两条都能正常返回，
说明现在：

- gateway
- rollout
- worker bootstrap

这三类事件信号，
已经切到长期 counter 路径了。

### 3. 在 Alertmanager 里看默认规则是否进入 firing

打开：

- `http://127.0.0.1:9093`

当前这套配置默认只做本地 UI 展示，
不会主动发通知。

所以你在这一步主要确认的是：

- 规则有没有被正确评估
- 哪些告警已经进入 `firing`
- 它们的标签是不是足够回到：
  - `03`
    看 blast radius
  - `05`
    按 `plane_id`、`platform_name`、`request_id`
    去钻日志

### 4. 回到 platform reliability 看单 plane 视角

这一章虽然主路径是 Prometheus，
但单 plane 自己的可靠性视图仍然保留：

```bash
curl -s \
  -H 'Authorization: Bearer admin-secret' \
  http://127.0.0.1:18080/api/v1/platform/reliability
```

你现在会比前一章多看到两类 alert：

- `deployment-rollout-stuck`
- `worker-bootstrap-stuck`

这两个 alert
就是 Prometheus 默认规则背后的本地信号来源。

## 这一章和 03、05 怎么接起来

这一章真正想让你形成的排障顺序应该是：

1. 先在 `06`
   看到：
   - alert firing
   - `SLO` 下降
2. 再去 `03`
   看：
   - 影响到了哪些 plane / provider / region
3. 再去 `05`
   用：
   - `plane_id`
   - `platform_name`
   - `request_id`
   继续钻日志
4. 最后回到 `06`
   确认：
   - alert 已恢复
   - `SLO` 已回到目标上方

所以：

- `03`
  是 blast radius
- `05`
  是证据链
- `06`
  是触发器和收敛器

## 这一章当前的边界

这一章现在已经做成的是：

- 单 plane 新可靠性信号接入
- 原始 `good/total`
  指标导出
- fleet plane 同步状态指标导出
- 本地 `Prometheus + Alertmanager + Grafana`
  栈
- 4 类默认规则

这一章刻意还没做的是：

- `provider API failure`
  默认告警
- 多窗口 burn-rate
- 自动把 alert 变成 incident
- 单独的 fleet reliability JSON API

这些事情不是不做，
而是现在先不越界。

这一章先把：

- 指标口径
- 规则入口
- 告警闭环

这三件事收稳。

## 本章涉及的主要文件

- [observability.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/observability/observability.go)
- [reliability_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/observability/reliability_test.go)
- [observability_store.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/store/observability_store.go)
- [observability_handler.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/observability_handler.go)
- [router.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/router.go)
- [httpapi_integration_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/httpapi_integration_test.go)
- [docker-compose.yml](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/compose/docker-compose.yml)
- [config.yml](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/compose/prometheus/config.yml)
- [minicloud-alerts.yml](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/compose/prometheus/rules/minicloud-alerts.yml)
- [config.yml](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/compose/alertmanager/config.yml)
- [prometheus.yaml](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/compose/grafana/provisioning/datasources/prometheus.yaml)
- [README.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/compose/README.md)

## 本章检查点

- plane 自己的 `/metrics`
  现在已经补上：
  - `deployment-rollout-stuck`
  - `worker-bootstrap-stuck`
  相关信号
- fleet 指标已经单独拆到：
  - `/metrics/fleet`
  不再和 plane 指标混用
- 本地 `Prometheus + Alertmanager + Grafana`
  栈已经能按 compose 配置真实启动
- 默认规则当前先覆盖：
  - `plane offline`
  - `deployment stuck`
  - `worker bootstrap stuck`
  - `gateway error ratio`
- 当前全局 `SLO`
  仍基于：
  - 固定窗口 `window_events`
    聚合
  下一步会继续补长期 counter 和 `rate()/increase()`
