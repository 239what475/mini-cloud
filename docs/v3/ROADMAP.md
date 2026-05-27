# Mini Cloud v3 路线图

回到总路线图：

- [projects/mini-cloud/docs/ROADMAP.md](../ROADMAP.md)

## v3 总体判断

做完 `v2` 以后，
`mini-cloud`
最自然的下一步不是直接变成：

- 一个 control-plane 同时管理多家云
- 高可用多副本 control-plane

而是先把下面这条线做完整：

- 同一套代码
- 同一套 Terraform 组织方式
- 部署前通过同一个 Terraform 入口里的 `provider_name` 选定后端
- 平台自有基础设施改成由 `Terraform` 创建
- 平台运行时 worker 改成由 provider `SDK` 动态创建和回收
- 一套 control-plane 只绑定一个 provider
- `v3` 前半段先把阿里云做完整
- `v3` 后半段再接入腾讯云

这里要特别把边界说清楚：

- `v3`
  - 支持阿里云版和腾讯云版部署
  - 但不是一个 control-plane 同时管理两家云
- `v4`
  - 再去做多 control-plane
  - 跨 provider 更高层统一入口
  - control-plane 高可用

也就是说，
`v3`
真正要解决的是：

- 单 provider control-plane 的正式化
- `Terraform bootstrap + SDK runtime`
- 第二家 provider 的真实落地

## v3 的明确目标

`v3`
这一版优先要做成的，
应该是下面这件事：

- 给定同一套代码和 Terraform 目录约定
- 在部署前通过同一个 Terraform root 的：
  - `provider_name`
  - 明确这次要落到：
    - 阿里云
    - 或腾讯云
- 用 `Terraform`
  - 创建平台“自己”的基础设施
- 再由平台在运行时通过 provider `SDK`
  - 动态创建和回收 worker 资源

这里继续明确分层：

1. `bootstrap layer`
   - 负责平台自有基础设施
   - 负责：
     - `VPC`
     - 子网
     - `security group`
     - control-plane 主机
     - 平台公网入口
     - 平台初始化所需身份
   - 这一层优先用 `Terraform`
2. `platform layer`
   - 负责 control-plane 自身安装、配置、启动和升级
   - 首次启动时写入 provider 绑定信息
3. `runtime provisioning layer`
   - 负责平台运行时按应用需求动态创建和回收 worker
   - 这一层优先用 provider `SDK`

在这个目标里，
最重要的是把这些问题做完整：

1. 怎样把现有 `bootstrap`
   - 从“平台资源也靠 `SDK` 创建”
   - 改成：
   - “平台资源靠 `Terraform` 创建”
2. 怎样约定统一的 Terraform 目录结构
   - 让阿里云和腾讯云都遵守同样的模块布局
   - 同时把教学主入口统一到：
     - `deploy/terraform/lab/`
   - provider 选择通过：
     - `provider_name`
3. control-plane 怎样在首次安装时绑定 provider
   - 后续只管理这一家的运行时资源
4. 应用发布怎样触发平台自动扩容 worker
5. worker 怎样自动注册、心跳、排空和回收
6. provider 的差异怎样在代码里被正式收口
   - 并在接入腾讯云时真正落到代码里
   - 但又不提前滑向“多云统一调度”
7. 阿里云这条线怎样先做成完整主链
8. 再怎样把腾讯云作为第二家 provider 真正接进来
9. 怎样分别做真实阿里云和真实腾讯云验收

所以：

- `v3` 不是“同时管理两家云”
- 而是：
  - 同一套代码
  - 同一套 Terraform 组织方式
  - 同一个 Terraform root
  - 每次部署前在 `provider_name` 里选定一家云
  - 部署后这套 control-plane 就绑定到这一家云

## v3 的非目标

`v3`
暂时不把下面这些问题拉进主线：

- 一个 control-plane 同时管理多家云
- 跨 provider 调度和故障切换
- 多副本高可用 control-plane
- 全局统一入口层
  - 例如：
    - 跨云统一网关
    - 跨云统一流量切换
- 平台按租户或应用需求自动创建独立 `VPC`
  - 这会明显把问题继续做大
- 第三家 provider
- 更复杂的计费系统
- 完整 IAM / 多租户权限体系

这些方向不是不做，
而是：

- 不应该和 `Terraform bootstrap`
- 单 provider 绑定式 control-plane
- 第二家 provider 的接入

这三条主线同时继续膨胀。

