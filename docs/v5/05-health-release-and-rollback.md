# v5/05 Health Release And Rollback

`v5/04`
已经把一件很重要的事情做完了：

- `service`
  已经可以被真正访问到
- `public / private`
  暴露语义已经稳定
- 平台域名和服务域名都进入了正式数据面
- `Caddy`
  已经成为唯一正式入口

但这还不等于平台已经具备“长期在线服务的发布能力”。

因为一个服务能被访问，
和一个服务能被安全升级，
是两回事。

这一章要解决的，
就是后者。

## 这一章先回答什么问题

这一章要把下面这些问题收成正式模型：

1. 一个新版本什么时候只能算：
   - candidate
   而不是 current
2. 为什么新版本没有健康通过前，
   流量不能切过去
3. 一个发布失败后，
   为什么旧版本应该继续扛流量
4. 什么叫：
   - `startup`
   - `readiness`
   - `liveness`
5. 为什么：
   - `retry`
   - `rollback`
   不是一回事
6. 什么叫：
   - pause
   - resume
7. 旧版本什么时候才能被真正摘流量并退出

一句话说，
这一章收的是：

- 单个 `service`
  的发布与健康状态机

不是：

- 多副本扩缩容
- 多节点批量滚动
- 全局流量调度

## 为什么 `v5/04` 之后还不够

`v5/04`
只保证了：

- 数据面入口是正式的
- 已经 ready 的 backend
  能接到公网流量
- backend 没 ready 时，
  入口会返回受控 `503`

但平台还没有把下面这些事收成明确边界：

- 一个新 revision
  处于什么阶段
- 什么时候能提升成 current release
- 发布推进能不能暂停
- 失败后平台应该自动停在哪
- 旧实例下线时应该先做什么

如果这些不清楚，
平台就只能算：

- 能跑服务

而不能算：

- 能做服务发布

## 当前代码里其实已经有什么雏形

这一章不是从零开始。

当前代码里其实已经有几块很关键的雏形：

1. `service.status.currentRelease`
   已经存在
2. 新 spec 变更时，
   当前 release 不会立刻切走
3. 新 candidate 失败时，
   旧 release 会继续扛流量
4. 已经有：
   - `probe`
   - `retry`
   - `rollback`
   这些动作接口
5. 当前集成测试已经证明了：
   - v2 candidate 失败后，
     current 仍保持 v1
   - retry 成功后，
     current 才提升到 v2
   - rollback 会重新拉起旧 revision，
     再把 current 切回去

也就是说，
底层方向已经对了。

但现在的问题是：

- 这些能力还比较零散
- 发布状态还是隐含在 deployment / execution 变化里
- 健康模型还只有一个很粗的 `/healthz`
  视角
- `pause / resume`
  和 graceful shutdown
  还没有正式语义

所以 `v5/05`
不是重做发布链，
而是把当前零散能力收成正式产品模型。

## 这一章的核心边界

这一章只围绕：

- 单个长期在线 `service`

收出下面 4 件事：

1. 健康模型
2. 发布状态机
3. 切流与摘流量顺序
4. 操作动作：
   - pause
   - resume
   - retry
   - rollback

## 这一章明确不做什么

这一章不做：

- 多副本扩缩容
- worker 扩容 / 回收
- 容量调度
- 多副本滚动批次
- `CDN`
- 外部 front door
- OTel / trace / metrics 体系
- 项目级配额和护栏
- 更多 workload 类型

这里要特别强调一个容易误解的点：

`ROADMAP`
里写了：

- rolling update

但在当前产品边界下，
它在 `05`
里应该先理解成：

- 单副本 service
  的安全切换语义

不是：

- 多副本分批滚动发布

真正的多副本和扩容问题，
应该留到：

- `v5/06`

## 这一章之后，service 的发布模型应该长什么样

这一章之后，
一个 `service`
至少要能稳定表达这两个版本位：

### `current release`

`current release`
表示：

- 当前真正接正式流量的 release

它必须满足：

- 已经通过健康门禁
- 已经完成提升

### `candidate release`

`candidate release`
表示：

- 正在被验证的新 release

它的状态可能是：

- preparing
- starting
- waiting_readiness
- paused
- failed
- ready_to_promote

在 candidate 还没完成健康验证前：

- `current release`
  不能被替换
- 数据面也不能把正式流量切过去

## 为什么这一章必须拆出三类探针

当前平台里已经有：

- `readinessPath`

但只有这一层还不够。

因为“容器是否起来了”和“是否能接流量”以及“运行中是否还活着”，
本来就是三件不同的事。

### `startup`

`startup`
要回答的是：

- 这个新实例是不是还在正常启动窗口里

只要 startup 还没完成，
平台就不应该急着判定它失败。

它的作用是避免这种误伤：

- 镜像启动慢
- 应用初始化慢
- 依赖连接慢

### `readiness`

`readiness`
要回答的是：

- 这个实例现在能不能接正式流量

它直接影响：

- candidate 是否能被提升
- current route 是否能切过去

在 `v5/05`
里，
它是最关键的一道门。

### `liveness`

`liveness`
要回答的是：

- 这个已经在运行的实例是不是还活着

它不负责决定首次切流，
它负责的是：

- 运行中实例失活后，
  平台要不要把它视为故障实例

所以三者的职责必须分清：

- `startup`
  决定“刚起来时别太早判死”
- `readiness`
  决定“能不能接流量”
- `liveness`
  决定“活着的实例后来是不是坏了”

## 单副本发布在这一章里到底怎么工作

当前平台里，
`replicas`
还被限制为：

- 只能是 `1`

所以 `05`
里的发布主线应该非常明确：

