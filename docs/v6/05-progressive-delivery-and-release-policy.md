# 05 Progressive Delivery And Release Policy

`v6/04`
把控制逻辑正式收回到了
`control-plane`，
这一章继续把发布语义也一起收紧。

但这里有一个必须先说清楚的边界：

当前平台还不具备一个真正的
`rolling / canary`
数据面。

原因很直接：

- 每个 `cell`
  在 `cloud-plane`
  内部仍然是“一个稳定版本 + 一个候选版本”的发布模型
- 入口流量还没有细到“同一个 `cell` 内按百分比切给两个 revision”
- 平台也没有
  `maxSurge / maxUnavailable`
  这一类副本级流量语义

所以，
这一章不再假装实现：

- `rolling`
- `canary`
- `maxSurge / maxUnavailable`

而是把能力诚实地收成：

- `cell`
  级
  `candidate cutover`
- 明确的
  `release policy`
- 明确的
  `rollout status`
- 明确的
  `pause / promote / abort`

也就是说，
这章真正完成的是：

- 一个能被真实解释、
  也能被当前代码真实执行的发布模型

## 这一章的目标

这一章只解决一件事：

- 一个 `service cell`
  怎样从稳定版本进入候选版本，
  再由 `control-plane`
  决定暂停、提升或放弃

这一章明确不解决：

- 多个 `cell`
  之间的切流
- `front door`
  怎么选 `cell`
- 同一个 `cell`
  内的百分比流量分配
- 真正的
  `rolling / canary`
  发布

这些都不是这章的目标。

## 这一章收敛后的发布模型

### 1. 发布边界是 `service cell`

平台的发布对象不是全局
`service`，
而是每个具体的
`cell`。

所以：

- 主 `cell`
  可以单独发布
- 备 `cell`
  也可以单独发布
- 每个 `cell`
  有自己的：
  - `release policy`
  - `rollout status`
  - placement
  - replica assignment

这样做的好处是：

- 跨云主备仍然是一个全局服务
- 但每个 `cell`
  的发布观察和控制是独立的

### 2. `control-plane` 是对外发布控制的唯一入口

这一章之后，
发布动作的职责边界是明确的：

- `control-plane`
  负责：
  - 选择 `plane`
  - 选择节点 assignment
  - 下发 apply plan
  - 观察远端 rollout
  - 决定是否自动 promotion
  - 响应人工
    `pause / promote / abort`
- `cloud-plane`
  负责：
  - 执行 revision
    变更
  - 维护 stable /
    candidate
    状态
  - 回报 rollout
    观测值

为了把这个边界写死，
这章还做了一个重要收口：

- `cloud-plane`
  的 northbound 服务写接口
  不再允许直接做 rollout
  写操作
- 如果外部直接打到
  `cloud-plane`
  northbound：
  - `pause`
  - `promote`
  - `abort`
  会直接返回
  `FailedPrecondition`

也就是说，
对外部调用者来说：

- rollout
  的控制入口只允许走 fleet
  `control-plane`

而在内部执行面上：

- `cloud-plane`
  仍然维护 stable /
  candidate
  的本地执行状态机
- `control-plane`
  负责观察它、
  驱动它，
  但不再允许旁路 northbound
  直接改它

### 3. release policy 先只保留当前真实支持的最小集合

这章把
`cell`
的发布策略正式收成：

- `strategy`
  - 当前只允许：
    - `candidate`
- `promotionPolicy`
  - `automatic`
  - `manual`

这里刻意没有保留更多字段，
因为当前平台并没有真实支持它们。

这比留下大量“看起来先进，
但其实没有底层语义”的字段更干净。

当前的解释非常直接：

- `candidate`
  表示：
  - 新版本先以候选版本存在
  - 是否切成稳定版本，
    由后续 promotion
    决定
- `automatic`
  表示：
  - `control-plane`
    在观察到候选副本已经达到可用条件后，
    自动发起 promotion
- `manual`
  表示：
  - `control-plane`
    只同步观察状态
  - operator
    需要显式调用：
    - `pause`
    - `promote`
    - `abort`

### 4. rollout status 正式进入 `cell` 状态面

这一章把原本分散的发布观测，
正式收成
`cell.rollout`。

最小状态包括：

- `phase`
  - `idle`
  - `progressing`
  - `paused`
  - `promoting`
  - `aborting`
  - `failed`
- `message`
- `stableRevisionID`
- `candidateRevisionID`
- stable 副本观察值
  - `desired`
  - `ready`
  - `available`
- candidate 副本观察值
  - `desired`
  - `ready`
  - `available`
- `lastObservedAt`

这样，
`control-plane`
就不再只是知道：

- “某次 update
  已经发出去了”

而是可以明确表达：

- 当前稳定版本是谁
- 当前候选版本是谁
- 候选版本是不是已经达到 promotion
  条件
- 当前是暂停中、
  失败中、
  还是已经回到稳定态

## 这章的实际控制流程

### 1. placement planner 现在返回 replica assignments

这章把 placement
决策继续往下收了一步：

- 不只是决定：
  - 这个 `cell`
    去哪个 `plane`