## v3 章节规划

`v3`
继续不单独保留一个：

- 总览章

原因和前面一样：

- `docs/v3/README.md`
  - 会承担导航和总说明作用

所以正式章节直接从：

- `01`

开始。

### `01-aliyun-terraform-platform-module`

- 用标准 Terraform 入口
  - 创建并销毁阿里云平台底座
- 这里一次收掉：
  - `Terraform` 目录结构
  - state 约定
  - outputs 约定
  - inventory 产物
  - `terraform.tfvars.example` 约定
- 结果不是“只讲 Terraform”
  - 而是真的把阿里云平台底座建起来
- 这一章之后，
  阿里云底座目录会在后续继续演进，
  但入口仍然是直接的：
  - `terraform init / plan / apply / destroy`

### `02-aliyun-cloud-init-control-plane-install`

- 把阿里云 Terraform 目录进一步拆成：
  - `network-base`
  - `platform-host`
- 在 `platform-host` 的 `user_data` 里直接写 `cloud-init` 首启安装逻辑
- 平台主机第一次启动时自动安装：
  - Docker
  - Postgres 容器
  - `mini-cloud control-plane`
- 让平台真正启动起来
- 首次启动时把 provider 绑定为：
  - `aliyun`
- 后续运行时只允许管理阿里云资源
- 至少打通：
  - `healthz`
  - `platform-config` 状态回读
  - 平台服务重启后的 provider 一致性检查

### `03-aliyun-app-triggered-scale-out`

- 在阿里云版平台上提交一个真实应用
- 当现有容量不够时
  - 平台自动通过阿里云 `SDK` 创建 worker
- worker 自动初始化并注册回平台
- scheduler 把 workload 放上去
- 应用真正上线

### `04-aliyun-runtime-retry-and-reclaim`

- 把阿里云运行时链路补完整
- 处理：
  - worker 创建慢
  - worker 创建失败
  - agent 未注册成功
  - runtime inventory
  - `terraform destroy` 之前的运行时资源回收
  - 节点排空后回收
  - 删除失败后的清理和重试
- 这一章结束时，
  阿里云单 provider 这条线应该已经比较完整

### `05-single-provider-boundary-refactor`

- 这一章不是继续堆新功能，
  而是先做一次必要的中段重构
- 目标很明确：
  - 在不改变“阿里云单 provider 已有行为”的前提下
  - 把代码、架构和输入契约收干净
  - 为第二家 provider 接入腾出清晰边界
- 重点处理这些问题：
  - provider-neutral contract 现在还不够清楚
  - 阿里云实现和主链代码还有直接耦合
  - HTTP handler 里混了太多运行时编排细节
  - `Terraform -> control-plane`
    的输入约定还没有收成真正稳定的 provider contract
  - 代码里仍然存在：
    - `aliyun`
      默认值
    - 隐式回退
      这种会干扰第二家 provider 接入的逻辑
- 这一章结束时要达到的状态是：
  - 新增第二家 provider 时
    主要是“新增实现”
  - 而不是“回头大改主链”

### `06-real-aliyun-end-to-end`

- 在完成 `05`
  的边界收口后，
  再回到真实阿里云做一次完整验收
- 从：
  - `Terraform bootstrap`
  - control-plane 安装
  - 应用触发扩容
  - worker 自动注册
  - 应用上线
  - `terraform destroy`
    前自动 teardown
- 全链路走一遍
- 这一章的重点不是“再加能力”，
  而是证明：
  - 经过 `05`
    重构后，
    阿里云主链仍然成立
- 当前这章的正式入口不是：
  - `go test`
- 而是一套独立的黑盒程序：
  - `cd projects/mini-cloud/tests && go run .`
- 这套程序会自动完成：
  - 读取 `~/.aliyun/config.json`
  - 读取 `tests/aliyun/config.json`
  - 探测当前公网 IP
  - `Terraform apply`
  - 平台 API 验证
  - 创建 app 触发 scale-out
  - teardown 与 destroy

### `07-tencent-terraform-platform-module`

- 继续沿用同样的 Terraform 目录结构
  - 只切到底层腾讯云 module 组合
- 用 `Terraform` 创建并销毁腾讯云平台底座
- 这一章不急着把运行时一起做完，
  先验证：
  - `05`
    里整理出来的 bootstrap / provider / config contract
  - 是否真的足够支撑第二家 provider 的底座接入
