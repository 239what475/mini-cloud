# mini-cloud 代码审核准则

这个文档用于之后审查 mini-cloud 的设计和实现。目标不是追求抽象完整，而是让这个项目作为一个专注运维体验的 CaaS demo 保持清晰、可读、可运行、可回收。

## 1. 产品边界

mini-cloud 不是 Kubernetes，也不应该长成半个 Kubernetes。审核功能时先问：

- 这个能力是否服务于“把一个服务部署到某个 cloud plane，并自动准备运行节点、入口流量和基础观测”？
- 是否引入了用户暂时不会直接感知的复杂度？
- 是否为了兼容历史设计保留了无意义参数、状态或 API？
- 是否把调度、发布、灰度、权限、多租户、复杂拓扑等问题提前做进来了？

默认删除这些内容：

- 多副本、复杂调度、cell/topology、runtime node pool CRUD。
- 手动 plane apply/deploy 入口。
- 复杂 RBAC、actor/target 细节审计。
- incident、runbook、SLO、daily check 等派生视图。
- 为测试制造的奇怪接口、fetcher、helper 层。

默认保留这些内容：

- control-plane 管理 plane 注册、全局 service 入口绑定、DNS 修改和聚合门户。
- cloud-plane 持有本 plane 的 service truth，并管理本云运行节点、内部 execution intent、入口路由、provider driver、CDN。
- node-agent 执行 workload、上报状态，并在启动失败时上报容器尾日志作为诊断信息。
- Aliyun + Tencent 两套 backend。
- 自动扩缩 runtime node。
- service name 自动生成三级域名。
- 简单 readiness、metrics、alert signal、事件日志。

## 2. 三平面职责

### control-plane

control-plane 是对外 API、全局入口控制器和聚合门户：

- 接收用户 service API。
- 管理 plane 注册信息、简单事件日志。
- 创建 service 时生成全局 host，并记录 host -> service -> cloud-plane 的绑定关系。
- 调用目标 cloud-plane 下发 service spec。
- 统一修改 DNS 记录。
- 按需读取 cloud-plane snapshot，展示全局 inventory。

control-plane 不应该：

- 持有 service runtime truth。
- 运行 service reconcile loop。
- 直接操作 runtime node、container、Caddy route、CDN origin。
- 直接管理 node-agent。
- 做复杂调度或自动选择 cloud-plane。
- 保留绕过 service API 的 apply/deploy 入口。

### cloud-plane

cloud-plane 是单云运维控制器：

- 注册到 control-plane。
- 持有本 plane 的 service desired state。
- 接收 control-plane 下发的 service spec。
- 将 service spec 转换为本 plane 内部 execution intent。
- 管理本云 runtime node 生命周期。
- 调用 provider driver 创建/删除节点。
- 维护本云 Caddy 路由和 CDN frontdoor。
- 向 control-plane 返回 frontdoor 所需 DNS action。

cloud-plane 不应该：

- 暴露用户级 API。
- 直接修改 DNS。
- 管理其他 cloud-plane 的 CDN 资源。
- 创建 bootstrap 级资源，例如既有入口机、VPC 基础身份等。
- 在内部保留多套 node/runtime-node/node-agent 概念。

### node-agent

node-agent 是 worker 上的执行器：

- 注册到 cloud-plane。
- 拉取 work item。
- 启停 workload。
- 上报 execution、node capacity，并在启动失败时提供容器尾日志诊断。

node-agent 不应该：

- 知道 control-plane。
- 直接访问云厂商 API。
- 决策 service lifecycle。

## 3. API 和模型

API 要少，语义要直：

- 用户 API 只围绕 service。
- control API 只围绕 plane、inventory、events。
- internal API 只服务 cloud-plane 自动注册和内部同步。
- 不做“为了以后可能需要”的 API。

模型审核重点：

- 同一概念只出现一个名字。例如 node / runtime node / node-agent 必须边界清楚。
- input/request/model/store entity 不要层层重复，除非边界确实不同。
- 不保留兼容旧字段。
- 状态字段要能被用户理解，不堆砌 condition。
- service 必须显式指定 plane，不做半个调度器。

## 4. 配置

配置文件是唯一配置入口。审核时检查：

- 不再从环境变量读取业务配置。
- YAML 结构与 control-plane、cloud-plane、node-agent 三者风格一致。
- 嵌套只表达真实归属，不为了“看起来结构化”而嵌套。
- demo 不需要暴露的参数写成常量。
- 不保留 fileConfig/config 两套结构。
- token、云账号、本地 tfvars 等真实配置必须被 ignore，不能提交。

## 5. 存储和迁移

store 要表达业务事实，不要变成无意义 repository 层：

