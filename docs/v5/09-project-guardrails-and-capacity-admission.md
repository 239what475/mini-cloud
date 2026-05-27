# v5/09 Project Guardrails And Capacity Admission

`v5/08`
已经把：

- `human_user`
- `service_account`
- `break_glass`
- `project.owner_user`

这套身份、归属和授权边界收下来了。

`v5/06`
也已经把：

- 多副本运行
- `replicas`
  的 scale-up
- 本云 `worker`
  扩容

这条执行链立住了。

但平台如果还回答不了下面这个问题：

- 这个 `project`
  现在到底还能不能再接一个新的
  `service`
  规格

那后面的：

- 外部前门
- 完整平台验收

都会缺一层很硬的治理边界。

所以这一章只收一件事：

- `project`
  级的声明式 capacity guardrail
  和 admission

## 这一章只做什么

这一章明确只做：

- `project`
  级 quota
- 当前已声明 usage
- remaining
- create
  前的 preview
- 会增加声明式容量的 update
  前的 preview
- scale-up
  前的 preview
- 真正写路径里的 admission
- 稳定的 reject reasons

这一章明确不做：

- cost
- budget
- payment
- balance
- 云账单
- 域名数量 quota
- `maxReplicas`
  quota
- per-region quota
- scheduler
  的实时可放置容量判断

也就是说，
这章只回答：

- 这个 `project`
  的 guardrail 是多少
- 它现在已经承诺了多少容量
- 如果再加一个 `service`
  或再扩一些副本，
  会不会超

不回答：

- 实际 CPU 用了多少
- 实际内存用了多少
- 当前云里到底还剩几台节点能放下

## 先把最容易混的边界讲清楚

这里最容易混的不是 quota 本身，
而是下面 3 层很像但其实完全不同的东西：

1. `project guardrail`
   - 这个项目被允许声明多少容量
2. `admission`
   - 这次 create / scale-up
     要不要被接纳
3. `scheduler / placement`
   - 当前这朵云现在到底放不放得下

这一章只收：

- `project guardrail`
- `admission`

不收：

- `scheduler / placement`

因为：

- `project guardrail`
  是项目治理问题
- `scheduler / placement`
  是节点和 `service cell`
  的运行容量问题

如果把这两层混在一起，
平台就会立刻变得很乱：

- preview
  会开始依赖实时节点状态
- API
  会把项目治理和云内调度混成同一层错误
- 后面再做：
  - `front door`
  - 完整 `e2e`
  时也很难解释到底哪一层失败了

## 这章里的 `usage` 到底是什么意思

这一章里的：

- `usage`

不是：

- 实时 CPU 使用率
- 实时内存使用率
- 容器观测数据

它指的是：

- 这个 `project`
  当前已经声明并保留出去的容量总量

也就是：

- 现有 `service`
  的期望规格
- 按 `instanceClass * replicas`
  计算出来的总请求量

这里故意选这条语义，
是因为 admission 要解决的是：

- 项目现在还能不能再申请更多容量

而不是：

- 某几个容器此刻的瞬时负载是不是很低

如果 admission 改看实时采样，
你会立刻遇到两个问题：

1. preview
   不再稳定
2. create / scale-up
   结果会依赖瞬时波动

这都不适合做平台治理边界。

所以这一章的结论很硬：

- `usage`
  是声明式 reserved capacity
- 不是 telemetry

## 这章最后冻结哪些 quota 字段

这一章只冻结 3 个 quota 维度：

- `maxServices`
- `cpuMilli`
- `memoryMi`

它们的含义分别是：

- `maxServices`
  - 这个 `project`
    最多允许存在多少个
    `service`
- `cpuMilli`
  - 这个 `project`
    所有 `service`
    总共最多允许声明多少 CPU
- `memoryMi`
  - 这个 `project`
    所有 `service`
    总共最多允许声明多少内存

这一章明确不保留：

- `monthlyBudgetCents`
- `estimatedMonthlyCostCents`
- 任何价格或预算字段

因为它们会把这章重新拉回：

- quota + cost

而不是：

- pure capacity guardrail

## 这章最后冻结哪些容量视图字段

### `guardrails`

```text
maxServices
cpuMilli
memoryMi
```

### `usage`

```text
services
cpuMilli
memoryMi
```

### `remaining`

```text
services
cpuMilli
memoryMi
```

### `preview.requestedDelta`

```text
services
cpuMilli
memoryMi
```

