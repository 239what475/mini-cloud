# Mini Cloud v6 路线图

回到总路线图：

- [projects/mini-cloud/docs/ROADMAP.md](../ROADMAP.md)

## v6 现在的出发点

做完
`v5`
以后，
`mini-cloud`
已经有了：

- 一个只支持长期在线服务的多云 `CaaS`
  形态
- 全局 `control-plane`
- 多套真实 `cloud-plane`
- `service`
  资源模型
- 配置、密文配置和项目资源绑定
- 发布、回滚、扩容和放置
- front door
  路由模型
- 身份与授权基础
- 应用侧可观测性
- 真实单云和真实双云 `E2E`

但与此同时，
也暴露出了四个问题：

1. 控制链路还不够像正式 controller
   - 还有不少地方更像“写请求触发一次动作”
   - 不是持续对账和自然收敛
2. 内部信任链和安全治理还不够强
   - token / secret / bootstrap
     轮转不完整
   - `control-plane` 和 `cloud-plane`
     身份模型成熟度不一致
3. front door、
   发布和容量治理还不够像生产平台
   - 有能力
   - 但还不够稳
   - 也还不够可治理
4. 运维闭环还偏 operator workflow
   - 有脚本
   - 有演练
   - 但还没完全收成正式平台能力

所以：

- `v6`
  不再继续开新产品面
- `v6`
  也不再继续扩 workload
  类型

而是先把重点收回到：

- 生产化硬化
- 运维能力收口
- 更强的安全和可观测性

## v6 的一句话定位

如果只用一句话描述
`v6`，
我建议就是：

- 把 `mini-cloud`
  从“已经能跑通的多云 `CaaS`”
  推到“更可运营、更可收敛、更可恢复的多云 `CaaS`”

## v6 的明确目标

`v6`
现在要正式收成的是：

- 一个以 controller /
  reconcile
  为核心行为模型的平台
- 一个发布、切流、恢复和替换都更像生产平台的多云 `CaaS`
- 一套可以让 operator
  长期运行的平台能力：
  - 安全
  - 轮转
  - 维护
  - 升级
  - 恢复
  - 演练

这里要特别强调：

- `v6`
  不是继续加更多对象
- `v6`
  也不是开始做新平台产品线

这一版最关心的是：

- “现有主线是否已经足够稳、足够清楚、足够可信”

## v6 的核心设计

### 1. 平台行为要从“动作型”走向“收敛型”

`v6`
会把关键对象进一步收成：

- `spec`
- `status`
- `conditions`
- `observedGeneration`

也就是说，
平台不再只是在写请求到来时：

- 立刻做一次动作

而是要更明确地形成：

- 期望状态
- 当前状态
- 后台对账
- 漂移修复

这一点是
`v6`
成立的第一前提。

### 2. 文件只能是 bootstrap
载体，不能是运行时真相

到了现在这个阶段，
平台里已经同时存在：

- 仓库里的模板文件
- 机器上的 env /
  json
  部署文件
- 进程已经加载的配置
- `spec / status`
  和 provider
  inventory

如果这些东西同时都能表达“当前系统是什么样”，
平台就会很快失去 authority
边界。

所以
`v6`
后半段要明确收死：

- 文件只负责：
  - bootstrap
  - 进程启动
  - 本机部署边界
- 运行时真相必须回到：
  - API / store
    中的期望状态
  - 进程真实已加载配置
  - provider
    inventory
  - 可观测系统里的事实状态
- 不再接受：
  - 手改运行中机器上的文件
    就等于平台状态变更
- 任何会影响运行行为的配置变更，
  都必须有明确的：
  - 写入入口
  - 下发与生效生命周期
  - reload /
    restart
    语义
  - status
    反馈

这也是后面：

- 域名 /
  TLS /
  CDN
- 真实应用验收
- 长期运行排障

还能保持清楚的前提。

### 3. 服务拓扑要被明确建模

到
`v5`
结束时，
平台已经尝试过把
`service`
往“跨 `plane`
的全局逻辑对象”方向推进，
但这会让：

- 用户理解里的长期服务
- 控制面里的发布对象
- front door
  入口对象

三层语义混在一起。

所以
`v6`
要先把资源边界重新收干净：

- `service`
  = 单地域 /
    单 provider
    的长期运行单元
- `pinnedPlaneID`
  只是可选 pin
