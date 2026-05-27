# v5/07 App Observability With OTel

`v5/06`
已经把：

- 多副本运行
- 放置
- `worker`
  扩容

收下来了。

但平台如果只会：

- 起容器
- 暴露域名
- 做发布

还不够。

因为一旦服务真的开始跑，
开发者马上会问三类问题：

1. 这个服务刚才到底打印了什么日志
2. 这个服务的指标现在长什么样
3. 这一条请求到底慢在哪一段

这一章收的就是这三件事，
但要先把边界说清楚。

## 这一章只做什么

这一章明确只做：

- 应用侧观测

也就是：

- 受管应用容器的日志
- 应用自己上报的指标
- 应用自己上报的 trace

这一章明确不重做：

- 平台日志
- 平台级指标
- 平台审计
- 宿主机日志总线

这些能力前面章节已经有了，
这里不重复造一套。

## 这一章明确不做什么

为了避免边界再散开，
这里再明确几条不做：

- 不做宿主机全量日志采集
- 不扫整机所有 Docker 容器日志
- 不采 `docker daemon`
  日志
- 不采 `journald`
  日志
- 不引入 `docker compose`
  作为应用运行模型
- 不要求用户给每个服务自己塞一个 `sidecar`

这一章的前提一直都是：

- `mini-cloud`
  只管理自己启动的应用容器

所以观测范围也只覆盖：

- `mini-cloud`
  自己管理的应用容器

## 这一章最后的观测拓扑

这一章收出来的最终拓扑是：

```text
应用容器 stdout/stderr
  -> cloud-worker
    -> Loki
      -> ServiceService.QueryServiceLogs
        -> Web / curl / CLI

应用容器 OTel metrics / traces
  -> OTel Collector
    -> Prometheus
    -> Tempo
      -> Grafana
```

这里有两个很重要的分流：

1. 日志链路
2. 指标和 trace 链路

它们不是一条链。

## 为什么日志和 metrics / trace 不走同一条实现

日志这里要解决的是：

- 就算用户服务没有埋点，
  平台也至少要把：
  - `stdout`
  - `stderr`
    兜底收上来

所以日志最稳妥的做法是：

- `cloud-worker`
  直接跟随 Docker 容器日志
- 然后直接推到 `Loki`

而指标和 trace 不一样。

它们天然依赖：

- 应用自己埋点
- 应用 SDK
- 应用导出器

所以这里收成：

- `OTel Collector`
  作为统一接收入口

这样职责就很干净：

- 日志：
  - 平台兜底采集
- metrics / traces：
  - 应用自愿埋点
  - 平台只提供统一出口

## 应用日志这条链现在怎么工作

### 1. 采集范围

当前只采：

- `mini-cloud`
  启动出来的应用容器
  的：
  - `stdout`
  - `stderr`

不采：

- `cloud-plane`
  自己的进程日志
- `cloud-worker`
  自己的进程日志
- 其他随机 Docker 容器

### 2. 采集位置

采集发生在：

- `cloud-worker`

更具体地说：

- 容器启动成功后，
  `worker`
  用 Docker API 的：
  - `ContainerLogs(Follow=true, Timestamps=true)`
    跟随日志流

### 3. 日志写到哪里

日志不落数据库。

而是直接写到：

- `Loki`

这样做有两个好处：

1. 不把高频日志写进平台状态库
2. 查询层可以继续复用：
   - `Loki`
     的时间窗口和全文过滤能力

### 4. 一条日志会补哪些上下文字段

`worker`
在推送日志时，
会把应用运行上下文一起带上，
至少包括：

- `platform_name`
- `project_id`
- `service_id`
- `service_name`
- `deployment_id`
- `execution_id`
- `replica_index`
- `node_id`
- `container_name`
- `stream`
  - `stdout`
  - `stderr`

所以这里不是“单纯存一行文本”，
而是把日志和服务实例上下文绑在一起了。

## 应用日志为什么走 service 作用域查询