### `preview.projectedUsage`

```text
services
cpuMilli
memoryMi
```

### `preview`

```text
allowed
rejectReasons[]
```

这套字段已经够支撑：

- CLI
- Web UI
- Terraform

去稳定表达 admission 结果了。

这一章先不要继续加：

- `domainCount`
- `regions[]`
- `maxReplicas`
- `estimatedCost`
- `balance`

因为这些字段都属于另一层问题。

## create、update、scale-up 到底各算什么

这章里还有一个很容易写错的边界：

- 什么操作该重新做 admission
- 什么操作不该重新做 admission

### 1. create service

create 新 service
会影响：

- `services`
- `cpuMilli`
- `memoryMi`

所以它一定要做 admission。

### 2. scale-up

改：

- `replicas`

本质上是 capacity change，
不是 revision change。

所以 scale-up
会影响：

- `cpuMilli`
- `memoryMi`

它也一定要做 admission。

但它不应该再增加：

- `services`

因为：

- 你没有新建一个 service
- 你只是给当前 deployment
  增加更多副本

### 3. capacity-increasing update

如果 update
会改变：

- `instanceClass`
- `replicas`

那它本质上仍然是：

- capacity change

所以它应该走这章的 preview
和 admission。

这里最典型的例子是：

- `small -> medium`
- `1 replica -> 2 replicas`

它们都不是单纯的 rollout，
而是在增加项目声明式容量。

### 4. revision-only update

改镜像、
改命令、
改环境变量、
改健康检查，
这类 revision 变化，
如果不改变：

- `instanceClass`
- `replicas`

那它不应该重新引入新的 quota 语义。

也就是说：

- revision change
  是 rollout
- `replicas`
  change
  是 capacity adjustment

这条边界必须和
`v5/06`
完全一致。

### 5. scale-down

scale-down
不会申请更多容量，
只会释放容量。

所以它不需要 admission。

## preview 和真实 admission 的关系

这一章还有一条非常重要的实现边界：

- preview
  和真实写路径
  必须复用同一套规则

preview
不是另一套“教学计算器”，
而应该是：

- 真 admission
  的 dry-run

也就是说：

1. owner 或项目级自动化身份
   先做 preview
2. preview
   返回：
   - current usage
   - requested delta
   - projected usage
   - remaining
   - allowed
   - reject reasons
3. 真正 create / scale-up
   时复用同一判断逻辑

这样才能保证：

- preview
  通过以后，
  真实写路径不会突然换另一套规则

当然，
真实写路径仍然要以“当前最新状态”为准，
所以 preview
不会帮你做：

- reservation
- hold
- 锁定 quota

它只是一个稳定的 dry-run。

## 这章最后该有哪些 northbound 能力

如果只追求最小闭环，
这一章的 northbound
只需要收 4 类能力：

### 1. 看单个 project 的容量视图

至少要能稳定看见：

- guardrails
- usage
- remaining

这里的重点是：

- owner
  不应该只能在失败时报错
- 它应该能主动看到自己项目当前还有多少余量

### 2. 做 service capacity preview

也就是：

- 给一个 candidate service plan
  做 pure admission preview

这个 preview
既可以被：

- CLI
- Web UI
- Terraform

复用，
也可以被后面的：

- 完整 `e2e`

直接拿来验证平台语义。

### 3. 改 project guardrails

这章既然要把：

- guardrail

定义成正式产品边界，
那就必须有对应的治理入口。

但这里的边界也要写死：

- 这不是 owner
  自助表单
- 这是 platform
  侧治理能力

所以这章应该明确存在一条：

- 修改 project guardrails

的 northbound 能力，
但它只开放给：

- `platform_owner`
- `platform_admin`

### 4. 在真实写路径里做 admission

也就是：

- create service
- capacity-increasing update
- scale-up

都要显式走 quota 校验，
而不是只靠前端 preview。

## reject reason 先做哪几个最值钱

这一章第一版只做 3 个 reject reason
就够了：

- `services_quota_exceeded`
- `cpu_quota_exceeded`
- `memory_quota_exceeded`

这样已经能覆盖最核心的失败场景。

这里要特别注意：

这些 reason
应该是：

- admission reasons

而不是把所有失败都塞进来。

所以这章先不要把下面这些混进来：

- `invalid_instance_class`
- `replicas_must_be_positive`
- `project_access_denied`
- `placement_failed`
- `worker_scale_out_failed`

因为它们分别属于：

