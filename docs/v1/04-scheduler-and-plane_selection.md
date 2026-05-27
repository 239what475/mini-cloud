# 04 Scheduler And Placement

这一章开始讨论：

- 平台已经知道自己有哪些节点了
- 那接下来，应该把一个工作负载放到哪台节点上

如果说 `03` 解决的是：

- 节点怎么接入平台
- 节点怎么持续上报状态

那么 `04` 解决的就是：

- 在这些节点里，怎么选出一个合适的目标节点

这里可以先记一句最重要的话：

- `register`
  - 解决“你是谁”
- `heartbeat`
  - 解决“你现在怎么样”
- `schedule`
  - 解决“那我该把工作放到谁那里”

## 这一章实际会落什么代码

这一章不只是继续设计，
而是会真正把下面这些内容落下来：

- 一个独立的 `scheduler` 包
- `PlacementRequest`
- `PlacementResult`
- `placement_decisions` 表
- `POST /api/v1/platform/placements/preview`
- `GET /api/v1/platform/placements`
- 前端里的 `Placements` 页面

也就是说，
这一章结束后，
`mini-cloud`
已经能：

- 接收一次放置预览请求
- 在当前节点清单里做过滤和打分
- 返回“为什么选它”或者“为什么没选出来”
- 把成功的放置决策持久化下来

## 为什么现在就讲调度

很多人第一眼看到 roadmap 会觉得顺序有点怪：

- `04`
  - 先讲调度
- `05`
  - 才讲 `Release` 和 `Deployment` 状态机

这个顺序之所以还能成立，
是因为这一章要先抽出来的，
不是完整“平台调度闭环”，
而是：

- 一个最小可解释的放置决策核心

也就是说，
这一章先解决：

- 给我一组节点
- 给我一个调度请求
- 告诉我应该选哪个节点

而：

- 谁来触发调度
- 调度成功后怎么推进状态
- 调度失败后怎么重试

这些事情，
放到 `05`
再和：

- `Release`
- `Deployment`

一起接起来会更顺。

所以这一章更像是在做：

- scheduler core

而不是完整：

- release orchestrator

## 这一章想解决什么

这章的目标先收敛成三件事：

1. 定义最小调度输入
2. 定义最小候选节点过滤规则
3. 定义最小放置决策输出

也就是说，
这一章结束后，
平台至少应该能回答：

- 哪些节点根本不可能被选中
- 哪些节点虽然能选，但更差
- 最后选中的是哪一个
- 这次决策为什么这么选

## 这一章不做什么

为了不把主题拉散，
这一章先明确不做这些：

- 多 provider 调度
- 多副本复杂分布策略
- 亲和性 / 反亲和性
- 污点 / 容忍
- 优先级 / 抢占
- 故障后自动重调度
- 调度后的容器真实拉起

这里一定要注意：

- 这一章只决定“选谁”
- 还不负责“真的跑起来”

## 04 的边界

我建议 `04`
只落下面这些东西：

### 核心逻辑

- 一个独立的 `scheduler` 包
- 一个最小 `PlacementRequest`
- 一个最小 `PlacementDecision`
- 一套可解释的过滤和打分规则

### 持久化

- `placement_decisions` 表

### 平台视角

- 能看到一次调度决策的结果
- 能看到为什么某些节点被排除

### 验证方式

- 以纯 Go 测试为主
- 用假节点和假请求验证调度行为

## 为什么这一章先不做“真实运行”

因为一旦把：

- 调度
- 容器拉起
- 健康检查
- 状态回写

全都混在一起，
读者会很难分辨：

- 是调度规则错了
- 还是运行时执行错了

所以这一章的目标很单纯：

- 把“选择节点”这件事单独讲透

## 调度的输入应该是什么

这一章里，
我建议先不要直接把完整 `Deployment`
整坨塞给调度器。

更好的做法是先抽一个最小输入对象：

- `PlacementRequest`

它先只保留调度真正需要的信息。

## 最小 `PlacementRequest`

建议先有这些字段：

- `provider`
- `region`
- `cpuMilliRequest`
- `memoryMiRequest`
- `replicas`

这里可以先这样理解：

- `provider`
  - 这是平台范围约束
- `region`
  - 这是硬约束
- `cpuMilliRequest`
  - 这是资源需求
- `memoryMiRequest`
  - 这是资源需求
- `replicas`
  - 暂时先存在模型里
  - 但 `04`
    - 可以先只把单副本放置做清楚

也就是说，
虽然模型里保留了：

- `replicas`

但第一版实现时，
这章可以先按：

- `replicas = 1`

的最小场景来讲。

当前实现里，
如果：

