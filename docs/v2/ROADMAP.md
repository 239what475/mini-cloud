# Mini Cloud v2 路线图

回到总路线图：

- [projects/mini-cloud/docs/ROADMAP.md](../ROADMAP.md)

## v2 总体判断

做完 `v1` 以后，
`v2` 最合理的目标不是马上变成：

- 真正的多云平台
- 多副本高可用控制面
- 很重的“云厂商级”产品面

而是先把 `mini-cloud`
做成一个：

- 完整的阿里云单 provider 项目

这里的“完整”，
不是指功能无限扩张，
而是指它已经具备：

- `bootstrap`
- 自动创建平台自有基础设施
- 自动安装平台
- 真实环境运维
- 完整模拟环境 example

也就是说，
`v2` 的关键词应该是：

- 单 provider
- `bootstrap`
- 平台自有基础设施
- 真实环境
- 完整模拟环境
- 部署与升级
- 入口与 `TLS`
- 运维与恢复

## v2 的明确目标

`v2` 这一版要优先做成的，
应该是下面这件事：

- 给定一台带阿里云 `RAM` 权限的执行机
  - 可以是本机
  - 也可以是一台 seed `ECS`
- 由 `bootstrap`
  - 自动创建 `mini-cloud` 自己需要的阿里云资源
  - 再自动把平台安装上去

这里要明确分成两层：

1. `bootstrap layer`
   - 负责创建和更新平台自有阿里云资源
   - 负责首次安装、重装、升级、销毁
2. `platform layer`
   - 负责平台启动后的应用托管、节点管理、发布和运维

在这个目标里，
最重要的是把这些问题做完整：

1. `bootstrap` 需要哪些最小输入
2. 平台自有资源图怎么定义
3. 怎样自动创建：
   - `VPC`
   - `vSwitch`
   - `SecurityGroup`
   - `EIP`
   - platform `ECS`
   - first worker `ECS`
4. 这些资源怎样做到可重试、可重装、可销毁
5. control-plane 怎样被自动安装、配置和升级
6. worker 怎样被自动纳管
7. 应用怎样安全发布、回滚和恢复
8. 域名和 `TLS` 怎样正式接入
9. 日志、指标、告警和 runbook 怎样形成闭环
10. 状态数据库怎样备份和恢复
11. 要有一套可重复、可故障注入、可离线学习的完整模拟环境

所以 `v2`
应该明确保持两条验证主线：

1. 完整模拟环境
   - 稳定
   - 低成本
   - 可重复
   - 适合作为教学 example 和回归基线
2. 真实阿里云环境
   - 用来验收：
   - `bootstrap -> 安装 -> 纳管 -> 发布`

## v2 的非目标

`v2` 暂时不把下面这些问题拉进主线：

- 真正接入第二家 provider
- 做成高可用多副本 control-plane
- 全面迁移到 `k3s` / Kubernetes
- 完整 IAM / 多租户权限体系
- 平台根据租户或应用需求自动创建独立云资源
- 平台自带镜像仓库
- 平台内建镜像上传和镜像构建流程
- 支付、账单、充值、发票
- 托管数据库、对象存储这类独立云产品

这些方向都不是不做，
而是：

- 不应该在 `v2` 一开始就把问题摊太大

其中最重要的一条边界是：

- `v2`
  - 只自动创建平台“自己”的基础设施
- `v3`
  - 再去做平台根据用户需求自动创建独立资源

## v2 章节规划

`v2`
不再单独保留一个：

- 总览章

因为：

- `docs/v2/README.md`
  - 已经承担导航和总说明作用

所以正式章节直接从：

- `01`

开始。

### `01-v2-scope-and-target-architecture`

- `v2` 的产品边界
- `v2` 的总体架构图
- `bootstrap layer` 和 `platform layer` 的分层
- 从 `v1` 到 `v2` 的关键变化
- 哪些对象继续保留
- 哪些对象需要扩展

### `02-aliyun-bootstrap-inputs-and-resource-graph`

- `bootstrap` 运行在哪
  - 本机
  - 或 seed `ECS`
- 最小输入参数有哪些
- 需要哪些 `RAM` 权限
- 平台自有资源图怎么定义
- 命名、标签、幂等键怎么约定

### `03-bootstrap-resource-provisioning`

- 用阿里云 `SDK` 自动创建和更新资源
- 先落共享网络底座：
  - `VPC`
  - `vSwitch`
  - `SecurityGroup`
  - 安全组规则
- 资源存在时怎样回读复用
- 资源冲突和重试怎么做
- 输出一份可读的 resource inventory
- 为后面的安装步骤提供稳定输入

### `04-platform-host-provisioning-with-instance-role-and-eip`

- 在 `03` 产出的网络底座上准备 platform `ECS`
- 创建或复用 platform 实例 `RAM` 角色
- 给角色挂上最小起步所需的系统策略
- 在 `RunInstances` 时直接把 `RamRoleName` 带进平台主机
- 创建或复用平台 `EIP`
- 把 `EIP` 绑定到平台 `ECS`
- 输出 platform inventory
- 先把“平台宿主机”这一层打通