- 如果不 pin，
  placement
  也只能在这个运行范围里选一个 plane
- front door
  = 流量入口和路由门控对象
- 跨地域、多云主备、
  自动切换
  不再塞进
  `service`
  本体

也就是说，
`v6`
中段会先做一次明确的语义回收，
后面的章节再建立在这个更干净的模型之上。

### 4. 容量治理要变成 node pool
和 headroom

`v6`
不会满足于：

- “需要时再扩一台节点”

而是要把这件事正式收成：

- `runtime node pool`
- `min / max`
- warm headroom
- 供给状态视图
- 供给侧保留策略

这样：

- 发布
- 扩容
- 主备切换
- 节点替换

才会变得更可预测。

### 5. 控制逻辑要从“双脑”收回到单一 `control-plane`

做到
`v6/03`
这里，
平台虽然已经有：

- `control-plane`
  侧的全局对象和 controller
- `cloud-plane`
  侧的单云执行链路

但很多关键决策仍然是拆开的：

- 上层选一部分
- 下层再决定一部分

这会直接带来：

- placement
  判断来源不一致
- rollout
  推进者不唯一
- 容量动作和发布动作互相穿透

所以
`v6`
从这一章开始，
要正式把下面这件事收死：

- `control-plane`
  负责所有控制决策
- `cloud-plane`
  只负责状态汇聚和执行
- `agent`
  只负责节点侧落地

这是后面：

- 渐进发布
- front door
  路由发布 /
  门控
- 中央容量治理

能成立的前提。

### 6. 用户流量侧要从“可路由”走向“可发布、可门控、可诊断”

`v5`
已经有了：

- front door
  对象模型
- route
  reconcile
- per-service
  ingress /
  gateway
  适配

但到了
`v6`，
这条线还要继续往前走：

- 路由发布
- 健康门控
- route 状态与排障入口
- 路由一致性
- 证书和入口连续性

这里依然不做：

- `service mesh`

但要把：

- “用户流量在前门怎么走”

这件事做得更像真实平台。

### 7. 安全、轮转和 `day-2`
能力要成为正式产品能力

到了
`v6`，
这些不应该再只是：

- 附加脚本
- 一次性实验

而要成为正式能力：

- token / secret / 证书轮转
- 节点维护、替换、退役
- plane
  升级和恢复
- 备份、恢复、演练
- operator
  级 `SLO`
  和看板

## v6 的非目标

`v6`
现在明确不做下面这些事情：

- `job / cron / oneoff`
- 第三家 provider
- `service mesh`
- 有状态服务主线
- 对象存储、数据库等新产品线
- 支付、余额、账单
- 多活 `control-plane`
  共识系统

这些方向不是没有价值，
而是：

- 现在还不该压过
  `v6`
  的生产化硬化主线

## 新的章节顺序

`v6`
仍然不单独保留总览章。

原因和前面一样：

- `docs/v6/README.md`
  已经承担导航和总说明作用

所以正式章节仍然从：

- `01`

开始。

这里要特别说明：

- `06`
  不是在
  `05`
  之上继续加一个普通功能
- `06`
  是一次明确的资源模型回收
- 从
  `06`
  开始，
  后续章节都以：
  - 单地域 `service`
  - front door
    做流量层对象
  作为新的基础

### `01-controller-reconcile-model-v2`

- 把关键对象统一收成：
  - `spec`
  - `status`
  - `conditions`
  - `observedGeneration`
- 把当前仍然偏“请求触发动作”的行为，
  尽量改成更清楚的 controller /
  reconcile
  语义
- 这一章只处理：
  - 行为模型
  - 状态语义
  - controller
    契约

这里故意不把它和大规模目录清扫绑在一起，
避免一开始就做成一章过重的横切迁移。

这一章结束时，
平台最关键的行为模型必须先稳定下来。

### `02-service-topology-and-placement-policy-v2`

- 正式定义：
  - `service`
    的拓扑形态
- 至少收出：
  - 单 cell
  - 主备
  - 显式 cell
    选择
  - provider / region
    约束
- 同时明确：
  - `1 service -> N cell intents`
    的 control-plane
    目标形态
- 把放置策略和服务拓扑真正接起来
- 这一章形成的是：
  - 一个后续会被
    `06`
    明确回收的中间模型

这一章结束时，
平台要明确知道：

