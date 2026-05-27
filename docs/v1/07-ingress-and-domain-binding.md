# 07 Ingress And Domain Binding

这一章开始给 `mini-cloud v1` 补最小入口层。

前面的 `06` 已经能做到：

- agent 拉到 work
- 起容器
- 健康检查通过
- `Deployment` 进入 `running`

但那时应用只能靠：

- 节点地址
- 宿主机随机端口

直接访问。

这显然还不像一个平台。

因为平台用户真正更关心的是：

- 我给应用绑定了什么域名
- 平台能不能按域名把请求转到正确后端

这一章解决的就是这个问题。

## 这一章先记一句最重要的话

`07`
先不做完整网关系统，
而是先做一个最小但真实可用的入口闭环：

```text
bind host
  ->
request arrives at control-plane
  ->
control-plane looks up current running backend
  ->
reverse proxy to node privateIP + hostPort
```

所以当前实现里：

- control-plane
  - 既是 API 服务
  - 也是最小 gateway

## 这一章实际落下了什么

当前代码里，
这章真正新增的是：

- `app_domains` 表
  - 记录某个 `App` 绑定了哪些域名
- `internal/ingress`
  - 定义域名绑定和 gateway route 模型
- `internal/store/ingress_store.go`
  - 负责域名绑定存储和 route resolve
- `GET /api/v1/apps/{appID}/domains`
  - 查看应用的域名绑定
- `POST /api/v1/apps/{appID}/domains`
  - 给应用绑定一个域名
- `internal/httpapi/gateway_handler.go`
  - 按请求里的 `Host` 头做反向代理
- `Apps` 页面里的 `Domain bindings`
  - 可以直接绑定和查看 host

## 为什么这一章先做最小 gateway，不先做“真正 Ingress”

很多人看到“域名绑定”会下意识想到：

- Nginx
- Traefik
- Kubernetes Ingress Controller
- 证书
- 自动 DNS

这些东西后面当然都值得做，
但如果现在一口气全上，
你会把下面几件事混在一起：

1. 平台资源模型里怎么表达“这个域名属于哪个应用”
2. 请求到了入口层以后怎么找到当前 running 后端
3. 代理转发到底是怎么工作的
4. TLS 和 DNS 自动化又是另一套问题

所以这章故意只做最小闭环：

- 先把：
  - `host -> app -> current execution -> backend`
  - 这条链讲清楚

## 先把新资源讲清楚

### `app_domains`

`app_domains`
可以先理解成：

- 平台保存的域名绑定关系

它当前很简单，
只有这些核心字段：

- `id`
- `app_id`
- `host`
- `created_at`
- `updated_at`

现在先只支持：

- 一个完整 host

例如：

- `hello.demo.local`
- `api.demo.local`

暂时还不做：

- 通配符域名
- path routing
- TLS 证书
- DNS 自动下发

### `Route resolve`

这一章里还有一个很关键的动作：

- resolve route

也就是入口层收到一个 `Host` 之后，
要把它翻译成真正的后端地址。

当前控制面会按这条链去找：

1. 先查 `app_domains`
   - 看这个 host 绑到了哪个 `App`
2. 再查这个 `App` 当前最新 `Deployment`
3. 要求这个 `Deployment` 现在是：
   - `running`
4. 再查这个 `Deployment` 最新 `execution`
5. 要求这个 `execution` 也是：
   - `running`
6. 最后取出：
   - 节点 `privateIP`
   - `execution.hostPort`

然后拼成真正的代理目标：

- `http://{node.privateIP}:{execution.hostPort}`

## 为什么 `07` 还要改 `docker run` 的端口发布方式

这里有一个很关键但很容易忽略的细节。

在 `06` 里，
我们当时把容器端口发布成了：

- 只监听在节点本机 `loopback`

也就是更像：

- `127.0.0.1:{hostPort}`

这对 agent 自己做健康检查是够的，
因为 agent 就在节点本机上。

但对 gateway 来说就不够了。

因为 gateway 现在要做的是：

- 从 control-plane 机器
- 去访问目标节点的可达地址

所以这一章把 `docker run -p` 改成了：

- 发布到节点可达地址

这样 control-plane 才能按：

- `node.privateIP + hostPort`

把流量转过去。

这也意味着：

- 当前 `v1/07`
  - 的暴露方式还比较原始
  - 后端 hostPort 其实仍然直接暴露在节点上

所以这章的重点是：

- 先把平台入口链路讲清楚

而不是：

- 现在这版入口已经足够安全或足够正式

## 当前 gateway 到底怎么工作

可以按这一条链来理解：

```text
[1] 用户给 App 绑定 host
    例如：hello.demo.local

[2] 请求打到 control-plane
    Host: hello.demo.local

[3] gateway 先查 app_domains
    找到这个 host 属于哪个 App

[4] gateway 再查这个 App 当前最新 running deployment

[5] 再查这个 deployment 当前最新 running execution

[6] 再拿到：
    node.privateIP + execution.hostPort

[7] control-plane 反向代理过去
```

这就是最小入口层的核心。

## 为什么这里还不需要真的配 DNS

这一章是教程，
不是先做一个完整公网演示。

所以本地验证时，
我们不需要先去真的改 DNS。

因为 HTTP 路由最关键的信息其实是：

- `Host` 头

所以这章本地实验直接用：

```bash
curl -H 'Host: hello.demo.local' http://127.0.0.1:8080/
```

就足够验证：

- 域名绑定有没有生效
- gateway 有没有按 Host 找到正确后端

