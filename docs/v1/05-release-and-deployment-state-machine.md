# 05 Release And Deployment State Machine

这一章开始把：

- `App`
- `Release`
- `Deployment`

这三者真正串起来。

如果说：

- `03`
  - 解决的是“节点怎么进入平台”
- `04`
  - 解决的是“选哪个节点”

那么：

- `05`
  - 解决的就是“用户提交一个新版本之后，控制面到底怎么推进状态”

这里先记一句最重要的话：

- `App`
  - 是长期对象
- `Release`
  - 是一次不可变版本
- `Deployment`
  - 是“把这个版本推进到实际运行状态”的一次尝试

## 这一章实际会落什么代码

这一章不只是继续设计，
而是会真正把下面这些内容落下来：

- `apps` 表
- `releases` 表
- `deployments` 表
- `deployment_transitions` 表
- `internal/app`
- `internal/release`
- `internal/deployment`
- `POST /api/v1/apps`
- `GET /api/v1/apps`
- `GET /api/v1/apps/{appID}`
- `POST /api/v1/apps/{appID}/releases`
- `GET /api/v1/apps/{appID}/releases`
- `GET /api/v1/platform/deployments`
- 一个真正可用的 `Apps` 页面

也就是说，
这一章结束后，
`mini-cloud`
已经不只是：

- 能创建 `Project`
- 能纳管 `Node`
- 能做 placement preview

而是已经能：

- 创建一个应用
- 给这个应用提交一个新版本
- 自动生成 `Deployment`
- 自动跑一次调度
- 把状态推进到：
  - `assigned`
  - 或 `failed`

## 为什么这一章先做状态机

很多人一开始会下意识想先写：

- Docker 拉镜像
- Docker 起容器
- 健康检查
- 运行结果回报

但如果现在直接去写这些，
你会很容易把三类问题混在一起：

1. 用户到底想部署哪个版本
2. 控制面到底把它推进到了哪个阶段
3. 节点执行时到底出了什么问题

所以更稳的顺序应该是：

1. 先把：
   - `Release`
   - `Deployment`
   - 状态推进
   - 讲清楚
2. 再在 `06`
   - 把 runtime 执行接进去

这一章的核心不是“容器已经跑起来”，
而是：

- 控制面终于开始真正管理一次发布流程了

## 先把三个对象重新说透

### `App`

`App`
是长期存在的应用对象。

它表达的是：

- 这个项目里有一个叫：
  - `hello-web`
  - 的应用
- 它想部署到哪个地域
- 它想要几个副本
- 它用哪个规格档位

所以：

- `App`
  - 更像长期声明

### `Release`

`Release`
是一次不可变版本。

它表达的是：

- 这次要发布哪个镜像
- 这个镜像监听哪个端口
- 健康检查路径是什么
- 这次版本号是什么

这里有个关键点：

- `Release`
  - 一旦创建
  - 就不再修改

这样做的好处是：

- 回头看历史版本时语义很清楚
- 后面做回滚也更顺

### `Deployment`

`Deployment`
不是用户手工创建的。

它是控制面在：

- 接收到一次 `Release`

之后，
自动创建出来的“推进对象”。

可以先这样记：

- `Release`
  - 表达“我要发布什么”
- `Deployment`
  - 表达“平台正在怎样把它推进出去”

## 为什么提交 Release 会自动生成 Deployment

这个点非常重要。

在 `mini-cloud` 的设计里：

- 用户不直接创建 `Deployment`

而是：

- 提交一个 `Release`
- 控制面自动生成一个新的 `Deployment`

原因是：

- `Deployment`
  - 本质上是平台内部推进状态的工作对象
- 它不是用户真正关心的主对象

对用户来说，
更自然的动作应该是：

1. 我先创建应用
2. 我再提交一个新版本
3. 平台开始推进这次发布

这就更像真实平台的用户心智。

## 这一章真正落下来的状态推进链

这一章当前真实落下来的状态链是：

```text
submit release
  ->
create deployment (pending)
  ->
start scheduling (scheduling)
  ->
assigned
or
failed
```

也就是说，
当前代码已经把：

- `pending`
- `scheduling`
- `assigned`
- `failed`

这几个关键状态真正用起来了。

## 为什么这里先停在 assigned / failed

你可能会马上问：

- 为什么成功分支不是：
  - `running`
- 而只是：
  - `assigned`