- 一个服务“允许存在在哪里”

### `03-runtime-node-pools-and-capacity-headroom`

- 把当前较分散的供给侧约束，
  收成：
  - `runtime node pool`
  - `min / max`
  - warm headroom
  - pool
    状态视图
- 让容量预留和供给状态
  真正成为一套治理模型
- 这一章先不做：
  - `control-plane`
    直接触发真实扩缩
  - 自动 scale-down
    回收执行

这一章结束时，
平台不能再只靠：

- “不够了就临时申请节点”

来表达容量。

### `04-centralized-control-plane-and-thin-cloud-plane-refactor`

- 正式把控制决策收回到：
  - `control-plane`
- 明确把 `cloud-plane`
  收成：
  - plane
    内执行面
  - inventory
    汇聚面
  - provider
    adapter
- 把下面这些能力上移到
  `control-plane`：
  - `plane`
    选择
  - `node`
    选择
  - rollout
    推进
  - runtime node
    容量动作决策
- 同时补上：
  - `syncVersion`
  - `nodeEpoch`
  - `planVersion`
  - `actionVersion`
    这组最小版本栅栏

这一章结束时，
平台不该再存在：

- `control-plane`
  和 `cloud-plane`
  同时推进控制逻辑

### `05-progressive-delivery-and-release-policy`

- 把发布能力从：
  - 基础发布
  - 回滚
  往前推进到：
  - `cell`
    级 candidate release
  - `release policy`
  - `rollout status`
  - pause / promote / abort
  - 固定回滚点
  - 自动 / 手工 promotion
- 这章仍然建立在
  `service -> cell`
  的中间模型上
- `06`
  会把这些发布语义重新绑定回
  单地域 `service`

这一章结束时，
平台的发布语义要明显更像正式平台，
而不是只会：

- 一次性替换

### `06-service-resource-model-single-region-refactor`

- 明确回收
  `02-05`
  中引入的：
  - `service -> cell`
    用户可见语义
- 把 `service`
  重新定义成：
  - 单地域
  - 单 provider
  - 长期运行服务
- `pinnedPlaneID`
  只表示可选的固定执行面
- 如果不显式 pin，
  `service`
  仍然只允许在自己的
  provider / region
  范围内解析出一个 active plane
- 把这些字段收回到
  `service`
  本体：
  - `provider`
  - `region`
  - `pinnedPlaneID`
  - `replicas`
  - `instanceClass`
- 把下面这些状态也统一收回到
  `service`
  级：
  - placement
  - rollout
  - readiness /
    health
- 节点 assignment
  留在内部执行面，
  不再作为用户主资源模型的一部分
- 明确从这一章开始：
  - 跨地域
  - 多云主备
  - 流量切换
  不再由
  `service`
  本体承载

这一章结束时，
平台的资源边界应该重新变得直观：

- `service`
  是 deployable unit
- front door
  才是流量层对象

### `07-front-door-route-publication-and-readiness-gating`

- 把 `v5`
  的 front door
  从“声明式挂载”
  推进到：
  - 路由发布
  - 健康门控
  - 路由收敛与排障入口
- 保持：
  - front door
  - `service backend`
    这个 front door
    内部绑定成员视图 /
    gateway
    两段模型
- 这一章只收：
  - 最小路由发布行为
  - 最小可执行的健康判定
  - 最小排障入口

更完整的：

- 指标
- 看板
- operator `SLO`

留到
`11`
统一产品化。

这一章结束时，
平台要真正具备：

- “前门该不该发布这条 route”
- “当前 route 为什么还没真正发布”
- “当前 route 当前解析到了哪个入口目标”

的更强控制力。

### `08-control-plane-single-active-deployment-and-recovery`

- 把 `control-plane`
  自己收成正式可部署单元
- 补齐：
  - 二进制 +
    `systemd`
    启动布局
  - 独立环境文件
  - 单活 `Postgres`
    依赖布局
  - 备份恢复
  - cold-standby /
    replace-in-place
    流程
- 但不做：
  - 多活 `control-plane`
- 这一章只解决：
  - `control-plane`
    自身的部署、
    持久化和存活性

`cloud-plane / node / service`
侧的 `day-2`
动作，
留到
`10`
去收。

这一章结束时，
`control-plane`
不该再只是：

- 本地实验入口

### `09-control-chain-security-and-token-rotation`

