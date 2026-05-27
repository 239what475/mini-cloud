# 06 Runtime Execution

这一章是 `mini-cloud v1` 第一次真正进入：

- 节点执行面
- 容器运行面

前面的 `05` 已经把：

- `Release`
- `Deployment`
- 调度

串起来了，
但当时流程只走到：

- `assigned`

也就是说，
控制面已经知道：

- 该把哪个版本放到哪个节点

但还没有人真的去做：

- 拉取镜像
- 起容器
- 做健康检查
- 把执行结果写回 control-plane

这一章补的就是这段缺口。

## 这一章先记一句最重要的话

从 `06` 开始，
`mini-cloud`
第一次具备了下面这条真实闭环：

```text
submit release
  ->
deployment assigned
  ->
agent polls work
  ->
docker run
  ->
HTTP readiness check
  ->
report running or failed
```

所以这章不是只画图，
而是真的把：

- 节点拿任务
- 节点执行
- 控制面收回执

这三件事接上了。

## 这一章实际落下了什么

当前代码里，
这章真正新增的是：

- `deployment_executions` 表
  - 记录一次具体执行
- `internal/execution`
  - 定义执行对象和回报模型
- `internal/runtime`
  - 定义 runtime 抽象
- `internal/runtime/docker_cli.go`
  - 第一版用 `Docker CLI` 跑容器
- `GET /api/v1/nodes/{nodeID}/work`
  - agent 拉取自己要执行的工作
- `POST /api/v1/nodes/{nodeID}/executions/{executionID}/report`
  - agent 上报执行结果
- `go run ./cmd/agent work`
  - 第一个真正会起容器的 agent 子命令
- `Apps` 页面里的 `currentExecution`
  - 可以直接看到容器名、宿主机端口、执行状态

## 为什么这里先做一次性 `work`，不先做常驻 daemon

这一章故意不先写长期运行的 agent daemon，
而是先写：

- 一次执行一轮的：
  - `go run ./cmd/agent work --node-id ...`

原因很简单：

如果现在同时把下面这些都混在一起，
初学者会更难看清主线：

- 常驻循环
- 重试
- backoff
- 多任务并发
- runtime 执行

所以这里先故意收敛成：

1. control-plane 先把工作准备好
2. agent 主动拉一次 work
3. 执行一轮
4. 回报结果

这样你会更容易看清：

- `assigned`
  - 是控制面的结果
- `deploying / running / failed`
  - 是执行面回写上来的结果

## 先把三个新对象讲清楚

### `WorkItem`

`WorkItem`
可以先理解成：

- control-plane 发给某个节点的一份“开工单”

它里面会带上：

- `deploymentID`
- `releaseID`
- `image`
- `command`
- `args`
- `env`
- `containerPort`
- `readinessPath`
- `containerName`

也就是说，
agent 拿到它以后，
已经知道：

- 要跑什么镜像
- 要监听哪个容器端口
- 健康检查该打哪里

### `deployment_executions`

这个表记录的是：

- 某次 `Deployment`
  - 在某个节点上
  - 具体执行成了什么样

它和 `Deployment` 的区别要分清：

- `Deployment`
  - 更像控制面的推进对象
- `deployment_executions`
  - 更像节点实际执行记录

当前最重要的字段是：

- `status`
  - `deploying / running / failed`
- `container_name`
- `container_id`
- `host_port`
- `status_reason`
- `started_at`
- `finished_at`

### `Runtime`

这里故意先抽了一层：

- runtime 执行适配层

也就是说，
agent 现在不会直接在业务代码里到处散落：

- `exec.Command("docker", ...)`

而是先抽象成：

- `Run`
- `Stop`
- `Logs`

当前第一版实现是：

- `DockerCLI`

后面如果你想换成：

- 直接调 Docker Engine API
- 调 containerd
- 甚至接 Kubernetes

至少上层 agent 流程不需要全部重写。

## `assigned` 之后到底发生了什么

可以按这一条链去理解：