这一章新增的 northbound 接口不是：

- 平台级日志查询

而是：

- `service`
  级日志查询

对应的是：

- `ServiceService.QueryServiceLogs`

它的用途很明确：

- 我是某个服务的开发者
- 我现在只想看这个服务的日志

所以这里的过滤条件围绕：

- `service_id`
- `deployment_id`
- `execution_id`
- `contains`

展开。

而且这个接口会强制补上：

- 当前服务所属的 `project_id`
- `component=app`

也就是说：

- 你查的是“某个服务的应用日志”
- 不是“平台上所有日志”

## 这个日志接口长什么样

通过 `grpc-gateway`
之后，
HTTP 路径是：

```text
GET /api/v1/services/{serviceID}/logs
```

常用查询参数有：

- `since`
  - 例如 `15m`
- `limit`
- `direction`
  - `backward`
  - `forward`
- `deploymentID`
- `executionID`
- `contains`

例如：

```bash
curl -H "Authorization: Bearer <token>" \
  "http://127.0.0.1:18080/api/v1/services/svc_xxx/logs?since=15m&limit=50&contains=error"
```

返回结构会是：

```json
{
  "start": "2026-04-16T12:00:00Z",
  "end": "2026-04-16T12:15:00Z",
  "limit": 50,
  "direction": "backward",
  "items": [
    {
      "timestamp": "2026-04-16T12:12:31Z",
      "labels": {
        "component": "app",
        "stream": "stderr"
      },
      "line": "time=\"2026-04-16T12:12:31Z\" project_id=\"prj_xxx\" service_id=\"svc_xxx\" msg=\"database connection failed\""
    }
  ]
}
```

## metrics / traces 这条链怎么工作

日志是平台兜底，
metrics / traces
则是：

- 应用自愿埋点

这里选用的开源组件是：

- `OTel Collector`
- `Prometheus`
- `Tempo`
- `Grafana`

职责分工是：

- `OTel Collector`
  - 统一接收应用上报的 `OTLP`
- `Prometheus`
  - 抓取应用指标出口
- `Tempo`
  - 保存 trace
- `Grafana`
  - 统一查看日志、指标和 trace

## 平台怎么把 OTel 出口告诉应用

这一章不要求每个服务手写 exporter 地址。

平台会在 `worker`
启动应用容器前，
自动补一组标准环境变量：

- `OTEL_EXPORTER_OTLP_ENDPOINT`
- `OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf`
- `OTEL_SERVICE_NAME`
  - 如果用户自己没写，
    默认用当前 `service name`
- `OTEL_RESOURCE_ATTRIBUTES`
  - 默认补：
    - `mini_cloud.project_id`
    - `mini_cloud.service_id`
    - `mini_cloud.deployment_id`
    - `mini_cloud.execution_id`
    - `mini_cloud.replica_index`
    - `mini_cloud.platform_name`

这里要注意两点：

1. `OTEL_EXPORTER_OTLP_ENDPOINT`
   由平台统一注入，
   这样应用埋点默认就能接到平台观测栈
2. `OTEL_SERVICE_NAME`
   和 `OTEL_RESOURCE_ATTRIBUTES`
   只在用户没有自己设置时才补默认值

也就是说：

- 导出地址是平台统一收口
- 服务身份字段允许用户显式自定义

## 为什么本地实验里用 `host.docker.internal`

本地实验时，
常见做法是：

- `OTel Collector`
  跑在宿主机上的 `docker compose`
  里
- 应用容器由 `mini-cloud`
  再通过 Docker API 启动

这样应用容器要访问：

- 宿主机暴露出来的 `4318`

所以这一章把运行时也顺手收了：

- 受管应用容器默认带上：
  - `host.docker.internal:host-gateway`

这样本地平台配置里就可以直接写：

- `http://host.docker.internal:4318`

不用再让文档去解释宿主机桥接网关地址。

## 本地观测栈现在包含哪些组件

`deploy/compose`
这一章之后新增了：

