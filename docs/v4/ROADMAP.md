# Mini Cloud v4 路线图

回到总路线图：

- [projects/mini-cloud/docs/ROADMAP.md](../ROADMAP.md)

## v4 现在进入了一个新阶段

做完 `v4/08`
以后，
`mini-cloud`
已经把下面这些能力先跑起来了：

- fleet 资源模型
- plane 注册与同步
- 全局 inventory
- 全局日志、指标、告警
- manual target deploy
- explainable auto placement

但与此同时，
也暴露出了一个更根本的问题：

- 当前实现里的“全局层”和“单云层”还混在同一个进程里
- `internal/`
  下的包也还没有真正按层拆开

所以从现在开始，
`v4`
不能再简单沿着原顺序继续堆：

- maintenance
- `RBAC`
- backup / restore

而应该先把层次设计和彻底重构做对。

## v4 的明确目标

`v4`
现在要正式收成的是：

- 一个三层模型的 `mini-cloud`

不是：

- 一个进程里既当 fleet，
  又当单 provider control-plane

这里的三层明确改成：

1. `control plane`
   - 全局入口
   - 负责多套 `cloud plane`
     的注册、观察、绑定、选址、审计、事故和全局 `SLO`
   - 不直接碰云厂商 SDK
   - 不直接管 worker
2. `cloud plane`
   - 单 provider、单 region 的控制面
   - 负责本地 app / deployment / node / runtime worker / gateway / provider API
   - 对上暴露统一 plane API
   - 对下调度 `cloud worker`
3. `cloud worker`
   - 数据面执行节点
   - 负责注册、心跳、拉取 work、执行容器和回报结果

也就是说，
`01` 到 `08`
做出来的 fleet / plane / worker 能力不会推倒重来，
但从 `09`
开始，
要先把它们正式收敛到这套三层模型里。

## 为什么这里必须先停下来重构

后续这些能力都强依赖层次边界：

例如：

- maintenance / drain
  到底是 `control plane`
  动作还是 `cloud plane`
  动作
- `RBAC`
  到底是全局角色，
  还是单 plane 角色
- backup / restore
  到底备份哪一层数据库
- dual-plane 验收
  到底是在验证三层拓扑，
  还是在验证“上两层混住”的旧形态

所以从工程顺序上，
现在最合理的做法不是继续做原来的功能章，
而是先插入：

- 一章设计定界
- 一章彻底重构和清扫

然后再继续原来的功能线。

## v4 的非目标

`v4`
现在仍然不做下面这些事情：

- 一个 `cloud plane`
  直接跨多 provider 管运行时资源
- 跨 plane 自动流量切换
- 更复杂的全局调度器
- 更正式的计费系统
- 第三家 provider
- `k3s` / Kubernetes 运行底座迁移
- 很重的前端优先路线

这些方向不是不做，
而是现在还不该压过：

- 三层边界清楚
- 控制链路清楚
- 恢复路径可信

## 新的章节顺序

`v4`
仍然不单独保留总览章。

原因和前面一样：

- `docs/v4/README.md`
  已经承担导航和总说明作用

所以正式章节仍然从：

- `01`

开始，
但 `09`
之后的顺序要整体调整。

### `01-fleet-manager-and-resource-model`

- 新增 fleet 层最小资源模型
- 至少收出：
  - `plane`
  - `plane_status`
  - `capacity_snapshot`
  - `incident`
  - `operation_event`
- 明确 fleet 和 plane 的边界：
  - fleet 不直接替代 plane
  - fleet 负责纳管、聚合和下发

### `02-plane-registration-and-sync`

- 让现有阿里云 / 腾讯云 plane 可以注册到 fleet
- 补齐：
  - plane bootstrap token
  - 连通性校验
  - 周期心跳
  - 快照同步
- 至少同步：
  - provider
  - region
  - health
  - node/app/deployment 摘要

### `03-global-inventory-and-capacity-view`

- 在 fleet 里统一查看多套 plane 的：
  - provider
  - region
  - nodes
  - apps
  - deployments
  - worker capacity
  - health
- 这一章重点是全局只读资源视图，
  不是下发动作

### `04-structured-logging-foundation`

- 把 fleet、plane、agent、gateway、runtime worker 的日志字段统一
- 至少强制带上：
  - `plane_id`
  - `request_id`
  - `project_id`
  - `app_id`
  - `deployment_id`
  - `node_id`
  - `worker_id`
- 把“日志能不能串起来看”这件事先做对

### `05-log-aggregation-and-query`

- 引入日志聚合层
- 第一版优先用：
  - `Loki`
- 把这些日志真正收进来：
  - fleet
  - control-plane
  - agent
  - gateway
  - `cloud-init`
  - runtime worker 生命周期
- 让你能按：
  - plane
  - app
  - deployment
  查日志

### `06-global-metrics-alerting-and-slo`

- 把 metrics 和 alert 面拉到 fleet 级
- 第一版优先用：
  - `Prometheus`
  - `Alertmanager`
  - `Grafana`
- 至少补齐下面这些全局告警：
  - plane offline
  - deployment stuck
  - worker register failure
  - provider API failure
  - gateway error ratio
- 继续收出全局和单 plane 两级 `SLO`

### `07-fleet-deploy-api-manual-target`

- 让 fleet 先具备最小可用部署入口
- 第一版先只支持：
  - 明确指定目标 plane
- 不急着自动选 plane
- 重点先把：
  - fleet -> plane
  - app create/update
  - 状态回读
  这条动作链打通

