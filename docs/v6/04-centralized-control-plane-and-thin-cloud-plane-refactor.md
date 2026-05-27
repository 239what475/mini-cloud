# 04 Centralized Control Plane And Thin Cloud Plane Refactor

`v6/01`
先把平台行为
收成了更像 controller /
reconcile
的模型。

`v6/02`
再把：

- `1 service -> N cell intents`

这层服务拓扑收了出来。

`v6/03`
又把：

- `runtime node pool`
- `minReady / maxReady`
- headroom

收成了正式的供给策略对象。

但做到这里，
平台还有一个更根上的问题没有解决：

- 控制逻辑仍然是拆开的

具体说，
当前平台里仍然同时存在两套控制倾向：

- `control-plane`
  已经在做全局对象、
  placement、
  front door
  和 controller
- `cloud-plane`
  仍然自己带着：
  - 本地 scheduler
  - 本地 deploy
    决策
  - 本地 rollout
    推进
  - 本地“放不下就扩一台再试”

这会直接带来三个问题：

1. 决策来源不唯一
   - placement
     和 rollout
     都可能在上下两层同时推进
2. 状态语义不干净
   - 上层看到的是一套状态
   - 下层实际推进的是另一套状态
3. 后面的章节没有稳固地基
   - 渐进发布
   - front door
     切换
   - 中央容量治理
   如果继续建立在双脑上，
   后面还得整体重做一次

所以
`v6/04`
不再直接往后做新能力，
而是先做一次专门的设计 cut-over：

- 把所有控制决策
  正式集中回
  `control-plane`
- 把 `cloud-plane`
  收成 plane
  内执行面

## 这一章的核心决定

这一章要把三层职责明确收成：

- `control-plane`
  - 唯一控制决策面
- `cloud-plane`
  - 单云执行面
  - inventory /
    observed state
    汇聚面
  - provider
    adapter
- `agent`
  - 节点侧执行进程

这意味着从这一章开始，
下面这条原则要被正式说死：

- `control-plane`
  决定
- `cloud-plane`
  执行
- `agent`
  落地

只要还存在：

- `cloud-plane`
  也能自己决定 placement
  或 rollout

那平台后面就还会继续保留双脑。

## 这一章要解决什么

这一章只解决：

- 控制决策的唯一写者是谁
- `control-plane`
  和 `cloud-plane`
  之间的契约怎么收
- node inventory
  怎样稳定地上收给
  `control-plane`
- assignment /
  capacity action
  怎样带版本栅栏
- `cloud-plane`
  在新模型下还保留什么、
  删掉什么

这一章不直接解决：

- 渐进发布策略本身
- front door
  健康路由本身
- `control-plane`
  高可用
- 第三家 provider
- 新 workload
  类型

这些会在后面的章节里继续做。

## 为什么这一章必须先做

如果跳过这一章，
继续往后做：

- `05`
  渐进发布
- `06`
  front door
  切换

那后面几章都会默认：

- 有些控制逻辑在
  `control-plane`
- 有些控制逻辑在
  `cloud-plane`

这样最后一定会遇到：

- rollout
  到底谁推进
- assignment
  到底谁写
- scale-out
  到底谁决定
- front door
  到底信谁

这些边界打架的问题。

所以这里不能再小修小补，
而是要先把“唯一控制器”
这个前提补齐。

## 新的三层职责

### 1. `control-plane`

从这一章开始，
`control-plane`
要明确成为唯一的：

- 声明源
- controller
- scheduler
- rollout controller
- capacity controller

它负责：

- 项目、服务、
  `cell`
  等全局对象
- 选哪个 `plane`
- 选哪个 `node`
- 是否要扩
  runtime node
- rollout
  该推进、暂停、
  提升还是放弃
- front door
  该把流量送到哪里

它不直接做：

- 调 provider API
- 跟 node 上的容器运行时通信
- 直接给 `agent`
  发任务

### 2. `cloud-plane`

这一章之后，
`cloud-plane`
不再是“第二个产品控制面”，
而是：

- plane
  内执行面

它保留三类职责：

1. 接 `agent`
   - 注册
   - 心跳
   - work
     分发
   - 执行结果回报
2. 接 `control-plane`
   - 上报 node inventory
   - 上报 execution /
     provider
     observed state
   - 接收 assignment /
     capacity action /
     gateway snapshot
3. 调本地 actuator
   - provider API
   - gateway engine
   - work queue

它不再负责：

- 本地 scheduler
- 本地 rollout
  决策
- 本地 promote /
  rollback
- 本地“调度失败就自己扩一台再试”

### 3. `agent`

`agent`
继续只负责节点侧执行：

- 上报节点资源和状态
- 领取工作
- 启停容器
- 回报执行结果

它不需要知道：

- 全局 `service`
  拓扑
- `plane`
  选择
- front door
  路由
- 发布策略

## 这一章要删掉哪些旧边界

这一章要刻意删掉下面这些“看起来方便、
但会导致双脑”的设计：

1. `cloud-plane`
   本地 scheduler
2. `cloud-plane`
   本地 deployservice
   里的最终 placement
   决策
3. `cloud-plane`
   execution report
   路径里推进
   rollout /
   promote
4. `cloud-plane`
   本地 safe cutover
   的最终 node
   选择
5. `cloud-plane`
   本地 decide-then-scale
   的容量动作决策

这些逻辑后面都应上移到：

- `control-plane`

## 新的数据流怎么走

这一章之后，
最小可行的数据流应该收成下面这样：