- 把当前已经存在、
  但成熟度不一致的几条
  `token`
  控制链收成闭环
- 这一章明确只收：
  - `control-plane`
    northbound
    `admin token / project token`
    语义
  - `control-plane -> cloud-plane`
    southbound
    `token`
    轮转入口
  - `cloud-plane -> agent`
    的
    `bootstrap token -> session token`
    链
  - front door
    adapter
    的最小鉴权
- 这一章明确不收：
  - `PKI / mTLS`
  - 证书续期
  - `Vault / KMS`
  - provider
    凭证体系大改
  - `secret / registry credential`
    完整生命周期

这一章结束时，
平台内部每一跳都要有：

- 明确 token
- 明确作用域
- 明确失效或轮转路径

### `10-day2-maintenance-upgrade-and-decommission`

- 把
  `day-2`
  里最危险、
  也最容易直接烧钱的一条链先收干净：
  - runtime
    资源退役
  - platform teardown
  - `terraform destroy`
    之间的 authority
    边界
- 这一章先不追求把所有升级、
  漂移治理、
  节点替换一次做完
- 这一章先把下面这条链做成正式语义：
  - runtime node
    `intent`
    先持久化
  - provider
    创建后再绑定实例身份
  - `cloud-plane teardown`
    以 provider
    inventory
    为准
  - destroy
    走
    fail-closed
    语义

这一章结束时，
平台必须具备下面这些能力：

- provider
  authoritative
  runtime inventory
- provider tag
  合同统一
- store
  inventory
  和 provider
  inventory
  的 reconcile
- orphan runtime
  可回收
- destroy
  失败时不会继续误删底座

也就是说，
这一章的重点不是：

- 把脚本重试得更久

而是：

- 把资源生命周期 authority
  改对

### `11-real-control-plane-and-first-production-plane`

- 先把平台真正落到一台长期在线的真实服务器上
- 当前明确采用：
  - 腾讯云 `2c4g`
    服务器承载
    `control-plane`
  - 同时承载首个长期运行的
    Tencent
    `cloud-plane`
    / front door
    / runtime
    入口
- 这一章不再接受：
  - 手工
    `go run`
  - 临时实验进程
  - 只靠当前 shell
    存活

这一章结束时，
必须至少具备：

- 正式的
  `systemd + env file + binary`
  部署方式
- 机器重启后自动恢复
- `control-plane`
  数据、配置和恢复入口可验证
- 首个 Tencent
  长期运行面里至少有一个稳定可调度节点

也就是说，
从这一章开始，
`mini-cloud`
要从“真实测试环境”正式进入：

- 真实长期运行环境

### `12-production-access-and-control-chain-security`

- 平台一旦真实暴露到公网，
  就不能再沿用教学期的：
  - 单一静态 token
  - 模糊的配置放置
  - 依赖 SSH
    才能做日常操作
- 这一章专门收：
  - operator
    访问入口
  - `control-plane -> cloud-plane`
    控制链身份
  - front door
    入口密钥和管理密钥
  - 恢复文件、
    备份文件和 break-glass
    入口

这一章结束时，
要明确固定：

- 管理入口应该怎么进
- 凭证应该放哪里
- 日常轮转怎么做
- 紧急恢复怎么做

这一章故意放在真实部署之后、
可观测性之前，
因为：

- 先把公网入口和控制链边界收干净，
  后面的长期运行才有意义

### `13-reliable-teardown-and-destroy-readiness`

- `10`
  已经把最危险的资源生命周期 authority
  改对了
- 但到了真实长期环境里，
  还需要把这条链进一步收成：
  - 正式退役能力
  - 正式 destroy
    前置条件
  - 正式 orphan
    治理能力
- 这一章要继续把下面这些东西收死：
  - `platform teardown`
    对 provider
    inventory
    为零负责
  - destroy
    必须经过
    destroy-ready
    校验
  - 失败时一律
    fail-closed
  - orphan runtime
    可以被发现、
    对账和回收

这一章结束时，
必须能比较自然地回答：

- 现在能不能安全退役这套平台
- 还有没有残留云资源
- 为什么这次 destroy
  被允许 /
  被拒绝

### `14-second-production-plane-on-aliyun`

- 到这一步，
  平台不再只是一台真实入口机
- 这一章把阿里云
  `2c2g`
  服务器正式接入成第二个长期运行面
