# Mini Cloud v1 路线图

回到总路线图：

- [projects/mini-cloud/docs/ROADMAP.md](../ROADMAP.md)

`v1`
是 `mini-cloud` 的第一条实现主线。

它解决的问题是：

- 先做一个最小可运行的平台
- 把控制面、节点纳管、调度、发布、观测这些核心能力串起来
- 不急着一开始就把所有“像云厂商”的能力做全

## `v1` 章节规划

### `00-overview`

- 项目目标
- 非目标
- 总体架构
- 为什么现在开始做这件事
- 为什么 `v1` 先只做阿里云后端

### `01-product-scope-and-resource-model`

- 第一版到底提供什么
- 第一版不提供什么
- 为什么先把：
  - `Node`
  - 收敛成阿里云 `ECS` 节点
- 资源模型：
  - `Project`
  - `Node`
  - `App`
  - `Release`
  - `Deployment`

### `02-control-plane-skeleton`

- API 服务骨架
- 数据库初始化
- 最小状态存储

### `03-node-agent-and-registration`

- 节点注册
- 心跳
- 节点状态上报

### `04-scheduler-and-placement`

- 按：
  - `region`
  - `capacity`
- 做最小放置
- `v1` 暂时不做跨 provider 选择

### `05-release-and-deployment-state-machine`

- 发布状态机
- 重试
- 回滚基础

### `06-runtime-execution`

- agent 如何拉镜像
- agent 如何起停容器
- 基础健康检查

### `07-ingress-and-domain-binding`

- 应用怎样对外暴露
- 域名怎样绑定到应用

### `08-observability-and-basic-operations`

- 日志
- 指标
- 基础运维操作

### `09-failure-retry-and-node-maintenance`

- 节点失联
- 节点维护模式
- 重调度

### `10-usage-quota-and-cost-preview`

- 基础用量统计
- 配额
- 成本预估

### `11-complete-test-suite-and-smoke-tests`

- 补齐 `v1` 的完整测试矩阵
- 单元测试
- store / migration 集成测试
- control-plane API 集成测试
- 少量端到端 smoke 测试
- 让关键主链能够稳定复现

### `12-v1-hardening-and-docs-polish`

- 错误处理收口
- 关键页面和接口的小毛刺修正
- 实验步骤整理
- 文档统一查漏补缺
- 为 `v1` 做一次真正的收尾加固

### `13-final-architecture-review`

- 回看 `v1` 的边界
- 说明 `v2` 的扩展方向

## `v1` 测试策略

这部分最初的策略是：

- 先不急着为 `mini-cloud` 铺一整套很重的完整测试体系

原因是当时：

- `v1` 还在持续补功能
- 资源模型和主链路还没有完全定型
- 现在就把完整测试矩阵全部铺开
  - 后面维护成本会偏高
  - 也容易因为功能持续变化而反复重写

所以前一阶段一直保持：

- `./scripts/check.sh`
  - 负责：
    - `go test ./...`
    - `go vet ./...`
    - `staticcheck ./...`
- 前端：
  - `npm run check`
  - `npm run build`
- 每个关键章节至少保留一条人工可复现的主链实验
  - 例如：
    - `v1/06`
      - 本地 runtime 执行闭环

但现在路线判断已经变了：

- `v1` 的主功能链路已经基本收齐
- 接下来不应该直接宣布 `v1` 结束
- 而应该先进入一轮真正的：
  - 完整测试套件
  - 稳定性加固
  - 文档统一收尾

也就是说，
路线图里的：

- `11`
  - 会专门补完整测试套件
- `12`
  - 会专门做加固和文档整理

到这个阶段，
再系统补齐：

- 纯逻辑单元测试
  - 校验函数
  - 状态机
  - 调度器
- 数据库集成测试
  - store 层
  - migration 之后的真实读写
- control-plane API 集成测试
  - 从用户提交请求到状态推进
- agent / runtime 主链测试
  - `assigned -> deploying -> running`
  - `assigned -> deploying -> failed`
- 关键页面的接口契约检查
- 少量端到端 smoke 测试
  - 保证一条真实主链不会断

所以现在可以把这部分理解成：

- 之前：
  - 先用最小但高价值的检查推进功能开发
- 接下来：
  - 进入 `v1` 的测试和收尾阶段