- `tempo`
- `otel-collector`

所以本地最小观测栈现在是：

- `Loki`
  - 应用日志
- `Promtail`
  - 平台文件日志
- `Prometheus`
  - 指标
- `Alertmanager`
  - 告警
- `Grafana`
  - 看板
- `Tempo`
  - trace
- `OTel Collector`
  - 应用 metrics / traces
    统一入口

## 平台配置里这一章新增要关心什么

这一章后，
平台配置里的：

- `observability`

段里，
真正被运行链路直接消费的关键字段有两个：

```json
{
  "observability": {
    "lokiUrl": "http://10.88.0.10:3100",
    "appOtlpEndpoint": "http://host.docker.internal:4318",
    "grafanaBaseUrl": "http://10.88.0.10:3000"
  }
}
```

这里各字段的职责是：

- `lokiUrl`
  - 应用日志最终写到哪里
- `appOtlpEndpoint`
  - 应用埋点要发到哪个 `OTLP`
    入口
- `grafanaBaseUrl`
  - 一个给上层界面或文档使用的展示入口配置位
  - 当前这一章里还没有直接进入运行时数据链路

这里最关键的是：

- `appOtlpEndpoint`
  必须是“应用容器看得到”的地址，
  不是 `cloud-plane`
  自己看得到就行

## 这一章之后，开发者怎么看自己的服务

现在开发者要看自己服务，
大致分成三种入口：

### 1. 看最近日志

走平台 API：

- `GET /api/v1/services/{serviceID}/logs`

适合：

- 快速排障
- 看某次 deployment
  的应用输出
- 看某个 execution
  的失败原因

### 2. 看指标

走：

- `Grafana`
  -> `Prometheus`

适合：

- 看延迟
- 看吞吐
- 看错误率
- 看自定义业务指标

### 3. 看 trace

走：

- `Grafana`
  -> `Tempo`

适合：

- 看一次请求跨组件的调用路径
- 看慢请求卡在哪一段

## 这一章和前面平台观测能力的关系

这里再强调一次，
避免后面再混掉：

- 平台日志
  - 继续看平台自己的：
    - 结构化日志
    - 平台指标
    - 审计
    - SLO
- 应用日志
  - 走：
    - `QueryServiceLogs`
- 应用指标和 trace
  - 走：
    - `OTel Collector`
    - `Prometheus`
    - `Tempo`
    - `Grafana`

也就是说，
这一章不是“重做 observability 总线”，
而是把：

- 应用侧观测

补完整。

## 这一章现在怎么做本地端到端验证

这一章已经补了一个纯本地的真实场景：

- 启动本地 `cloud-plane`
- 启动本地静态 `worker`
- 启动 `Loki`
- 启动 `Prometheus`
- 启动 `Tempo`
- 启动 `OTel Collector`
- 构建一个带 `OTel`
  埋点的 demo app
- 通过平台 API 创建服务
- 真的发请求打到这个服务
- 再分别验证：
  - 应用日志能通过
    `QueryServiceLogs`
    查到
  - `Prometheus`
    能查到应用计数器
  - `Tempo`
    能查到刚才那次请求的 trace

运行入口是：

```bash
cd projects/mini-cloud/tests
go run . local-observability
```

这条验证链不是模拟数据，
而是真把：

- 应用容器
- 日志
- metrics
- trace

整条链路跑一遍。

## 本地 e2e 这次实际验证了什么

这次本地端到端验证里，
关键检查点是：

1. `worker`
   能注册并进入 `ready`
2. demo app
   能被平台正常拉起并通过健康检查
3. `Prometheus`
   能抓到 `OTel Collector`
   的指标出口
4. 应用请求打出去后，
   `QueryServiceLogs`
   能查到带 `probe_id`
   的应用日志
5. `Prometheus`
   能查到应用计数器
   `demo_requests_total`
6. `Tempo`
   能按 trace ID
   查回刚才那次请求

也就是说，
现在这一章不是只停留在：

