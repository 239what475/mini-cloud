# 03 Node Agent And Registration

这一章开始把“节点怎么进入平台”这件事真正讲清楚。

如果说 `02` 解决的是：

- control-plane 先站起来
- 第一批资源和接口骨架先落下来

那么 `03` 解决的就是：

- 一台机器怎样被平台识别为一个 `Node`
- 这台机器怎样持续汇报自己的当前状态
- 平台管理员怎样看到这些节点

这里先记一句最重要的话：

- `register`
  - 解决“你是谁”
- `heartbeat`
  - 解决“你现在怎么样”

## 这一章实际会落什么代码

这一章不只是继续设计，
而是会真正把下面这些内容落下来：

- `nodes` 表
- `node_heartbeats` 表
- 节点注册接口
- 节点心跳接口
- 平台侧节点列表接口
- 一个最小可用的 `agent` 命令行
- 一个真正能显示节点状态的 `Nodes` 页面

也就是说，
这一章结束后，
`mini-cloud`
就不再只是“能创建 `Project` 的骨架”，
而是已经开始真的纳管节点了。

## 为什么先做这一章

在调度器、发布状态机、运行时执行之前，
平台必须先知道：

- 现在有哪些节点
- 节点属于哪个地域
- 节点大概有多少容量
- 节点最近是不是还活着

如果这一步没有先收清楚，
后面的：

- 调度
- 放置
- 重试
- 故障处理

都会失去基础。

所以从实现顺序上，
`03`
应该先于：

- `04-scheduler-and-placement`
- `05-release-and-deployment-state-machine`
- `06-runtime-execution`

## 这一章想解决什么

这一章的目标先收敛成三件事：

1. 节点注册
2. 节点心跳
3. 平台侧节点视图

也就是说，
这一章结束后，
平台至少应该能回答：

- 现在有哪些节点已经接入
- 它们分别来自哪个 `provider`
- 它们位于哪个 `region`
- 它们最近一次心跳是什么时候
- 它们最近一次上报的状态是什么

## 这一章不做什么

为了不把主题拉散，
这一章先明确不做这些：

- 调度器
- 容器拉起和停止
- 节点失联后的自动摘除
- `draining` 完整维护流程
- 复杂认证授权
- Docker 自动起一个“假 worker”并自动注册

这里特别强调最后一点。

我们当然会在后面做更真实的模拟实验，
但不放在这一章。

原因是：

- 这一章的核心是协议和状态语义
- 不是运行时和容器编排

如果一开始就在这里引入：

- Docker 容器
- 自动注册
- 定时心跳
- 容器网络

读者很容易把：

- agent 协议问题

和：

- 运行环境问题

混在一起。

所以这一章先只做最小闭环。

## 03 的边界

我建议 `03`
只落下面这些东西：

### 后端

- `nodes` 表
- `node_heartbeats` 表
- `POST /api/v1/nodes/register`
- `POST /api/v1/nodes/{nodeID}/heartbeat`
- `GET /api/v1/platform/nodes`

### agent

- 一个最小可用的 `agent`
- 先支持两类动作：
  - `register`
  - `heartbeat`

### 前端

- 把 `Nodes` 页面做成真正可读的管理页
- 只负责展示
- 不做“手工填写节点信息并注册”的表单

## 为什么 Nodes 页面只做展示

这点很重要。

`Node`
不是普通租户用户手工创建的资源，
它更像是：

- 一台机器
- 通过 agent 主动接入平台

所以更合理的教学方式是：

- 用命令或 agent 行为去注册节点
- 用平台页面去观察节点状态

而不是让读者在网页里手工填一堆：

- `instanceID`
- `privateIP`
- `cpuMilliCapacity`

去伪造一个节点。

## 资源模型怎么理解

这一章里，
最关键的两个对象是：

- `Node`
- `NodeHeartbeat`

### `Node`

`Node`
代表一台已经被平台纳管的机器。

在 `v1`
里，
它先默认对应一台阿里云 `ECS`。

它更偏：

- 静态身份
- 基础容量
- 调度可见信息

例如：

