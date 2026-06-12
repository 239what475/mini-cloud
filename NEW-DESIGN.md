# New Design

## 目标

mini-cloud 后续架构要让 control-plane 变成 serverless-ready 的门户和全局入口控制器，让每个 cloud-plane 成为所在云内的自治运行面。

核心目标：

- control-plane 不需要常驻也不影响已部署服务运行。
- cloud-plane 持有本 plane 的 service truth，并负责运行态闭环。
- DNS 由 control-plane 统一修改，CDN 和运行态由 cloud-plane 管理。
- 后续可以把 control-plane 打包成 serverless container，但当前先完成代码架构调整。

## 组件职责

### control-plane

control-plane 是用户入口、全局域名控制器和多 cloud-plane 聚合门户。

负责：

- 提供 Web/API 入口。
- 管理 cloud-plane 注册信息。
- 创建 service 时生成全局 service host。
- 记录 host、service、cloud-plane、DNS record 的绑定关系。
- 调用目标 cloud-plane 下发 service spec。
- 根据 cloud-plane 返回的 frontdoor DNS action 写入 DNSPod TXT/CNAME 记录。
- 从各 cloud-plane 拉取 service、node、execution、frontdoor 状态并聚合展示。

不负责：

- 不持有 service runtime truth。
- 不运行 service reconcile loop。
- 不做跨 cloud-plane 调度。
- 不维护 worker node、container、Caddy route、CDN origin。
- 不要求持续在线。

control-plane 可以保留数据库，但只保存全局入口绑定、plane registry、事件日志和必要缓存，不再作为 service desired state owner。

### cloud-plane

cloud-plane 是每个云内部稳定运行的自治控制面。

负责：

- 持有本 plane 的 service desired state。
- 接收 control-plane 下发的 service spec。
- 将 service spec 转换为本 plane 内部 execution。
- 管理 worker node 自动扩缩容。
- 管理 node-agent work item 和 execution 状态。
- 管理本 plane 的 Caddy route。
- 创建和维护本 provider 的 CDN domain。
- 向 control-plane 返回 frontdoor 所需的 DNS action。
- 在 control-plane 离线时继续维护已部署服务。

不负责：

- 不直接修改 DNSPod。
- 不持有全局 service 命名空间。
- 不决定其他 cloud-plane 的服务归属。

### node-agent

node-agent 继续保持简单运行节点代理。

负责：

- 注册到 cloud-plane。
- 上报 heartbeat 和节点容量。
- 拉取并执行 work item。
- 启动、停止、清理单容器 workload。
- 上报 execution 结果和 workload 日志。

## Service 所属关系

service 属于某一个明确的 cloud-plane。

创建 service 时必须指定目标 cloud-plane。control-plane 不做自动选择，也不做调度。

service truth 存在目标 cloud-plane。control-plane 可以缓存和展示 service 状态，但缓存不是 truth。

## DNS 与 CDN

DNS 是全局入口资源，由 control-plane 统一拥有。

CDN 是 provider-local 资源，由对应 cloud-plane 拥有。

原因：

- DNS zone 是多 cloud-plane 的共享资源，需要一个唯一 owner。
- CDN domain、origin、公网入口、Caddy route 与 provider 和 cloud-plane 强绑定，应该由 cloud-plane 管理。
- cloud-plane 不应持有 DNSPod 写权限。

## 创建 Service 流程

目标流程：

1. 用户通过 control-plane 创建 service，并指定 cloud-plane。
2. control-plane 生成 service host。
3. control-plane 保存 host -> service -> cloud-plane 的 pending 绑定。
4. control-plane 调用目标 cloud-plane 下发 service spec。
5. cloud-plane 保存 service desired state。
6. cloud-plane 创建或推进 CDN domain。
7. cloud-plane 返回 frontdoor DNS action。
8. control-plane 写入 DNSPod TXT 验证记录或 CNAME 记录。
9. control-plane 后续访问时继续推进 pending frontdoor 状态。
10. cloud-plane 独立维护 service runtime。

CDN 需要域名归属验证时，流程允许多次推进：

1. cloud-plane 返回 TXT verification action。
2. control-plane 写 TXT。
3. control-plane 再调用 cloud-plane 推进 CDN 验证。
4. cloud-plane 返回 CDN CNAME。
5. control-plane 写 CNAME。
6. control-plane 删除临时 TXT。

## 删除 Service 流程

目标流程：

1. 用户通过 control-plane 删除 service。
2. control-plane 调用目标 cloud-plane 删除 service。
3. cloud-plane 删除本地 service desired state，并清理 execution、Caddy route、CDN domain。
4. cloud-plane 返回需要删除的 DNS record。
5. control-plane 删除 DNSPod CNAME/TXT 记录。
6. control-plane 删除 host binding。

如果 control-plane 中途退出，下次启动时应能从 host binding 和 cloud-plane 状态继续推进清理。

## Serverless-ready 要求

control-plane 后续可以部署为 serverless container，但当前先按 serverless-ready 架构实现。

要求：

- control-plane 启动时不依赖本地状态。
- control-plane 不跑必须常驻的后台循环。
- control-plane 的 API 请求可以按需读取 cloud-plane 状态。
- 长流程以 pending 状态推进，不依赖单个 HTTP 请求长时间阻塞。
- 已部署 workload 不依赖 control-plane 存活。
- DNSPod 权限后续可以通过云角色绑定授予 control-plane。

## 暂不做

- 不做 Docker Compose runtime。
- 不做多副本 service。
- 不做跨 cloud-plane 自动调度。
- 不做复杂 release、灰度、权重流量。
- 不自建 Prometheus、Loki、Jaeger、Grafana。
- 不把 control-plane 拆成多个函数。
