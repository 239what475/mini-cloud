# 02 Control Plane Skeleton

这一章开始从“产品边界”进入“控制面骨架”。

这一章不再只是画资源草图，
而是把第一版 control-plane 的最小骨架先真正跑起来。

也就是说，
这章会同时做两件事：

- 把第一版控制面到底负责什么先讲清楚
- 把最小可运行实现先落下来

## 这一章想解决什么

上一章我们已经定了：

- `v1`
  - 是一个面向阿里云 `ECS` 节点池的最小应用平台
- 普通用户面对的是：
  - `Project`
  - `App`
  - `Release`
- 平台面才关心：
  - `Node`
  - `Capacity`
  - `Heartbeat`

那么这章就要回答：

- 第一版控制面到底保存哪些状态
- 哪些状态由用户写入
- 哪些状态由 agent 上报
- 第一批 API 到底先开哪些

同时也会真正落下第一批基础设施：

- control-plane 启动骨架
- PostgreSQL 连接
- migration 初始化
- 第一个真正可写入的资源：
  - `Project`

## 这一章实际落地了什么

当前代码里，
这章已经真正落下来的内容是：

1. 一个可启动的 `control-plane`
2. 启动时自动连库并执行 migration
3. 一个最小 `Project` 存储层
4. 第一批 HTTP 接口
5. 一个很薄的前端壳子

这里要特别注意：

- 下面文档里会继续定义：
  - `App`
  - `Release`
  - `Deployment`
  - `Node`
- 但这些在本章里还主要是资源设计草图
- 当前真正已经落库并打通 API 的，
  - 只有 `Project`

## 控制面的职责

第一版控制面，
建议先只负责这几件事：

1. 保存平台核心资源
2. 接收用户提交的期望状态
3. 推进发布和部署状态机
4. 接收节点心跳与执行结果
5. 给用户和平台管理员提供查询入口

反过来说，
第一版控制面暂时不直接负责：

- 底层容器运行时细节
- 真实日志采集实现
- 复杂流量入口实现
- 复杂认证授权

这些会分别下沉给：

- agent
- 配套基础设施
- 后续章节

## 第一版控制面的最小资源

我建议第一批先只落下面这些资源：

### 用户侧资源

- `Project`
- `App`
- `Release`
- `Deployment`

### 平台侧资源

- `Node`
- `NodeHeartbeat`
- `PlacementDecision`

这里的重点不是“一开始把所有对象都建全”，
而是先把：

- 发布
- 调度
- 执行
- 回传

这条主链需要的对象建齐。

## 第一批资源字段草图

这一节不是在定最终数据库表结构，
而是在定：

- 第一批 API 至少要表达哪些字段
- 哪些字段属于期望状态
- 哪些字段属于观察状态

### `Project`

建议先只保留最小字段：

- `id`
- `name`
- `displayName`
- `createdAt`

这里先故意不引入太多：

- 组织
- 成员
- 权限模型

因为 `v1` 还不准备展开完整认证授权。

### `App`

`App` 更像“一个长期存在的应用壳子”，
建议先有这些字段：

- `id`
- `projectID`
- `name`
- `displayName`
- `region`
- `replicas`
- `instanceClass`
- `defaultPort`
- `readinessPath`
- `env`
- `currentReleaseID`
- `status`
- `createdAt`
- `updatedAt`

这里可以先这样理解：

- `region`
  - 表达应用想部署到哪个阿里云地域
- `replicas`
  - 表达期望副本数
- `instanceClass`
  - 先表达平台给应用分配的规格档位
  - 暂时不直接暴露底层 `ECS` 规格名
- `status`
  - 表达应用当前总体状态

### `Release`

`Release` 是一次可追踪发布，
建议先有这些字段：

- `id`
- `appID`
- `version`
- `image`
- `command`
- `args`
- `env`
- `port`
- `readinessPath`
- `createdAt`

这里先把：

- 镜像
- 启动参数
- 环境变量
- 健康检查

都绑定到 `Release`，
这样回滚时语义更清楚：