原因很简单：

- 当前还没有把 runtime 执行接进来

这一章里，
控制面做到的事情是：

1. 接收新版本
2. 建 `Deployment`
3. 跑调度
4. 选出目标节点

但它还没有开始：

- 拉镜像
- 起容器
- 跑健康检查

这些是 `06`
才会接上的事情。

所以：

- `assigned`
  - 现在的含义是：
  - “控制面已经选中节点，接下来就等节点执行层介入了”

## AppStatus 和 DeploymentStatus 不是一回事

这一章最容易混淆的地方之一，
就是：

- `App.status`
- `Deployment.status`

到底有什么区别。

### `Deployment.status`

它更偏一次“推进过程”。

例如当前这一章里：

- `pending`
- `scheduling`
- `assigned`
- `failed`

这些都是在描述：

- 这次部署尝试推进到了哪一步

### `App.status`

它更偏应用总体视角。

当前实现里，
当一次新 `Release`
提交后：

- 如果已经选中节点
  - `App.status = deploying`
- 如果调度失败
  - `App.status = failed`

这里的：

- `deploying`

不是说容器已经在跑了，
而是说：

- 从应用视角看
  - 新版本正在被推进

而：

- `failed`

表达的是：

- 当前这次最新发布已经失败

## 为什么 assigned 时 App 还是 deploying

这点第一次看会有点绕。

当前成功路径里，
你会看到：

- `Deployment.status = assigned`
- `App.status = deploying`

这不是冲突，
而是两层语义不同。

可以这样理解：

- `Deployment.assigned`
  - 说的是：
  - 节点已经选出来了
- `App.deploying`
  - 说的是：
  - 这次新版本还在推进过程中

因为从应用角度看，
它还没有真正进入：

- `running`

所以：

- `deploying`
  - 才是更合理的总体状态

## 04 是怎么接进 05 的

`04`
里我们做的是：

- 平台侧 placement preview
- 独立 scheduler 核心

到 `05`
时，
这套东西第一次不再只是“平台管理员预览”，
而是接入了真实主链：

1. `Release` 创建
2. `Deployment` 创建
3. 控制面调用：
   - `scheduler.Evaluate(...)`
4. 如果选中节点：
   - 创建 `placement_decisions`
   - 并把 `deployment_id`
     - 真的写进去

这就是 `04 -> 05`
最关键的衔接点。

`04`
时：

- `PlacementDecision`
  - 还可以没有 `deploymentID`

`05`
时：

- placement 已经不再只是 preview
- 它开始成为某次真实 `Deployment`
  - 的调度结果

## 这一章的表结构到底在表达什么

### `apps`

这一张表保存应用的长期信息：

- 属于哪个 `Project`
- 应该部署到哪个 `region`
- 想要几个副本
- 用哪个 `instanceClass`
- 默认端口和健康检查
- 当前正在关注哪个 `current_release_id`
- 当前总体状态是什么

### `releases`

这一张表保存每次不可变版本：

- 版本号
- 镜像
- 启动参数
- 环境变量
- 端口
- 健康检查路径

### `deployments`

这一张表保存一次具体推进尝试：

- 对应哪个 `App`
- 对应哪个 `Release`
- 想推几个副本
- 当前状态是什么
- 当前原因是什么

### `deployment_transitions`

这张表是这一章非常关键的一张表。

它保存的是：

- 某次 `Deployment`
  - 每一步状态变化

也就是说，
这一章不是只把最终状态写回：

- `deployments.status`

而是把过程也记下来。

例如成功路径里，
你会看到类似：

- `<nil> -> pending`
- `pending -> scheduling`
- `scheduling -> assigned`

失败路径里，
则会看到：

- `<nil> -> pending`
- `pending -> scheduling`
- `scheduling -> failed`

这就是“状态机”真正被落到数据里了。

## 为什么 deployment_transitions 很重要

如果只存：

- `deployments.status`

你最终只能看到一个结果，
却看不到过程。

那后面一旦出问题，
你很难分清：

- 它根本没开始推进
- 它推进到了 scheduling
- 它是调度失败
- 还是后面 runtime 失败

而有了：

- `deployment_transitions`

之后，
你就能明确知道：

- 这次发布到底走到了哪一步

这对后面做：

- 重试
- 回滚
- 故障定位

都很关键。

## 这一章实际暴露了哪些 API