- 参数校验
- 鉴权
- 调度 / 执行失败

不是同一层问题。

## reject reason 应该长什么样

第一版最少也应该冻结：

- `code`
- `message`

如果想再多带一点上下文，
最多加：

- `current`
- `requestedDelta`
- `projected`
- `limit`

例如：

```json
{
  "code": "cpu_quota_exceeded",
  "message": "project cpu quota would be exceeded",
  "current": 4000,
  "requestedDelta": 2000,
  "projected": 6000,
  "limit": 5000
}
```

这样：

- CLI
  能直接展示
- Web UI
  能稳定渲染
- Terraform
  也不用去猜错误文案

## 这章和 `v5/08` 的关系

`v5/08`
先回答：

- 谁能看
- 谁能改
- 谁能操作项目

`v5/09`
再回答：

- 这个项目还能不能再接更多容量

所以：

- `project.owner_user`
  - 可以看自己项目的：
    - guardrails
    - usage
    - remaining
    - preview
  - 可以发起：
    - create
    - scale-up
    并收到 reject reasons
- `service_account(scope=project)`
  - 可以做 preview
  - 可以做服务变更
  - 但不改 project guardrails
- `platform_admin`
  - 可以跨项目看 usage / preview
  - 可以改 guardrails
- `platform_owner`
  - 同样可以改 guardrails
- `platform_operator`
  - 可以看 usage / remaining / preview
  - 可以在运维动作里消费 admission 结果
  - 但默认不改 guardrails

这也意味着：

- quota
  是 platform guardrail
- 不是 project owner
  自助随便改的参数

## 这章和 `v5/10` 的关系

`v5/10`
要做的是：

- 外部 front door
- `CDN`
- 每云固定入口机
- `Caddy`
  两段路由

它应该建立在：

- 已经 admitted 的
  `public service`

之上，
而不是在 front door
里重新发明一套准入逻辑。

也就是说：

- `v5/09`
  决定某个 service plan
  能不能被 project 接纳
- `v5/10`
  决定这个已接纳的
  public service
  怎样对外暴露

最小 front door
不应该再引入：

- cost
- budget
- front door 自己的 quota 计费逻辑

## 这章和 `v5/11` 的关系

`v5/11`
做完整平台验收时，
至少应该覆盖下面 4 条：

1. owner
   先 preview，
   再 create service
2. scale-up
   先 preview，
   超限后被稳定拒绝
3. platform admin
   调整 guardrail 后，
   同一操作重新通过
4. reject reason
   在 API / CLI / Terraform
   视角下都还是可解释的

如果这 4 条成立，
那就说明：

- `v5/08`
  的授权边界是真的
- `v5/09`
  的 admission
  是稳定的
- `v5/10`
  的入口接入
  建立在已经 admitted
  的服务之上

## 这一章之后，平台边界怎样变化

做完这一章以后，
平台就不再只是：

- 能认证
- 能部署
- 能扩副本

而是第一次具备了真正的：

- project-level guardrail
- admission preview
- stable reject reason

也就是说，
它终于开始能稳定回答：

- 为什么这个项目现在不能再创建新服务
- 为什么这个项目现在不能再继续扩副本
- 平台拒绝的到底是：
  - project guardrail
  还是：
  - runtime placement

这层边界一旦立住，
后面的：

- 外部前门
- 完整 `e2e`
- 边界复盘

就都有了一个稳定前提。

## 本章检查点

如果这一章做对了，
你现在应该已经能稳定回答下面这些问题：

1. 为什么这章里的：
   - `usage`
   不是实时监控指标，
   而是声明式 reserved capacity
2. 为什么这章只做：
   - `maxServices`
   - `cpuMilli`
   - `memoryMi`
   这 3 个 quota
3. 为什么 create service
   和 scale-up
   都必须 admission，
   但 scale-down
   不需要
4. 为什么 preview
   和真实写路径
   必须复用同一套规则
5. 为什么：
   - `project guardrail`
   和：
   - `scheduler / placement`
   不能混成同一层问题
6. 为什么 reject reason
   只先做：
   - services
   - cpu
   - memory
   这 3 类最值钱的结果
7. 为什么这章必须放在：
   - `v5/08`
   后面，
   以及：
   - `v5/10`
   前面

如果这些问题都已经能稳定回答，
那 `v5/09`
的 project-level guardrail
边界就算真正立住了。

对应提交：

- `6f67e20af6cda891d3d1a91098537e913d416352`