- `replicas > 1`

会直接返回输入错误。

这样后面扩到多副本时，
心智会顺很多。

## 调度器可见的节点信息是什么

调度器不需要知道节点上的所有细节，
它只需要知道“对放置决策有用的信息”。

这一章里，
我建议调度器看到的节点摘要先只包含：

- `id`
- `provider`
- `region`
- `status`
- `schedulable`
- `cpuMilliCapacity`
- `memoryMiCapacity`
- `cpuMilliAllocated`
- `memoryMiAllocated`

这里有个关键点：

调度器最好直接使用：

- `nodes`
  - 表里的最新摘要

而不是每次临时扫描：

- `node_heartbeats`

来现算。

原因是：

- 调度路径要尽量短
- `nodes`
  - 本来就已经是最新摘要
- `node_heartbeats`
  - 更适合做历史记录和后续分析

## 调度过滤规则先怎么定

我建议 `04`
先只做最小过滤链：

1. `provider` 必须符合当前平台范围
2. `status` 必须是 `ready`
3. `schedulable` 必须是 `true`
4. `region` 必须匹配
5. 资源必须放得下

### 1. provider

虽然 `v1`
现在先只做阿里云，
但我建议仍然保留这一层判断。

原因是：

- 资源模型里已经有 `provider`
- 调度器从一开始就应该体现：
  - “只在当前支持的 provider 范围里选”

在 `v1`
里，
这一步大多只是把：

- 非 `aliyun`

节点排除掉。

### 2. 节点状态

节点必须是：

- `ready`

才进入候选集。

下面这些状态先直接排除：

- `registering`
- `not_ready`
- `draining`
- `offline`

这里的语义很直接：

- 只有已经接入并且最近状态正常的节点
  - 才能接新工作

### 3. schedulable

即使节点是：

- `ready`

只要：

- `schedulable = false`

也不能放进去。

这个字段的意义是：

- 节点当前是不是允许继续接新工作

后面做维护模式时，
这个字段会更有价值。

### 4. region

这是本章最重要的硬约束之一。

如果一个应用请求的是：

- `cn-beijing`

那调度器就不应该把它放到：

- `cn-hangzhou`

这里先不做什么“跨地域回退”。

规则先保持得非常明确：

- region 不匹配
  - 直接淘汰

### 5. 资源容量

这是本章第二个最重要的硬约束。

资源够不够，
建议先按下面的方式判断：

- `cpuMilliCapacity - cpuMilliAllocated >= cpuMilliRequest`
- `memoryMiCapacity - memoryMiAllocated >= memoryMiRequest`

也就是说，
只要剩余 CPU 或剩余内存有一个不够，
就直接淘汰。

## 调度打分先怎么定

过滤之后，
可能还会剩下多个候选节点。

这时就需要一个最小打分规则。

这一章里，
我建议先不要上太复杂的策略，
而是用一个非常直观的规则：

- 优先选择剩余资源更多的节点

原因是：

- 好解释
- 好验证
- 不会过早引入复杂的 packing / spreading 争论

## 一个足够简单的打分方式

可以先按下面的顺序比较候选节点：

1. 放置后剩余 CPU 更多者优先
2. 放置后剩余内存更多者优先
3. 如果还相同
   - 按 `node id` 字典序稳定排序

这里最重要的不是这个规则有多“聪明”，
而是：

- 它稳定
- 它可解释
- 它能被测试直接验证

## 为什么这里不急着做 bin packing

你当然也可以设计成：

- 谁放进去以后剩余资源更少
  - 就优先选谁

也就是更偏：

- packing

的策略。

但我建议 `04`
先不要把讨论拉到这里。

原因是：

- 这章的目标是先把调度主链打通
- 不是先讨论高级资源利用率策略

更合适的做法是：

- 先有一套简单稳定规则
- 后面如果真的有必要
  - 再单独演进评分函数

## 调度输出应该是什么

如果调度成功，
我建议输出一个：

- `PlacementDecision`

它的作用不是“再发明一个复杂对象”，
而是把“这次调度到底怎么决定的”显式留下来。

## 最小 `PlacementDecision`

建议先有这些字段：

- `deploymentID`
- `nodeID`
- `region`
- `score`
- `reason`

这里可以先这样理解：

- `nodeID`
  - 最终选中的节点
- `score`
  - 这次打分结果
- `reason`
  - 给人看的解释文本

而：

- `createdAt`

更适合放在持久化后的：

- `StoredDecision`

里。

例如：

- `matched region cn-beijing and had the highest remaining cpu/memory after placement`

这样做的好处是：

