# center 侧生产观测栈

这一组文件给 `v6/15`
使用，
目标很明确：

- 只在 `control-plane`
  所在主机放中心观测后端
- 让各个 `cloud-plane`
  只保留轻量采集和本 plane
  的 `Prometheus`
- 不再复用
  `deploy/compose`
  的本地实验栈

## 目录里的文件

- [docker-compose.yml.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/observability-center/docker-compose.yml.example)
  - 中心栈 compose 模板
- [prometheus/config.yml](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/observability-center/prometheus/config.yml)
  - 中心 `Prometheus`
    抓取与 federation 配置
- [prometheus/file_sd/planes.json.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/observability-center/prometheus/file_sd/planes.json.example)
  - plane `Prometheus`
    目标模板
- [prometheus/rules/minicloud-production-alerts.yml](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/observability-center/prometheus/rules/minicloud-production-alerts.yml)
  - 最小告警包
- [loki/config.yaml](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/observability-center/loki/config.yaml)
- [tempo/config.yml](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/observability-center/tempo/config.yml)
- [alertmanager/config.yml](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/observability-center/alertmanager/config.yml)
- [grafana/dashboards/mini-cloud-operations.json](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/observability-center/grafana/dashboards/mini-cloud-operations.json)

## 这层只负责什么

- 统一存储：
  - `Loki`
  - `Tempo`
- 统一查询：
  - `Grafana`
- 统一告警：
  - `Prometheus`
  - `Alertmanager`
- 统一抓：
  - 本机 `control-plane`
    的
    `/metrics/control`
  - 各个 plane
    的
    `Prometheus /federate`

这里刻意不负责：

- 直接接应用 `OTLP`
- 直接收workload 日志 push
- 直接 tail
  各个 plane
  上的日志文件

这些都在 plane
侧资产里解决。

## 推荐主机布局

- 目录：
  - `/opt/mini-cloud/observability-center`
- compose 文件：
  - `/opt/mini-cloud/observability-center/docker-compose.yml`
- token 文件：
  - `/opt/mini-cloud/observability-center/secrets/control-plane-metrics.token`
- plane file_sd：
  - `/opt/mini-cloud/observability-center/prometheus/file_sd/planes.json`

## 最小启动方式

### 1. 准备实际文件

```bash
cp deploy/observability-center/docker-compose.yml.example deploy/observability-center/docker-compose.yml
cp deploy/observability-center/prometheus/file_sd/planes.json.example deploy/observability-center/prometheus/file_sd/planes.json
mkdir -p deploy/observability-center/secrets
```

然后把具备
`control.read`
权限的 token
写到：

- `deploy/observability-center/secrets/control-plane-metrics.token`

这个 token
专门给中心 `Prometheus`
抓：

- `http://127.0.0.1:18080/metrics/control`

### 2. 启动中心栈

```bash
docker compose -f deploy/observability-center/docker-compose.yml up -d
```

### 3. 健康检查

```bash
curl -fsS http://127.0.0.1:3100/ready
curl -fsS http://127.0.0.1:3200/ready
curl -fsS http://127.0.0.1:9090/-/ready
curl -fsS http://127.0.0.1:3000/api/health
```

## plane federation 现在怎么接

中心 `Prometheus`
不会直接跨云抓每台 worker。

它只抓：

- 本机 `control-plane`
  的
  `/metrics/control`
- 每个 plane
  自己暴露的
  `Prometheus /federate`

所以实际需要维护的是：

- `prometheus/file_sd/planes.json`

里面写各个 plane
主机上对外暴露的：

- `<plane-routable-ip>:19090`

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
- Grafana Alloy
  `otelcol.receiver.otlp`
  - https://grafana.com/docs/alloy/latest/reference/components/otelcol/otelcol.receiver.otlp/
- Grafana Alloy
  `otelcol.exporter.otlphttp`
  - https://grafana.com/docs/alloy/latest/reference/components/otelcol/otelcol.exporter.otlphttp/