- 这里故意不把它设计成：
  - 临时 worker
  - 只为某次实验存在的节点

而是明确把它收成：

- 第二个长期运行的
  `cloud-plane`
  / runtime
  入口

这一章结束时，
平台应该正式具备：

- 腾讯云长期运行面
- 阿里云长期运行面
- `control-plane`
  对双运行面的稳定纳管
- 服务显式运行到指定运行面的能力

这一章刻意不继续做：

- 跨 plane
  服务切换
- 双活 /
  主备流量
- 全局 destroy
  编排

### `15-production-observability-stack`

- 从这一章开始，
  `v6`
  不再只说“要可观测”，
  而是把真实生产观测栈正式落地
- 当前默认目标仍然是：
  - `Prometheus`
    做 metrics
  - `Loki`
    做 logs
  - `Tempo`
    做 traces
  - `Alloy`
    做统一采集 /
    转发 /
    pipeline
  - `Grafana`
    做统一查询和 dashboard

但这章从一开始就不采用：

- 每个 `cloud-plane`
  一整套
  `Prometheus / Loki / Tempo / Grafana`
  再做聚合
- 中心直接跨云去抓所有
  `worker`
  指标

这一章明确采用下面这条分层设计：

- `worker`
  - 继续跑 `agent`
  - 应用 stdout/stderr
    先推到本 `plane`
    的日志接入口
  - 应用 `OTLP`
    先送到本 `plane`
    的采集入口
- `cloud-plane`
  - 跑 `Alloy`
  - 聚合本 `plane`
    的平台日志和workload 日志
  - 再跑一个小的
    `OTel Collector`
  - 接应用
    `OTLP metrics / traces`
  - 再跑一个小的
    `Prometheus`
  - 抓本 `plane`
    的
    `cloud-plane /metrics`
  - 抓本 `plane`
    的
    `OTel Collector /metrics`
  - 需要时再补抓
    私网 `worker /metrics`
- `control-plane`
  所在节点
  - 跑中心
    `Prometheus`
  - 通过 federation
    抓每个
    `plane`
    的
    `Prometheus`
  - 同时跑：
    - `Loki`
    - `Tempo`
    - `Grafana`
    - 告警组件

也就是说，
这一章明确收成：

- `node`
  侧采集
- `plane`
  侧聚合 /
  抓取
- `control-plane`
  节点侧统一存储 /
  查询 /
  告警

这里还要特别固定一条网络边界：

- 每个云里默认只有
  `cloud-plane`
  所在节点带公网入口
- `worker`
  默认不带公网
  IP
- 所以：
  - logs /
    app traces
    通过
    `worker -> plane -> center`
    上送
  - metrics
    通过
    `plane Prometheus -> center Prometheus federation`
    汇总

但这一章必须明确接受现实约束：

- 这不是大集群
- 这是小规格长期服务器
- 所以：
  - retention
    要保守
  - 采样要克制
  - 存储占用要可控
  - 平台侧与应用侧观测都要分层设计

这一章结束时，
至少要完成：

- 双运行面的 metrics
  分层抓取和中心 federation
- 双运行面的日志聚合
- 双运行面的 trace
  汇聚
- operator
  常用 dashboard
- 最小告警包

### `16-runtime-truth-config-and-state-convergence`

- `15`
  已经先把真实观测面搭起来了
- 所以下一章不应该立刻接域名 /
  CDN
- 而是先把：
  - 谁才是 authoritative
    state
  - 文件和运行态的边界
  - 配置如何进入 reconcile
  - 进程如何报告自己真正加载了什么
  这条链收干净

这一章明确要解决的不是：

- “把文件模板写得更全”
- “把脚本再包一层”

而是下面这些根问题：

- repo
  模板、机器上的 env /
  json
  文件、运行态状态彼此重叠
- 手工改文件和 API /
  controller
  变更容易混成一件事
- `cloud-plane / agent / observability`
  的运行配置，
  缺少统一的 authority
  和收敛反馈

这一章会明确分出三类东西：

- bootstrap
  文件
  - 只负责进程启动、
    身份和最小连接信息
- 平台配置对象
  - 通过正式 API /
    store
    表达期望状态
- 运行态观测事实
  - 通过进程状态、
    provider inventory、
    metrics / logs
    反映真实加载和真实生效结果