- 旧版本继续接流量
- 新 candidate 在后台起
- 新 candidate 通过健康门禁后，
  才切 current
- 切完以后，
  再优雅地下线旧实例

这里的“后台起”
只是在描述发布控制语义：

- 平台先验证新 candidate
- 旧 current
  在此之前继续对外服务

它不表示：

- 这一章已经支持多副本并行滚动
- 已经进入 `v5/06`
  的扩容语义

也就是这条顺序：

```text
v1 current
  -> 创建 v2 candidate
  -> v2 startup/readiness 检查
  -> v2 通过
  -> current 切到 v2
  -> v1 摘流量
  -> v1 graceful shutdown
```

如果 v2 失败，
顺序就停在这里：

```text
v1 current
  -> 创建 v2 candidate
  -> v2 健康检查失败
  -> current 仍然是 v1
  -> service 进入 degraded / failed rollout
```

这个模型是 `05`
最核心的产物。

## graceful shutdown 在这一章里是什么意思

这一章里的 graceful shutdown
不是一个“容器退出得优雅一点”的空话。

它要表达的是一个顺序保证：

1. 旧实例先不再接新流量
2. 等当前正在处理的请求尽量结束
3. 再真正停止旧实例

所以它必须和：

- 数据面切流
- readiness 摘除

连起来理解。

如果没有这一步，
平台虽然也能切 current，
但会在切换瞬间把用户请求砍断。

## pause / resume 在这一章里到底暂停什么

`pause`
不是：

- 暂停整个 service
- 暂停数据面入口
- 暂停 worker

它暂停的是：

- candidate release 的自动推进

比如：

- 新 candidate 已经起来
- 但你暂时不想让它继续推进到提升

那平台就应该允许你把 rollout 停住。

`resume`
则是：

- 从这个停住的 rollout 继续推进

所以 `pause / resume`
是 release controller 的动作，
不是运行时开关。

## retry 和 rollback 为什么不是一回事

这两个词很像，
但语义完全不同。

### `retry`

`retry`
表示：

- 我还是想要这个 candidate release
- 只是刚才那次尝试失败了
- 请基于同一个目标 revision 再来一次

所以 retry 解决的是：

- 瞬时失败
- 临时拉镜像失败
- 启动偶发问题
- 某次部署过程出错

### `rollback`

`rollback`
表示：

- 我不想继续推进当前 candidate 了
- 我要回到某个之前已知的稳定 release

所以 rollback 解决的是：

- 新版本本身就有问题
- 即使重试也不值得继续推进

这里必须强调：

rollback 不是简单把指针改回去。

更合理的模型是：

- 基于旧 revision
  再创建一个新的 candidate deployment
- 它通过健康门禁后，
  再重新成为 current

这样发布链路才是一致的。

## 这一章要补的正式状态机

这一章之后，
至少应该把下面这些状态和阶段明确下来：

### service 视角

- running
- degraded
- failed

并且能清楚区分：

- 当前是否有稳定 current
- 当前是否有 candidate 正在推进
- 当前 rollout 是不是被 paused

### release / rollout 视角

- preparing
- progressing
- paused
- failed
- promoted
- rolled_back

### execution 视角

当前已经有：

- deploying
- running
- superseded
- failed

这一章要做的，
不是把 execution 状态搞得特别复杂，
而是把它和：

- startup
- readiness
- liveness
- graceful shutdown

之间的关系讲清楚。

## 这一章落地后，API 应该长成什么样

这一章不一定要大改 northbound 主对象，
但至少要让 API 能清楚表达这些信息：

1. 当前 `service status`
   里谁是 current release
2. 当前有没有 candidate release
3. 当前 rollout 是否 paused
4. 最近失败原因是什么
5. 当前探针配置是什么
6. 当前 rollout 能执行哪些动作：
   - pause
   - resume
   - retry
   - rollback

当前已经存在的：

- `probe`
- `retry`
- `rollback`

会继续保留，
但它们会从“零散动作”
变成“正式 rollout 模型的一部分”。

## 这一章结束时，平台要具备什么能力

到 `v5/05`
结束时，
平台至少要能稳定演示下面这条主线：

1. 先有一个已经在接流量的 `v1`
2. 发布 `v2`
3. `v2`
   先作为 candidate 启动
4. `v2`
   没通过健康门禁时，
   current 仍保持 `v1`
5. `v2`
   通过后，
   current 切到 `v2`
6. `v1`
   被优雅地下线
7. 如果 `v2`
   后来失败，
   可以：
   - retry
   - rollback
8. rollout 过程中可以：
   - pause
   - resume

如果这条主线能完整跑通，
那 `mini-cloud`
才算第一次真正具备：

- 长期在线服务的发布能力

## 本章检查点

如果这一章做对了，
你现在应该已经能稳定回答下面这些问题：

1. 为什么：
   - `startup`
   - `readiness`
   - `liveness`
   不能继续混成一个 `/healthz`
2. 为什么 candidate 没通过健康门禁前，
   current 不能提前切走
3. 为什么单副本发布也能成立：
   - 安全切换
   - retry
   - rollback
   这一整套模型
4. 为什么：
   - `pause`
   - `resume`
   暂停的是 rollout 推进，
   不是 service 本身
5. 为什么：
   - `retry`
   - `rollback`
   是两种完全不同的动作
6. 为什么 graceful shutdown
   必须和摘流量顺序一起理解
7. 为什么 `05`
   不应该提前去做多副本滚动和扩缩容

如果这些问题都已经能稳定回答，
那 `v5/05`
的发布边界就算真正立住了。

对应提交：

- `bca0c67706b705d5accb87dd03b5edec50dce1ae`