- 代码能编译

而是已经有一条真实本地实验链路。

## 这次本地验证里踩到的两个关键问题

### 1. 观测容器访问宿主机端口时，不能只绑 `127.0.0.1`

本地实验里：

- `Prometheus`
- `OTel Collector`
- `Tempo`
- 应用容器

都可能需要从容器里访问宿主机上的端口。

如果宿主机只监听：

- `127.0.0.1`

那么容器里通过：

- `host.docker.internal`

访问时，
实际上走到的是宿主机桥接地址，
不是容器自己的 `127.0.0.1`，
这时就会连接失败。

所以本地 e2e 里，
需要被容器反向访问的那些端口，
必须显式监听在宿主可达地址上，
不能只绑本机回环。

### 2. `grpc-gateway` 的空 timestamp 会把日志时间窗口压成 `1970`

这次日志链路还有一个很隐蔽的问题：

- `GET /api/v1/services/{serviceID}/logs?since=10m`

看起来传了：

- `since=10m`

但经过 `grpc-gateway`
之后，
缺省的：

- `start`
- `end`

可能会变成“零值 protobuf timestamp”。

如果直接对它们调用：

- `AsTime()`

就会得到：

- `1970-01-01T00:00:00Z`

这样后面的：

- `since=10m`

就完全失效了，
最后实际发给 `Loki`
的是：

- `start=1970`
- `end=1970`

查询长度变成：

- `0s`

结果当然查不到刚刚产生的应用日志。

所以现在时间窗口归一化逻辑会把：

- protobuf 零值 timestamp

当成“未提供”，
再正确按：

- `since`

推导真实查询窗口。

## 这章对应的关键代码

- [grpc_service_service.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/grpc_service_service.go)
  - `QueryServiceLogs`
- [grpc_logs.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/grpc_logs.go)
  - 日志时间窗口和过滤输入归一化
- [loki.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudworker/applogs/loki.go)
  - 应用日志采集和写入 `Loki`
- [managed.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/cmd/agent/managed.go)
  - 容器启动后跟随应用日志
- [main.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/cmd/agent/main.go)
  - 注入 `OTEL_*`
    环境变量
- [docker-compose.yml](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/compose/docker-compose.yml)
  - 本地观测栈新增 `tempo` 和 `otel-collector`
- [main.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/tests/main.go)
  - 本地 e2e 入口新增
    `local-observability`
- [run.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/tests/localobservability/run.go)
  - 本地观测链端到端场景
- [runner.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/tests/internal/localcloudplane/runner.go)
  - 本地 `cloud-plane + worker + Loki + Prometheus + Tempo + OTel Collector`
    拉起逻辑

## 本章检查点

如果这一章做对了，
你现在应该已经能比较稳定地回答下面这些问题：

1. 为什么应用日志要由：
   - `worker`
     直接跟随容器 `stdout/stderr`
     再写进 `Loki`
2. 为什么应用 metrics / traces
   要走：
   - `OTel Collector`
     而不是复用日志链路
3. 为什么：
   - `QueryServiceLogs`
     必须天然带上：
     - `project_id`
     - `service_id`
     - `component=app`
     这些作用域约束
4. 为什么本地实验里，
   需要被容器反向访问的宿主机端口，
   不能只监听：
   - `127.0.0.1`
5. 为什么 `grpc-gateway`
   里的空 protobuf timestamp
   会把日志查询窗口错误压到：
   - `1970-01-01`
6. 为什么 `Tempo`
   的 trace 查询不能只按十六进制 `traceID`
   做字符串匹配，
   还要理解它返回里的编码形式
7. 为什么这一章必须真的补一条本地 e2e，
   把：
   - 日志
   - Prometheus 指标
   - Tempo trace
     三条链一起跑通

如果这些问题都已经能稳定回答，
那 `v5/07`
的应用侧观测链路就算真正收住了。

对应提交：

- `b9ba2aea0930caef0694244880443889b070b561`