- 回滚的其实是“回到某个旧 release”

### `Deployment`

`Deployment` 是把某个 `Release`
推进到实际运行状态的桥梁，
建议先有这些字段：

- `id`
- `appID`
- `releaseID`
- `desiredReplicas`
- `readyReplicas`
- `availableReplicas`
- `status`
- `statusReason`
- `createdAt`
- `updatedAt`

这里先故意不把它拆得太细，
因为 `v1`
还不需要一开始就变成一个完整的：

- `ReplicaSet`
- `Pod`

风格对象系统。

### `Node`

`Node` 在 `v1`
先表示一个被纳管的阿里云 `ECS` 节点，
建议先有这些字段：

- `id`
- `provider`
- `region`
- `name`
- `privateIP`
- `publicIP`
- `instanceID`
- `instanceType`
- `cpuMilliCapacity`
- `memoryMiCapacity`
- `cpuMilliAllocated`
- `memoryMiAllocated`
- `status`
- `schedulable`
- `lastHeartbeatAt`
- `createdAt`
- `updatedAt`

这里的：

- `instanceID`
- `instanceType`

是为了保留和阿里云资源的直接映射。

### `NodeHeartbeat`

心跳对象可以理解成一个“上报包”，
不一定必须单独长期存一张复杂大表，
但从 API 语义上先把它当成一个独立对象更容易理解。

建议先表达这些字段：

- `nodeID`
- `reportedAt`
- `agentVersion`
- `cpuMilliFree`
- `memoryMiFree`
- `runningContainers`
- `status`

### `PlacementDecision`

这个对象主要是为了把“为什么选中某个节点”显式记录下来，
便于后面解释调度。

第一版可以先有这些字段：

- `deploymentID`
- `nodeID`
- `region`
- `score`
- `reason`
- `createdAt`

## 哪些字段是期望状态，哪些是观察状态

这一点很关键，
因为后面控制面、调度器、agent 的责任边界都要靠它划。

### 期望状态

这类字段大多来自用户或控制面主动写入，
例如：

- `App.region`
- `App.replicas`
- `Release.image`
- `Release.env`
- `Deployment.desiredReplicas`

### 观察状态

这类字段大多来自控制面推进或 agent 上报，
例如：

- `App.status`
- `App.currentReleaseID`
- `Deployment.readyReplicas`
- `Deployment.availableReplicas`
- `Deployment.status`
- `Node.lastHeartbeatAt`
- `Node.cpuMilliAllocated`
- `Node.memoryMiAllocated`

可以先用一句话记：

- 用户写“我想要什么”
- 平台回报“现在做到什么了”

## 第一批状态枚举

这里先不要追求过细，
先定一批能支撑 `v1`
主链的状态就够了。

### `AppStatus`

建议第一版先有：

- `creating`
- `idle`
- `deploying`
- `running`
- `degraded`
- `failed`

可以先这样理解：

- `creating`：应用刚创建，还没有完成第一版可运行配置
- `idle`：应用对象存在，但当前还没有成功跑起来的版本
- `deploying`：正在推进新 release
- `running`：当前主版本运行正常
- `degraded`：有部分副本不可用，但系统还没完全失败
- `failed`：当前发布整体失败

### `DeploymentStatus`

建议第一版先有：

- `pending`
- `scheduling`
- `assigned`
- `deploying`
- `running`
- `failed`

这里比 `AppStatus`
更偏执行过程：

- `pending`：deployment 已创建，但还没开始真正推进
- `scheduling`：正在做节点选择
- `assigned`：已经选中目标节点，等待 agent 执行
- `deploying`：agent 正在拉镜像、起容器或检查健康状态
- `running`：当前 deployment 已达到可运行目标
- `failed`：当前 deployment 推进失败

### `NodeStatus`

建议第一版先有：

- `registering`
- `ready`
- `not_ready`
- `draining`
- `offline`

可以先这样理解：

- `registering`：节点刚接入，信息还没收全
- `ready`：可正常调度
- `not_ready`：有心跳，但不满足正常接单条件
- `draining`：正在维护，不再接新 workload
- `offline`：心跳超时或已失联