```text
[1] 用户提交 Release
    控制面创建 Deployment

[2] 调度器选出目标节点
    Deployment: scheduling -> assigned

[3] 这个节点上的 agent 拉取 work
    GET /api/v1/nodes/{nodeID}/work

[4] control-plane 抢占这份工作
    Deployment: assigned -> deploying
    同时插入 deployment_executions

[5] agent 用 runtime 起容器
    当前是 docker run

[6] agent 对 127.0.0.1:hostPort/path 做 HTTP 健康检查

[7] agent 回报结果
    POST /api/v1/nodes/{nodeID}/executions/{executionID}/report

[8] control-plane 更新：
    execution.status
    deployment.status
    app.status
```

这里有一个很关键的点：

- agent 不是自己偷偷把容器跑起来就算结束

而是必须把结果再回报回去，
control-plane 才会真正知道：

- 这次部署跑成功了
- 还是跑失败了

## 本章的最小健康检查策略

当前实现很克制，
只做最小 HTTP 健康检查：

- agent 先拿到 `docker run` 返回的随机宿主机端口
- 再去请求：
  - `http://127.0.0.1:{hostPort}{readinessPath}`
- 默认每 `1s` 试一次
- 默认最多试 `10` 次

只要其中一次返回：

- `2xx`
- 或 `3xx`

就认为应用已经起来了，
然后回报：

- `running`

如果一直不成功，
就会：

- 抓一点容器日志
- 尝试停容器
- 回报：
  - `failed`

所以你现在要特别注意：

- `running`
  - 不是单纯指“docker run 成功返回了 container id”
- 而是指：
  - 容器已经被 agent 通过健康检查确认可访问

## 做一次本地闭环实验

这一章建议直接在本机跑一遍。

这里我们先不用真实 `ECS`，
而是把当前这台开发机先临时当成一个被纳管节点。

这样做的目的不是“伪装成云”，
而是为了先把整条控制链跑通。

### 1. 启动 PostgreSQL

在 `projects/mini-cloud/` 下执行：

```bash
docker compose -f deploy/compose/docker-compose.yml up -d
```

### 2. 启动 control-plane

新开一个终端：

```bash
cd projects/mini-cloud
go run ./cmd/control-plane
```

### 3. 注册一个本地测试节点

再开一个终端：

```bash
cd projects/mini-cloud
go run ./cmd/agent register \
  --server http://127.0.0.1:8080 \
  --provider aliyun \
  --region cn-beijing \
  --name local-docker-node \
  --private-ip 10.0.0.10 \
  --instance-id i-local-demo-01 \
  --instance-type ecs.u1-c1m2.large \
  --cpu-milli-capacity 2000 \
  --memory-mi-capacity 4096
```

记下返回结果里的：

- `id`

下面假设它是：

- `node_xxx`

### 4. 给节点打一条 `ready` 心跳

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

### 5. 创建项目和应用

```bash
PROJECT_ID="$(
  curl -s http://127.0.0.1:8080/api/v1/projects \
    -H 'Content-Type: application/json' \
    -d '{"name":"demo","displayName":"Demo"}' \
  | jq -r '.id'
)"

APP_ID="$(
  curl -s http://127.0.0.1:8080/api/v1/apps \
    -H 'Content-Type: application/json' \
    -d "{
      \"projectID\":\"$PROJECT_ID\",
      \"name\":\"hello-web\",
      \"displayName\":\"Hello Web\",
      \"region\":\"cn-beijing\",
      \"replicas\":1,
      \"instanceClass\":\"small\",
      \"defaultPort\":80,
      \"readinessPath\":\"/\",
      \"env\":{}
    }" \
  | jq -r '.id'
)"

printf 'project=%s\napp=%s\n' "$PROJECT_ID" "$APP_ID"
```

### 6. 提交一个真实能跑起来的 Release

这里用一个最简单的公开镜像：

- `nginx:1.27-alpine`

它监听：

- `80`

首页路径：

- `/`

所以和上面的应用配置能直接对上。

