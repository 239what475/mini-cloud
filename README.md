# mini-cloud

`mini-cloud` 是一个面向运维体验的 CaaS（Container as a Service）原型。它不尝试复刻 Kubernetes，而是聚焦一个清晰场景：

> 用户通过 Web UI 部署一个单容器 service 到指定云厂商，平台自动准备 worker node、配置公网入口，并提供实验环境一键回收。

项目把真实云上的控制面、运行面、节点执行器、云厂商 driver、DNS/CDN 入口和端到端测试串成一个闭环。

## TL;DR

- **多云运行面**：同一套 control-plane 同时管理腾讯云和阿里云两个 cloud-plane。
- **自动化运维**：创建 service → 自动创建 worker node → 自动部署容器 → 自动配置 DNS/CDN/Caddy 入口。
- **完整运维 CLI**：`minictl` 提供 `check`、`deploy`、`update`、`destroy` 等命令，覆盖真实云环境从部署到回收的完整生命周期。

## Architecture

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

Public service traffic:

user
  -> DNSPod CNAME
  -> provider CDN
  -> cloud-plane Caddy
  -> worker node host port
  -> container
```

## Tech Stack

| 层次 | 选型 |
|------|------|
| 语言 | Go 1.26 + TypeScript (React) |
| RPC 框架 | gRPC + Protobuf（buf 管理） |
| 传输安全 | control-plane -> cloud-plane 使用私有 CA TLS、client certificate 和 bearer token；node-agent -> cloud-plane 使用服务端 TLS 校验和 token |
| 数据库 | PostgreSQL（cloud-plane 持久化 service 状态、work item、execution 历史） |
| 容器运行时 | Docker API（worker 上拉取并启动业务容器） |
| 反向代理 | Caddy（cloud-plane 入口机 Host 路由） |
| CDN | 腾讯云 CDN / 阿里云 CDN |
| DNS | DNSPod API（腾讯云） |
| IaC | Terraform（多云 provider、多 workspace） |
| 前端 | React + TanStack Query + Vite |
| E2E 测试 | Playwright（浏览器驱动，操作真实 Web UI） |
| 部署形态 | 腾讯云 SCF（Serverless 控制面） + Tencent Lighthouse / Aliyun ECS 入口机 + 动态 worker node |

## Components

### control-plane

control-plane 是用户入口、全局 DNS owner 和多 cloud-plane 聚合门户。运行在腾讯云 SCF HTTP 函数上，Web UI、二进制和配置快照打包进单个容器镜像。

负责：

- 提供 Web / API。
- 持有 plane 列表、登录 token、DNSPod 配置和事件日志。
- 创建 service 时生成 `<service-name>.<baseDomain>`。
- 将 service spec 下发到用户指定的 cloud-plane。
- 根据 cloud-plane 返回的 CDN verification / CNAME 信息维护 DNSPod 记录。
- 按需读取各 cloud-plane snapshot，展示 service、node、execution 和 frontdoor 状态。

不负责：

- 不持有 service runtime truth。
- 不常驻运行 service reconcile loop。
- 不做跨 cloud-plane 调度。
- 不直接管理 worker node、container、Caddy route 或云厂商 CDN。

### cloud-plane

cloud-plane 是某个云内的自治运行面，部署在该云的入口机上。

负责：

- 持有本 plane 的 service desired state。
- 接收 control-plane 下发的 service spec。
- 自动创建和回收 worker node。
- 管理 node-agent work item 和 execution 状态。
- 维护本云 Caddy route 和 CDN domain。
- 向 control-plane 返回 DNS 所需的 verification / CNAME 信息。

不负责：

- 不直接修改 DNSPod。
- 不持有全局 service 命名空间。
- 不管理其他 cloud-plane 的资源。
- 不承接 workload；workload 只运行在动态创建的 worker node 上。

### node-agent

node-agent 是 worker node 上的执行器。

负责：

- 注册到所属 cloud-plane。
- 上报 heartbeat 和节点容量。
- 拉取 work item。
- 启动、停止、清理单容器 workload。
- 上报 execution 结果，启动失败时截取容器尾日志辅助诊断。

node-agent 不知道 control-plane，也不访问云厂商 API。

## Network Model

- 用户访问 service：公网 → DNSPod CNAME → 云厂商 CDN → cloud-plane Caddy → worker node host port。
- control-plane → cloud-plane：跨云 gRPC，私有 CA TLS、client certificate 和 bearer token。
- node-agent → cloud-plane：同云内网 gRPC，服务端 TLS 校验和 token。
- worker 下载 node-agent：从 cloud-plane 入口机内网 artifact server 下载。
- workload 出公网：业务容器和 Docker daemon 通过入口机 Tinyproxy（`workloadProxy`）。

worker node 默认不分配公网 IP。Tinyproxy 是 workload 的受控公网出口，不是控制链路的依赖。

## Product Scope

当前保留：

- Aliyun + Tencent 两套 backend。
- service 显式指定 cloud-plane。
- 自动扩缩 worker node。
- service name 自动生成三级域名。
- Caddy + provider CDN + DNSPod 的入口链路。
- 简单 readiness、service 状态、节点状态、事件和基础 metrics。
- 真实云 ops 的 `check`、`deploy`、`update`、`e2e`、`destroy`。

当前不做：

- 多副本 service。
- Docker Compose runtime。
- 跨 cloud-plane 自动调度。
- 用户自带域名。
- release、灰度、权重流量。
- 自建 Prometheus、Loki、Jaeger、Grafana。
- 复杂 RBAC、多租户、incident、runbook、SLO 派生视图。

## Repository Layout

```
cmd/               control-plane、cloud-plane、node-agent、minictl 入口
internal/
  controlplane/    control-plane API、配置、模型和 cloud-plane 调用
  cloudplane/      cloud-plane gRPC、运行态控制器、provider driver、
                   store、frontdoor、Caddy
  nodeagent/       worker 注册、heartbeat、workload 执行和失败诊断
  ops/             真实云检查、部署、回收和 e2e 编排
  transport/       gRPC TLS、bearer token、request ID 等公共传输层
