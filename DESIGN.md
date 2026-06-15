# mini-cloud design notes

`mini-cloud` 的设计目标是做一个专注运维体验的 CaaS 原型，而不是做一个小型 Kubernetes。这份文档记录当前的核心架构取舍和设计决策，方便理解项目边界。

## Core Principle

只保留能支撑这个闭环的能力：

```text
user creates one service
  -> chooses one cloud-plane
  -> cloud-plane creates one worker node if needed
  -> node-agent runs one container
  -> platform exposes one generated domain
  -> user can observe status
  -> user deletes service
  -> platform cleans up resources
```

不服务这个闭环的能力默认不做。

## Why Not Kubernetes

Kubernetes 解决的是通用容器编排问题，包含复杂调度、多副本、service discovery、deployment rollout、RBAC、CRD、operator 等完整生态。

`mini-cloud` 的目标更窄：

- 展示 CaaS 平台的控制面/运行面分工。
- 展示多云 backend 接入。
- 展示自动创建 worker node 的运维能力。
- 展示真实公网入口和 e2e 回收闭环。

所以这个项目刻意避免长成半个 Kubernetes。

## Why Single Container Service

当前 service 是单容器模型：

- image
- env
- default port
- readiness path
- instance class
- exposure mode
- planeID

这个模型能覆盖很多自用和 demo 场景，例如小型 HTTP 服务、API 服务、个人工具、静态 Web 服务。

暂时不支持 Docker Compose，原因是 Compose 会引入更多语义：

- 多容器依赖顺序。
- volume 和文件挂载。
- 内部网络。
- 数据持久化。
- 多端口暴露。
- 部分更新和回滚。

这些能力会显著扩大系统边界，不适合当前阶段。

## Why No Replicas

不做 replicas 是一个明确取舍。

如果支持 replicas，就需要进一步处理：

- 多实例调度。
- 负载均衡。
- 滚动更新。
- 实例健康检查和摘除。
- 容量分配。
- 多 worker 上的状态聚合。

这些会把项目推向通用编排器。当前 demo 更重视“一个 service 在指定 cloud-plane 上被可靠部署、访问和回收”。

## Why Service Must Specify Cloud-plane

service 必须显式指定 `planeID`。

原因：

- 这是一个运维导向的 CaaS demo，用户应该清楚服务运行在哪个云。
- 多云自动调度会引入半个 scheduler，例如容量评分、成本评分、亲和性、失败重试、跨云迁移。
- 当前最有价值的展示点是多 backend 能力和每个 cloud-plane 的自治能力，而不是调度算法。

因此 control-plane 不做自动选择 cloud-plane。

## Why Stateless Control-plane

control-plane 是门户和全局 DNS owner，不保存 service runtime truth。

它负责：

- 提供 Web/API。
- 管理 plane 列表和访问 token。
- 接收用户请求。
- 把 service spec 下发到目标 cloud-plane。
- 管理 DNSPod CNAME。
- 聚合各 cloud-plane snapshot 用于展示。

它不负责：

- 持久化 service desired state。
- 运行 service reconcile loop。
- 直接操作 worker node 或 container。
- 管理 cloud-plane 内部 Caddy/CDN 生命周期。

这样 control-plane 可以更接近 serverless 形态：平时不需要承载运行态稳定性，更新时也不影响 cloud-plane 上已经运行的 workload。

## Why Cloud-plane Owns Runtime Truth

每个 cloud-plane 是本云内的自治运行面。

它持有：

- 本 plane 的 service desired state。
- 当前 run 状态。
- worker node 状态。
- node-agent session 和 heartbeat。
- Caddy route。
- provider CDN domain。

这样即使 control-plane 临时不可用，cloud-plane 仍然能维持本 plane 的运行状态和内部 reconcile。

## Why DNS Belongs To Control-plane

DNS 是跨 cloud-plane 的唯一全局入口资源。

control-plane 管 DNS 的原因：

- DNSPod zone 是全局资源，不属于某一个 cloud-plane。
- 同一个 base domain 下的 service name 必须全局唯一。
- control-plane 能看到 service -> cloud-plane 的绑定关系。
- cloud-plane 可以只返回自己 provider CDN 的 verification/CNAME 信息，不需要知道全局 DNS 账号。

最终分工是：

```text
cloud-plane manages provider CDN
control-plane manages DNSPod CNAME
```

## Why CDN/Caddy Both Exist

Caddy 负责 cloud-plane 内部路由：

```text
Host header -> service -> worker host port
```

CDN 负责公网入口：

```text
public domain -> provider CDN -> cloud-plane origin
```

这样同一个 cloud-plane 上多个 service 可以共享入口机 80 端口，通过 Host 区分不同 service。

## Why Worker Nodes Have No Public IP

worker node 默认不分配公网 IP。

原因：

- 减少公网暴露面。
- 避免每个 worker 产生独立公网 IP 和流量成本。
- 所有入口流量统一经过 CDN/Caddy。
- workload 出公网统一走 entry host 上的 Tinyproxy。

Tinyproxy 是 workload 的受控公网出口，不是 control-plane/cloud-plane/node-agent 控制链路的依赖。

## Why Aliyun And Tencent Drivers Stay Separate

Aliyun 和 Tencent 两套 driver 有重复代码，但当前刻意不强行抽象成复杂 framework。

原因：

- 云厂商 API 差异真实存在。
- 过度抽象会隐藏关键细节，例如镜像、磁盘、网络、安全组、标签、CDN 验证方式。
- 当前只有两个 backend，重复成本可接受。
- 清楚展示两套真实 backend 的差异比强行抽象成统一层更有价值。

公共抽象只保留在必要边界，例如 node provider interface 和 frontdoor 行为。

## Why Terraform Only Does Bootstrap

Terraform 负责基础资源：

- worker node 所在网络。
- node security group。
- cloud-plane entry host 所需安全组规则。
- Tencent Lighthouse + CVM VPC 的 CCN attachment。

Terraform 不负责运行态资源：

- service。
- runtime worker node。
- CDN service domain。
- DNSPod service CNAME。
- Caddy route。

运行态资源由 control-plane/cloud-plane 根据 service 生命周期管理。`destroy` 可以做实验兜底回收，但不会改变这个归属边界。

## Why Real E2E Matters

这个项目最核心的可信度来自真实云 e2e。

单元测试只能证明局部逻辑，真实 e2e 证明：

- Terraform bootstrap 可运行。
- SCF control-plane 可访问。
- cloud-plane 可以安装和启动。
- control-plane 能通过 TLS 调 cloud-plane。
- Web UI 能真实创建 service。
- cloud-plane 能创建 worker node。
- node-agent 能注册并启动容器。
- CDN/DNS/Caddy 能把公网流量打到容器。
- update 不破坏 control-plane 可用性。
- destroy 可以回收资源。

真实云 e2e 是这个项目可信度的核心来源——它直接证明了系统在多云环境下的完整工作闭环。

## Current Non-goals

当前不做：

- 多副本。
- Docker Compose。
- 自动跨云调度。
- 用户自定义域名。
- 灰度发布。
- release/version 概念。
- 完整多租户。
- 复杂 RBAC。
- 自建观测平台。

这些能力不是永远不能做，而是当前会削弱项目的清晰度。