- `provider`
- `region`
- `instanceID`
- `instanceType`
- 总 CPU
- 总内存

### `NodeHeartbeat`

`NodeHeartbeat`
更像一个“当前状态上报包”。

它更偏：

- 时间点
- 当前空闲资源
- 当前运行容器数
- 当前状态判断

例如：

- `reportedAt`
- `cpuMilliFree`
- `memoryMiFree`
- `runningContainers`
- `status`

所以可以先这样记：

- `Node`
  - 更像节点档案
- `NodeHeartbeat`
  - 更像节点脉搏

## 为什么要同时存 Node 和 NodeHeartbeat

如果只存 `Node`，
你会丢掉很多“状态变化过程”。

如果只存 `NodeHeartbeat`，
你又会很难稳定表达：

- 这个节点到底是谁
- 它的长期身份信息是什么

所以更合理的方式是：

- `nodes`
  - 存稳定身份和最新摘要
- `node_heartbeats`
  - 存每次上报

这样平台既能看到：

- 当前最新状态

也能为以后扩展：

- 心跳历史
- 超时判定
- 状态变化分析

留下位置。

## 注册语义怎么定

这一章里，
注册接口不要做成“每次注册都创建一个新节点”。

我建议的语义是：

- 同一台底层实例重复注册
  - 仍然对应同一个 `Node`

这里最自然的唯一标识是：

- `provider + instanceID`

例如：

- `aliyun + i-abc123`

这样做的原因是：

- agent 重启后重新注册很常见
- 节点重新接入平台也很常见
- 如果每次注册都新建记录，
  - 平台里会很快出现一堆重复节点

所以注册流程建议这样定义：

1. 如果平台里还没有这个：
   - `provider + instanceID`
   - 就创建一个新 `Node`
2. 如果已经存在
   - 就更新它的静态信息
   - 继续复用原来的 `node id`

这就是一种最小的：

- 幂等注册

## 心跳语义怎么定

心跳不是“重新声明节点是谁”，
而是声明：

- 我还活着
- 我现在的状态是这样

所以心跳更适合负责这些事情：

- 刷新 `lastHeartbeatAt`
- 刷新节点最新状态
- 刷新最新空闲资源摘要
- 写入一条 `node_heartbeats` 历史记录

这里我建议先让心跳能更新：

- `Node.lastHeartbeatAt`
- `Node.status`
- `Node.cpuMilliAllocated`
- `Node.memoryMiAllocated`

其中：

- `cpuMilliAllocated`
- `memoryMiAllocated`

可以先通过：

- 总容量
- 减去最近一次心跳上报的空闲量

推出来。

这样 `GET /api/v1/platform/nodes`
就不需要每次再临时做复杂聚合。

## 这一章先用哪些状态

虽然在 `02`
里我们已经列过完整一些的 `NodeStatus` 草图，
但 `03`
不需要一次把所有生命周期都做全。

这一章里，
我建议先真正用起来的只有这些：

- `registering`
- `ready`
- `not_ready`

其中可以先这样理解：

- `registering`
  - 刚注册，还没有收到有效心跳
- `ready`
  - 已收到有效心跳，节点可正常接单
- `not_ready`
  - 节点已存在，但最近心跳上报自己不可用

下面这些状态先保留在资源模型里，
但不在 `03`
里真正实现完整流程：

- `draining`
- `offline`

原因是：

- `draining`
  - 更适合在维护模式章节展开
- `offline`
  - 更适合在失联和超时判定章节展开

## 03 要开的接口

### `POST /api/v1/nodes/register`

用途：

- 节点首次接入
- 或重复接入时刷新静态信息

请求建议先包含：

```json
{
  "provider": "aliyun",
  "region": "cn-beijing",
  "name": "ecs-cn-bj-01",
  "privateIP": "10.0.0.10",
  "publicIP": "203.0.113.10",
  "instanceID": "i-abc123",
  "instanceType": "ecs.u1-c1m2.large",
  "cpuMilliCapacity": 2000,
  "memoryMiCapacity": 4096
}
```

响应建议先包含：