## 第一批动作流

把字段和状态放在一起后，
第一版最小主链可以先被理解成：

1. 用户创建 `Project`
2. 用户创建 `App`
3. 用户提交 `Release`
4. 控制面生成 `Deployment`
5. 调度器为 `Deployment`
   - 生成 `PlacementDecision`
6. agent 在目标 `Node`
   - 拉镜像
   - 起容器
   - 回报结果
7. 控制面更新：
   - `Deployment.status`
   - `App.status`
   - `App.currentReleaseID`

也就是说，
后面无论是写数据库、写 API，
还是写 orchestrator，
都应该围绕这条主链来展开。

## 建议的职责分界

可以先按下面这个方式分责任：

### 用户写入

- 创建 `Project`
- 创建 `App`
- 提交 `Release`
- 修改应用期望副本数

### 控制面生成或推进

- 为某个 `Release`
  - 创建 `Deployment`
- 为 `Deployment`
  - 生成放置结果
- 推进状态：
  - `Pending`
  - `Scheduling`
  - `Deploying`
  - `Running`
  - `Failed`

### agent 上报

- `NodeHeartbeat`
- 节点容量变化
- 容器启动结果
- 健康检查结果
- 实际运行状态

## 第一批接口应该先开哪些

第一版建议先开最小接口集，
不要一上来就追求“面面俱到”。

### 用户侧接口

- `POST /v1/projects`
- `GET /v1/projects`
- `POST /v1/apps`
- `GET /v1/apps`
- `GET /v1/apps/{appID}`
- `POST /v1/apps/{appID}/releases`
- `GET /v1/apps/{appID}/releases`
- `POST /v1/apps/{appID}/scale`

### 平台侧接口

- `POST /v1/nodes/register`
- `POST /v1/nodes/{nodeID}/heartbeat`
- `GET /v1/platform/nodes`
- `GET /v1/platform/deployments`

这里故意没有一开始就把所有接口都做成：

- 通用 CRUD

因为我们当前更想表达的是：

- 业务动作
- 状态推进

而不是“为了 REST 而 REST”。

## 当前代码已经实现的接口

虽然上面列的是第一批完整接口草图，
但本章当前真正已经实现的是这几条：

- `GET /api/healthz`
- `GET /api/v1/projects`
- `POST /api/v1/projects`
- `GET /v1/projects`
- `POST /v1/projects`

另外还有一个开发期根路径行为：

- `GET /`
  - 如果前端已经构建出 `web/dist/index.html`
    - 直接返回构建好的前端页面
  - 如果前端还没构建
    - 直接返回一段简单 JSON
    - 用来说明 control-plane 已启动、但 UI 还没 build

## 第一批请求 / 响应草图

这一节的目标不是把最终 OpenAPI 一次写完，
而是先让我们对：

- 请求里要填什么
- 响应里至少返回什么

形成共同直觉。

### 创建 `Project`

请求：

```json
{
  "name": "demo",
  "displayName": "Demo Project"
}
```

响应：

```json
{
  "id": "prj_01",
  "name": "demo",
  "displayName": "Demo Project",
  "createdAt": "2026-04-09T12:00:00Z"
}
```

### 创建 `App`

请求：

```json
{
  "projectID": "prj_01",
  "name": "hello-web",
  "displayName": "Hello Web",
  "region": "cn-beijing",
  "replicas": 1,
  "instanceClass": "small",
  "defaultPort": 8080,
  "readinessPath": "/healthz",
  "env": {
    "APP_ENV": "prod"
  }
}
```

响应：

```json
{
  "id": "app_01",
  "projectID": "prj_01",
  "name": "hello-web",
  "displayName": "Hello Web",
  "region": "cn-beijing",
  "replicas": 1,
  "instanceClass": "small",
  "defaultPort": 8080,
  "readinessPath": "/healthz",
  "env": {
    "APP_ENV": "prod"
  },
  "currentReleaseID": "",
  "status": "idle",
  "createdAt": "2026-04-09T12:01:00Z",
  "updatedAt": "2026-04-09T12:01:00Z"
}
```

