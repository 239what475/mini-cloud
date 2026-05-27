# 08 Observability And Basic Operations

这一章开始给 `mini-cloud v1` 补最小观测面。

前面的 `07` 已经把：

- 域名绑定
- Host 路由
- gateway 反代

串起来了。

也就是说，
现在平台已经不只是“应用能跑”，
而是已经开始真的接流量了。

一旦走到这一步，
你马上就会碰到两个现实问题：

1. 请求到底有没有真的经过 gateway
2. 当我怀疑应用有问题时，
   - 平台侧有没有最小的运维动作可以帮我确认

这一章就是先解决这两个问题。

## 这一章先记一句最重要的话

`08`
故意不直接跳到：

- 完整日志平台
- tracing
- 大而全 dashboard
- 远程容器日志采集

而是先做一个很克制的第一版：

- 平台汇总指标
- gateway 访问日志
- 一个最小人工运维动作：
  - 手动 probe 当前 backend

所以这一章更像是在回答：

- “平台现在至少能不能看见自己在干什么”

## 这一章实际落下了什么

当前代码里，
这一章真正新增的是：

- `gateway_request_events` 表
  - 保存最近经过 gateway 的访问日志
- `internal/observability`
  - 定义平台汇总、访问日志、probe 结果模型
- `GET /metrics`
  - 暴露最小 Prometheus exposition
- `GET /api/v1/platform/overview`
  - 返回平台汇总统计
- `GET /api/v1/platform/access-logs`
  - 返回最近 gateway 访问日志
- `POST /api/v1/apps/{appID}/operations/probe`
  - 主动探测当前 backend 的健康端点
- 前端 `Observability` 页面
  - 展示：
    - 平台汇总
    - `/metrics` 预览
    - 最近访问日志
- `Apps` 页面里的 `Run backend probe`
  - 让你直接从应用视角做一次人工探测

## 为什么这一章的“日志”先做 gateway 访问日志

这个点非常重要。

很多人一看到“日志”，
第一反应会是：

- 容器 stdout
- 容器 stderr
- agent 持续回传日志

但我们当前 `v1`
还没有这些条件：

- 还没有长期驻留 agent
- 还没有远程日志采集链路
- 还没有专门日志存储

如果现在强行让 control-plane 直接去读远端容器日志，
架构会很别扭，
因为那等于把：

- control-plane
  - 变成了直接摸运行时的远程运维脚本

这和我们前面一直在建立的边界不一致。

所以这一章故意先收敛成：

- gateway 访问日志

原因是：

- gateway 请求本来就经过 control-plane
- control-plane 自己天然就看得见这些流量
- 不需要额外发明远程日志采集链

所以你现在可以先这样理解：

- `08`
  - 先解决“入口流量有没有经过平台”
- 容器 stdout/stderr
  - 留到更后面 agent 和日志链路更完整时再做

## 为什么 `/metrics` 现在就值得做

前面在 `02`
里其实已经埋过一个判断：

- 第一版建议先做最小可用：
  - 结构化日志
  - `/metrics`
  - 事件表

到 `08`
这里，
平台已经至少有：

- `Project`
- `App`
- `Node`
- `Deployment`
- `Execution`
- `Domain`
- gateway 请求

所以现在补 `/metrics`，
价值已经很直接了。

当前 `/metrics`
先暴露的是最小平台指标：

- `projects_total`
- `apps_total`
- `apps_status{status=...}`
- `nodes_total`
- `nodes_status{status=...}`
- `deployments_total`
- `deployments_status{status=...}`
- `executions_total`
- `executions_status{status=...}`
- `domains_total`
- `gateway_requests_total`
- `gateway_requests_recent_total{window="5m"}`

也就是说，
这一版不是为了“监控体系一步到位”，
而是为了先建立一件事：

- 关键平台状态可以被外部系统抓走

这一步很重要，
因为后面无论你接：

- Prometheus
- 告警
- dashboard

都需要它先存在。

## 这一章的“基础运维操作”是什么

这一章的基础运维操作，
不是：

- 自动重试
- 自动重调度
- 节点维护

这些会留给 `09`。

当前 `08`
只先做一个非常朴素、但很有用的动作：

- `Run backend probe`

它做的事情是：