- 前端以后可以展示“为什么选了它”
- 调试时也不容易只看到一个神秘结果

## 如果调度失败怎么办

这章里，
失败结果也应该被明确表达出来。

我建议先区分两类失败：

### 没有候选节点

例如：

- 没有 `ready` 节点
- 没有 region 匹配节点

### 有候选节点，但容量不够

例如：

- region 匹配
- 状态也正常
- 但所有节点都放不下

这两类失败在用户体验上是完全不同的。

所以我建议调度器返回结果时，
不要只给一个模糊的：

- `no node available`

而要带上更具体的失败原因。

## 失败原因我建议怎么表达

这一章里，
可以先用一个很简单的方式：

- 返回：
  - `PlacementResult`

里面包含：

- `decision`
- `failureReason`
- `filteredCounts`

这里的：

- `filteredCounts`

可以表达：

- 有多少节点被 `status` 淘汰
- 有多少节点被 `region` 淘汰
- 有多少节点被 `capacity` 淘汰

这会让后面的平台页和调试日志更有价值。

## 这一章的持久化范围

我建议 `04`
持久化只新增一张表：

- `placement_decisions`

先不要在这章里把：

- 调度事件流
- 重试历史
- 失败日志

全铺开。

原因是：

- 这些更适合和 `Deployment` 状态机一起讨论

这章先只落：

- “成功选中了谁”

就够了。

当前表里先保存这些最小字段：

- `deployment_id`
  - 先允许为空
  - 因为 `05`
    - 才会把它真正接到 `Deployment`
- `node_id`
- `region`
- `cpu_milli_request`
- `memory_mi_request`
- `replicas`
- `score`
- `reason`
- `created_at`

## 那失败决策要不要落库

这一点我建议先保守一点：

- 成功的 `PlacementDecision`
  - 落库
- 失败原因
  - 先通过返回值和日志表达

这样做的原因是：

- `04`
  - 还没有完整 `Deployment` 状态机
- 如果现在就急着设计失败落库模型
  - 很容易在 `05`
    - 又推翻重来

## 这一章实际暴露的接口

这一章目前有两个和 placement 直接相关的接口：

### `POST /api/v1/platform/placements/preview`

这是平台侧的“放置预览”接口。

它的语义是：

- 给一组最小调度输入
- 让 control-plane 用当前 `nodes` 摘要做一次调度
- 返回：
  - 成功决策
  - 或失败原因
  - 以及过滤统计

这里有个实现细节很重要：

- 如果只是“逻辑上没选出节点”
  - 这不算 HTTP 错误
  - 仍然返回 `200`
- 只有：
  - JSON 非法
  - 输入字段不合法
  - 数据库异常
  - 才返回真正的 HTTP 错误码

也就是说：

- “没有候选节点”
  - 是一次有效查询结果
- 不是服务器故障

### `GET /api/v1/platform/placements`

这个接口返回最近成功的放置决策历史。

这里故意只展示：

- 成功决策

因为这一章还没有完整：

- `Deployment`
- 重试
- 失败事件历史

所以先把模型收在最小范围里。

## 平台页面为什么要单独做 Placements

这一章新增的 `Placements` 页面，
不是“应用发布页”，
而是：

- 平台管理员视角的调度观察页

它现在主要做三件事：

1. 填最小 placement 请求
2. 看本次 preview 的过滤统计
3. 看最近成功的 placement history

这里刻意不把它和：

- `App`
- `Release`
- `Deployment`

混在一起，
因为 `04`
还在讲：

- scheduler core

而不是完整发布链路。

## 04 最适合怎么测试

这一章我建议主要用：

- 纯 Go 单元测试

而不是：

- Docker
- 真节点
- 真容器

原因很简单：

- 调度核心本来就应该是纯逻辑
- 最好让它不依赖运行时环境

## 测试场景我建议至少覆盖这些

### 场景 1：只有一个可用节点

预期：

- 直接选中它

### 场景 2：region 不匹配

预期：

- 调度失败
- 失败原因明确指出：
  - 没有 region 匹配节点

### 场景 3：节点不是 `ready`

预期：

- 该节点被过滤掉

### 场景 4：容量不足

预期：

- 调度失败
- 失败原因指向：
  - capacity

### 场景 5：多个节点都可用

预期：

- 按固定评分规则选出同一个节点
- 结果稳定可重复

### 场景 6：`schedulable = false`

预期：

- 即使节点是 `ready`
  - 也不能入选

当前实现里，
`internal/scheduler/scheduler_test.go`
已经把这些最小场景覆盖进去了。

## 这一章的最小实验

如果你想在本地亲手走一遍，
这一章最小实验可以按下面这样做。

