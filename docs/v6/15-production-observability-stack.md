# 15 Production Observability Stack

这一章正式把
`v6`
里的生产观测栈落地。

这里不再停留在：

- 本地 `compose`
  实验
- 单机日志 /
  指标 /
  trace
  验证

而是把已经存在的生产拓扑继续收成正式资产：

- Tencent
  主机
  - `control-plane`
  - Tencent `cloud-plane`
- Aliyun
  主机
  - 第二个
    `cloud-plane`

## 这章只做什么

这章只做四件事：

1. 正式增加中心观测栈部署资产
2. 正式增加每个 plane
   的轻量采集部署资产
3. 把平台进程日志收成可被中心 `Loki`
   统一查询的正式链路
4. 把生产平台配置里的观测字段语义拆干净

## 这章明确不做什么

这章刻意不做：

- 平台 tracing
  全链路埋点改造
- 每个 plane
  一整套
  `Grafana / Loki / Tempo`
- 中心直接跨云抓所有 worker
- 新业务 API
- front door /
  CDN /
  TLS /
  域名收口

## 为什么这章现在必须做

到 `14`
结束时，
平台已经具备：

- 两个长期运行的 plane
- 正式注册与绑定链路
- 显式 placement
- 可重复的 control /
  plane 部署资产

但 operator
还没有一套正式的生产观测栈。

这会带来两个问题：

1. 平台日志仍然分散在各台主机上
2. workload 日志 /
   trace /
   plane 指标
   还没有真正收成：
   - plane 侧采集
   - center 侧统一查询

## 这章最终采用的分层

这一章不再沿用：

- 一个大而全的本地
  `deploy/compose`
  实验目录

而是正式拆成两组生产资产：

- `deploy/observability-center/`
- `deploy/observability-plane/`

### center 侧

center
只放：

- `Prometheus`
- `Alertmanager`
- `Loki`
- `Tempo`
- `Grafana`

职责只有三类：

- 统一存储
- 统一查询
- 统一告警

它抓的也只有两类目标：

- 本机
  `control-plane`
  的
  `/metrics/control`
- 各个 plane
  自己暴露的
  `Prometheus /federate`

### plane 侧

每个 plane
只放轻量采集：

- `Alloy`
  - tail
    平台日志文件
  - 接workload 日志 push
  - 往中心 `Loki`
    转发
- `OTel Collector`
  - 接应用
    `OTLP metrics / traces`
  - 往中心 `Tempo`
    转发 traces
  - 对本 plane
    `Prometheus`
    暴露 metrics
- `Prometheus`
  - 抓
    `cloud-plane /metrics`
  - 抓
    `OTel Collector /metrics`
  - 后续需要时，
    再显式补
    私网 worker
    `/metrics`

这样收的好处是：

- center
  不直接跨云抓细粒度节点
- 每个 plane
  都有独立的采集入口
- 观测链路和平台三层结构保持一致

## 这章修掉的配置问题

在这章之前，
平台配置里的旧
`lokiUrl`
字段
其实混了两种语义：

- 一种是给 node `agent`
  往上推workload 日志
- 另一种是给
  `cloud-plane`
  自己查日志

这在生产分层里是不干净的，
因为：

- workload 日志 push
  入口应该指向本 plane
  的 `Alloy`
- 日志查询后端
  应该指向中心 `Loki`

所以这一章把它正式拆成：

- `workloadLogPushUrl`
- `lokiQueryUrl`
- `workloadOtlpEndpoint`

同时，
这一章正式收口后的日志标签约定是：

- 所有进中心
  `Loki`
  的平台日志 /
  workload 日志
  都统一带：
  - `job="mini-cloud"`

这样现有平台日志查询实现，
才可以继续稳定用：

- `{job="mini-cloud"}`

作为统一 selector。

## 这一章新增的正式资产

### 1. center 侧生产观测目录

新增：

- [deploy/observability-center/README.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/observability-center/README.md)
- [deploy/observability-center/docker-compose.yml.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/observability-center/docker-compose.yml.example)
- [deploy/observability-center/prometheus/config.yml](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/observability-center/prometheus/config.yml)
- [deploy/observability-center/prometheus/file_sd/planes.json.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/observability-center/prometheus/file_sd/planes.json.example)
- [deploy/observability-center/prometheus/rules/minicloud-production-alerts.yml](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/observability-center/prometheus/rules/minicloud-production-alerts.yml)

### 2. plane 侧生产观测目录

新增：

- [deploy/observability-plane/README.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/observability-plane/README.md)
- [deploy/observability-plane/docker-compose.yml.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/observability-plane/docker-compose.yml.example)
- [deploy/observability-plane/alloy/config.alloy](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/observability-plane/alloy/config.alloy)
- [deploy/observability-plane/otel-collector/config.yml](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/observability-plane/otel-collector/config.yml)
- [deploy/observability-plane/prometheus/config.yml.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/observability-plane/prometheus/config.yml.example)

### 3. 平台进程日志落盘

这一章还改了正式
`systemd`
模板：

- [mini-cloud-control-plane.service.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/control-plane/systemd/mini-cloud-control-plane.service.example)
- [mini-cloud-cloud-plane.service.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/cloud-plane/systemd/mini-cloud-cloud-plane.service.example)
- [mini-cloud-agent.service.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/cloud-plane/systemd/mini-cloud-agent.service.example)

现在它们会把 stdout/stderr
正式落到：