- service 相关逻辑集中放置。
- plane 相关逻辑集中放置。
- helper 只有在明显减少重复或隐藏底层细节时才保留。
- JSON marshal/unmarshal、Close、clone 等小 helper 如果让阅读跳转更多，就不要保留。
- migration 当前只保留必要 schema，不保留历史演进噪音。

审核 store 时重点看：

- 是否还有没人调用的接口。
- 是否还有历史字段。
- 是否还有为了旧设计存在的表或状态。
- 是否能从文件名直接知道要读哪里。

## 6. Provider 和云资源

Aliyun + Tencent 是项目亮点，应该保留，但抽象要克制：

- provider interface 只抽运行节点生命周期和必要查询。
- 不为了统一而隐藏关键云厂商差异。
- cloud-plane 只能管理自己 provider 产生的资源。
- CDN/DNS 清理必须按 provider 判断归属，不能按 base domain 粗暴删除。
- 创建和回收必须成对。
- e2e 后必须能确认没有 runtime node、CDN、DNS、security group、CCN/VPC attachment 残留。

前置 bootstrap 资源和运行态资源要分清：

- bootstrap 管入口机、基础网络、基础安全组、Terraform state。
- cloud-plane 管 service 产生的 runtime node 和 frontdoor。
- destroy 可以做实验回收，但不能模糊这两类资源的归属。

## 7. Go 代码风格

优先读起来直接，不追求“企业级分层”。

包和文件：

- 包名用业务概念，不用 manager、helper、common 这种泛名。
- 一个包内文件可以少一点、大一点，只要阅读路径清楚。
- 相关逻辑尽量放近，例如 service 一个文件、plane 一个文件。
- 不要因为测试而拆出奇怪的小接口或 fetcher。
- 跨平面共用的小包必须有明确边界，例如 `transport` 只放 HTTP/gRPC 元信息。
- 不把业务字段塞进 `context.Context` 做隐式日志传播；request id 可以随 context 传播，业务日志字段就地 `logger.With(...)`。

函数：

- 函数名表达业务动作，不要 `do`、`handle`、`process` 泛化。
- 一层函数只在能减少认知负担时保留。
- 只被一个地方调用、且没有隐藏复杂性的 helper 应考虑内联。
- 不写无意义 wrapper，例如只调用 `c.JSON` 的 `writeJSON`。

错误处理：

- 错误要靠近产生位置处理。
- 输入错误用明确错误信息。
- 不维护巨大 `isXInputError` 列表。
- 不吞掉云资源清理错误，除非明确是 not found / already deleted。

接口：

- 接口由使用方定义。
- 不为了 mock 提前定义接口。
- 如果一个接口只有一个实现、且不是边界依赖，优先去掉。

并发：

- background loop 必须有清楚的 owner、context 和 shutdown 路径。
- scale out/in 这类同一状态机的动作不要拆成互相竞争的循环。
- timeout 要靠近外部调用边界。

## 8. 测试标准

测试要覆盖行为，不要锁死实现细节。

应该保留：

- 配置解析和校验测试。
- store 关键持久化测试。
- control-plane API 关键路径测试。
- cloud-plane provider/frontdoor 行为测试。
- node-agent workload 执行测试。
- lab e2e 手工流程记录。

应该删除或避免：

- 只为测试私有小函数而暴露接口。
- 测试无意义 wrapper。
- 过细 mock 导致实现绕来绕去。
- 只验证函数被调用而不验证业务结果。

真实 e2e 检查项：

- bootstrap 可重复执行。
- install 后 plane 自动注册，且状态 ready。
- 每个 provider 创建一个 service。
- runtime node 自动创建，node-agent 注册。
- readiness 通过。
- service 域名通过 CDN/DNS 返回 workload。
- delete service 后 runtime node 缩到 0。
- service CDN/DNS 被清理。
- destroy 后 Terraform state、runtime node、CDN、DNS、远端服务、容器都无残留。

## 9. 文档

文档只写当前真实设计，不写已经删除的历史方案。

- 当前不保留历史 `docs/` 目录，历史设计交给 git。
- README、REVIEW 和 deploy/lab 示例要能指导真实 e2e。
- 不保留“暂时兼容”“旧模式仍可用”这类内容。
- 示例配置不能包含真实 token、真实账号密钥。

## 10. 审核输出格式

之后做代码审核时，优先输出问题，不先写总结。

每个问题包含：

- 位置：文件和函数。
- 问题：为什么不干净、不必要或有风险。
- 影响：会导致什么复杂度、bug 或维护成本。
- 建议：删掉、合并、改名、移动、重写或补测试。

问题按严重程度排序：

1. 资源泄漏、数据错误、不可回收、e2e 失败。
2. 架构职责错位。
3. 历史兼容或无意义功能残留。
4. 代码组织混乱。
5. 命名、helper、测试粒度问题。

如果没有发现问题，要明确说“没有发现必须修改的问题”，并列出剩余风险。