### 1. 启动数据库

```bash
cd projects/mini-cloud
docker compose -f deploy/compose/docker-compose.yml up -d postgres
```

### 2. 启动 control-plane

```bash
go run ./cmd/control-plane
```

### 3. 注册三个节点

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

go run ./cmd/agent register \
  --server http://127.0.0.1:8080 \
  --provider aliyun \
  --region cn-hangzhou \
  --name ecs-cn-hz-01 \
  --private-ip 10.0.1.10 \
  --instance-id i-demo-hz-01 \
  --instance-type ecs.u1-c1m2.large \
  --cpu-milli-capacity 2000 \
  --memory-mi-capacity 4096

go run ./cmd/agent register \
  --server http://127.0.0.1:8080 \
  --provider aliyun \
  --region cn-beijing \
  --name ecs-cn-bj-02 \
  --private-ip 10.0.0.11 \
  --instance-id i-demo-bj-02 \
  --instance-type ecs.u1-c1m2.large \
  --cpu-milli-capacity 2000 \
  --memory-mi-capacity 4096
```

每次 `register`
都会返回一段 JSON，
记下里面的：

- `id`

下一步发送 `heartbeat`
时要用到它。

### 4. 给它们发送不同状态的心跳

把第一台北京节点变成：

- `ready`
- 余量充足

把杭州节点变成：

- `ready`
- 但 region 不匹配

把第二台北京节点变成：

- `ready`
- 但容量不够

例如：

```bash
go run ./cmd/agent heartbeat --server http://127.0.0.1:8080 --node-id <北京节点1> --cpu-milli-free 1400 --memory-mi-free 3000 --running-containers 2 --status ready
go run ./cmd/agent heartbeat --server http://127.0.0.1:8080 --node-id <杭州节点> --cpu-milli-free 1500 --memory-mi-free 3200 --running-containers 1 --status ready
go run ./cmd/agent heartbeat --server http://127.0.0.1:8080 --node-id <北京节点2> --cpu-milli-free 300 --memory-mi-free 400 --running-containers 5 --status ready
```

### 5. 发送一次 placement preview

```bash
curl -X POST http://127.0.0.1:8080/api/v1/platform/placements/preview \
  -H 'Content-Type: application/json' \
  -d '{
    "provider": "aliyun",
    "region": "cn-beijing",
    "cpuMilliRequest": 500,
    "memoryMiRequest": 512,
    "replicas": 1
  }'
```

预期你会看到：

- `decision.nodeID`
  - 选中北京那台余量更大的节点
- `filteredCounts.region = 1`
  - 杭州节点被 region 过滤
- `filteredCounts.capacity = 1`
  - 第二台北京节点被容量过滤

### 6. 查看最近决策历史

```bash
curl http://127.0.0.1:8080/api/v1/platform/placements
```

或者直接查数据库：

```sql
SELECT id, node_id, region, cpu_milli_request, memory_mi_request, replicas, score
FROM placement_decisions
ORDER BY created_at DESC;
```

### 7. 清理实验现场

```bash
docker compose -f deploy/compose/docker-compose.yml down -v
```

## 04 和后续章节怎么衔接

这一章结束后，
平台应该已经具备一个独立的：

- placement core

接下来：

### `05`

把：

- `Release`
- `Deployment`
- 调度触发
- 状态推进

真正串起来。

### `06`

再把：

- 调度结果
- agent 执行
- 容器真实拉起

串起来。

所以更准确地说：

- `04`
  - 解决“选谁”
- `05`
  - 解决“什么时候选、选完怎么推进状态”
- `06`
  - 解决“选完以后怎么真的执行”

## 这一章完成后应该达到什么状态

如果 `04`
按这个设计收口，
那么这一章结束后，
项目应该至少达到这些状态：

- 平台已经有一个独立调度核心
- 调度输入和输出模型已经清楚
- 节点过滤规则已经清楚
- 最小评分规则已经清楚
- 成功决策可以被显式记录
- 下一章可以开始把调度接进：
  - `Release`
  - `Deployment`
  - 状态机

## 这一章的结论

`04`
本质上是在回答一个很朴素的问题：

- 节点已经接进来了
- 那到底该选哪一个

它先不追求“聪明”，
而是先追求：

- 规则稳定
- 行为可解释
- 输出可测试

这一步做扎实以后，
后面无论是接：

- 发布状态机
- runtime 执行
- 故障重调度

都会清楚很多。

## 本章检查点

- commit: `2b8e41e445a07b3d1677f320e39254cf0ed09626`
- 状态：
  - `scheduler` 核心、placement API、placement history 和 `Placements` 页面已经落地