- 具体资源先收成：
  - `VPC`
  - `subnet`
  - `security group`
  - platform `CVM`
  - `EIP`
  - `EIP association`
- 安全组规则直接使用：
  - `tencentcloud_security_group_rule_set`
  - 不再使用已经 deprecated 的 lite rule
- platform host 从这一章开始就直接走：
  - 自动创建并绑定实例角色
  - `user_data`
  其中 `user_data` 本章仍然先不启用
- 这一章结束时，
  应该先得到和阿里云同类的 outputs：
  - `platform_public_ip`
  - `control_plane_base_url`

### `08-tencent-control-plane-install-and-runtime-scale-out`

- 在腾讯云平台主机上自动安装 control-plane
- 首次启动时绑定：
  - `tencent`
- 再跑通一条完整运行时主链：
  - 应用提交
  - 容量不足
  - 通过腾讯云 `SDK` 自动创建 worker
  - worker 自动注册
  - 应用成功上线
- 这一章里，
  真正验证：
  - `05`
    整理出来的 provider runtime contract
  - 是否已经足够承载：
    - 阿里云
    - 腾讯云
    两家实现

### `09-real-tencent-end-to-end`

- 在真实腾讯云里跑完整验收
- 验证：
  - `Terraform bootstrap`
  - provider 绑定
  - 运行时扩容
  - 节点回收
- 但仍然是一套独立的单 provider control-plane

### `10-unified-terraform-bootstrap-entry`

- 把 `v3`
  现在已经存在的：
  - 阿里云 Terraform root
  - 腾讯云 Terraform root
  收口成一个真正统一的教学入口
- 底层 provider module 仍然分开实现：
  - `deploy/terraform/providers/aliyun/modules/*`
  - `deploy/terraform/providers/tencent/modules/*`
- 但从这章开始，
  推荐入口统一成：
  - `deploy/terraform/lab`
- 这一章真正统一的是：
  - bootstrap root
  - 输入变量形状
  - outputs 形状
  - 真实 E2E 测试使用的 stack 路径
- 这里最关键的边界是：
  - 入口统一了
  - 但一次部署仍然只能选一家云
  - 选择方式就是：
    - `provider_name = "aliyun"`
    - 或 `provider_name = "tencent"`

### `11-mini-cloud-terraform-entry`

- 平台底座统一到：
  - `deploy/terraform/lab`
  以后，
  再往上补一层“面向 mini-cloud 本身”的 Terraform 入口
- 目标不是再创建阿里云或腾讯云底座，
  而是让用户已经拿到：
  - `control_plane_base_url`
  - `project api token`
  之后，
  可以像使用：
  - `aliyun`
  - `tencent`
  这类 Terraform 入口一样，
  直接用 Terraform 去声明自己项目里的平台资源
- 第一版先收最小闭环：
  - service
  - app
  - app 自己承载完整的期望运行规格
  - revision / deployment 状态回读
  - rollback 这类必要动作
- 这里先明确一个关键边界：
  - Terraform 第一版直接管理的是：
    - `project`
    - `app`
  - 不是直接管理：
    - `deployment`
- 也就是说，
  `11`
  先要把 mini-cloud 自己的 API 收成真正的声明式资源接口：
  - `project`
  - `app`
  再去写 Terraform 入口
- 服务端内部负责：
  - app 规格变更后自动生成 `revision`
  - 推进新的 `deployment`
- 这一章最关键的边界是：
  - `10`
    解决的是“怎样把 mini-cloud 平台自己建起来”
  - `11`
    解决的是“平台建好后，怎样把 mini-cloud 当成 Terraform 入口来使用”
- 也就是说，
  到这一步，
  Terraform 入口会分成两层：
  - 平台底座入口
    - 管阿里云 / 腾讯云基础设施
  - 平台资源入口
    - 管 mini-cloud 自己暴露出来的 `project` / `app`
    - 回读 `revision` / `deployment` 状态

### `12-v3-tests-and-hardening`

- 把 `v3` 的主链补成稳定可回归状态
- 包括：
  - `Terraform` outputs 校验
  - provider contract tests
  - runtime reconcile tests
  - 关键脚本回归
  - 文档统一查漏补缺

### `13-mini-cloud-terraform-provider`

- 在 `11`
  已经把 mini-cloud 资源 API 收成 Terraform 友好的 northbound 以后，
  正式开始写真正的 Terraform provider
- 这里说的 provider，
  不是：
  - 再包一层 shell
  - 再写一个 Go CLI
  - 再补一个教学 module
