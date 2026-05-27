# 09 Failure Retry And Node Maintenance

这一章给 `mini-cloud v1` 补上最小恢复面。

前面的 `08` 已经能做到：

- 节点注册
- 调度
- 发布
- runtime 执行
- 域名接入
- 基础观测

但如果平台真的开始跑应用，
很快就会遇到下面三类问题：

1. 某台节点要维护，
   - 暂时不想再接新 workload
2. 某台节点长时间没有 heartbeat，
   - 平台要不要把它当成失联
3. 某个 deployment 已经失败，
   - 平台怎么再试一轮

这一章就是先把这三件事接起来。

## 这一章先记三个词

### `drain`

在这一章里，
`drain` 的意思非常克制：

- 这台节点不再接收新的放置决策

但它还不负责：

- 驱逐已经在跑的 workload
- 优雅迁移已有实例

也就是说，
当前 `drain`
只是：

- `status = draining`
- `schedulable = false`

所以你可以先把它理解成：

- “先别再往这台机器上放新东西了”

### `offline`

`offline`
不是 agent 主动上报的维护状态，
而是 control-plane 根据：

- `lastHeartbeatAt`

推导出来的一个平台视角状态。

这一章里，
平台提供一个显式动作：

- `reconcile-heartbeats`

它会检查：

- 哪些节点的最后心跳时间已经早于阈值

然后把这些节点标成：

- `offline`

### `retry`

这一章的 `retry`
不是：

- 把旧 deployment 从 `failed` 改回 `pending`

而是：

- 基于当前 release
- 新建一个全新的 deployment
- 再走一轮：
  - `pending -> scheduling -> assigned -> deploying -> running|failed`

这个点很重要，
因为它更接近真实平台思路：

- 旧尝试保留历史
- 新尝试单独记账

## 这一章实际新增了什么

后端新增了这些动作：

- `POST /api/v1/platform/nodes/{nodeID}/drain`
- `POST /api/v1/platform/nodes/{nodeID}/activate`
- `POST /api/v1/platform/nodes/reconcile-heartbeats`
- `POST /api/v1/apps/{appID}/operations/retry`

前端新增了这些入口：

- `Nodes` 页面
  - 可以手动：
    - `Drain node`
    - `Activate node`
    - `Mark stale nodes offline`
- `Apps` 页面
  - 当当前 deployment 已失败时，
    - 可以点：
      - `Retry deployment`

同时后端状态机也补了一条很关键的转换：

- `running -> failed`

否则当节点失联时，
平台没法把“之前明明已经跑起来的 deployment”
重新打回失败态。

## 这一章的恢复链路到底是什么

这一章的最小恢复链路可以按下面这条顺序理解：

1. 平台先通过 `drain`
   - 把节点从“可接新单”切到“停止接新单”
2. 调度器随后只会继续看：
   - `status = ready`
   - `schedulable = true`
   的节点
3. 如果某台节点长期没有 heartbeat，
   - 平台执行一次 `reconcile-heartbeats`
4. 这一步会把 stale 节点标成：
   - `offline`
5. 同时把这台节点上仍处于：
   - `assigned`
   - `deploying`
   - `running`
   的 deployment 打成 `failed`
6. 应用侧此时就能看到：
   - 当前 deployment 失败
7. 操作员再显式执行一次：
   - `retry`
8. 平台会新建一轮 deployment，
   - 再次交给 scheduler 选择其他仍然健康的节点

所以这章真正想讲明白的是：

- 节点维护
- 节点失联
- deployment 重试

这三件事不是孤立的，
它们合起来才是最小恢复面。

## 这章故意没有做什么

这几个东西，
这一章故意没有做：

- 自动后台循环持续扫 stale 节点
- deployment 自动重试
- drain 时自动驱逐正在运行的 workload
- 真正远程停掉失联节点上的容器

也就是说，
`09`
仍然是一个很克制的第一版：

- 平台提供恢复动作
- 但恢复时机仍由操作员明确触发

## 一个很重要的实现细节

这一章里：

- `draining`
  - 是平台管理员显式设置的状态
- `offline`
  - 是平台根据心跳过期推导出来的状态

所以两者的处理逻辑不同：

- 当节点已经被 `drain` 后，
  - 后续 heartbeat 不会把它自动改回 `ready`