### `POST /api/v1/apps`

创建应用。

这一章里，
`replicas`
虽然保留在模型里，
但当前实现仍然限制：

- `replicas = 1`

原因很直接：

- `04`
  - 的 scheduler 还只支持单副本

### `GET /api/v1/apps`

返回应用列表。

当前返回里不只包含：

- `App`

还会带上：

- 当前 `Deployment`
- 当前 placement

这样前端 `Apps` 页才能直接看到：

- 当前发布状态
- 当前分配到了哪台节点

### `GET /api/v1/apps/{appID}`

返回单个应用详情。

这里会把下面这些内容一起带回来：

- `app`
- `currentDeployment`
- `currentPlacement`
- `releases`
- `deploymentTransitions`

也就是说，
这一条接口基本就是：

- 一个应用当前发布状态的聚合视图

### `POST /api/v1/apps/{appID}/releases`

这是这一章最关键的动作接口。

它的语义不是：

- “只是插一条 release 记录”

而是：

1. 创建 `Release`
2. 创建 `Deployment`
3. 推进到：
   - `pending`
   - `scheduling`
   - `assigned`
   - 或 `failed`
4. 成功时创建 `PlacementDecision`
5. 更新 `App.currentReleaseID`
6. 更新 `App.status`

这里要特别注意：

- 即使调度失败
  - 这次 `Release`
    - 仍然已经创建成功

所以这种情况不应该返回：

- HTTP 500

它更像：

- 发布请求是有效的
- 但这次发布流程推进失败了

### `GET /api/v1/apps/{appID}/releases`

返回某个应用的所有 release 历史。

### `GET /api/v1/platform/deployments`

这是平台侧接口。

它的价值在于：

- 从平台管理员视角看最近 deployment
- 看它对应哪个项目、哪个应用、哪个版本
- 看有没有已经分配到具体节点

## 当前 release 提交时到底发生了什么

如果用一句话概括当前代码里的真实流程，
那就是：

```text
用户提交 release
  ->
控制面创建 release
  ->
控制面创建 deployment(pending)
  ->
控制面推进到 scheduling
  ->
调 scheduler
  ->
有节点
    -> placement decision
    -> deployment assigned
    -> app deploying
  ->
没节点
    -> deployment failed
    -> app failed
```

## 这一章里的“重试 / 回滚基础”到底指什么

roadmap 里说这一章会碰到：

- 重试
- 回滚基础

这里要注意：

- 当前实现还没有单独暴露：
  - `retry deployment`
  - `rollback release`
  - 这样的独立动作 API

但“基础”已经开始具备了。

具体体现在三点：

### 1. Release 是不可变的

这意味着：

- 每次提交新版本
  - 都会留下独立记录

后面做回滚时，
你就不是“把旧记录改回来”，
而是：

- 重新基于某个旧 release 再发起一次推进

### 2. Deployment 是一次独立尝试

这意味着：

- 同一个 `App`
  - 可以有多次 `Deployment`

也就是说，
后面做重试时，
更自然的模型不是：

- 原地改一条 deployment

而是：

- 发起一条新的推进尝试

### 3. 状态转移过程已经被记录

这意味着后面无论是：

- retry
- rollback
- failure analysis

都不再只是猜，
而是可以沿着：

- `deployment_transitions`

看清楚这次尝试到底卡在哪一步。

## 这一章前端页面为什么现在就值得做

到了 `05`
时，
前端 `Apps` 页终于不再是占位了。

现在这个页面已经能做三件很关键的事：

1. 创建 `App`
2. 给某个 `App` 提交 `Release`
3. 直接看到：
   - 当前 deployment
   - release history
   - deployment transitions

这样做的意义很大，
因为你现在看到的已经不只是：

- 一堆数据库记录

而是一个真正开始有“平台味道”的控制台页面。

## 这一章的最小实验

建议你按下面这个顺序亲手跑一遍。

### 1. 启动数据库

```bash
cd projects/mini-cloud
docker compose -f deploy/compose/docker-compose.yml up -d postgres
```

### 2. 启动 control-plane

```bash
go run ./cmd/control-plane
```

### 3. 准备一个可调度的北京节点

先注册：