### 提交 `Release`

请求：

```json
{
  "version": "2026-04-09.1",
  "image": "registry.example.com/demo/hello-web:2026-04-09.1",
  "command": [],
  "args": [],
  "env": {
    "APP_ENV": "prod"
  },
  "port": 8080,
  "readinessPath": "/healthz"
}
```

响应：

```json
{
  "id": "rel_01",
  "appID": "app_01",
  "version": "2026-04-09.1",
  "image": "registry.example.com/demo/hello-web:2026-04-09.1",
  "command": [],
  "args": [],
  "env": {
    "APP_ENV": "prod"
  },
  "port": 8080,
  "readinessPath": "/healthz",
  "createdAt": "2026-04-09T12:02:00Z"
}
```

这里有一个重要约定：

- 提交 `Release`
  - 之后
- 控制面会自动生成一个新的 `Deployment`

也就是说，
用户不直接创建 `Deployment`，
而是通过“提交 release”触发它。

### 查询 `App` 详情

响应建议先把“应用主信息 + 当前 deployment 摘要”一起带回来：

```json
{
  "app": {
    "id": "app_01",
    "projectID": "prj_01",
    "name": "hello-web",
    "displayName": "Hello Web",
    "region": "cn-beijing",
    "replicas": 1,
    "instanceClass": "small",
    "defaultPort": 8080,
    "readinessPath": "/healthz",
    "env": {
      "APP_ENV": "prod"
    },
    "currentReleaseID": "rel_01",
    "status": "deploying",
    "createdAt": "2026-04-09T12:01:00Z",
    "updatedAt": "2026-04-09T12:03:00Z"
  },
  "currentDeployment": {
    "id": "dep_01",
    "appID": "app_01",
    "releaseID": "rel_01",
    "desiredReplicas": 1,
    "readyReplicas": 0,
    "availableReplicas": 0,
    "status": "deploying",
    "statusReason": "waiting for node assignment",
    "createdAt": "2026-04-09T12:02:00Z",
    "updatedAt": "2026-04-09T12:03:00Z"
  }
}
```

### 注册 `Node`

请求：

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

响应：

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

### 上报 `NodeHeartbeat`

请求：

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

响应：

```json
{
  "nodeID": "node_01",
  "accepted": true,
  "status": "ready",
  "receivedAt": "2026-04-09T12:05:00Z"
}
```

## 最小表结构草图

这一节同样不是最终 SQL，
而是先把第一批表之间的关系说清楚。

### `projects`

建议先有这些列：

- `id`
- `name`
- `display_name`
- `created_at`

这一张表在当前代码里已经真正落地，
并通过 `goose` migration 创建。

### `apps`

建议先有这些列：

- `id`
- `project_id`
- `name`
- `display_name`
- `region`
- `replicas`
- `instance_class`
- `default_port`
- `readiness_path`
- `env_json`
- `current_release_id`
- `status`
- `created_at`
- `updated_at`

### `releases`

建议先有这些列：

- `id`
- `app_id`
- `version`
- `image`
- `command_json`
- `args_json`
- `env_json`
- `port`
- `readiness_path`
- `created_at`

### `deployments`

建议先有这些列：

- `id`
- `app_id`
- `release_id`
- `desired_replicas`
- `ready_replicas`
- `available_replicas`
- `status`
- `status_reason`
- `created_at`
- `updated_at`

### `nodes`

建议先有这些列：

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

### `node_heartbeats`

建议先有这些列：

- `id`
- `node_id`
- `reported_at`
- `agent_version`
- `cpu_milli_free`
- `memory_mi_free`
- `running_containers`
- `status`

### `placement_decisions`

建议先有这些列：

- `id`
- `deployment_id`
- `node_id`
- `region`
- `score`
- `reason`
- `created_at`

## 关系先怎么理解

第一版可以先按下面这条关系来理解：

- 一个 `Project`
  - 有多个 `App`