- 当节点只是被推导成 `offline` 时，
  - 一旦又收到新 heartbeat
  - 它就会恢复成 agent 当前上报的状态

也就是说：

- `drain`
  - 更像管理员意图
- `offline`
  - 更像平台观察结果

## 本章实验

这一章建议直接沿用前面几章的本地环境：

- 一台本机
- 一个本地 Postgres
- control-plane 监听：
  - `127.0.0.1:8080`

下面这个实验会跑出一条完整闭环：

1. 注册两个本地节点
2. 先 `drain` 其中一个节点
3. 观察预览调度避开它
4. 再把它激活回来
5. 提交一个真正的 release
6. 让它先跑在 `node-a`
7. 再把 `node-a` 判成 `offline`
8. 最后手动 `retry`
   - 让新 deployment 重调度到 `node-b`

### 1. 启动数据库和 control-plane

```bash
cd projects/mini-cloud

docker compose -f deploy/compose/docker-compose.yml up -d
go run ./cmd/control-plane
```

另开一个终端继续下面命令。

### 2. 注册两个节点

先注册 `node-a`：

```bash
cd projects/mini-cloud
go run ./cmd/agent register \
  --server http://127.0.0.1:8080 \
  --region cn-beijing \
  --name local-node-a \
  --private-ip 127.0.0.1 \
  --instance-id i-local-09-a \
  --instance-type ecs.u1-c1m2.large \
  --cpu-milli-capacity 2500 \
  --memory-mi-capacity 4096
```

再注册 `node-b`：

```bash
cd projects/mini-cloud
go run ./cmd/agent register \
  --server http://127.0.0.1:8080 \
  --region cn-beijing \
  --name local-node-b \
  --private-ip 127.0.0.1 \
  --instance-id i-local-09-b \
  --instance-type ecs.u1-c1m2.large \
  --cpu-milli-capacity 2000 \
  --memory-mi-capacity 4096
```

记下返回里的：

- `node-a` 的 `id`
- `node-b` 的 `id`

下面假设它们分别是：

- `node_a_id`
- `node_b_id`

### 3. 给两个节点各打一条 ready heartbeat

```bash
cd projects/mini-cloud
go run ./cmd/agent heartbeat \
  --server http://127.0.0.1:8080 \
  --node-id node_a_id \
  --cpu-milli-free 2300 \
  --memory-mi-free 3800 \
  --running-containers 0 \
  --status ready

go run ./cmd/agent heartbeat \
  --server http://127.0.0.1:8080 \
  --node-id node_b_id \
  --cpu-milli-free 1800 \
  --memory-mi-free 3500 \
  --running-containers 0 \
  --status ready
```

### 4. 先把 `node-a` 设成维护模式

```bash
curl -s -X POST \
  http://127.0.0.1:8080/api/v1/platform/nodes/node_a_id/drain \
  | jq '{id: .node.id, name: .node.name, status: .node.status, schedulable: .node.schedulable}'
```

你应该看到：

- `status`
  - `draining`
- `schedulable`
  - `false`

### 5. 做一次调度预览，确认它已经不再接新单

```bash
curl -s -X POST \
  http://127.0.0.1:8080/api/v1/platform/placements/preview \
  -H 'Content-Type: application/json' \
  -d '{
    "provider":"aliyun",
    "region":"cn-beijing",
    "cpuMilliRequest":500,
    "memoryMiRequest":512,
    "replicas":1
  }' \
  | jq '{decision, filteredCounts}'
```

这个时候，
虽然 `node-a`
容量更大，
但预览结果应该会落到：

- `node-b`

因为 `node-a`
已经被 `drain` 了。

### 6. 激活 `node-a`，然后创建一个真实应用

先重新激活：

```bash
curl -s -X POST \
  http://127.0.0.1:8080/api/v1/platform/nodes/node_a_id/activate \
  | jq '{id: .node.id, name: .node.name, status: .node.status, schedulable: .node.schedulable}'
```

再创建项目、应用和 release：