### `08-fleet-placement-policy`

- 在 `07`
  的基础上再补一个简单自动选择策略
- 第一版先按：
  - provider
  - region
  - plane health
  - 剩余容量
  做选择
- 这里仍然不是全局复杂调度器

### `09-three-layer-architecture-and-boundaries`

- 正式把当前实现收成三层：
  - `control plane`
  - `cloud plane`
  - `cloud worker`
- 明确每层的：
  - 资源归属
  - northbound / southbound API
  - 凭证边界
  - 数据库边界
  - 可观测性字段边界
- 重新设计：
  - `cmd/`
  - `internal/`
  的目标结构
- 这一章只负责把最终形态和禁止事项定死，
  不讨论兼容迁移路径

### `10-full-three-layer-refactor-and-clean-cutover`

- 按 `09`
  定下来的边界，
  做一次不留兼容包袱的彻底重构和清扫
- 这一章直接把现有实现重组为：
  - 全局 `control plane`
  - 单云单 region 的 `cloud plane`
  - `cloud worker`
- 至少要完成：
  - 进程装配重做
  - 路由入口重做
  - `cmd/` 结构重做
  - `internal/` 包边界重做
  - northbound / southbound API 分离
  - 状态读写归属重做
  - 旧 handler / service / DTO / 命名清理
- 这一章明确不追求：
  - 新功能扩张
  - 新 provider 扩展
  - 新 UI 能力
- 这一章结束时，
  目标就是让仓库和运行形态都只剩新架构，
  后续章节不再背旧设计包袱

### `11-maintenance-drain-and-incident-ops`

- 把运维动作补成正式模型
- 至少包括：
  - plane maintenance mode
  - plane drain
  - 停止接收新部署
  - incident 建立 / 更新 / 关闭
  - runbook 链接
- 让事故流和运维流真正进入系统

### `12-auth-rbac-and-audit`

- 先不急着上重登录系统
- 这一章先收口最小正式权限模型：
  - `control plane` 的 service token
  - project-scoped `RBAC`
  - northbound audit 闭环
- 重点先解决：
  - 谁是谁
  - 谁能访问哪个 project
  - 越权时系统会不会明确拒绝并留下审计
- 这一章先不追求：
  - `OIDC`
  - 人类用户 / 组织 / 成员模型
  - 复杂角色矩阵

### `13-backup-restore-and-ha-drills`

- 这一章不先做很重的分布式一致性高可用
- 先做务实版：
  - `control plane` DB 备份恢复
  - `cloud plane` 重新接入
  - standby / 重装
  - 失效演练
- 这一章真正要回答的是：
  - 出问题以后，能不能用清晰步骤把系统拉回来

### `14-real-dual-plane-end-to-end`

- 跑一轮真实双 plane 验收
- 至少包括：
  - 一套阿里云 `cloud plane`
  - 一套腾讯云 `cloud plane`
  - 注册到同一个 `control plane`
  - 全局 inventory 可见
  - 全局日志和指标可见
  - 下发部署到指定 plane
  - 简单自动放置
  - 模拟单 plane 故障
  - 告警触发
  - 恢复路径验证

## v4 的验证策略也要跟着调整

`v4`
的验证不能只看单元测试，
因为这一版同时涉及：

- `control plane`
- `cloud plane`
- `cloud worker`
- logs / metrics / alerts
- 真实阿里云和腾讯云

所以验证至少分成五层。

### 第一层：本地日常检查

- `go test ./...`
- `go vet ./...`
- `staticcheck ./...`
- 前端 `npm run check`
- 前端 `npm run build`

### 第二层：control-plane / cloud-plane 合同测试

- plane 注册
- plane 心跳
- inventory snapshot 同步
- fleet deploy API
- placement policy
- 三层边界合同
- incident / audit / auth contract

### 第三层：日志与指标链路测试

- 结构化日志字段检查
- 日志聚合回读
- 核心指标暴露
- alert rule 触发
- `SLO` 计算与展示

### 第四层：本地双 plane 模拟验收

- 起两套本地或半模拟 `cloud plane`
- 注册到同一个 `control plane`
- 跑：
  - inventory
  - deploy
  - incident
  - restore

### 第五层：真实双 plane 验收

- 一套真实阿里云 `cloud plane`
- 一套真实腾讯云 `cloud plane`
- 注册到同一个 `control plane`
- 验证：
  - 全局管理面
  - 全局观测面
  - 手工和自动两种部署入口
  - 事故与恢复动作

## v4 的技术选型判断

这里先把最核心的选型判断定下来，
避免后面每章又重新讨论。

### 高概率进入主线

- `Prometheus`
- `Alertmanager`
- `Grafana`
- `Loki`
- `OIDC`

### 可以晚一点再看

- tracing / `OpenTelemetry`
- `Redis`
- `gRPC`
- 消息队列

这些能力可能后面会有价值，
但 `v4`
当前更重要的是：

- 三层边界是否清楚
- 日志能不能串起来
- 指标能不能聚起来
- 告警能不能真正触发
- 故障能不能真正恢复

## v4 之后的判断

如果 `v4`
把：

- 三层架构
- 全局观测运维面
- 务实高可用

都做稳了，
那更自然的下一步才会是：

- 更高阶的跨 plane 发布与流量编排
- 更复杂的多 provider / 多 region 策略
- 更正式的租户、配额和计费模型
- 第三家 provider

也就是说：

- `v4`
  先把“正式三层平台”做出来
- `v5`
  再去做更高阶的“多云平台”能力