```json
{
  "id": "node_01",
  "provider": "aliyun",
  "region": "cn-beijing",
  "name": "ecs-cn-bj-01",
  "privateIP": "10.0.0.10",
  "publicIP": "203.0.113.10",
  "instanceID": "i-abc123",
  "instanceType": "ecs.u1-c1m2.large",
  "cpuMilliCapacity": 2000,
  "memoryMiCapacity": 4096,
  "cpuMilliAllocated": 0,
  "memoryMiAllocated": 0,
  "status": "registering",
  "schedulable": true,
  "lastHeartbeatAt": "",
  "createdAt": "2026-04-09T12:04:00Z",
  "updatedAt": "2026-04-09T12:04:00Z"
}
```

### `POST /api/v1/nodes/{nodeID}/heartbeat`

用途：

- 节点汇报当前最新状态

请求建议先包含：

```json
{
  "reportedAt": "2026-04-09T12:05:00Z",
  "agentVersion": "0.1.0",
  "cpuMilliFree": 1800,
  "memoryMiFree": 3500,
  "runningContainers": 1,
  "status": "ready"
}
```

响应建议先包含：

```json
{
  "nodeID": "node_01",
  "accepted": true,
  "status": "ready",
  "receivedAt": "2026-04-09T12:05:00Z"
}
```

### `GET /api/v1/platform/nodes`

用途：

- 平台管理员查看当前节点清单和最新摘要

返回建议至少包含：

- 节点身份信息
- 总容量
- 最新已分配资源摘要
- 当前状态
- 最近一次心跳时间

这一章里，
我建议先不急着做：

- `GET /api/v1/platform/nodes/{nodeID}`
- `GET /api/v1/platform/nodes/{nodeID}/heartbeats`

因为 `03`
最重要的是先把主链闭环跑通。

## 数据表我建议怎么落

### `nodes`

建议字段：

- `id`
- `provider`
- `region`
- `name`
- `private_ip`
- `public_ip`
- `instance_id`
- `instance_type`
- `cpu_milli_capacity`
- `memory_mi_capacity`
- `cpu_milli_allocated`
- `memory_mi_allocated`
- `status`
- `schedulable`
- `last_heartbeat_at`
- `created_at`
- `updated_at`

这里建议额外加一个唯一约束：

- `(provider, instance_id)`

它的作用就是前面说的：

- 幂等注册

### `node_heartbeats`

建议字段：

- `id`
- `node_id`
- `reported_at`
- `agent_version`
- `cpu_milli_free`
- `memory_mi_free`
- `running_containers`
- `status`

这一章里，
`node_heartbeats`
先主要承担：

- 保存上报历史
- 给后续超时判定和观测留基础

## agent 在 03 里应该长什么样

这一章的 `agent`
不要再只是一个 placeholder。

但也不用一上来就做成长期驻留、定时循环、自动重试很重的程序。

我建议它先是一个：

- 最小可用命令行工具

先支持两类动作：

1. `register`
2. `heartbeat`

也就是说，
我们可以先这样使用它：

```bash
go run ./cmd/agent register ...
go run ./cmd/agent heartbeat ...
```

这样做的好处是：

- 协议最直观
- 调试最简单
- 文档里的实验也最好理解

后面如果需要，
再把它继续演进成：

- 常驻 agent
- 定时 heartbeat
- work polling

## 这一章的最小实验流

建议按下面这条最短路径验证：

1. 起本地 `postgres`
2. 起 `control-plane`
3. 执行一次 `agent register`
4. 执行一次 `agent heartbeat`
5. 查询平台节点列表
6. 打开前端 `Nodes` 页面

如果用当前项目目录来跑，
命令大概是：

```bash
docker compose -f deploy/compose/docker-compose.yml up -d postgres
```

```bash
go run ./cmd/control-plane
```

```bash
go run ./cmd/agent register \
  --region cn-beijing \
  --name ecs-cn-bj-01 \
  --private-ip 10.0.0.10 \
  --instance-id i-abc123 \
  --instance-type ecs.u1-c1m2.large \
  --cpu-milli-capacity 2000 \
  --memory-mi-capacity 4096
```