```bash
PROJECT_ID="$(
  curl -s http://127.0.0.1:8080/api/v1/projects \
    -H 'Content-Type: application/json' \
    -d '{"name":"demo09","displayName":"Demo 09"}' \
  | jq -r '.id'
)"

APP_ID="$(
  curl -s http://127.0.0.1:8080/api/v1/apps \
    -H 'Content-Type: application/json' \
    -d "{
      \"projectID\":\"$PROJECT_ID\",
      \"name\":\"hello-recover\",
      \"displayName\":\"Hello Recover\",
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
  }' \
  | jq '{deployment, placementDecision}'
```

因为这里 `node-a`
已经恢复为可调度，
而且它容量更高，
所以这次真实放置应该会重新选中：

- `local-node-a`

### 7. 让 `node-a` 执行这次 deployment

```bash
cd projects/mini-cloud
go run ./cmd/agent work \
  --server http://127.0.0.1:8080 \
  --node-id node_a_id \
  --health-attempts 20
```

成功后，
你会看到：

- deployment 进入：
  - `running`

### 8. 模拟 `node-a` 失联

这一节故意把阈值压得很小，
只是为了让实验能很快看到效果。

先给 `node-b`
补一条最新 heartbeat，
确保它仍然是 fresh 的：

```bash
cd projects/mini-cloud
go run ./cmd/agent heartbeat \
  --server http://127.0.0.1:8080 \
  --node-id node_b_id \
  --cpu-milli-free 1800 \
  --memory-mi-free 3500 \
  --running-containers 0 \
  --status ready
```

然后立刻执行一次 stale-heartbeat 扫描：

```bash
curl -s -X POST \
  http://127.0.0.1:8080/api/v1/platform/nodes/reconcile-heartbeats \
  -H 'Content-Type: application/json' \
  -d '{"staleAfterSeconds":5}' \
  | jq
```

这时你应该能看到：

- `nodesMarkedOffline`
  - 至少包含：
    - `node-a`
- `impactedDeployments`
  - 会列出刚才跑在 `node-a` 上的 deployment

再看一次应用详情：

```bash
curl -s http://127.0.0.1:8080/api/v1/apps/"$APP_ID" \
  | jq '{app: .app.status, deployment: .currentDeployment.status, execution: .currentExecution.status}'
```

现在应该会变成：

- `app`
  - `failed`
- `deployment`
  - `failed`
- `execution`
  - `failed`

### 9. 对失败应用手动执行一次 retry

```bash
curl -s -X POST \
  http://127.0.0.1:8080/api/v1/apps/"$APP_ID"/operations/retry \
  | jq '{deployment, placementDecision}'
```

这次新的 deployment 应该会被重新放到：

- `node-b`

因为此时：

- `node-a`
  - 已经是 `offline`
- `node-b`
  - 仍然是 `ready`

### 10. 让 `node-b` 执行这次新 deployment

```bash
cd projects/mini-cloud
go run ./cmd/agent work \
  --server http://127.0.0.1:8080 \
  --node-id node_b_id \
  --health-attempts 20
```

最后再看一次应用详情：

```bash
curl -s http://127.0.0.1:8080/api/v1/apps/"$APP_ID" \
  | jq '{app: .app.status, deployment: .currentDeployment.status, currentNode: .currentPlacement.nodeName}'
```

你应该能看到：

- `app`
  - `running`
- `deployment`
  - `running`
- `currentNode`
  - `local-node-b`

## 如果实验里两个节点都被判成 offline 了怎么办

这是这一章里最常见的实验现象。

原因通常不是逻辑错了，
而是你给的：

- `staleAfterSeconds`
  - 太小

同时你在终端里停留得比预期久。

这时处理方法很简单：

1. 给还想继续使用的那台节点再打一条 heartbeat
2. 再执行一次 `retry`

也就是说，
`reconcile-heartbeats`
本身没有“记仇”：

- stale 节点只要重新发 heartbeat
- 就能恢复成 agent 当前上报的状态

## 这一章还留下了什么空白

做到这里，
`mini-cloud v1`
已经第一次有了最小恢复面。

但它依然只是一个第一版：

- 还没有后台自动扫 stale 节点
- 还没有自动 retry policy
- 还没有真正的 workload evacuation
- 还没有 node 维护窗口和更正式的运维编排

这些东西，
会留给后面的版本继续扩。

## 本章检查点

- commit:
  - `2f3e06feb0822dcabd69160ce20ee29662b14cce`
- 状态：
  - 节点维护、心跳失联标记、失败重试和重调度已经串起来
