# mini-cloud

`mini-cloud` 是一个面向运维体验的 CaaS（Container as a Service）原型。它不尝试复刻 Kubernetes，而是聚焦一个清晰场景：

```text
创建一个单容器 service
  -> 显式选择一个 cloud-plane
  -> 按需创建 worker node
  -> 通过 node-agent 启动容器
  -> 通过 CDN + Caddy + DNS 暴露公网入口
  -> 验证并回收真实云资源
```

项目把 serverless control-plane、自治 cloud-plane、worker node agent、云厂商 driver、DNS/CDN 入口和浏览器端到端测试串成一个真实云闭环。

## 核心能力

- **多云运行面**：同一套 control-plane 管理 Tencent 和 Aliyun 两个 cloud-plane。
- **明确的运维模型**：用户显式选择目标 cloud-plane，不隐藏成自动调度器。
- **自动 worker 生命周期**：cloud-plane 根据 service 状态创建和回收 worker node。
- **真实公网入口**：service 流量经过 DNSPod CNAME、云厂商 CDN、cloud-plane Caddy 和 worker host port。
- **无状态 control-plane**：control-plane 负责 Web/API、plane registry、DNS 记录和 snapshot 聚合；cloud-plane 持有 runtime truth。
- **真实 E2E**：Playwright 通过 Web UI 操作真实 Tencent/Aliyun 资源，验证后自动回收。

## 架构

```text
                         operator / browser
                                 |
                                 v
                    +--------------------------+
                    | control-plane on SCF     |
                    | - Web UI / API           |
                    | - plane registry         |
                    | - DNSPod CNAME owner     |
                    | - stateless snapshots    |
                    +-------------+------------+
                                  |
                    gRPC over private CA TLS
              +-------------------+-------------------+
              |                                       |
              v                                       v
 +-----------------------------+        +-----------------------------+
 | Tencent cloud-plane         |        | Aliyun cloud-plane          |
 | - service desired state     |        | - service desired state     |
 | - worker node lifecycle     |        | - worker node lifecycle     |
 | - CDN domain lifecycle      |        | - CDN domain lifecycle      |
 | - Caddy route lifecycle     |        | - Caddy route lifecycle     |
 +-------------+---------------+        +-------------+---------------+
               |                                      |
      intranet | node-agent gRPC             intranet | node-agent gRPC
               v                                      v
 +-----------------------------+        +-----------------------------+
 | worker node                 |        | worker node                 |
 | - node-agent                |        | - node-agent                |
 | - Docker workload           |        | - Docker workload           |
 | - host port mapping         |        | - host port mapping         |
 +-----------------------------+        +-----------------------------+

Public traffic:

user
  -> DNSPod CNAME
  -> provider CDN
  -> cloud-plane Caddy
  -> worker node host port
  -> container
```

## 组件

| 组件 | 职责 |
|------|------|
| `control-plane` | Web/API 入口、plane registry、登录 token、DNSPod 记录、cloud-plane snapshot 聚合。 |
| `cloud-plane` | 单个云内的 runtime truth、service desired state、worker node、Caddy route、provider CDN domain。 |
| `node-agent` | worker 注册、heartbeat、Docker workload 执行、execution 上报。 |
| `minictl` | Makefile 背后的本地运维工具，支撑 `deploy`、`update`、`e2e`、`destroy`。 |

## 范围

当前保留：

- Aliyun 和 Tencent 两套 backend。
- 每个 service 是一个单容器 workload。
- service 必须显式指定 `planeID`，不做跨云自动调度。
- service name 自动生成域名。
- Caddy + provider CDN + DNSPod 入口链路。
- 基础 service、node、event、metric 可见性。
- 真实云 deploy、update、e2e 和 destroy。

当前不做：

- 多副本。
- Docker Compose runtime。
- 用户自定义域名。
- release/version、灰度、权重流量。
- 自建 Prometheus、Loki、Tempo、Grafana。
- 复杂 RBAC、多租户、incident、runbook、SLO 产品能力。

## 常用命令

```bash
make check      # 本地质量门禁
make build      # 构建本机二进制
make release    # 构建 linux/amd64 二进制和 Web UI
make image      # 构建 control-plane 容器镜像
make deploy     # 部署真实云环境
make update     # 只更新 serverless control-plane
make e2e        # 运行真实 Web UI E2E 并回收
make destroy    # 回收真实云环境
```

真实云操作通过 Makefile 调用 `minictl`。日常使用不需要直接执行 `go run ./cmd/minictl`。

## 快速演示路径

第一次真实部署前，先准备本地 ops 配置和 Terraform 变量文件：

```bash
cp deploy/ops/config.yaml.example deploy/ops/config.yaml
cp deploy/terraform/ops/tencent/terraform.tfvars.example deploy/terraform/ops/tencent/terraform.tfvars
cp deploy/terraform/ops/aliyun/terraform.tfvars.example deploy/terraform/ops/aliyun/terraform.tfvars
```

需要在配置里填好 cloud-plane 入口机、SSH alias、域名、token、TCR/SCF 配置，以及云厂商凭据文件路径。真实云配置不提交到仓库。

推荐演示顺序：

```bash
make check
make e2e
```

`make e2e` 会完成一次完整真实云闭环：

1. 构建 Go 二进制和 Web UI。
2. 部署 serverless control-plane。
3. 并行 bootstrap/install Tencent 和 Aliyun 两个 cloud-plane。
4. 通过真实 Web UI 登录并创建两个 `nginx:alpine` service。
5. 等待 service ready、CDN/DNS 入口可访问。
6. 删除 service。
7. 更新 control-plane 并做 Web smoke。
8. 自动执行 destroy 回收实验资源。

如果只想部署后手动体验：

```bash
make deploy
```

部署完成后，终端会输出 control-plane URL。打开这个 URL，使用 `deploy/ops/config.yaml` 中的 `tokens.controlPlaneAdmin` 登录，然后在 Web UI 中创建 service。service 的公网域名格式是：

```text
<service-name>.<install.ingressBaseDomain>
```

只更新 Web UI 或 control-plane 逻辑时使用：

```bash
make update
```

实验结束后回收真实云资源：

```bash
make destroy
```

## 仓库结构

```text
cmd/               control-plane、cloud-plane、node-agent、minictl
internal/
  controlplane/    Web/API、配置、模型和 cloud-plane client
  cloudplane/      gRPC API、runtime controller、provider driver、store、ingress
  nodeagent/       worker 注册、heartbeat、workload 执行
  ops/             真实云 deploy/update/e2e/destroy 编排
  transport/       TLS、bearer token、request ID 公共传输层工具
proto/             gRPC 协议定义
web/               React 控制台
deploy/
  container/       control-plane 容器构建文件
  terraform/       真实云 bootstrap 资源
  ops/             本地 ops 配置示例和状态目录
doc/               架构、运维和 E2E 文档
```

## 文档

- [架构设计](doc/architecture.md)：架构取舍和非目标。
- [运维操作](doc/operations.md)：真实云部署、更新、网络和回收。
- [端到端验证](doc/e2e.md)：浏览器驱动的真实云验证流程。