这一步预期会返回：

- 一个 `node_xxx` 风格的 `id`
- 节点状态先是：
  - `registering`

然后再发一次心跳：

```bash
go run ./cmd/agent heartbeat \
  --node-id node_xxx \
  --cpu-milli-free 1800 \
  --memory-mi-free 3500 \
  --running-containers 1 \
  --status ready
```

这一步预期会返回：

- `accepted: true`
- 状态变成：
  - `ready`

最后可以查询：

```bash
curl http://127.0.0.1:8080/api/v1/platform/nodes
```

这里应该能看到：

- 节点已经出现在列表里
- `lastHeartbeatAt` 已有值
- `cpuMilliAllocated`
  - 已经从心跳空闲量推导出来
- `latestHeartbeat`
  - 里有最近一次上报摘要

## 前端 Nodes 页我建议展示什么

`Nodes`
页这章先做成平台侧观察页面就够了。

建议先展示这些字段：

- `name`
- `provider`
- `region`
- `instanceType`
- `status`
- `lastHeartbeatAt`
- `cpuMilliCapacity`
- `memoryMiCapacity`
- `cpuMilliAllocated`
- `memoryMiAllocated`

如果心跳摘要也一起返回，
还可以再展示：

- `runningContainers`
- 最新空闲 CPU
- 最新空闲内存

但重点不是把页面做得很复杂，
而是让人一眼能看出：

- 节点已经接入了
- 心跳已经生效了

## 03 怎么验证

这章我建议的主验证方式不是：

- Docker 启一个自动注册的 worker 容器

而是：

- 本地最小闭环验证

原因前面已经说过：

- 先验证协议和状态语义
- 暂时不引入运行时环境噪音

所以这章的验证建议是：

1. 起本地 `postgres`
2. 起 `control-plane`
3. 手动执行一次 `agent register`
4. 手动执行一次 `agent heartbeat`
5. 用 API 查看节点列表
6. 用前端 `Nodes` 页面查看结果

也就是说，
这里验证的是：

- 注册逻辑
- 心跳逻辑
- 落库逻辑
- 节点展示逻辑

而不是：

- 容器自动编排
- 长驻 agent 调度

## 那什么时候做更真实的模拟实验

这点我们已经提前定过了。

我建议按后面的 roadmap 分阶段做：

### `03`

- 只做协议和状态闭环

### `06-runtime-execution`

- 开始做“节点上真的跑容器”的模拟实验
- 那时再引入：
  - Docker
  - runtime 执行
  - agent 执行结果回报

### `09-failure-retry-and-node-maintenance`

- 再做更像系统演练的实验
- 例如：
  - 心跳超时
  - 节点失联
  - 节点维护模式

所以：

- `03`
  - 不是不做实验
- 而是先做最小实验
- 更真实的大一点模拟实验
  - 留到更合适的章节

## 这一章完成后应该达到什么状态

如果 `03`
按这个设计收口，
那么这一章结束后，
项目应该至少达到这些状态：

- 平台里已经有真正的 `Node` 资源
- 节点可以幂等注册
- 节点可以主动上报心跳
- 平台可以看到最新节点清单
- 前端 `Nodes` 页不再是占位页
- 下一章可以开始讨论：
  - 怎么根据：
    - `region`
    - `capacity`
  - 为 `Deployment` 选择目标节点

## 这一章的结论

`03`
其实是在给后面的调度和执行打地基。

没有这一章，
平台只是一套：

- 能写项目
- 能想象资源模型

的 control-plane 骨架。

有了这一章以后，
平台才开始真正知道：

- 自己手里有哪些机器
- 这些机器现在大概是什么状态

这一步虽然还没有进入“调度”和“运行容器”，
但它已经让 `mini-cloud`
从纯骨架进入了真正的平台状态流。

## 本章检查点

- commit: `be86519f8b217b059cbe45e19db55022c4869265`
- 状态：
  - `Node`、注册、心跳、平台节点页和最小 agent CLI 已经落地
