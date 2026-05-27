# 04 阿里云运行时重试与回收

先记住这一章补的不是“再建一台 worker”，
而是把上一章留下来的这一条责任边界真正落成代码：

- runtime worker
  是 control-plane 运行时通过阿里云 `SDK` 临时创建的
- 它不在 Terraform state 里
- 所以不能指望：
  - `terraform destroy`
  自动帮你把这些 worker 一起回收

这一章就是把这件事正式补齐：

1. 平台自己记住都创建过哪些 runtime worker
2. 平台自己判断哪些 worker 可以安全回收
3. 平台自己调用阿里云：
   - `DeleteInstance`
4. 删除后继续轮询：
   - `DescribeInstances`
   直到实例真的消失
5. `terraform destroy`
   先调用平台自己的 teardown API，
   再销毁 control-plane 和底座

## 上一章检查点

- `v3/03`
  - `216d1f30b29fc7a0da26508495e2f1901ae2d78d`

## 这一章解决了什么

做完以后，
`mini-cloud`
在阿里云单 provider 这条线上，
不只是：

- 容量不足时自动建 worker

还补上了：

- 平台内 runtime worker inventory
- 平台内单 worker reclaim
- 平台内批量 platform teardown
- reclaim 时的删除轮询与重试等待
- `terraform destroy`
  前自动清场

也就是说，
control-plane 至少已经有了这条基础闭环：

- `RunInstances`
  创建 worker
- 记录到平台自己的 inventory
- worker register + heartbeat
- inventory 自动变成：
  - `ready`
- 管理员从平台发起 reclaim
- `DeleteInstance`
  删除 worker
- 平台继续轮询到：
  - `DescribeInstances`
  查不到它
- `terraform destroy`
  会先触发：
  - `POST /api/v1/platform/teardown`
  等平台把 runtime worker 清完，
  再继续删 Terraform state 里的底座

## 为什么这一章一定要有自己的 inventory

上一章已经把边界说清楚了：

- platform 底座
  - 由 Terraform 管
- runtime worker
  - 由 control-plane 在运行时通过阿里云 `SDK` 管

所以如果平台自己不保存 inventory，
后面立刻就会遇到两个问题：

1. 你不知道哪些 ECS 是平台临时申请出来的
2. 你也没法在平台里实现：
   - reclaim
   - retry
   - destroy 前清场

这一章新增了一张表：

- `runtime_workers`

它专门记录：

- provider / region
- instance id / name / type
- 这是为哪个 app / deployment 申请的
- 有没有回链到平台里的 node
- 当前生命周期状态

对应文件在：

- `internal/store/migrations/00015_create_runtime_workers.sql`
- `internal/store/runtime_worker_store.go`
- `internal/runtimeworker/runtimeworker.go`

## 运行时 worker 的生命周期现在怎么表示

这一章先把状态收成 5 个：

- `provisioning`
- `ready`
- `reclaiming`
- `reclaimed`
- `reclaim_failed`

可以这样理解：

- `provisioning`
  - 云上实例已经申请了
  - 但平台还没看到 ready heartbeat
- `ready`
  - 这台 worker 已经注册回来并能正常调度
- `reclaiming`
  - 平台已经开始发起回收
- `reclaimed`
  - 平台已经确认云上实例消失
- `reclaim_failed`
  - 这次回收没有成功
  - 但后续允许继续 retry

## runtime worker 什么时候写进 inventory

不是等 worker ready 之后才写，
而是在上一章的 scale-out 主链里：

- `RunInstances`
  成功返回实例 ID

之后，
control-plane 就立刻把它写进：

- `runtime_workers`

状态先记成：

- `provisioning`

对应代码在：

- `internal/httpapi/app_scale_out.go`

这样做的意义是：

- 即使 worker 后面一直没注册回来
- 平台也已经知道“自己确实创建过这台机器”

这对后面的：

- reclaim
- retry
- destroy 前清场

都很关键。

## worker 为什么会自动从 provisioning 变成 ready

这一章没有先去搞额外的后台同步器，
而是先把最直接的一条链补上：

- worker 发送 ready heartbeat
- `nodes` 表刷新
- 同一个事务里顺手回写：
  - `runtime_workers`

也就是说，
当前 inventory 里的：

- `ready`