proto/             gRPC 协议定义
web/               React 控制台
deploy/
  container/       control-plane 容器构建文件
  terraform/       真实云 bootstrap 底座（Tencent / Aliyun / 公共 module）
  ops/             真实云 ops 配置和说明
```

## Development

常用质量门禁：

```bash
make check
```

分项命令：

```bash
go test ./...                     # 运行测试
go vet ./...                      # Go 静态检查
npm --prefix web run check        # 前端检查
buf lint                          # Proto 检查
```

构建发布产物：

```bash
make build          # 构建本机 Go 二进制
make release        # 构建 linux/amd64 二进制和 Web UI
make image          # 构建 control-plane 容器镜像
```

生成 proto 代码：

```bash
make proto
```

## Real Cloud Ops

真实云操作通过 Makefile 暴露。Makefile 会先构建本地 `minictl`，再执行对应动作；日常使用不需要直接 `go run ./cmd/minictl`。

### 前置准备

```bash
cp deploy/ops/config.yaml.example deploy/ops/config.yaml
cp deploy/terraform/ops/tencent/terraform.tfvars.example deploy/terraform/ops/tencent/terraform.tfvars
cp deploy/terraform/ops/aliyun/terraform.tfvars.example deploy/terraform/ops/aliyun/terraform.tfvars
```

按要求填写各文件中的 token、云账号密钥、域名和 SSH 主机信息。

### 命令一览

```bash
make release
make deploy
make update
make e2e
make destroy
```

### 命令语义

| 命令 | 作用 | 修改云资源 |
|------|------|-----------|
| `release` | 构建 release 二进制和 Web UI | 否 |
| `update` | 只更新 control-plane / SCF，不动 cloud-plane | 是 |
| `deploy` | 组合命令，先做部署前检查，再构建产物、准备基础设施并安装 control-plane/cloud-plane | 是 |
| `e2e` | 在真实云中跑完整 Web 流程，最后自动 destroy | 是 |
| `destroy` | 回收 cloud-plane、worker node、CDN/DNS 记录、Terraform 资源和 SCF control-plane | 是 |

`deploy/ops/config.yaml`、`deploy/ops/state/`、Terraform var file、token 和云账号密钥只保存在本地，不提交。

更多细节见 [`deploy/ops/README.md`](deploy/ops/README.md)。

## Testing

单元测试：

```bash
make check          # 质量门禁：gofmt、go test、go vet、staticcheck、golangci-lint、buf lint 等
```

端到端验证：

```bash
make e2e
```

`e2e` 在真实云环境中通过 Playwright 驱动 Web UI，完整验证 service 创建、公网访问、control-plane 更新和资源回收。具体覆盖范围见 [`DEMO.md`](DEMO.md)。

## Docs

- [`DESIGN.md`](DESIGN.md)：架构取舍和非目标说明。
- [`DEMO.md`](DEMO.md)：完整端到端演示流程和手动操作 checklist。
- [`deploy/ops/README.md`](deploy/ops/README.md)：真实云部署和回收细节。
