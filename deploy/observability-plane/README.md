# plane 侧生产观测栈

这一组文件给每个长期运行的
`cloud-plane`
主机使用。

它只负责本 plane
的轻量采集：

- `Alloy`
  - tail 平台日志文件
  - 接workload 日志 push
  - 转发到中心 `Loki`
- `OTel Collector`
  - 接应用
    `OTLP metrics / traces`
  - 把 traces
    转发到中心 `Tempo`
  - 把 metrics
    暴露给本 plane
    `Prometheus`
- `Prometheus`
  - 抓：
    - 本机
      `OTel Collector /metrics`
    - 后续可选的
      私网 worker
      `/metrics`
  - 再由中心
    `Prometheus`
    federation 抓走

## 目录里的文件

- [docker-compose.yml.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/observability-plane/docker-compose.yml.example)
- [alloy/config.alloy](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/observability-plane/alloy/config.alloy)
- [otel-collector/config.yml](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/observability-plane/otel-collector/config.yml)
- [prometheus/config.yml.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/observability-plane/prometheus/config.yml.example)
- [prometheus/file_sd/worker-targets.json.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/observability-plane/prometheus/file_sd/worker-targets.json.example)

## 和平台配置的关系

plane 侧观测启动后，
要把下面三个地址写进：

- `/etc/mini-cloud/cloud-plane/cloud-plane.yaml`
  的
  `observability.workloadLogPushUrl`
  - 指向本机
    `Alloy`
    的
    `3101`
- `/etc/mini-cloud/cloud-plane/cloud-plane.yaml`
  的
  `observability.workloadOtlpEndpoint`
  - 指向本机
    `OTel Collector`
    的
    `4318`
- `/etc/mini-cloud/cloud-plane/cloud-plane.yaml`
  的
  `observability.lokiTenantId`
  - 按中心 Loki 的租户规划填写；没有多租户时留空

这里要特别区分：

- `observability.workloadLogPushUrl`
  - 给 node `agent`
    往上推workload 日志
- Loki 查询入口不再放在 cloud-plane；日志查询应由 control-plane 或独立观测入口负责。

## 最小启动方式

### 1. 准备实际文件

```bash
cp deploy/observability-plane/docker-compose.yml.example deploy/observability-plane/docker-compose.yml
cp deploy/observability-plane/prometheus/config.yml.example deploy/observability-plane/prometheus/config.yml
cp deploy/observability-plane/prometheus/file_sd/worker-targets.json.example deploy/observability-plane/prometheus/file_sd/worker-targets.json
```

然后按 plane
自己的真实信息修改：

- `prometheus/config.yml`
  里的：
  - `external_labels.plane`
  - `external_labels.provider`
  - `external_labels.region`

### 2. 需要提供的环境变量

- `MINICLOUD_PLATFORM_NAME`
- `MINICLOUD_PLANE_NAME`
- `MINICLOUD_PROVIDER`
- `MINICLOUD_REGION`
- `MINICLOUD_CENTER_LOKI_PUSH_URL`
  - 例如：
    `http://10.0.0.10:3100/loki/api/v1/push`
- `MINICLOUD_CENTER_TEMPO_OTLPHTTP_URL`
  - 例如：
    `http://10.0.0.10:4318`
- `MINICLOUD_LOKI_TENANT_ID`
  - 可留空

如果 center 和当前 plane
就在同一台主机、
但分别跑在两个独立
`docker compose`
项目里，
推荐直接写成：

- `MINICLOUD_CENTER_LOKI_PUSH_URL=http://host.docker.internal:3100/loki/api/v1/push`
- `MINICLOUD_CENTER_TEMPO_OTLPHTTP_URL=http://host.docker.internal:4318`

这一份
`docker-compose.yml.example`
已经给
`alloy`
和
`otel-collector`
补了：

- `extra_hosts:`
  - `host.docker.internal:host-gateway`

这样 plane
侧容器才能稳定回连同机的 center。

另外，
`alloy`
现在会把自己的文件读取状态持久化到：

- `/var/lib/alloy/data`

对应的
`docker-compose.yml.example`
已经挂了专用 volume。

这样在 plane
侧
`alloy`
容器重建后，
不会因为 positions
 丢失而把整份平台日志从头重放到中心
`Loki`。

### 3. 启动 plane 侧采集

```bash
docker compose -f deploy/observability-plane/docker-compose.yml up -d
```

### 4. 健康检查

```bash
curl -fsS http://127.0.0.1:19090/-/ready
```

然后至少确认：

- `plane Prometheus`
  里能看到：
  - `minicloud_`
    指标
  - `otelcol_`
    指标

## 暴露端口怎么理解

- `3101/tcp`
  - 本 plane
    workload 日志 push
    入口
- `4318/tcp`
  - 本 plane
    应用
    `OTLP HTTP`
    入口
- `19090/tcp`
  - 本 plane
    `Prometheus`
    对中心 federation
    暴露的入口

推荐收法：

- `3101`
  和
  `4318`
  只放行本云 VPC /
  子网范围
- `19090`
  只放行中心
  `control-plane`
  主机的固定来源

如果有第二个 plane
跨云把日志和 traces
推到中心，
除了 plane
自己的
`19090`
之外，
还要在 center
所在云的防火墙 /
安全组里额外放行：

- `3100/tcp`
  - 给 `Loki push`
- `4318/tcp`
  - 给 `Tempo OTLP HTTP`

否则最常见的现象就是：

- `Alloy`
 里出现
  `connect: connection refused`
  或
  `context deadline exceeded`
- `OTel Collector`
 里出现 export timeout
- plane
  观测容器都在跑，
  但中心
  `Loki / Tempo`
  里没有对应数据

## 日志标签约定

平台日志和workload 日志现在都会统一带上：

- `job="mini-cloud"`

平台日志还会继续带：

- `component`
- `platform`
- `plane`
- `provider`
- `region`
- `source="platform"`

这样中心侧现有的日志查询实现，
就能继续用：

- `{job="mini-cloud"}`

作为统一 selector，
再按
`component`
和日志内容里的
`logfmt`
字段做进一步过滤。

## 参考文档

- Grafana Alloy
  `loki.source.api`
  - https://grafana.com/docs/alloy/latest/reference/components/loki/loki.source.api/
- Grafana Alloy
  `loki.source.file`
  - https://grafana.com/docs/alloy/latest/reference/components/loki/loki.source.file/
- Grafana Alloy
  `local.file_match`
  - https://grafana.com/docs/alloy/latest/reference/components/local/local.file_match/