不是靠猜，
而是靠平台已经收到的真实 heartbeat 推出来的。

对应代码在：

- `internal/store/node_store.go`

这个设计有两个好处：

1. 不需要额外定时任务，ready 状态天然跟着心跳走
2. 即使 `app_scale_out`
   那次等待超时了，
   只要 worker 后面真的注册回来，
   inventory 仍然会被修正成：
   - `ready`

## 这一章新增了哪些平台接口

### 1. 列出 runtime worker inventory

- `GET /api/v1/platform/runtime/workers`

这个接口会返回：

- `runtime_workers` 记录
- 关联的 node
- 当前是否：
  - `reclaimable`
- 如果不能 reclaim，阻塞原因是什么

这里最重要的是：

- 平台终于能直接回答：
  - “我到底还握着哪些运行时临时资源？”

### 2. 回收单个 runtime worker

- `POST /api/v1/platform/runtime/workers/{workerID}/reclaim`

它现在的流程是：

1. 读取 inventory 记录
2. 如果 node 还挂着资源分配：
   - 直接拒绝
3. 如果 node 还可调度：
   - 先 drain
4. 把 runtime worker 状态写成：
   - `reclaiming`
5. 调阿里云：
   - `DeleteInstance`
6. 持续轮询：
   - `DescribeInstances`
   直到实例真的消失
7. 成功后：
   - node 标成 `offline`
   - runtime worker 标成 `reclaimed`
8. 如果失败：
   - runtime worker 标成 `reclaim_failed`

对应代码在：

- `internal/httpapi/runtime_worker_handler.go`
- `internal/runtimeprovision/aliyun.go`
- `internal/store/node_maintenance_store.go`

### 3. 平台销毁前清场

- `POST /api/v1/platform/teardown`

这个接口不是“运维里手工回收一台空闲 worker”，
而是另一种更强的语义：

- 平台已经准备进入：
  - teardown
- 后续不再接受新的 app update / retry / rollback
- control-plane 要先把自己创建过的 runtime worker 清掉
- 清完以后，
  Terraform 才能继续销毁 control-plane 和网络底座

它和单 worker reclaim 的区别是：

- 单 worker reclaim
  - 保守
  - 如果 node 上还有 allocated workload，就拒绝
- platform teardown
  - 面向整个平台销毁
  - 会强制回收所有未回收的 runtime worker
  - 即使这些 worker 还承载着 workload，
    也会把它们视为“平台下线前必须清掉的运行时资源”

所以这里其实是两套不同的心智模型：

- 手工回收一台 worker
  - 要求安全、保守
- 整个平台销毁前清场
  - 要求边界清晰、最终能把 runtime 资源清空

## 这里的“retry”具体落在什么地方

这一章说的 retry，
先不是指复杂的异步任务队列，
而是更基础、也更现实的这一层：

- 你发出：
  - `DeleteInstance`
  不等于实例已经立刻彻底消失

所以当前 reclaim 主链会继续轮询：

- `DescribeInstances`

直到它真的查不到那台实例为止。

这就是这章最核心的第一层 retry / wait：

- 写操作：
  - `DeleteInstance`
- 读视图确认：
  - `DescribeInstances`

这个思路和我们前面在阿里云 `14`、`15`、`16`
里学过的“写后观察”和“最终一致性”是一致的。

也就是说，
这一章不是把删除当成：

- “请求发出就等于完成”

而是把删除当成：

- “请求发出后，还要继续观察直到 absent”

## 为什么 reclaim 不能无脑执行

这一章的 reclaim 先做得比较保守：

- 如果 node 上还有：
  - `cpu_milli_allocated > 0`
  - 或 `memory_mi_allocated > 0`
- 平台就直接拒绝回收

原因很简单：

- 这说明平台自己眼里，这台 node 仍然承载着 workload
- 这时直接删云上实例，本质上就是强拆

所以当前的规则是：

- 没 workload
  - 才允许 reclaim
- 仍有 workload
  - 先返回冲突，要求你先把 workload 迁走

这也是为什么这章虽然已经有 reclaim，
但还不算“全自动缩容”。

## 为什么 platform teardown 又允许强制 reclaim

这里不要把两个动作混在一起：