1. control-plane 先解析出这个 `App` 当前的 route
   - 也就是：
     - `node.privateIP`
     - `execution.hostPort`
     - `readinessPath`
2. 然后 control-plane 主动去请求：
   - `http://targetHost:targetPort/healthPath`
3. 返回：
   - 是否成功
   - 状态码
   - 用时
   - 目标 URL

所以这个动作的意义很明确：

- 当你怀疑应用不对劲时，
  - 可以让平台自己从“入口之外”的视角
  - 直接探测当前 backend

它不负责修复问题，
但它能帮你先回答：

- “当前后端到底活不活着”

## 当前观测面到底能看到什么

### 1. 平台汇总

`GET /api/v1/platform/overview`
当前会给你一个最小总览：

- 一共有多少项目
- 一共有多少应用
- 有多少应用在：
  - `idle`
  - `deploying`
  - `running`
  - `failed`
- 有多少节点在：
  - `ready`
  - `offline`
  - `draining`
- 有多少 `Deployment / Execution`
  - 分别处于什么状态
- 一共有多少绑定域名
- gateway 总共代理了多少请求
- 最近五分钟代理了多少请求

### 2. gateway 访问日志

`GET /api/v1/platform/access-logs`
当前会返回最近一批 gateway 访问事件。

每条日志里最重要的字段是：

- `host`
- `method`
- `path`
- `statusCode`
- `durationMs`
- `appName`
- `executionID`
- `target`

也就是说，
你现在至少能回答这些问题：

- 某个域名最近有没有真的有流量打进来
- 请求落到了哪个应用
- 它是被代理到了哪个 backend
- 返回码是什么
- 大概耗时多少

### 3. `/metrics`

这一版的 `/metrics`
已经是标准的 Prometheus 文本格式，
所以哪怕现在还没有真的接 Prometheus，
你也已经可以先用：

```bash
curl -s http://127.0.0.1:8080/metrics
```

直接看平台暴露出来的指标。

### 4. 人工 probe

这一步让“看状态”和“做动作”第一次连了起来。

也就是说，
`08`
不是只会展示：

- “我看见它好像在 running”

而是还能再往前一步：

- “我主动去探一下它当前 backend 还活不活”

## 本地闭环实验

这一章建议继续复用 `07` 的本地单机实验方式。

也就是说：

- control-plane 跑在本机
- 节点也继续把 `privateIP`
  - 注册成：
    - `127.0.0.1`

这样就能把：

- gateway 请求日志
- `/metrics`
- probe

这三件事都很直观地看见。

### 1. 启动 PostgreSQL

```bash
cd projects/mini-cloud
docker compose -f deploy/compose/docker-compose.yml up -d
```

### 2. 启动 control-plane

```bash
cd projects/mini-cloud
go run ./cmd/control-plane
```

### 3. 注册本地节点并发心跳

```bash
cd projects/mini-cloud
go run ./cmd/agent register \
  --server http://127.0.0.1:8080 \
  --provider aliyun \
  --region cn-beijing \
  --name local-observability-node \
  --private-ip 127.0.0.1 \
  --instance-id i-local-observability-01 \
  --instance-type ecs.u1-c1m2.large \
  --cpu-milli-capacity 2000 \
  --memory-mi-capacity 4096
```

记下返回的：

- `nodeID`

下面假设它是：

- `node_xxx`

再发一条心跳：

```bash
cd projects/mini-cloud
go run ./cmd/agent heartbeat \
  --server http://127.0.0.1:8080 \
  --node-id node_xxx \
  --cpu-milli-free 1800 \
  --memory-mi-free 3500 \
  --running-containers 0 \
  --status ready
```

### 4. 创建项目、应用、发布一个 nginx 应用