- `/var/log/mini-cloud/control-plane.log`
- `/var/log/mini-cloud/cloud-plane.log`
- `/var/log/mini-cloud/agent.log`

这样 plane 侧
`Alloy`
就可以直接按固定文件采集。

另外，
这套正式资产里的
`Alloy`
也会持久化自己的文件读取状态，
避免容器重建后把整份平台日志重新回放到中心
`Loki`。

## 生产平台配置现在该怎么写

`deploy/cloud-plane/platform-config.*.json.example`
现在都补上了：

```json
{
  "observability": {
    "workloadLogPushUrl": "http://<plane-private-ip>:3101",
    "lokiQueryUrl": "http://<center-loki-host>:3100",
    "lokiTenantId": "",
    "workloadOtlpEndpoint": "http://<plane-private-ip>:4318",
    "grafanaBaseUrl": "https://<grafana-host>"
  }
}
```

要特别注意：

- `workloadLogPushUrl`
  和
  `workloadOtlpEndpoint`
  都应该指向本 plane
  主机
- `lokiQueryUrl`
  应该指向中心
  `Loki`
- `grafanaBaseUrl`
  应该指向中心
  `Grafana`

## operator 现在的最小操作链

### 1. 在中心主机起中心观测栈

直接看：

- [deploy/observability-center/README.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/observability-center/README.md)

### 2. 在每个 plane
主机起 plane 侧采集

直接看：

- [deploy/observability-plane/README.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/observability-plane/README.md)

### 3. 回填 plane
平台配置

把：

- `workloadLogPushUrl`
- `workloadOtlpEndpoint`
- `lokiQueryUrl`

写回：

- [platform-config.tencent.json.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/cloud-plane/platform-config.tencent.json.example)
- [platform-config.aliyun.json.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/cloud-plane/platform-config.aliyun.json.example)

对应的实际文件。

### 4. 验证结果

最小验证要看到四件事：

1. 中心 `Prometheus`
   能看到：
   - `minicloud_plane_count`
   - federated
     `minicloud_gateway_request_events_total`
   - federated
     `minicloud_deployments_stuck`
2. 中心 `Loki`
   能同时查到：
   - `control-plane`
     日志
   - `cloud-plane`
     日志
   - `agent`
     日志
   - 应用 stdout/stderr
3. 中心 `Tempo`
   能查到双 plane
   上应用送来的 trace
4. 中心 `Grafana`
   默认带起：
   - `mini-cloud Production Operations`
     看板

另外，
如果这一台中心主机上的
`control-plane`
还要直接提供：

- `GET /api/v1/control/logs`

那么它自己的进程环境里也必须补：

- `MINICLOUD_LOKI_URL`

否则就算中心
`Loki`
已经跑起来，
控制面日志查询接口仍然会直接返回：

- `503`
- `log query backend is not configured`

## 真实验证补充

这章在真实双云环境里，
额外确认了两个非常具体的部署约束：

### 1. center 和 plane
同机但分两个
`compose`
项目时，
不能把 center
地址写成容器里的
`127.0.0.1`

原因很直接：

- plane
  侧的
  `alloy`
  和
  `otel-collector`
  都跑在容器里
- 容器里的
  `127.0.0.1`
  只指向容器自己
- 它不会自动回到宿主机上的 center

这次真实验证里，
就出现了：

- `Alloy`
  往
  `127.0.0.1:3100`
  push
  失败
- 日志采集容器在跑，
  但中心
  `Loki`
  查不到平台日志

因此同机部署时要改成：

- `host.docker.internal`

并确保：

- `alloy`
- `otel-collector`

都带上：

- `extra_hosts: host.docker.internal:host-gateway`

同时，
这次也确认了：

- plane
  侧
  `Alloy`
  需要带持久化 data
  目录

否则容器一旦重建，
平台文件日志会从头重放，
中心
`Loki`
 里会出现重复日志。

### 2. 跨云 plane
往中心推
logs / traces
时，
中心云防火墙还要额外放行

这次真实验证里，
指标联邦之所以先成功，
是因为阿里云 plane
对外放行了：

- `18081`
- `19090`

但日志 /
trace
链路仍然失败，
根因不是程序崩了，
而是中心所在云没有给远端 plane
放行：

- `3100/tcp`
- `4318/tcp`

表现就是：

- `Alloy`
  报
  `context deadline exceeded`
- `Loki`
  里没有远端 plane
  的日志
- `Tempo`
  也收不到远端 plane
  的应用 trace

也就是说，
真实生产里至少要把三类跨云入口分开看：

- plane southbound API
  - `18081`
- plane Prometheus federation
  - `19090`
- center 日志 /
  trace
  接收入口
  - `3100`
  - `4318`

### 3. 平台日志如果不带统一
`job`
标签，
平台自己的日志查询 API
会直接漏数

这次复查里还确认了一个实现约束：

- 控制面 /
  plane
  侧现有日志查询，
  入口 selector
  就是：
  - `{job="mini-cloud"}`

所以 plane
侧
`Alloy`
采集平台文件日志时，
必须显式带上：

- `job="mini-cloud"`

否则虽然日志已经进入中心
`Loki`，
平台自己的日志查询接口仍然会查不到这些流。

## 这章结束后的平台形态

到这里，
平台第一次具备正式的生产观测分层：

- plane 侧只做轻量采集和本地汇聚
- center 侧统一存储、查询和告警
- 平台日志、
  workload 日志、
  应用 trace、
  plane 指标
  都有清晰归属
