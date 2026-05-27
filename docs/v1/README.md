# Mini Cloud v1

`v1` 是 `mini-cloud` 的第一条实现主线。

它的目标是：

- 先做一个最小可运行的平台
- 把控制面、节点纳管、调度、发布、观测这些核心能力串起来
- 不急着把所有“像云厂商”的能力一次做全

## 当前规则

- `docs/v1/00-overview.md`、`docs/v1/01-product-scope-and-resource-model.md` 这样的编号文件负责文档导航
- 每一章结束时做一个独立 `commit`
  - 作为这一章的代码检查点
- `commit` 是检查点
  - 文档文件才是主要导航

## 当前章节

- `00-overview`
- `01-product-scope-and-resource-model`
- `02-control-plane-skeleton`
- `03-node-agent-and-registration`
- `04-scheduler-and-placement`
- `05-release-and-deployment-state-machine`
- `06-runtime-execution`
- `07-ingress-and-domain-binding`
- `08-observability-and-basic-operations`
- `09-failure-retry-and-node-maintenance`
- `10-usage-quota-and-cost-preview`
- `11-complete-test-suite-and-smoke-tests`
- `12-v1-hardening-and-docs-polish`
- `13-final-architecture-review`

后续章节以：

- `projects/mini-cloud/docs/v1/ROADMAP.md`

为准。