这一章至少要收成：

- `cloud-plane`
  和相关本机文件的最小化边界
- 需要热更新的配置项、
  需要重启的配置项、
  完全只允许 bootstrap
  指定的配置项
- 配置版本 /
  配置哈希 /
  observed config
  之类的最小状态反馈
- 文件与运行态不一致时的判定原则
- operator
  修改配置时的正式入口

这一章明确不做：

- 分布式配置中心
- 新的持久化系统
- 复杂动态插件系统
- 全量 secret
  生命周期重构

这一章结束时，
平台应该能比较明确地回答：

- 哪些文件只是启动材料
- 哪些东西才是平台真实配置
- 进程当前到底加载了哪一版配置
- 为什么某个配置改动还没真正生效

### `17-control-plane-northbound-grpc-and-operator-tui`

- 到
  `v6/16`
  为止，
  平台真正给 operator
  用的入口还是一组：
  - 手写 `HTTP`
    handler
  - 很薄的 `React`
    控制台
- 这套东西能用，
  但对当前项目不再合适：
  - 用户平时主要在命令行工作
  - 后端主链已经是 `Go`
  - `cloud-plane`
    侧已经有明确的 `proto + gRPC`
    契约
  - 继续维护一套 `TS/React`
    operator UI
    的收益很低

所以这一章要明确做一次收口：

- `control-plane`
  operator northbound
  收成：
  - `proto + gRPC`
  - 必要时继续保留
    `grpc-gateway`
    作为调试 /
    脚本 /
    集成入口
- 新增：
  - `cmd/tui`
  - 基于 `Go + Bubble Tea`
  - 作为新的 operator
    主入口
- 不再继续把 `web/`
  当成后续主交互面

这一章第一版只做最核心的 operator
闭环：

- overview
- planes
- projects
- services
- rollout
  基础动作

这里故意不把：

- 复杂前端设计
- 用户侧 HTTP
  门户
- 域名 /
  CDN
  入口

混进这一章。

这一章结束时，
平台应该至少具备：

- 一组清楚的 `control-plane`
  operator gRPC
  契约
- 一个可以直接跑起来的
  `cmd/tui`
- 不依赖 `React`
  页面，
  也能完成核心 operator
  工作流

### `18-config-and-secret-file-projection`

- 这一章不扩成完整 volume
  系统
- 只补两条当前最缺、也最干净的能力：
  - 把项目里的 config / secret
    键投影成容器内只读文件
  - 给单副本长期服务提供最小可写持久目录
- northbound
  `service spec`
  要能声明：
  - `projectedFiles`
    挂到哪里、
    来自哪类资源、
    来自哪个资源、
    取哪个 key
  - `persistentDirs`
    目录名和容器内挂载点
- control-plane
  负责：
  - 校验 source
    是否存在
  - 校验
    `persistentDirs`
    的 name /
    mountPath /
    overlap
    规则
  - 跨 plane
    资源同步时把本地资源 id
    重写成远端资源 id
- cloud-plane
  负责：
  - 把 projected file
    纳入 revision
    快照
  - 在 execution
    claim
    时物化成真正的运行时文件内容
  - 把 persistent dir
    纳入 service /
    revision /
    execution
  - 对带
    `persistentDirs`
    且已经产生
    current /
    candidate revision
    的 service，
    拒绝 revision-changing update，
    避免 candidate rollout
    争用同一个可写目录
    语义
- agent / runtime
  负责：
  - 在本机临时目录写出文件
  - 只读 bind mount
    到容器目标路径
  - 在容器停止后清理临时目录
  - 在节点本地固定根目录创建持久目录
  - 可写 bind mount
    到容器目录
  - 当前不自动清理持久目录
- 这一章明确不做：
  - 通用 volume DSL
  - hostPath
  - 跨 node
    自动迁移持久目录
  - 跨 plane
    自动迁移持久目录
  - 多副本共享可写卷
  - 运行中热更新文件

这一章结束时，
平台应该至少具备：

- `service`
  可以声明 projected files
- `service`
  可以声明 persistent dirs
- config / secret
  的 key
  可以以只读文件形式进入容器
- revision /
  execution
  已经使用 revision-scoped
  文件快照语义
- 单副本长期服务
  可以挂一个节点本地持久目录
  - 且在已有 release
    后不会自动迁移到别的 plane /
    node
