# v4/08 Fleet Placement Policy

`v4/07`
已经把这条链路打通了：

- 明确指定 `planeID`
- 复用 project binding
- 用远端 project token
  去 create / update app

所以这一章不再改写：

- `07`
  的 plane-scoped apply API

而是在它上面补一层：

- project-scoped 的自动选址入口

也就是：

- 先选 plane
- 再把选中的 `planeID`
  交回 `07`
  那条 remote apply 链路

## 这一章到底新增了什么

这一章新增了两条 project-scoped fleet API：

### 1. 只看选址解释

```text
POST /api/v1/fleet/projects/{projectID}/placements/preview-app
```

它只回答两件事：

- 现在哪些 plane 是候选
- 最终为什么选中了这个 plane

它不会写远端 plane。

### 2. 自动选址后直接 apply

```text
POST /api/v1/fleet/projects/{projectID}/actions/apply-app
```

它的流程是：

1. 先按 fleet policy 选 plane
2. 如果没有可用 plane
   - 返回冲突
3. 如果选中了 plane
   - 直接复用 `v4/07`
     的：
     - `fleetdeploy.Service.ApplyApp`

所以这一章不是又重新实现了一套：

- 远端 project token 校验
- 远端 app create/update

而是把：

- plane selection

单独抽了出来。

## 这一章的过滤条件是什么

当前 policy
先只做最小、可解释、确定性的选择。

候选 plane 必须同时满足：

1. 本地 `projectID`
   已经和这个 plane 建过 binding
2. plane 已完成 bootstrap 注册
3. plane 当前状态是：
   - `ready`
4. plane 的：
   - `provider`
   - `region`
   和请求匹配
5. plane 有最新 capacity snapshot
6. snapshot 里的剩余容量足够放下这次 app

这里特别注意：

- `08`
  的选址不是直接看远端 node 列表

fleet 当前掌握的是：

- plane status
- latest capacity snapshot

所以这一章做的是：

- plane 级选址

不是：

- node 级选址

真正的 node 选择，
仍然留在每个 plane 自己内部完成。

## 请求里的资源规格怎么解释

这一章没有重新引入一套：

- `cpuMilliRequest`
- `memoryMiRequest`

而是继续沿用 app 的：

- `instanceClass`
- `replicas`

然后在 fleet 里把它换算成：

- `requestedCPUMilli`
- `requestedMemoryMi`

当前仍然保持和前面一致的边界：

- `replicas > 1`
  先不支持

所以现在的策略不是复杂多副本全局调度，
而是：

- 给这次 app create / update
  先选一个最合适的 plane

## 选中 plane 的打分规则是什么

当前规则故意保持简单：

1. 先看放置后剩余 CPU 更多者优先
2. 再看放置后剩余内存更多者优先
3. 如果还相同
   - 用 `planeID`
     做稳定 tie-break

这样做的好处是：

- 结果稳定
- 不需要额外状态
- 文档和测试都容易解释

所以这章还没有做：

- 复杂权重
- 历史负载趋势
- 跨 region 成本偏好
- incident / maintenance / drain 影响

这些留给后面的章节继续补。

## 为什么这一章不对每个候选 plane 都做远端 token 校验

这里有一个很关键的边界。

你可能会问：

- 既然要自动选 plane，
  为什么不在 preview 阶段就对所有候选 plane
  都调一次远端 `whoami`

当前没有这么做。

原因是：

1. `v4/07`
   的 binding 写入时，
   已经做过一次：
   - deploy token 必须是目标 remote project 的 project token
2. 这一章真正写远端之前，
   最终还是会落到：
   - `fleetdeploy.Service.ApplyApp`
   那里还会再校验一次选中的 plane

所以 `08`
只做：

- 本地 binding + plane status + capacity snapshot
  的选址

而不是在 preview 阶段就把所有候选 plane
都打一遍远端 API。

这样边界更清楚：

- `08`
  负责：
  - 选 plane
- `07`
  负责：
  - 写远端

## preview 返回里现在能看到什么

当前 preview 结果里最重要的是三块：

### 1. `decision`

如果选中了 plane，
这里会给出：