### `05-managed-worker-registration-and-platform-topology`

- platform 节点和 worker 节点怎样分工
- `node.role` 怎样进入数据模型、数据库和 API
- scheduler 怎样只把 workload 放到 worker 节点
- first worker 怎样被创建和纳管
- `agent` 长跑模式怎样做注册、状态持久化和重连
- heartbeat 怎样避免把 control-plane 已保留的资源错误覆盖掉
- 节点标签、容量、角色怎么规范化
- 节点维护模式怎么真正落到真实节点

### `06-secrets-config-and-image-credentials`

- 应用配置怎么区分普通配置和敏感信息
- secrets 怎样存、怎样下发
- 外部公共镜像和外部私有镜像仓库怎样接入
- 私有镜像仓库凭据怎么管理
- agent 拉镜像时怎么安全使用这些凭据
- 明确 `v2` 只管理镜像地址和拉取凭据
- 不做平台自带 registry
- 不做镜像上传
- 不做镜像构建

### `07-safe-deployment-and-rollback`

- 发布策略从“能跑”升级到“更安全地跑”
- 单实例安全切流
- 候选版本健康门禁
- 最小回滚路径
- 失败后怎样自动停在安全位置
- 当前这一章先不做：
  - 多副本 rolling update
  - 跨节点 stop work queue

### `08-gateway-tls-and-domain-flow`

- 正式入口层怎么搭
- 域名接入的真实链路
- `TLS` 证书申请与续期
- 平台域名和应用域名怎样分层

### `09-auth-api-token-and-project-access`

- `v2` 最小认证方案
- 管理员登录
- 项目级 API Token
- 哪些操作至少要带身份信息
- 不做完整 IAM 时，边界怎么收住

### `10-operation-history-audit-and-basic-rbac`

- 关键运维动作要留下什么记录
- 谁在什么时候做了什么
- 基础角色边界怎么划
- 项目面和平台面怎么继续分开

### `11-alerting-slo-and-runbooks`

- 哪些指标需要告警
- 先定义哪些最小 `SLI` / `SLO`
- 发布失败、节点掉线、入口异常时的 runbook
- 平台日常巡检要检查什么

### `12-backup-restore-and-disaster-drills`

- Postgres 备份
- 配置和 secrets 备份
- control-plane 节点损坏后的恢复步骤
- 至少做一轮真正的恢复演练

### `13-complete-simulated-environment-example`

- 起一套完整模拟环境
  - `Postgres`
  - control-plane
  - gateway
  - 多个 agent / worker
  - 示例应用
- 让这套环境能一键启动、一键清理
- 让它能稳定复现：
  - 节点注册
  - 应用发布
  - 域名接入
  - 回滚
  - 节点掉线
  - 基础恢复
- 作为 `v2` 的完整 example
  - 教学时优先跑这套
  - 回归时也优先跑这套

### `14-real-aliyun-end-to-end-lab`

- 从 seed 执行机发起 `bootstrap`
- 自动创建平台自有阿里云资源
- 自动安装并启动平台
- 纳管 first worker
- 发布一个真实应用
- 接入域名和 `TLS`
- 验证日志、告警、回滚和恢复主链

### `15-bootstrap-reinstall-upgrade-and-destroy`

- 重跑 `bootstrap` 时怎样做到幂等
- 平台升级怎样做
- 资源销毁怎样做
- 失败后的清理和重试策略

### `16-v2-hardening-and-docs-polish`

- 把 `v2` 的脚本、安装说明、升级说明统一整理
- 修掉真实环境里暴露出来的小毛刺
- 做一轮 `v2` 收尾加固

### `17-v2-final-review`

- 回看 `v2` 到底做成了什么
- 说明哪些问题仍然留给后面版本
- 给 `v3` 的方向做收束

## v2 的验证策略

`v2` 不适合只靠：

- 本地单元测试

来判断是否完成，
因为它会明显更多地涉及：

- `bootstrap`
- 真实云资源
- 真实入口
- 真实证书
- 真实恢复动作

所以 `v2` 的验证要分成五层：

### 第一层：本地日常检查

- `go test ./...`
- `go vet ./...`
- `staticcheck ./...`
- 前端 `npm run check`
- 前端 `npm run build`

### 第二层：本地集成测试

- store / migration
- control-plane API
- 状态机
- 调度逻辑

### 第三层：完整模拟环境验收

- 一键拉起完整 example
- 节点注册
- 应用发布
- 回滚
- gateway 联通
- 节点掉线与恢复
- 基础告警与运维动作

这一层不是“再多跑几个单测”，
而是：

- 用一套稳定的模拟环境
  - 把 `v2` 主链完整跑一遍

### 第四层：`bootstrap` smoke

- 从 seed 执行机发起一次最小 `bootstrap`
- 创建平台自有资源
- 安装平台
- 打通 `healthz`
- 必要时做最小清理

### 第五层：真实阿里云验收实验

- 真实 `bootstrap`
- 平台资源自动创建
- 平台自动安装
- worker 自动纳管
- 应用发布
- 域名与 `TLS`
- 告警与 runbook
- 备份与恢复演练