- `TUI`
  创建服务时，
  可以录入 projected files /
  persistent dirs
  规则

### `19-domain-tls-and-tencent-cdn`

- 这一步开始把平台真正接到用户入口面上
- 用户后续会购买域名，
  并接入腾讯云 CDN
- 这一章明确只做：
  - 域名
  - TLS
  - CDN
  - 源站 /
    回源 /
    缓存 /
    失效
    这条链

这一章故意放在完整可观测之后，
因为如果没有前一章：

- 源站故障
- CDN
  缓存异常
- 回源头问题
- 证书问题

会非常难排障

这一章结束时，
至少要具备：

- 真正可访问的正式域名入口
- CDN
  与源站的清晰边界
- 绕过 CDN
  直查源站的调试路径
- 发布 /
  回滚 /
  路由 /
  缓存失效
  的基础验证能力

### `20-background-controller-error-boundary-and-degraded-health`

- 这一章不继续扩新产品面
- 而是回过头把
  `cloud-plane`
  后台控制器的错误边界收干净
- 当前最直接的问题是：
  - gateway
    后台 reconcile
    的一次运行时错误
    会把整个进程退出
- 这不符合生产语义
  - 启动期错误当然应该 fatal
  - 但后台 controller
    应该是：
    - 日志
    - 状态
    - 重试
    - 降级

这一章至少要完成：

- `gateway reconciler`
  默认不因运行期错误退出
- 增加最小 controller
  运行状态：
  - `ok`
  - `degraded`
  - `lastAttemptAt`
  - `lastSuccessAt`
  - `lastError`
  - `consecutiveFailures`
- `/api/healthz`
  接入 gateway
  状态
- plane snapshot
  的 `health.service`
  也把 gateway degraded
  计入
- 保留 strict
  开关，
  供调试和测试保留 fail-fast
  语义

这一章结束时，
平台应该已经具备：

- 启动期 fatal
- 后台错误降级
- 健康面暴露
- 自动重试

这条更像正式控制器的错误语义链。

### `21-open-source-app-validation-and-gameday`

- 最后一章不再只验证平台自己
- 而是部署一个真正可运行的开源项目
  来做整个平台的真实验收
- 这一章至少覆盖：
  - 镜像拉取
  - 配置与密文
  - 发布
  - 升级
  - 回滚
  - 扩缩
  - 日志 /
    指标 /
    trace
  - 域名 /
    CDN
    访问
  - 恢复 /
    回收 /
    destroy

这一章结束时，
`v6`
应该真正完成一条完整的生产化验收链：

- 两台长期入口机
- 一个真实长期运行的
  `control-plane`
- 双运行面
- 完整观测
- 正式域名与 CDN
- 真实开源项目验证
- 最终 runbook
  与 gameday
  场景

## 为什么不选其他方向当 v6 主线

### 不选“继续扩 workload 类型”

因为当前真正缺的不是：

- 更多对象

而是：

- 让现有长期在线服务主线更稳

### 不选“现在做第三家 provider”

因为：

- provider
  数量继续增加，
  只会把当前更关键的生产化问题继续摊薄

### 不选“现在做 `service mesh`”

因为：

- `mesh`
  会立刻把主线拉成：
  - 数据面治理
  - sidecar
  - 东西向流量

而不是：

- 多云 `CaaS`
  的生产化主线

### 不选“现在做支付 / 数据库 / 对象存储”

因为：

- 这些都是新产品面
- 它们现在会明显压过：
  - 发布
  - 切换
  - 安全
  - `day-2`
  - 可观测性

## 验证标准

`v6`
这一版如果做对了，
最后应该能比较自然地回答下面这些问题：

1. 一个服务是不是已经能声明清楚：
   - 它运行在哪个地域 /
     plane
   - 发布策略
2. 发布是不是已经能：
   - 渐进推进
   - 遇到健康问题暂停或回滚
3. front door
   是不是已经能根据健康和绑定关系：
   - 做路由收敛
   - 做 route 发布与阻塞诊断
4. token / secret / 证书
   是不是已经能更自然地轮转
5. 节点排空、替换、退役和 plane
   升级，
   是不是已经有正式流程
6. operator
   是不是已经能通过统一指标、日志和告警，
   看清平台行为是否达标
7. 最后一条完整 gameday
   链路，
   是不是已经能在双云环境里真正跑完