```bash
curl -s http://127.0.0.1:8080/api/v1/apps/"$APP_ID"/releases \
  -H 'Content-Type: application/json' \
  -d '{
    "version":"2026-04-09.1",
    "image":"nginx:1.27-alpine",
    "command":[],
    "args":[],
    "env":{},
    "port":80,
    "readinessPath":"/"
  }' | jq
```

这一步之后，
你应该会看到：

- `deployment.status`
  - 先走到 `assigned`

### 7. 让 agent 真正执行这份工作

```bash
cd projects/mini-cloud
go run ./cmd/agent work \
  --server http://127.0.0.1:8080 \
  --node-id node_xxx
```

你会看到这个命令自己做完：

- `GET /work`
- `docker run`
- 健康检查
- `POST /report`

如果成功，
最终 JSON 里会出现：

- `report.ack.execution.status`
  - `running`
- `runtimeRun.hostPort`
  - 一个随机宿主机端口

### 8. 回头看 control-plane 里的状态

```bash
curl -s http://127.0.0.1:8080/api/v1/apps/"$APP_ID" | jq '{
  appStatus: .app.status,
  deploymentStatus: .currentDeployment.status,
  deploymentReason: .currentDeployment.statusReason,
  executionStatus: .currentExecution.status,
  containerName: .currentExecution.containerName,
  hostPort: .currentExecution.hostPort
}'
```

成功的话，
这里应该会看到：

- `app.status`
  - `running`
- `currentDeployment.status`
  - `running`
- `currentExecution.status`
  - `running`

### 9. 直接访问这次执行出来的本地端口

```bash
HOST_PORT="$(
  curl -s http://127.0.0.1:8080/api/v1/apps/"$APP_ID" \
  | jq -r '.currentExecution.hostPort'
)"

curl -I http://127.0.0.1:"$HOST_PORT"/
```

如果返回：

- `HTTP/1.1 200 OK`

就说明：

- agent 真的把容器起起来了
- 健康检查也确实打通了

### 10. 清理这次实验留下的容器

这一章还没有做正式的删除工作流，
所以实验结束后可以先手工停掉教学容器：

```bash
docker ps --filter name=mini-cloud- --format '{{.Names}}'
docker stop "$(docker ps --filter name=mini-cloud- --format '{{.Names}}' | head -n 1)"
```

如果还要顺手把数据库环境也清掉：

```bash
docker compose -f deploy/compose/docker-compose.yml down -v
```

## 这一章完成后应该达到什么状态

如果 `06`
按当前实现收口，
那么项目至少应该达到这些状态：

- control-plane 已经能把 `assigned` 工作真正发给节点
- agent 已经能拉 work 并调用 runtime
- `docker run`
  - 已经真正接进主链
- 健康检查成功后会把状态推进到：
  - `running`
- 健康检查失败后会把状态推进到：
  - `failed`
- `Apps` 页面已经能看到：
  - `currentExecution`
  - `containerName`
  - `hostPort`

## 当前实现的边界

虽然这章已经是真实闭环，
但边界还是要讲清楚。

当前 `06`
还没有做这些事：

- agent 常驻循环
- 多副本执行
- 多任务并发
- 容器删除和重建策略
- 自动重试
- 失败后的再次调度
- 正式的日志采集和保留
- 域名和入口流量

所以这章更准确地说，
是在做：

- 第一版最小执行面

而不是：

- 完整运行时编排系统

## 这一章的结论

`06`
是 `mini-cloud v1`
第一个真正能让你感受到“平台开始运行了”的节点。

因为到这一步，
平台不再只是：

- 存资源
- 推状态
- 算调度

而是第一次真的让一个节点去：

- 执行工作
- 起容器
- 自证健康
- 回报结果

后面的章节，
就会在这个基础上继续补：

- 域名接入
- 观测
- 故障重试
- 节点维护

## 本章检查点

- commit:
  - `9f845396936067f2b834e339418b8895faedd446`
- 状态：
  - `deployment_executions`、work/report API、`agent work`、`Docker CLI runtime` 和最小健康检查已经落地
