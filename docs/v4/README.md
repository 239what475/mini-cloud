# Mini Cloud v4

`v4`
不再继续围着“单 provider control-plane 本身”打转。

从这一版开始，
`mini-cloud`
更自然的下一步是：

- 保持每套 `cloud plane`
  继续各自绑定单一 provider
- 在上面增加一个全局 `control plane`
- 再把日志、指标、告警、审计、事故处理和恢复能力补成真正的全局运维面

也就是说，
`v4`
真正要推进的是：

- three-layer control model
- global observability
- practical HA

而不是：

- 一个进程里同时混着：
  - `control plane`
  - `cloud plane`
  - `cloud worker`

## 当前规则

- `docs/v4/ROADMAP.md`
  - 负责 `v4` 的总规划
- `docs/v4/01-fleet-manager-and-resource-model.md`
  - 负责 `v4/01` 的 fleet 资源模型与边界
- `docs/v4/02-plane-registration-and-sync.md`
  - 负责 `v4/02` 的 plane 注册、心跳与快照同步
- `docs/v4/03-global-inventory-and-capacity-view.md`
  - 负责 `v4/03` 的全局资源视图
- `docs/v4/04-structured-logging-foundation.md`
  - 负责 `v4/04` 的结构化日志模型
- `docs/v4/05-log-aggregation-and-query.md`
  - 负责 `v4/05` 的日志聚合与检索
- `docs/v4/06-global-metrics-alerting-and-slo.md`
  - 负责 `v4/06` 的全局指标、告警与 `SLO`
- `docs/v4/07-fleet-deploy-api-manual-target.md`
  - 负责 `v4/07` 的按目标 plane 下发部署
- `docs/v4/08-fleet-placement-policy.md`
  - 负责 `v4/08` 的基础自动放置策略
- `docs/v4/09-three-layer-architecture-and-boundaries.md`
  - 负责 `v4/09` 的三层边界、资源归属、API 边界与目标结构
- `docs/v4/10-full-three-layer-refactor-and-clean-cutover.md`
  - 负责 `v4/10` 的彻底重构、包清扫与新架构切换
- `docs/v4/11-maintenance-drain-and-incident-ops.md`
  - 负责 `v4/11` 的维护模式、排空与事故流
- `docs/v4/12-auth-rbac-and-audit.md`
  - 负责 `v4/12` 的认证、`RBAC` 与审计
- `docs/v4/13-backup-restore-and-ha-drills.md`
  - 负责 `v4/13` 的备份、恢复、standby 与演练
- `docs/v4/14-real-dual-plane-end-to-end.md`
  - 负责 `v4/14` 的真实双 plane 验收
- 每一章结束时仍然单独做一个 `commit`
  - 作为章节检查点

## 当前章节

- `01-fleet-manager-and-resource-model`
- `02-plane-registration-and-sync`
- `03-global-inventory-and-capacity-view`
- `04-structured-logging-foundation`
- `05-log-aggregation-and-query`
- `06-global-metrics-alerting-and-slo`
- `07-fleet-deploy-api-manual-target`
- `08-fleet-placement-policy`
- `09-three-layer-architecture-and-boundaries`
- `10-full-three-layer-refactor-and-clean-cutover`
- `11-maintenance-drain-and-incident-ops`
- `12-auth-rbac-and-audit`
- `13-backup-restore-and-ha-drills`
- `14-real-dual-plane-end-to-end`

## 这一版怎么理解

如果说：

- `v3`
  解决的是：
  - 单 provider control-plane 能不能成为正式平台

那么：

- `v4`
  解决的就是：
  - 三层模型能不能真正收干净
  - 多套独立 `cloud plane` 能不能被统一纳管
  - 能不能有全局可观测性和运维面
  - 能不能做务实而清晰的恢复与高可用演练

这里最重要的工程边界是：

- `control plane`
  不直接替代现有 `cloud plane`
- `cloud plane`
  继续保留自己的 provider 绑定和运行时逻辑
- `control plane`
  主要负责：
  - 注册
  - 观察
  - 下发
  - 聚合
  - 运维

## 当前对技术方向的判断

### 高概率会在 `v4` 正式进入主线

- 多 control-plane / fleet 模型
- 更完整的日志聚合与检索
- 全局指标、告警和 `SLO`
- 更清晰的审计与事故事件模型
- 务实版高可用：
  - 备份
  - 恢复
  - standby
  - 演练

### 更可能留到 `v4` 之后

- 跨 plane 自动流量切换
- 跨 provider 统一调度
- 更复杂的租户、计费和配额系统
- 第三家 provider

### 这版仍然不急着做

- `k3s` / Kubernetes 运行底座迁移
- `Redis`
- `gRPC`
- 消息队列

这些方向不是没有价值，
而是现在还不该压过：

- 控制链路清晰
- 观测面完整
- 恢复路径可信
- 包和进程边界干净

更细的章节规划以：

- [projects/mini-cloud/docs/v4/ROADMAP.md](./ROADMAP.md)

为准。