- 一个 `App`
  - 有多个 `Release`
  - 也会对应多次 `Deployment`
- 一个 `Deployment`
  - 会落到一个或多个 `Node`
  - 第一版先用：
    - `PlacementDecision`
    - 来记录放置结果
- 一个 `Node`
  - 会持续上报多个 `NodeHeartbeat`

如果只记一条最重要的主链，
那就是：

- `Project -> App -> Release -> Deployment -> PlacementDecision -> Node`

## v1 技术选型

走到这一步，
我们其实已经可以把第一版技术底座定下来了。

这里的原则不是：

- 选“最先进”的栈

而是：

- 选最稳
- 选最少
- 选最容易落地

## 主语言

第一版建议直接选：

- `Go`

原因很简单：

- 仓库里前面已经有大量 `Go` 代码
- 我们前面已经用 `Go`
  - 做过：
  - Kubernetes controller/operator
  - 阿里云 SDK 实验
- `Go`
  - 很适合写：
  - 控制面
  - agent
  - 长驻服务

所以 `mini-cloud`
不需要再为了“多样化”换语言。

## 控制面 API

第一版建议：

- `HTTP + JSON`

路由层当前直接用：

- 标准库 `http.ServeMux`

原因是：

- 当前路由非常少
- 直接用标准库最直接
- 可以先避免为了很小的 API 面再引入额外框架

如果后面路由复杂度继续上升，
再评估是不是需要：

- `chi`

日志建议直接选：

- 标准库 `slog`

这样第一版就不需要再引入额外日志框架。

## 数据库和访问方式

第一版建议：

- 数据库用：
  - `PostgreSQL`
- 驱动用：
  - `pgx`
- migration 用：
  - `goose`
- SQL 风格：
  - 手写 SQL
  - 不上 ORM

这样选的原因是：

- 这类控制面项目的核心就是：
  - 资源模型
  - 状态机
  - 明确的数据关系
- 手写 SQL 更容易看清：
  - 表结构
  - 查询
  - 状态更新
- 第一版没必要把复杂度浪费在 ORM 抽象上

## 控制面进程形态

第一版建议先做成：

- 单进程 control-plane

也就是一个进程里先同时包含：

- API server
- scheduler loop
- reconciler loop
- heartbeat checker

这样做的原因是：

- 第一版最重要的是先把主链跑通
- 不是先把控制面拆成很多服务

后面如果真的有必要，
再拆成：

- 独立 scheduler
- 独立 reconciler

也不迟。

## agent 通信方式

第一版建议：

- agent 主动连接 control-plane

先只做三类动作：

- `register`
- `heartbeat`
- `poll work`

这里暂时不急着做：

- 控制面主动推送任务
- 长连接流式调度
- `gRPC`

原因是：

- 你现在的节点环境本来就跨网络边界
- agent 主动上报和轮询更容易穿透真实网络条件
- 实现复杂度也更低

## 运行时封装

第一版建议先抽一个：

- `Runtime`
  - 接口

然后先给它做一个：

- `Docker CLI`
  - 实现

也就是说：

- 控制面和 agent 的业务代码
  - 不要直接到处执行 `docker run`
- 而是统一走：
  - `Runtime`
  - 抽象

这样第一版虽然底层先用：

- `Docker`

但后面如果想换：

- `containerd`
- 或更正式的执行器

会容易很多。

## 本地开发和实验环境

第一版建议先用：

- `Docker Compose`

最小起步环境可以先只包含：

- `postgres`
- `control-plane`

后面需要时再补：

- 本地 agent
- mock node

真实环境侧，
继续复用前面已经学过的：

- 阿里云 `ECS`
- Terraform
- Ansible

这样就不会为了 `mini-cloud`
又重造一套基础设施准备流程。

## 观测和调试

第一版建议先做最小可用：

- 结构化日志
- `/metrics`
- 事件表

也就是说：

- 先保证调试能看见
- 先保证关键状态能暴露出来

而不是一开始就做很重的：

- tracing
- 大而全日志平台
- 很复杂的 dashboard