- 而是：
  - Terraform 可以直接加载的 provider 插件
  - 能正式声明：
    - `minicloud_service`
- 第一版目标先收最小闭环：
  - provider 配置：
    - `base_url`
    - `token`
  - provider 启动时先走：
    - `GET /api/v1/auth/whoami`
  - 只接受：
    - `project-scoped token`
  - `minicloud_service`
  - service 的常用状态字段回读
  - import / read / update / delete 基本链路成立
- 这里仍然坚持 `11` 定下来的边界：
  - provider 第一版直接管理：
    - `service`
  - 不直接管理：
    - `project`
    - `revision`
    - `deployment`
    - `execution`
- 这一章做完以后，
  mini-cloud 才算真正具备：
  - “项目 owner 可以像用云厂商 provider 一样，用 Terraform 管自己的 service”
    的第一版入口

### `14-v3-final-review`

- 回看 `v3` 到底做成了什么
- 明确：
  - 阿里云和腾讯云已经都能作为单 provider 后端成立
  - `Terraform bootstrap + SDK runtime + mini-cloud Terraform provider`
    这三层边界已经站住了
- 再给 `v4` 收束方向

## v3 的验证策略

`v3`
的验证不能只看：

- 单元测试

因为这一版同时涉及：

- `Terraform`
- 真实云资源
- provider 绑定
- 运行时动态扩缩容

所以 `v3`
的验证至少分成五层：

### 第一层：本地日常检查

- `go test ./...`
- `go vet ./...`
- `staticcheck ./...`
- 前端 `npm run check`
- 前端 `npm run build`

### 第二层：provider contract 与状态机测试

- `bootstrap` 输入解析
- provider 选择
- provider 绑定一致性
- runtime reconcile
- worker 生命周期推进

### 第三层：阿里云主链验收

- 正式入口：
  - `cd projects/mini-cloud/tests && go run .`
- `Terraform bootstrap`
- control-plane 安装
- 应用触发扩容
- worker 注册
- worker 回收

### 第四层：腾讯云主链验收

- 正式入口：
  - `cd projects/mini-cloud/tests && go run . tencent`
- `Terraform bootstrap`
- control-plane 安装
- 应用触发扩容
- worker 注册
- worker 回收

### 第五层：真实双 provider 独立验收

- 用统一 Terraform root + `provider_name = "aliyun"`
  - 单独跑完整实验
- 用统一 Terraform root + `provider_name = "tencent"`
  - 单独跑完整实验
- 重点验证：
  - 同一套 Terraform 组织方式
  - 不同 provider 配置
  - 两个单 provider 部署模式都成立

## `v3` 之后的技术演进判断

这里继续先把
`v4`
以及更后面的方向
做一个判断，
避免未来每次推进时又从头讨论。

### 高概率会在 `v4` 引入

- 多 control-plane
- control-plane 高可用
- 更高层统一入口
- 跨 provider 更正式的资源视图
- 更正式的认证系统
  - 例如：
    - `OIDC`
    - 外部身份提供者
- 更清晰的审计与运维事件模型

### 更可能留到 `v4` 之后

- 跨 provider 统一调度
- 平台按租户需求自动创建更完整的独立网络资源
- 更复杂的调度策略
- 更正式的计费系统

### 很可能会在后面评估，但不是默认必选

- `k3s` / Kubernetes 作为运行底座
- `Redis`

这里的判断是：

- 当平台规模继续长大后
- 你可能会越来越不想自己维护：
  - 容器生命周期
  - 发布细节
  - 服务运行面

这时：

- `k3s`
  - 可能会变成一个更合适的底座

而：

- `Redis`
  - 可能会在缓存、限流、短期状态之类的场景里变得有价值

但它们都不应该在 `v3`
一开始就强行进入主线。

### 可能需要，但通常会更晚

- `gRPC`
- 消息队列

只有当后面真的出现这些需求时，
它们才会值得进入主线：

- 高频双向 agent 通信
- 很强的异步任务解耦
- 更复杂的内部服务拆分

在那之前，
`HTTP + DB state machine`
通常仍然够用。

### 大概率不会成为核心方向

- ORM
- 重前端优先

这个项目的核心更像：

- 控制面
- 状态机
- 数据关系
- 运维可观测性

所以：

- SQL 可见性
- 控制链路清晰

比“先把 UI 做得很完整”更重要。
