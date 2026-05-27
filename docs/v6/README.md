# Mini Cloud v6

`v6`
不再继续扩 workload
类型，
也不再继续摊大新的平台产品线。

这一版明确只做一件事：

- 把 `mini-cloud`
  从“已经能跑通的多云 `CaaS`”
  往“更可运营、更可收敛、更可恢复的平台”
  推进一步

也就是说，
`v6`
已经不再问：

- 能不能做成一个多云长期在线服务平台

而是开始问：

- 这套平台能不能更像一个真的长期运行平台

## 这一版的产品边界

`v6`
仍然明确只解决下面这些问题：

- 怎样让服务状态更稳定地收敛
- 怎样把发布做得更安全
- 怎样把 `service`
  的资源边界重新收干净
- 怎样让 front door
  在单地域 `service`
  之上再做流量工作
- 怎样把控制链路的安全和轮转做得更可信
- 怎样把 `day-2`
  运维、替换、升级、退役收成正式能力
- 怎样从 operator
  视角真正看清平台是否健康

这一版仍然不解决：

- `job / cron / oneoff`
- 第三家 provider
- `service mesh`
- 有状态服务主线
- 支付 / 余额 / 账单
- 对象存储 / 数据库产品线

## 这一版的基本形态

`v6`
继续沿用现在已经收好的三层模型，
但会把控制决策进一步集中回
`control-plane`：

- `control-plane`
  - 全局管理面
  - 唯一控制决策面
- `cloud-plane`
  - 单云执行面
  - inventory / 执行状态汇聚面
- `agent`
  - 节点侧执行进程

从
`11`
开始，
这一套三层也会继续收成：

- 分开部署
- 分开启动
- 显式注册建联

而不是再让
`Terraform`
承担长期进程编排。

但这一版会把下面这些东西补成正式能力：

- controller / reconcile
  收敛模型
- 单地域 `service`
  资源模型
- front door
  与 `service`
  的分层
- `runtime node pool`
  和容量余量
- 渐进式发布
- front door
  路由发布 /
  readiness gating /
  排障视图
- 身份、安全和轮转
- `day-2`
  运维
- operator
  级可观测性

## 当前规则

- `docs/v6/ROADMAP.md`
  - 负责 `v6`
    的总规划
- `v6`
  仍然不单独保留 `00`
  总览章
  - `docs/v6/README.md`
    已经承担版本说明和导航作用
- 正式章节仍然从：
  - `01`
    开始
- 每一章结束时仍然单独做一个 `commit`
  - 作为章节检查点

## 当前章节规划

`v6`
当前规划成下面二十一章：

- `01-controller-reconcile-model-v2`
- `02-service-topology-and-placement-policy-v2`
- `03-runtime-node-pools-and-capacity-headroom`
- `04-centralized-control-plane-and-thin-cloud-plane-refactor`
- `05-progressive-delivery-and-release-policy`
- `06-service-resource-model-single-region-refactor`
- `07-front-door-route-publication-and-readiness-gating`
- `08-control-plane-single-active-deployment-and-recovery`
- `09-control-chain-security-and-token-rotation`
- `10-day2-maintenance-upgrade-and-decommission`
- `11-real-control-plane-and-first-production-plane`
- `12-production-access-and-control-chain-security`
- `13-reliable-teardown-and-destroy-readiness`
- `14-second-production-plane-on-aliyun`
- `15-production-observability-stack`
- `16-runtime-truth-config-and-state-convergence`
- `17-control-plane-northbound-grpc-and-operator-tui`
- `18-config-and-secret-file-projection`
- `19-domain-tls-and-tencent-cdn`
- `20-background-controller-error-boundary-and-degraded-health`
- `21-open-source-app-validation-and-gameday`

这里要特别说明：

- `06`
  不是在
  `05`
  之上继续加能力
- `06`
  会明确回收
  `02-05`
  中引入的
  `service -> cell`
  用户可见语义
- 从
  `06`
  开始，
  `service`
  会重新收回：
  - 单地域
  - 单 provider
  - 单运行范围
  - 长期运行服务
    语义

## 这一版怎么理解

如果说：

- `v5`
  解决的是：
  - 多云长期在线服务平台能不能正式成立

那么：

- `v6`
  解决的就是：
  - 这套平台能不能更稳定地收敛
  - 能不能更安全地运行
  - 能不能更自然地做升级、替换、切换和恢复
  - 能不能从 operator
    视角被真正运营起来

也就是说，
`v6`
更准确的关键词不是：

- 新产品
- 新 workload
- 新 provider

而是：

- 生产化

## 当前已写章节

- [01-controller-reconcile-model-v2.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/docs/v6/01-controller-reconcile-model-v2.md)
- [02-service-topology-and-placement-policy-v2.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/docs/v6/02-service-topology-and-placement-policy-v2.md)
- [03-runtime-node-pools-and-capacity-headroom.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/docs/v6/03-runtime-node-pools-and-capacity-headroom.md)
- [04-centralized-control-plane-and-thin-cloud-plane-refactor.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/docs/v6/04-centralized-control-plane-and-thin-cloud-plane-refactor.md)
- [05-progressive-delivery-and-release-policy.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/docs/v6/05-progressive-delivery-and-release-policy.md)
- [06-service-resource-model-single-region-refactor.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/docs/v6/06-service-resource-model-single-region-refactor.md)
- [07-front-door-route-publication-and-readiness-gating.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/docs/v6/07-front-door-route-publication-and-readiness-gating.md)
- [08-control-plane-single-active-deployment-and-recovery.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/docs/v6/08-control-plane-single-active-deployment-and-recovery.md)
- [09-control-chain-security-and-token-rotation.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/docs/v6/09-control-chain-security-and-token-rotation.md)
- [10-day2-maintenance-upgrade-and-decommission.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/docs/v6/10-day2-maintenance-upgrade-and-decommission.md)
- [11-real-control-plane-and-first-production-plane.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/docs/v6/11-real-control-plane-and-first-production-plane.md)
- [12-production-access-and-control-chain-security.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/docs/v6/12-production-access-and-control-chain-security.md)
- [13-reliable-teardown-and-destroy-readiness.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/docs/v6/13-reliable-teardown-and-destroy-readiness.md)
- [14-second-production-plane-on-aliyun.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/docs/v6/14-second-production-plane-on-aliyun.md)
- [15-production-observability-stack.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/docs/v6/15-production-observability-stack.md)
- [16-runtime-truth-config-and-state-convergence.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/docs/v6/16-runtime-truth-config-and-state-convergence.md)
- [17-control-plane-northbound-grpc-and-operator-tui.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/docs/v6/17-control-plane-northbound-grpc-and-operator-tui.md)
- [18-config-and-secret-file-projection.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/docs/v6/18-config-and-secret-file-projection.md)
- [19-domain-tls-and-tencent-cdn.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/docs/v6/19-domain-tls-and-tencent-cdn.md)
- [20-background-controller-error-boundary-and-degraded-health.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/docs/v6/20-background-controller-error-boundary-and-degraded-health.md)
- 运营化
- 安全化
- `day-2`
  化

更细的章节规划以：

- [projects/mini-cloud/docs/v6/ROADMAP.md](./ROADMAP.md)

为准。