- `POST /api/v1/platform/runtime/workers/{workerID}/reclaim`
  - 是“日常运维”
  - 所以如果还有 workload，
    平台就拒绝
- `POST /api/v1/platform/teardown`
  - 是“整个 control-plane 都准备销毁了”
  - 这时问题已经不是“这台 worker 还能不能优雅缩容”
  - 而是“平台自己创建过的 runtime ECS 能不能在 destroy 前清干净”

所以 teardown 会做两件额外的事：

1. 先把平台切到 teardown 模式
   - 新的 app update / retry / rollback 会被拒绝
   - auto scale-out 也会被挡住
2. 对所有未回收的 runtime worker 发起强制 reclaim
   - 先尽量 drain / offline
   - 再走：
     - `DeleteInstance`
     - `DescribeInstances`
       轮询 absent

这不是为了“优雅缩容”，
而是为了保证：

- `terraform destroy`
  不会把 control-plane 自己删掉以后，
  还把一批运行时 ECS 孤儿留在云上

## 这一章和 terraform destroy 现在是什么关系

这章做完以后，
平台和 Terraform 的职责已经被真正串起来了：

1. `terraform destroy`
   先销毁一个独立的：
   - `terraform_data`
   清理钩子资源
2. 这个 destroy hook 会在 platform ECS 还活着时，
   本地执行：
   - `deploy/terraform/lab/destroy-runtime-cleanup.sh`
3. 这个脚本只做一件事：
   - 调：
     - `POST /api/v1/platform/teardown`
4. control-plane 根据自己的 inventory
   去回收所有 runtime worker
5. 只有 teardown API 返回成功，
   Terraform 才继续删除：
   - control-plane ECS
   - EIP
   - VPC / vSwitch / security group

也就是说，
现在的 destroy 语义已经变成：

- 先让平台自己清理“运行时动态资源”
- 再让 Terraform 清理“state 里那批底座资源”

如果 teardown API 返回失败，
当前实现会直接让：

- `terraform destroy`
  失败并停止

这是故意的，
因为比起“强行把 control-plane 删掉”，
更重要的是先避免留下无人接管的 runtime worker。

## 当前测试覆盖了什么

这一章至少补了这些集成验证：

1. runtime worker inventory 能列出：
   - 已 provision
   - 已 ready
   - 已回链 node
   的记录
2. reclaim 会：
   - 成功删除可回收 worker
   - 把 node 置为：
     - `offline`
   - 把 runtime worker 置为：
     - `reclaimed`
3. 如果 node 仍有 allocated workload
   - reclaim 会被拒绝
4. platform teardown 会：
   - 批量回收 active runtime worker
   - 跳过已经 `reclaimed`
   - 把平台切到 teardown 模式
   - 阻止新的 app update / retry / rollback
5. 如果某台 worker reclaim 失败
   - teardown API 会返回非 `2xx`
   - 让上游：
     - `terraform destroy`
       中止

对应文件在：

- `internal/httpapi/httpapi_integration_test.go`

## 当前边界

这一章故意还没做这些更大的东西：

- 定时自动清理空闲 worker
- worker 创建失败后的异步补偿队列
- “创建慢”这类更完整的异步重试状态机

所以这章的重点不是“大而全”，
而是先把这条基础主链做实：

- 平台自己记账
- 平台自己删
- 平台自己等到真的删干净

## 本章涉及的核心文件

- `internal/runtimeworker/runtimeworker.go`
- `internal/store/migrations/00015_create_runtime_workers.sql`
- `internal/store/runtime_worker_store.go`
- `internal/store/node_store.go`
- `internal/store/node_maintenance_store.go`
- `internal/httpapi/runtime_worker_handler.go`
- `internal/platformteardown/controller.go`
- `internal/httpapi/router.go`
- `internal/httpapi/app_scale_out.go`
- `internal/runtimeprovision/runtimeprovision.go`
- `internal/runtimeprovision/aliyun.go`
- `internal/httpapi/httpapi_integration_test.go`
- `deploy/terraform/lab/main.tf`
- `deploy/terraform/lab/variables.tf`
- `deploy/terraform/lab/outputs.tf`
- `deploy/terraform/lab/destroy-runtime-cleanup.sh`

## 本章检查点

- `v3/04`
  - `706688fc1622777c158bdd6bd7922e5932c2c69f`
