# Mini Cloud v2

`v2` 不再只是占位。

从现在开始，
它会是一条明确的新主线：

- 把 `mini-cloud`
  - 从“本地能跑通主链的最小平台骨架”
  - 推进到“能在阿里云里自动创建平台自有资源并完成安装的单 provider 项目”

## v2 的核心目标

`v2` 最重要的不是立刻去做：

- 第二家 provider
- 高可用 control-plane
- 很重的云厂商产品面

而是先把这些事情做完整：

1. 给定一台带 `RAM` 权限的 seed 执行机
2. `bootstrap` 自动创建平台自己的阿里云资源
3. `bootstrap` 自动安装并启动平台
4. worker 自动纳管
5. 应用安全发布与回滚
6. 域名、入口和 `TLS`
7. 告警、runbook、备份和恢复
8. 一套完整模拟环境 example

这里要明确分成两层：

1. `bootstrap layer`
   - 负责创建平台自有资源和首次安装
2. `platform layer`
   - 负责平台启动后的应用托管和运维

## v2 的非目标

`v2` 暂时不做：

- 真正接入第二家 provider
- 多副本 control-plane
- 全面迁移到 `k3s` / Kubernetes
- 完整 IAM
- 平台根据租户或应用需求自动创建独立云资源
- 账单和支付系统
- 托管数据库、对象存储等新产品线

其中：

- 平台按用户需求自动创建独立资源
  - 明确留给：
  - `v3`

## 当前章节规划

`docs/v2/README.md`
本身就负责：

- 导航
- 总说明

所以 `v2`
的正式章节直接从：

- `01`

开始。

- `01-v2-scope-and-target-architecture`
- `02-aliyun-bootstrap-inputs-and-resource-graph`
- `03-bootstrap-resource-provisioning`
- `04-platform-host-provisioning-with-instance-role-and-eip`
- `05-managed-worker-registration-and-platform-topology`
- `06-secrets-config-and-image-credentials`
- `07-safe-deployment-and-rollback`
- `08-gateway-tls-and-domain-flow`
- `09-auth-api-token-and-project-access`
- `10-operation-history-audit-and-basic-rbac`
- `11-alerting-slo-and-runbooks`
- `12-backup-restore-and-disaster-drills`
- `13-complete-simulated-environment-example`
- `14-real-aliyun-end-to-end-lab`
- `15-bootstrap-reinstall-upgrade-and-destroy`
- `16-v2-hardening-and-docs-polish`
- `17-v2-final-review`

## 当前进度

目前已经开始：

- `01-v2-scope-and-target-architecture`
- `02-aliyun-bootstrap-inputs-and-resource-graph`
- `03-bootstrap-resource-provisioning`
- `04-platform-host-provisioning-with-instance-role-and-eip`
- `05-managed-worker-registration-and-platform-topology`
- `06-secrets-config-and-image-credentials`
- `07-safe-deployment-and-rollback`
- `08-gateway-tls-and-domain-flow`
- `09-auth-api-token-and-project-access`
- `10-operation-history-audit-and-basic-rbac`
- `11-alerting-slo-and-runbooks`
- `12-backup-restore-and-disaster-drills`
- `13-complete-simulated-environment-example`
- `14-real-aliyun-end-to-end-lab`
- `15-bootstrap-reinstall-upgrade-and-destroy`
- `16-v2-hardening-and-docs-polish`

## 这一版应该怎么理解

如果说 `v1`
解决的是：

- “平台骨架能不能成立”

那么 `v2`
要解决的就是：

- “这个平台能不能先把自己安装到阿里云上，再稳定部署、运维和恢复”

所以 `v2`
依然会先保持：

- 阿里云单 provider
- 单 active control-plane

先把平台自安装和真实运维问题处理干净，
再为后面版本留出扩展点。

同时，
`v2`
还会显式保留两条完整 example：

1. 完整模拟环境
   - 低成本
   - 可重复
   - 适合教学和回归
2. 真实阿里云环境
   - 用来验收：
   - `bootstrap -> 安装 -> 纳管 -> 发布`

更细的章节规划以：

- `projects/mini-cloud/docs/v2/ROADMAP.md`

为准。

## 当前对后续技术演进的判断

### 高概率会在 `v2` 或 `v3` 引入

- 更正式的认证系统
- 更正式的入口层
- 更清晰的审计与运维事件模型

### 更可能留到 `v3` 之后

- 第二家 provider 的真实接入
- provider 抽象层
- 平台按用户需求自动创建独立资源
- 多副本 control-plane
- 更复杂的调度策略
- 更正式的计费系统

### 后面可能会评估

- `k3s` / Kubernetes 底座
- `Redis`

### 更晚再看

- `gRPC`
- 消息队列

### 大概率不会成为核心方向

- ORM
- 重前端优先