## 本地闭环实验

这一章建议继续在本机做，
但这里有一个和 `06` 不一样的点：

- 为了让 control-plane 能按“节点地址”访问到后端容器
- 本地教学节点的 `privateIP`
  - 这里直接注册成：
    - `127.0.0.1`

这不是在说真实云环境的私网地址会是 `127.0.0.1`，
而是在说：

- 本地单机实验里
- control-plane 和 node 其实就在同一台机器
- 所以用 `127.0.0.1`
  - 最容易把路由链讲清楚

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

### 3. 注册一个“本地节点”

```bash
cd projects/mini-cloud
go run ./cmd/agent register \
  --server http://127.0.0.1:8080 \
  --provider aliyun \
  --region cn-beijing \
  --name local-gateway-node \
  --private-ip 127.0.0.1 \
  --instance-id i-local-gateway-01 \
  --instance-type ecs.u1-c1m2.large \
  --cpu-milli-capacity 2000 \
  --memory-mi-capacity 4096
```

记下返回里的：

- `id`

下面假设它是：

- `node_xxx`

### 4. 发送一条 `ready` 心跳

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

### 5. 创建项目、应用、Release，并把它跑起来

这一段和 `06` 很像，
仍然用最简单的：

- `nginx:1.27-alpine`

先创建项目和应用：

```bash
PROJECT_ID="$(
  curl -s http://127.0.0.1:8080/api/v1/projects \
    -H 'Content-Type: application/json' \
    -d '{"name":"demo07","displayName":"Demo 07"}' \
  | jq -r '.id'
)"

APP_ID="$(
  curl -s http://127.0.0.1:8080/api/v1/apps \
    -H 'Content-Type: application/json' \
    -d "{
      \"projectID\":\"$PROJECT_ID\",
      \"name\":\"hello-gateway\",
      \"displayName\":\"Hello Gateway\",
      \"region\":\"cn-beijing\",
      \"replicas\":1,
      \"instanceClass\":\"small\",
      \"defaultPort\":80,
      \"readinessPath\":\"/\",
      \"env\":{}
    }" \
  | jq -r '.id'
)"
```

提交 release：

```bash
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

再让 agent 真正执行：

```bash
cd projects/mini-cloud
go run ./cmd/agent work \
  --server http://127.0.0.1:8080 \
  --node-id node_xxx \
  --health-attempts 20
```

### 6. 绑定一个 host

```bash
curl -s http://127.0.0.1:8080/api/v1/apps/"$APP_ID"/domains \
  -H 'Content-Type: application/json' \
  -d '{"host":"hello.demo.local"}' | jq
```

### 7. 用 `Host` 头验证 gateway

```bash
curl -i -H 'Host: hello.demo.local' http://127.0.0.1:8080/
```

如果成功，
你应该能看到：

- `HTTP/1.1 200 OK`

而且返回内容已经不是 control-plane 的 UI，
而是 `nginx` 的欢迎页。

这说明：

- control-plane 已经按 `Host`
  找到了绑定的应用
- 再按当前 running execution
  反代到了正确后端

### 8. 对比看一下“没带 Host”时会发生什么

```bash
curl -i http://127.0.0.1:8080/
```

这时候你看到的应该还是：

- control-plane 自己的根页面

因为：

- 没有匹配到绑定的域名
- gateway 就会回退到 control-plane 自己的路由

### 9. 也可以从 API 里回头确认绑定关系

```bash
curl -s http://127.0.0.1:8080/api/v1/apps/"$APP_ID" | jq '{
  app: .app.name,
  domains: .domains,
  deploymentStatus: .currentDeployment.status,
  executionStatus: .currentExecution.status,
  hostPort: .currentExecution.hostPort
}'
```

### 10. 清理实验环境

```bash
docker ps --filter name=mini-cloud- --format '{{.Names}}'
docker stop "$(docker ps --filter name=mini-cloud- --format '{{.Names}}' | head -n 1)"
docker compose -f deploy/compose/docker-compose.yml down -v
```

## 这一章完成后应该达到什么状态

如果 `07`
按当前实现收口，
那么项目至少应该达到这些状态：

- `App` 已经可以绑定域名
- control-plane 已经能按 `Host` 做最小反向代理
- 请求已经能从：
  - 稳定 host
  - 路由到当前 running execution
- `Apps` 页面已经能管理和查看域名绑定

## 当前实现的边界

虽然这章已经让平台更像“真的能对外提供访问”，
但边界还是要讲清楚。

当前 `07`
还没有做这些事：

- TLS / HTTPS
- 自动证书
- DNS 自动配置
- 多副本负载均衡
- 更正式的网关进程拆分
- path routing
- wildcard host
- auth / rate limit / WAF

所以当前这章更准确地说，
是在做：

- 最小 host-based gateway

而不是：

- 完整入口流量层

## 这一章的结论

`07`
把 `mini-cloud v1`
从“应用已经跑起来”
推进到了：

- 平台已经开始按稳定入口名来接流量

这一步很重要，
因为从平台心智上看，
用户真正感知到的往往不是：

- 某个随机宿主机端口

而是：

- 这个应用现在绑定了哪个入口

后面的章节，
就可以继续围绕这个入口层往上加：

- 观测
- 失败重试
- 节点维护

## 本章检查点

- commit:
  - `7c404f2214b98088372c18c2c033a8e56c225230`
- 状态：
  - `app_domains`、最小 gateway 反代、`Host` 路由和 `Apps` 页域名绑定入口已经落地