```bash
PROJECT_ID="$(
  curl -s http://127.0.0.1:8080/api/v1/projects \
    -H 'Content-Type: application/json' \
    -d '{"name":"demo08","displayName":"Demo 08"}' \
  | jq -r '.id'
)"

APP_ID="$(
  curl -s http://127.0.0.1:8080/api/v1/apps \
    -H 'Content-Type: application/json' \
    -d "{
      \"projectID\":\"$PROJECT_ID\",
      \"name\":\"hello-observe\",
      \"displayName\":\"Hello Observe\",
      \"region\":\"cn-beijing\",
      \"replicas\":1,
      \"instanceClass\":\"small\",
      \"defaultPort\":80,
      \"readinessPath\":\"/\",
      \"env\":{}
    }" \
  | jq -r '.id'
)"

curl -s http://127.0.0.1:8080/api/v1/apps/"$APP_ID"/releases \
  -H 'Content-Type: application/json' \
  -d '{
    "version":"2026-04-10.1",
    "image":"nginx:1.27-alpine",
    "command":[],
    "args":[],
    "env":{},
    "port":80,
    "readinessPath":"/"
  }' | jq
```

再执行：

```bash
cd projects/mini-cloud
go run ./cmd/agent work \
  --server http://127.0.0.1:8080 \
  --node-id node_xxx \
  --health-attempts 20
```

### 5. 绑定一个 host，并打几次流量

```bash
curl -s http://127.0.0.1:8080/api/v1/apps/"$APP_ID"/domains \
  -H 'Content-Type: application/json' \
  -d '{"host":"hello.observe.local"}' | jq

curl -s -H 'Host: hello.observe.local' http://127.0.0.1:8080/ >/dev/null
curl -s -H 'Host: hello.observe.local' http://127.0.0.1:8080/ >/dev/null
curl -s -H 'Host: hello.observe.local' http://127.0.0.1:8080/ >/dev/null
```

### 6. 看访问日志

```bash
curl -s http://127.0.0.1:8080/api/v1/platform/access-logs | jq '.items[0:3]'
```

你应该能看到类似：

- `host`
  - `hello.observe.local`
- `statusCode`
  - `200`
- `target`
  - 指向当前 backend 的：
    - `http://127.0.0.1:<hostPort>`

### 7. 看平台汇总

```bash
curl -s http://127.0.0.1:8080/api/v1/platform/overview | jq '{
  appsRunning,
  nodesReady,
  deploymentsRunning,
  executionsRunning,
  domainsTotal,
  gatewayRequestsTotal,
  gatewayRequestsLast5Minute
}'
```

### 8. 看 `/metrics`

```bash
curl -s http://127.0.0.1:8080/metrics | sed -n '1,40p'
```

### 9. 跑一次人工 probe

```bash
curl -s -X POST http://127.0.0.1:8080/api/v1/apps/"$APP_ID"/operations/probe | jq
```

成功的话，
你应该能看到：

- `successful`
  - `true`
- `statusCode`
  - `200`
- `targetURL`
  - 指向当前 backend 健康端点

### 10. 清理实验环境

```bash
docker ps --filter name=mini-cloud- --format '{{.Names}}'
docker stop "$(docker ps --filter name=mini-cloud- --format '{{.Names}}' | head -n 1)"
docker compose -f deploy/compose/docker-compose.yml down -v
```

## 这一章完成后应该达到什么状态

如果 `08`
按当前实现收口，
那么项目至少应该达到这些状态：

- 平台已经有最小汇总观测面
- gateway 已经能把访问日志落库
- `/metrics` 已经能暴露关键平台状态
- 用户已经可以从 `Apps` 页面直接做一次人工 probe
- `Observability` 页面已经能展示：
  - 平台汇总
  - 指标预览
  - 最近访问日志

## 当前实现的边界

虽然这章已经把“最小观测”和“最小运维动作”补上了，
但边界还是要讲清楚。

当前 `08`
还没有做这些事：

- 容器 stdout/stderr 远程采集
- 分布式 tracing
- 告警
- 正式 dashboard 系统
- 长期日志保留
- agent 侧日志回传
- 自动化诊断动作

所以这章更准确地说，
是在做：

- 平台最小 observability baseline

而不是：

- 完整观测平台

## 这一章的结论

`08`
让 `mini-cloud v1`
第一次具备了这样一种能力：

- 不只是“把应用跑起来”
- 还开始“看见流量、看见状态、做一点最小操作”

这一步很关键，
因为它让后面的：

- 失败重试
- 节点维护
- 更正式的运行运维

都开始有了观测基础。

## 本章检查点

- commit:
  - `d8decfcd2023cf936259e520ef483c5efd9ad1fb`
- 状态：
  - 平台汇总、`/metrics`、gateway 访问日志和人工 probe 已经落地