```bash
go run ./cmd/agent register \
  --server http://127.0.0.1:8080 \
  --provider aliyun \
  --region cn-beijing \
  --name ecs-cn-bj-01 \
  --private-ip 10.0.0.10 \
  --instance-id i-demo-bj-01 \
  --instance-type ecs.u1-c1m2.large \
  --cpu-milli-capacity 2000 \
  --memory-mi-capacity 4096
```

记下返回的：

- `node id`

然后发一条 `ready` 心跳：

```bash
go run ./cmd/agent heartbeat \
  --server http://127.0.0.1:8080 \
  --node-id <北京节点ID> \
  --cpu-milli-free 1800 \
  --memory-mi-free 3500 \
  --running-containers 1 \
  --status ready
```

### 4. 创建一个 Project

```bash
curl -X POST http://127.0.0.1:8080/api/v1/projects \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "demo-05",
    "displayName": "Demo 05"
  }'
```

记下返回的：

- `project id`

### 5. 创建一个北京应用

```bash
curl -X POST http://127.0.0.1:8080/api/v1/apps \
  -H 'Content-Type: application/json' \
  -d '{
    "projectID": "<project id>",
    "name": "hello-web",
    "displayName": "Hello Web",
    "region": "cn-beijing",
    "replicas": 1,
    "instanceClass": "small",
    "defaultPort": 8080,
    "readinessPath": "/healthz",
    "env": {}
  }'
```

### 6. 给它提交一个 Release

```bash
curl -X POST http://127.0.0.1:8080/api/v1/apps/<app id>/releases \
  -H 'Content-Type: application/json' \
  -d '{
    "version": "2026-04-09.1",
    "image": "ghcr.io/example/hello-web:2026-04-09.1",
    "command": [],
    "args": [],
    "env": {},
    "port": 8080,
    "readinessPath": "/healthz"
  }'
```

现在预期你会看到：

- `deployment.status = assigned`
- `app.status = deploying`

再查询：

```bash
curl http://127.0.0.1:8080/api/v1/apps/<app id>
```

你应该能看到：

- `currentDeployment`
- `currentPlacement`
- `deploymentTransitions`

其中成功链大致会是：

- `<nil> -> pending`
- `pending -> scheduling`
- `scheduling -> assigned`

### 7. 再做一个失败分支

创建一个：

- `cn-hangzhou`

的应用，
但当前节点池里仍然只有：

- `cn-beijing`

节点。

然后再提交 release。

这次你预期会看到：

- `deployment.status = failed`
- `app.status = failed`
- `statusReason = no nodes matched requested region cn-hangzhou`

这会帮你很直观地看清楚：

- 同一套主链里
- 成功分支和失败分支分别怎么落地

### 8. 清理实验现场

```bash
docker compose -f deploy/compose/docker-compose.yml down -v
```

## 当前实现的边界

这一章虽然已经很关键，
但还是要把边界讲清楚。

当前 `05`
还没有做这些事：

- runtime 真正执行
- 容器拉起
- 健康检查成功后推进到 `running`
- 多副本调度
- 独立 retry API
- 独立 rollback API

所以你现在看到的，
是一个已经非常有价值的控制面中段：

- 发布请求已经进来
- 状态机已经开始工作
- 调度已经接入真实主链

但最后的“节点执行面”，
还要等 `06`
接上。

## 这一章完成后应该达到什么状态

如果 `05`
按当前这个实现收口，
那么这一章结束后，
项目应该至少达到这些状态：

- `App / Release / Deployment` 已经真正成为活的资源对象
- 提交 `Release` 会自动触发一次 `Deployment`
- `Deployment` 状态机会真实推进
- 成功分支会落到：
  - `assigned`
- 失败分支会落到：
  - `failed`
- `deployment_transitions` 可以把过程明确记下来
- `04`
  - 的调度器已经真正接入业务主链
- `Apps` 页面已经能展示应用和发布过程

## 这一章的结论

`05`
本质上回答的是：

- 用户提交一个新版本之后
- 控制面到底做了什么

到这一步为止，
`mini-cloud`
已经不再只是：

- 一个有数据库和几个资源表的骨架

而是已经开始具备：

- 真正的平台发布心智

后面 `06`
要做的，
就不是重新发明这条链，
而是把：

- `assigned`

之后的执行面真正补上。

## 本章检查点

- commit: `e7d96767373bbf2bbc92b67307c3123d61c1639d`
- 状态：
  - `App / Release / Deployment` 资源、状态机、发布主链和真实 `Apps` 页面已经落地
