# v4/11：维护模式、排空与事故流

这一章开始，
`mini-cloud`
终于把“运维动作”收成正式模型。

前面几章虽然已经有：

- `plane`
  注册与同步
- 全局 `inventory`
- fleet deploy
- placement
- incident
  的最小创建 / 查看 / resolve

但还缺一块很关键的东西：

- 管理员怎么明确告诉系统：
  - 这套 `plane`
    现在先不要接新部署
  - 这套 `plane`
    正在维护
  - 这次故障还在处理中，
    说明、严重级别和 runbook
    需要持续更新

如果这块不补上，
系统虽然能“看见状态”，
但还不算真正进入正式运维。

## 这章最重要的一个拆分

这一章必须先把两个概念拆开：

1. `plane health`
2. `plane operation`

它们不是一回事。

### `plane health`

这里继续沿用前面已经存在的健康态：

- `registering`
- `ready`
- `degraded`
- `offline`

它表达的是：

- `control plane`
  通过 `planesync`
  看到的这套远端 `cloud plane`
  当前健康投影

也就是说，
它回答的是：

- 这套 `plane`
  看起来健不健康

### `plane operation`

这一章新增的是运维态：

- `active`
- `maintenance`
- `draining`

它表达的是：

- 管理员现在希望这套 `plane`
  以什么运维模式工作

也就是说，
它回答的是：

- 这套 `plane`
  现在应不应该继续接新部署

这里要特别注意：

- `health`
  是系统观察结果
- `operation`
  是管理员运维意图

所以它们必须分开建模，
不能混进同一个字段。

## 为什么不能把 maintenance / drain 塞进原来的 status

原因很直接：

- 原来的 `plane status`
  会被 `planesync`
  周期性重新计算并回写

如果把：

- `maintenance`
- `draining`

这种人工运维态也塞进去，
下一轮同步就会把它覆盖掉。

所以这一章的做法是：

- `status`
  继续只表示健康
- 新增 `operation`
  单独表示运维态

这样之后：

- `planesync`
  只负责写健康
- 管理员接口
  只负责写运维态
- `placement`
  和
  `deploy`
  同时看这两者

## 这一章新增了什么

### 1. plane operation 子资源

现在每个 fleet plane
除了：

- `status`
- `registration`
- `latestCapacitySnapshot`

还会带一个：

- `operation`

它最小只包含：

- `state`
- `reason`
- `updatedAt`

这里的 `state`
有三种：

- `active`
  - 正常接单
- `maintenance`
  - 维护中
  - 停止接收新部署
- `draining`
  - 正在退出轮转
  - 这一章也先只做停止接单

也就是说，
这一章里的 `draining`
还不是：

- 自动迁移已有 workload
- 自动驱逐已有实例

它先只是 fleet 级别的：

- 停止接单
- 状态可见

### 2. incident 可以 update 了

前面 incident
只有：

- create
- list
- resolve

这一章补成：

- create
- update
- resolve

这样才能表达真实事故流：

1. 先创建 incident
2. 处理中持续更新：
   - `severity`
   - `summary`
   - `description`
   - `runbookURL`
3. 问题解决后再 `resolve`

这里暂时不做：

- `ack`
- `reopen`

因为这一版还没有更正式的值班、
指派、
告警收敛、
case 管理模型。

## 这章里 drain 到底是什么意思

这里最容易误会。

### plane drain

这一章的：

- `plane operation = draining`

表示的是：

- 这套 `cloud plane`
  从 fleet 视角开始退出接单

它影响的是：

- auto placement
- 手工指定 plane 的 apply

也就是说，
只要一套 plane
进入：

- `maintenance`
  或
- `draining`

它就不再接收新部署。

### node drain

而 `cloud plane`
里原来就有的：

- `node drain`

表示的是：

- 某个具体节点停止接收新的 workload

它是 plane-local 的节点维护动作，
不是 fleet 级别的 `plane evacuation`。

所以这一章要明确记住：

- `plane drain != node drain`

这里的边界是：

- `control plane`
  只控制：
  - 这套 plane
    还接不接新部署
- `cloud plane`
  才控制：
  - 本地哪些节点可调度
  - 哪些节点在 drain

## 这章最终把哪条运维链打通了

现在这条链已经成立：

1. 管理员把 plane
   切到：
   - `maintenance`
     或
   - `draining`
2. `inventory`
   能直接看见这套运维态
3. `placement`
   会跳过它
4. 手工指定 plane 的 `apply-app`
   也会被拦住
5. incident
   可以持续 update
6. incident resolve
   只能从 open 进入 resolved

这意味着：

- “停止接单”
  已经不是口头约定
- “事故处理中更新信息”
  也不再只能靠文档外沟通

## 这一章新增的 northbound 接口

### plane operation

- `PUT /api/v1/fleet/planes/{planeID}/operation`

请求体示例：

```json
{
  "state": "maintenance",
  "reason": "kernel upgrade"
}
```

如果要恢复接单：

```json
{
  "state": "active"
}
```

这里还要注意一件事：

- `maintenance`
  和
- `draining`

都要求给 `reason`。

因为一旦系统已经支持正式运维态，
就应该顺手把“为什么切进去”
一起记录下来，
否则后面排查会很难看。

### incident update

- `PUT /api/v1/fleet/incidents/{incidentID}`

请求体示例：

```json
{
  "severity": "warning",
  "summary": "plane heartbeat recovered",
  "description": "heartbeat stream is back but still needs observation",
  "runbookURL": "https://runbooks.example.com/fleet/plane-degraded"
}
```

### incident resolve

- `POST /api/v1/fleet/incidents/{incidentID}/resolve`

这里这一章还顺手收紧了一点：

- 已经 `resolved`
  的 incident
  不能重复 `resolve`

也就是说，
这一章之后，
incident 的最小生命周期就是：

- `open -> resolved`

## placement 和 deploy 现在怎么看 plane

这章之后，
是否能把一个应用下发到某个 plane，
要同时满足两件事：

1. `health = ready`
2. `operation = active`

也就是说：

- 只健康还不够
- 只接单也不够

必须同时满足：

- 系统观察它健康
- 管理员也没有把它停收

这是这一章真正的行为收口点。

## inventory 现在多了什么

这章之后，
全局 `inventory`
除了继续展示：

- health
- capacity
- nodes/apps/deployments

还会额外展示：

- `operationState`
- `operationReason`
- `operationUpdatedAt`
- `acceptingNewDeployments`

并且 summary
里也会看到：

- `planesActive`
- `planesMaintenance`
- `planesDraining`

所以现在读 fleet 视图时，
你能一眼区分：

- 这套 plane
  是坏了
- 还是管理员主动把它切出了接单路径

## metrics 现在补了什么

这一章还顺手给 fleet metrics
补了两类信号：

- `minicloud_fleet_plane_operation_state`
- `minicloud_fleet_plane_accepting_new_deployments`

这样后面做告警或面板时，
就能明确地区分：

- health 告警
- 运维停收状态

## 这章没有做什么

这一章故意没有继续往下做：

- 自动迁移已有 workload
- control plane
  远程代理 cloud plane 的 node drain
- incident 自动驱动 maintenance
- resolve incident 自动恢复 plane

这些事情不是做不到，
而是现在还不该和这章混在一起。

当前更重要的是先把边界收清楚：

- `health`
  是健康投影
- `operation`
  是运维意图
- `incident`
  是故障记录

这三者可以相关，
但不能互相偷偷代替。