## 用户入口形态

虽然从产品角度，
我们前面已经定了：

- `API -> Web -> CLI`

但这里再把技术动作说得更具体一点：

- `API`
  - 是第一真相源
- `Web`
  - 第一版直接用：
    - `React`
    - 来做
  - 但仍然保持“薄控制台”定位
- `CLI`
  - 可以后补成一个很薄的 `mcctl`

也就是说，
前端技术上我们会直接接受：

- `React + TypeScript`

但产品和工程优先级上，
仍然坚持：

- `API`
  - 主导资源模型
- `Web`
  - 消费 API
  - 不反过来主导后端边界

## 第一版前端技术建议

既然前面已经决定：

- `v1`
  - 直接上 `React`

那我建议前端技术栈先收成：

- `React`
- `TypeScript`
- `Vite`
- `React Router`
- `TanStack Query`

第一版暂时先不要上：

- `Redux`
- 很重的 UI 框架
- 微前端
- 复杂 SSR

这样做的原因是：

- 足够现代
- 足够好用
- 但不会一上来就把复杂度拉太高

## 第一版不建议上的东西

为了保证范围收敛，
第一版建议明确先不选这些：

- `k3s` / Kubernetes 作为运行底座
- `gRPC`
- 消息队列
- `Redis`
- ORM
- `Keycloak` / `Authentik`
- 多副本 control-plane

这些东西不是永远不用，
而是：

- 不是 `v1`
  - 最需要先解决的问题

## 建议的代码结构

如果按这套技术选型推进，
第一版代码结构建议长这样：

```text
projects/mini-cloud/
  go.mod
  cmd/
    control-plane/
    agent/
  web/
  internal/
    httpapi/
    store/
    config/
    project/
  deploy/
    compose/
  docs/
```

这里的重点是：

- 先把已经真正出现的职责拆出来
- 暂时不要为了“未来可能会有”就把目录提前铺满

后面的：

- `scheduler/`
- `reconciler/`
- `runtime/`
- `node/`
- `app/`
- `release/`
- `deployment/`

再随着章节推进逐步补进来。

## 这一章关于技术选型的结论

第一版现在可以明确收成下面这套：

- `Go`
- `HTTP + JSON`
- 标准库 `http.ServeMux`
- `slog`
- `PostgreSQL`
- `pgx`
- `goose`
- `React`
- `TypeScript`
- `Vite`
- `React Router`
- `TanStack Query`
- 单进程 control-plane
- agent 主动上报和轮询
- `Runtime` 接口 + `Docker CLI` 实现
- `Docker Compose` 本地开发环境

这套组合的目标只有一个：

- 尽快把第一版主链稳定跑起来

## 第一版控制面最小页面或命令面

如果先按：

- `API`
- `Web`

推进，
那么第一批最小动作大概就是：

1. 创建项目
2. 创建应用
3. 提交一个 release
4. 查询应用状态
5. 注册节点
6. 上报心跳

而这些动作既可以通过：

- API 调用
- 也可以通过：
  - `React`
  - 控制台
  - 来完成第一批演示

也就是说，
`02`
这章结束后，
应该达到的状态是：

- 一个最小 control-plane 已经真的能启动
- 数据库初始化已经打通
- `Project` 已经可以通过 API 写入和查询
- 控制面的资源骨架清楚了
- 第一批接口边界清楚了
- 第一批字段和状态也清楚了
- 下一章可以正式开始写：
  - 节点注册流程
  - 心跳上报
  - 节点状态

## 这一章的结论

第一版控制面现在可以先被理解成：

- 一个保存期望状态和观察状态的中心
- 一个推进发布与部署状态机的地方
- 一个接收节点回报并对外展示状态的地方

它不是：

- 直接运行容器的地方
- 也不是一开始就提供完整云控制台的地方

## 本章检查点

- commit: `bbe96eccafa6af0219b13d04467799faf248e08b`
- 状态：
  - 最小 control-plane、数据库初始化、`Project` API、前端壳子已经落地
