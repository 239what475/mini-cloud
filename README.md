# mini-cloud

`mini-cloud` 是一个面向运维体验的 CaaS demo。它不尝试复刻 Kubernetes，而是聚焦一个清晰场景：

> 把一个单容器 service 部署到指定云平面，平台自动准备 worker node、入口流量、状态展示和实验环境回收。

这个项目的重点是把真实云上的控制面、运行面、节点执行器、云厂商 driver、DNS/CDN 入口和端到端测试串成一个闭环。

## Highlights

- **多云运行面**：同一套 control-plane 可以管理 Tencent Cloud 和 Aliyun 两个 cloud-plane。
- **显式运维模型**：创建 service 时必须指定 cloud-plane，不内置半个调度器。
- **自动 worker 扩缩容**：cloud-plane 根据 service 运行态创建和回收 worker node。
- **真实入口链路**：`<service-name>.<base-domain>` 通过 DNSPod CNAME 指向对应云厂商 CDN，再回源到 cloud-plane Caddy。
- **Stateless control-plane**：control-plane 作为门户和 DNS owner，不保存 service runtime truth；运行态事实来自各 cloud-plane snapshot。
- **真实云 e2e**：`minictl e2e` 会在 Tencent + Aliyun 上创建 service、验证公网 200、测试 control-plane update，并最终 destroy 回收资源。

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

## Components

### control-plane

control-plane 是用户入口、全局 DNS owner 和多 cloud-plane 聚合门户。当前真实云部署形态是腾讯云 SCF HTTP 函数，Web UI、二进制和配置快照都打包进容器镜像。

负责：

- 提供 Web/API。
- 持有 plane 列表、登录 token、DNSPod 配置和简单事件日志。
- 创建 service 时生成 `<service-name>.<serviceBaseDomain>`。
- 调用用户指定的 cloud-plane 下发 service spec。
- 根据 cloud-plane 返回的 CDN verification/CNAME 信息维护 DNSPod 记录。
- 按请求读取各 cloud-plane snapshot，用于展示 service、node、execution 和 frontdoor 状态。

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
- 向 control-plane 返回 DNS 所需的 verification/CNAME 信息。

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
- 上报 execution 结果，并在启动失败时截取容器尾日志辅助诊断。

node-agent 不知道 control-plane，也不访问云厂商 API。

## Network Model

- 用户访问 service：公网用户 -> DNSPod CNAME -> 云厂商 CDN -> cloud-plane Caddy -> worker node host port。
- control-plane 到 cloud-plane：跨云 gRPC，使用私有 CA 生成的 TLS 证书保护。
- node-agent 到 cloud-plane：同云内网 gRPC。
- worker 下载 node-agent：从 cloud-plane 入口机内网 artifact server 下载。
- workload 出公网：worker 上的业务容器和 Docker daemon 通过入口机上的 Tinyproxy，也就是 `workloadProxy`。

worker node 默认不分配公网 IP。Tinyproxy 不是控制链路依赖，而是 workload 的受控公网出口。

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

- `cmd/`: `control-plane`、`cloud-plane`、`node-agent`、`minictl` 入口。
- `internal/controlplane/`: control-plane API、配置、模型和 cloud-plane 调用。
- `internal/cloudplane/`: cloud-plane gRPC、运行态控制器、provider driver、store、frontdoor、Caddy。
- `internal/nodeagent/`: worker 注册、heartbeat、workload 执行和失败诊断。
- `internal/ops/`: 真实云检查、部署、回收和 e2e 编排。
- `proto/`: gRPC 协议定义。
- `web/`: React 控制台。
- `deploy/container/`: control-plane 容器构建文件。
- `deploy/terraform/`: 真实云 bootstrap 底座。
- `deploy/ops/`: 真实云 ops 配置和说明。

## Development

常用质量门禁：

```bash
make check
```

常用分项：

```bash
go test ./...
go vet ./...
npm --prefix web run check
buf lint
```

构建发布产物：

```bash
make build-release
npm --prefix web run build
make image-control-plane
```

生成 proto：

```bash
make proto
```

## Real Cloud Ops

准备本地配置：

```bash
cp deploy/ops/config.yaml.example deploy/ops/config.yaml
cp deploy/terraform/ops/tencent/terraform.tfvars.example deploy/terraform/ops/tencent/terraform.tfvars
cp deploy/terraform/ops/aliyun/terraform.tfvars.example deploy/terraform/ops/aliyun/terraform.tfvars
```

常用命令：

```bash
go run ./cmd/minictl check --config deploy/ops/config.yaml
go run ./cmd/minictl build --config deploy/ops/config.yaml
go run ./cmd/minictl bootstrap --config deploy/ops/config.yaml
go run ./cmd/minictl install --config deploy/ops/config.yaml
go run ./cmd/minictl update --config deploy/ops/config.yaml
go run ./cmd/minictl deploy --config deploy/ops/config.yaml
go run ./cmd/minictl e2e --config deploy/ops/config.yaml
go run ./cmd/minictl destroy --config deploy/ops/config.yaml
```

命令语义：

- `check`: 检查本机工具、配置、Terraform var file、腾讯云凭据文件和 SSH 连通性。
- `build`: 构建 release 二进制和 Web UI，不修改云资源。
- `bootstrap`: 用 Terraform 准备 worker node 所需的云基础设施。
- `install`: 安装或修复 control-plane/cloud-plane，不执行 Terraform apply。
- `update`: 只更新 control-plane/SCF，适合发布 Web UI 或 control-plane 变更。
- `deploy`: 组合命令，等价于 `build + bootstrap + install`。
- `e2e`: 在真实云中跑完整 Web 流程，最后自动 destroy。
- `destroy`: 回收 cloud-plane、worker node、CDN/DNS 记录、Terraform bootstrap 资源和 SCF control-plane。

`deploy/ops/config.yaml`、`deploy/ops/state/`、Terraform var file、token 和云账号密钥只保存在本地，不提交。

更多真实云部署细节见 [`deploy/ops/README.md`](deploy/ops/README.md)。

## E2E Coverage

`minictl e2e` 当前覆盖：

- 构建 control-plane/cloud-plane/node-agent/Web UI。
- 在 Tencent 和 Aliyun 两个 Terraform root 中 bootstrap 基础资源。
- 部署 control-plane 到腾讯云 SCF。
- 在两台 cloud-plane 入口机安装 Docker、Postgres、Caddy、Tinyproxy、cloud-plane 和 node-agent artifact。
- 通过真实 Web UI 登录。
- 分别在 Tencent 和 Aliyun plane 创建 nginx service。
- 等待 runtime node 创建、node-agent 注册、service readiness 通过。
- 访问 `<service-name>.<ingressBaseDomain>` 验证公网 HTTP 200。
- 删除 service 并验证清理。
- 执行一次 control-plane `update`。
- 更新后再次运行 Web/API smoke。
- 最后执行 `destroy` 回收实验环境。

最近一次真实云 e2e 已完整跑通：Tencent service 和 Aliyun service 均通过公网 HTTP 200，control-plane update 后 smoke 通过，最终 destroy 成功。