1. `cloud-plane`
   汇总本地：
   - nodes
   - executions
   - runtime node
     lifecycle
   - provider
     action status
2. `control-plane`
   周期性同步这些快照，
   形成全局 inventory
   读模型
3. `control-plane`
   的 controller
   基于：
   - service spec
   - cell
     topology
   - runtime pool
     policy
   - 全局 node inventory
   做 placement /
   rollout /
   capacity
   决策
4. `control-plane`
   生成：
   - assignment plan
   - capacity action
   - gateway snapshot
5. `cloud-plane`
   只负责：
   - 校验版本
   - 落本地执行记录
   - 分发给 `agent`
   - 调 provider /
     gateway
     actuator
6. `agent`
   执行并回报结果
7. `cloud-plane`
   再把 observed
   state
   回报给
   `control-plane`

也就是说：

- 决策上收
- 执行下沉

## 这一章最关键的版本栅栏

如果不加版本栅栏，
集中式控制会立刻遇到两个问题：

- `control-plane`
  用旧 inventory
  做了新决策
- `cloud-plane`
  收到过期 plan
  后仍然把它执行了

所以这一章至少要补四个版本概念：

### 1. `syncVersion`

每次
`cloud-plane -> control-plane`
同步 inventory
时，
都要带一个单调递增的：

- `syncVersion`

后续 plan
要明确记录：

- 自己是基于哪个
  `syncVersion`
  生成的

### 2. `nodeEpoch`

节点实例被替换、
重建或重新注册后，
原来的：

- `nodeID`

可能还是那个逻辑节点，
但语义已经变了。

所以 assignment
不能只绑定：

- `nodeID`

还要绑定：

- `nodeEpoch`

如果 epoch
不一致，
`cloud-plane`
必须拒绝这个 plan，
而不是本地重排。

### 3. `planVersion`

assignment
和 rollout
计划，
必须带：

- `planVersion`

这样：

- 重试下发
- 晚到旧命令
- 幂等 apply

才有明确边界。

### 4. `actionVersion`

容量动作也一样，
例如：

- provision runtime node
- reclaim runtime node
- drain node

这些动作也必须带：

- `actionVersion`

这样旧动作就不能覆盖新动作。

## `cloud-plane`
收到过期计划时怎么办

新模型里，
`cloud-plane`
可以拒绝计划，
但不能重写计划。

也就是说，
如果它发现：

- 本地 inventory
  已经比
  `basedOnSyncVersion`
  更新
- `nodeEpoch`
  不匹配
- 当前已经应用了更高
  `planVersion`

那它应当返回：

- stale /
  rejected /
  superseded

这类 observed
结果，
让：

- `control-plane`

去重新 reconcile。

这里最重要的边界是：

- `cloud-plane`
  可以说
  “这份 plan
  不能执行了”
- 但不能自己说
  “那我给你换一台 node”

## 这一章和 `03`
是什么关系

`03`
里收出来的：

- `runtime node pool`
- `minReady / maxReady`
- headroom

到了这一章之后，
语义要进一步明确成：

- 它们属于
  `control-plane`
  的中央容量治理输入

而不是：

- `cloud-plane`
  本地临时解释的 admission
  规则

也就是说：

- `03`
  提供的是 capacity policy
- `04`
  提供的是 centralized controller
  边界

后面真正的：

- rollout
- front door
- 容量动作

都会建立在这两层前提上。

## 这一章和 `05`
是什么关系

这一章做完以后，
后面的
`05`
才能自然建立在：

- 单一 rollout
  决策者
- 单一 assignment
  写入者
- 单一 capacity
  action
  决策者

这三个前提上。

换句话说：

- `04`
  先把“谁来决定”
  做干净
- `05`
  再把“决定之后怎样渐进发布”
  做干净

## 这一章预计会落到哪些代码层

这一章如果开做，
预计会主要落在：

- `internal/controlplane/`
  - placement
  - fleetservicecontroller
  - frontdoorcontroller
  - 以及新的全局 inventory /
    plan
    协调逻辑
- `internal/cloudplane/api/`
  - plane
    southbound
    契约
  - node agent
    southbound
    不变或小调
- `internal/cloudplane/store/`
  - inventory /
    execution /
    provider
    observed state
  - plan
    apply
    状态
- `internal/cloudplane/provider/`
  - 保留 actuator
  - 去掉最终决策语义
- `internal/cloudplane/gateway/`
  - 保留 apply
    snapshot
    能力

## 这一章刻意不做什么

### 1. 不做 `control-plane`
高可用

这一章只先把控制逻辑收干净，
不在这一章直接补：

- 多副本
- 主备
- failover

这些留给后面的正式部署章节。

### 2. 不做新的 provider

这一章不引入第三家 provider，
也不趁机做新的 provider
抽象层扩展。

### 3. 不做直接
`control-plane -> agent`
通信

这一章仍然保留：

- `control-plane`
  不直接碰 `agent`

所有节点侧执行仍然通过：

- `cloud-plane`

中转和落地。

## 这一章做完后的平台能力

如果这一章完成，
平台会第一次真正具备下面这些能力：

1. placement /
   rollout /
   capacity action
   都有唯一控制决策面
2. `cloud-plane`
   不再是第二个小控制面，
   而是正式的执行面
3. node inventory
   能稳定进入
   `control-plane`
   的全局读模型
4. assignment /
   capacity action
   有最小版本栅栏，
   可以拒绝过期计划
5. 后面的发布、
   切流和容量治理
   终于可以建立在单一控制语义上

## 检查点

- 待本章提交时回填 commit hash。