- 还要决定：
  - 每个 replica
    分配到哪个 `node`

因此：

- `placement.Decision`
  增加了
  `Assignments`
- `fleet_service_cell_assignments`
  正式带上
  `replica_index`

这意味着：

- 发布计划
  和容量占位
  使用的是同一份中央 assignment
  结果

### 2. reconcile 会先写 assignment，再下发 apply plan

`control-plane`
对某个 `cell`
做 reconcile
时，
流程现在是：

1. 选择 `plane`
   和 replica assignments
2. 把 assignments
   持久化到
   `control-plane`
   store
3. 通过 southbound
   `plane` API
   下发 apply plan
4. 回读远端 service
   当前状态
5. 把远端 rollout
   观察结果同步回
   `cell.rollout`

这样以后，
就算后面还要继续补：

- 更复杂的 rollout controller
- 更复杂的 front door
  策略

也都建立在统一的中央状态之上。

### 3. 自动 promotion 只发生在观察面确认之后

这一章没有做成“apply 完就立刻 promote”，
而是保留了 controller
风格的判断顺序：

- 先由 `cloud-plane`
  回报：
  - candidate revision
  - candidate available replicas
- `control-plane`
  在 refresh
  这轮观测状态时，
  根据：
  - `promotionPolicy=automatic`
  - 候选副本已达到
    `candidateAvailableReplicas >= candidateDesiredReplicas`
  再触发 promotion

这个顺序更符合平台 controller
模型：

- 先观测
- 再做控制决策

### 4. 人工动作是 cell 级别的

这一章新增了三个明确动作：

- `pause`
- `promote`
- `abort`

它们都是：

- 作用在单个 `cell`
- 由 fleet `control-plane`
  发起
- 通过 southbound
  `plane` RPC
  落到对应
  `cloud-plane`

这样 operator
可以明确控制：

- 候选版本先暂停观察
- 观察完成后手工 promotion
- 或者直接 abort
  回到稳定版本

## 这一章具体落到哪些代码层

### `internal/controlplane/fleetservice/`

- 增加：
  - `ReleasePolicy`
  - `RolloutStatus`
- 把 `cell`
  的发布语义收成正式字段

### `internal/controlplane/store/`

- `fleet_service_cells`
  增加：
  - `release_policy_json`
  - `rollout_status_json`
- `fleet_service_cell_assignments`
  增加：
  - `replica_index`
- 对应 migration：
  - `00038_add_fleet_cell_release_policy_rollout_and_replica_assignments.sql`

### `internal/controlplane/fleetservicecontroller/`

- reconcile
  负责：
  - 同步远端 rollout
  - 自动 promotion
  - 维护 placement /
    assignment
    与 rollout
    的一致性
- 新增：
  - `PauseCellRollout`
  - `PromoteCellRollout`
  - `AbortCellRollout`

### `internal/controlplane/api/`

- 增加 cell
  级 rollout action
  路由
- 对外暴露新的
  `releasePolicy`
  和 `rollout`
  视图

### `proto/minicloud/cloudplane/v1/plane.proto`

- 补齐 southbound
  `plane` RPC：
  - `PausePlaneServiceRollout`
  - `PromotePlaneServiceRollout`
  - `AbortPlaneServiceRollout`
- 同时把 rollout
  观测字段带回给
  `control-plane`

### `internal/cloudplane/api/northbound/`

- 禁止 northbound
  直接做 rollout
  写操作
- 强制调用者改走 fleet
  `control-plane`

### `internal/cloudplane/servicelifecycle/`

- 把 promotion /
  abort
  等动作从“单次替换”
  收成 stable /
  candidate
  语义
- 让整套动作更接近正式发布状态机

## 这一章明确没有做什么

### 1. 没有做真正的 rolling 发布

当前平台还没有：

- 在同一个 `cell`
  内同时稳定承接两套 revision
  的流量
- `maxSurge / maxUnavailable`
  语义

所以不要把这章理解成：

- 已经实现
  `Deployment`
  级 rolling update

### 2. 没有做 canary 百分比流量

当前也没有：

- 1%
- 5%
- 20%

这类流量切分能力。

所以这章也不是：

- `service mesh`
  风格的 canary

### 3. 没有做跨 cell 的发布波次编排

这章仍然只处理：

- 单个 `cell`
  自己的发布状态

并不做：

- 主 `cell`
  先升
- 备 `cell`
  后升
- 或跨 `cell`
  统一波次推进

那是后面 front door
和更高层发布编排的问题。

## 这一章结束后的平台能力

做到这里，
平台已经具备一个更诚实、
也更清晰的发布面：

- `cell`
  级 candidate release
- 明确的 stable /
  candidate
  观测状态
- 自动或手工 promotion
- 手工 pause /
  abort
- 与 placement /
  assignment
  一致的中央控制语义

这还不是最终形态的生产发布系统，
但已经不是“写一次 update，
希望远端自己做对”的模式了。

它已经是：

- 一个由
  `control-plane`
  统一决策、
  `cloud-plane`
  负责执行、
  并且状态可观察的
  最小可解释发布系统