- `planeID`
- `planeName`
- `provider`
- `region`
- `remoteProjectID`
- 放置后剩余：
  - `cpuMilliFreeAfter`
  - `memoryMiFreeAfter`
- `score`
- 选中理由

### 2. `candidates`

这里会把 fleet 眼里每个 plane
都列出来，
并给出：

- 是否已注册
- 当前状态
- 是否有 binding
- 当前剩余容量
- 是否 eligible
- 为什么被排除或为什么被选中

这一块很重要，
因为它让自动选址不是一个黑盒。

### 3. `filteredCounts`

这里按过滤阶段统计了：

- binding
- registration
- status
- provider
- region
- capacity

这样你可以很快看出来：

- 到底是因为没有 binding
- 还是因为 plane 不 ready
- 还是因为容量不够

## auto apply 和 manual target apply 现在怎么分工

到这一章为止，
fleet 里已经有两条 apply 路径：

### manual target

```text
POST /api/v1/fleet/planes/{planeID}/projects/{projectID}/actions/apply-app
```

它适合：

- 你已经明确知道要落到哪个 plane

### auto placement

```text
POST /api/v1/fleet/projects/{projectID}/actions/apply-app
```

它适合：

- 你只知道：
  - provider
  - region
  - app 规格
- 由 fleet 帮你选一个 plane

所以两者不是互相替代，
而是：

- `07`
  保留最低层的显式入口
- `08`
  在它上面补自动选择入口

## 这一章没有新增什么表

这一章刻意没有新增：

- `fleet placement policy table`
- `fleet scheduling history table`

原因是当前最小闭环不需要。

现在已经有：

- `fleet_planes`
- `fleet_plane_statuses`
- `fleet_plane_capacity_snapshots`
- `fleet_plane_project_bindings`

这几组数据已经足够回答：

- 这个 project 能不能写到某个 plane
- 这个 plane 现在能不能接单
- 这次容量够不够

所以这章先只新增：

- 计算逻辑
- API
- 测试

不急着再加一个 policy 持久化模型。

## 一次最小实验现在应该怎么看

当前集成测试走的是这条链：

1. 起两套远端 plane
2. 两套 plane 都各自有：
   - ready worker
   - remote project
   - project token
3. 同一个本地 project
   分别绑定到两套 plane
4. fleet 里给两套 plane
   写入不同的 capacity snapshot
5. 调：
   - `preview-app`
   看 fleet 会选谁
6. 再调：
   - 自动 `apply-app`
7. 最后验证：
   - app 只落到了被选中的那套 remote plane

这条测试真正说明的是：

- `08`
  不是只会“算一个答案”
- 而是已经能把答案交给 `07`
  那条写链路继续执行

## 当前还没做什么

这一章还没有做：

- incident / maintenance / drain 参与选址
- 多副本跨 plane 分配
- 复杂权重或成本优化
- 持久化 policy 模型
- 基于历史数据的全局调度

所以这章应该理解成：

- explainable automatic plane selection

而不是：

- fleet 级全局调度器完整版

## 本章涉及的主要文件

- [fleetplacement.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/placement/fleetplacement.go)
- [service.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/placement/service.go)
- [service_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/placement/service_test.go)
- [fleet_placement_handler.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/fleet_placement_handler.go)
- [router.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/router.go)
- [options.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/options.go)
- [fleet_deploy_store.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/store/fleet_deploy_store.go)
- [store_integration_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/store/store_integration_test.go)
- [httpapi_integration_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/httpapi_integration_test.go)

## 本章检查点

- fleet 现在已经有：
  - project-scoped 的选址预览入口
  - project-scoped 的自动 apply 入口
- 自动选址当前只基于：
  - binding
  - registration
  - ready status
  - provider
  - region
  - latest capacity snapshot
- 自动 apply
  已经不是重新实现一套远端写逻辑，
  而是复用：
  - `v4/07`
    的 remote apply service
- preview 结果已经能解释：
  - 哪些 plane 被过滤掉
  - 为什么被过滤
  - 为什么最终选中了这个 plane
- 当前仍然没有进入：
  - incident-aware
  - maintenance-aware
  - multi-plane rollout
  这类更复杂的全局调度阶段
