# mini-cloud

`mini-cloud` 是一个面向运维体验的 CaaS demo。它不尝试复刻 Kubernetes，而是聚焦一件事：

把一个单容器 service 部署到明确指定的 cloud-plane，并自动准备运行节点、入口流量和基础状态展示。

## 当前架构

### control-plane

control-plane 是用户入口、全局 DNS owner 和多 cloud-plane 聚合门户。

负责：

- 提供 Web/API。
- 管理 cloud-plane 注册信息。
- 创建 service 时生成 `<service-name>.<serviceBaseDomain>`。
- 保存 host、service、cloud-plane、DNS record 的绑定关系。
- 调用目标 cloud-plane 下发 service spec。
- 根据 cloud-plane 返回的 CDN verification/CNAME 状态维护 DNSPod 记录。
- 按请求读取 cloud-plane snapshot，展示 service、node、execution 和 frontdoor 状态。

不负责：

- 不持有 runtime truth。
- 不运行必须常驻的 service reconcile loop。
- 不做跨 cloud-plane 调度。
- 不直接管理 worker node、container、Caddy route 或 CDN。

### cloud-plane

cloud-plane 是某个云内的自治运行面。

负责：

- 持有本 plane 的 service desired state。
- 接收 control-plane 下发的 service spec。
- 生成本 plane 内部 execution intent。
- 自动创建和回收 worker node。
- 管理 node-agent work item 和 execution 状态。
- 维护本云 Caddy route 和 CDN domain。
- 向 control-plane 返回 DNS 所需的 verification/CNAME 信息。

不负责：

- 不直接修改 DNSPod。
- 不持有全局 service 命名空间。
- 不管理其他 cloud-plane 的资源。

### node-agent

node-agent 是 worker 节点上的执行器。

负责：

- 注册到 cloud-plane。
- 上报 heartbeat 和节点容量。
- 拉取 work item。
- 启动、停止、清理单容器 workload。
- 上报 execution 结果，并在启动失败时截取容器尾日志辅助诊断。

## 产品边界

当前保留：

- Aliyun + Tencent 两套 backend。
- service 显式指定 cloud-plane。
- 自动扩缩 worker node。
- service name 自动生成三级域名。
- Caddy + provider CDN + DNSPod 的入口链路。
- 简单 readiness、service 状态、节点状态、事件和基础 metrics。
- 真实云 lab 的 bootstrap、install、destroy。

当前不做：

- 多副本 service。
- Docker Compose runtime。
- 跨 cloud-plane 自动调度。
- 用户自带域名。
- release、灰度、权重流量。
- 自建 Prometheus、Loki、Jaeger、Grafana。
- 复杂 RBAC、多租户、incident、runbook、SLO 派生视图。

## 目录

- `cmd/`
  - `control-plane`
  - `cloud-plane`
  - `node-agent`
  - `labctl`
- `internal/controlplane/`
  - control-plane API、配置、store、service/DNS/plane 协调逻辑。
- `internal/cloudplane/`
  - cloud-plane gRPC、运行态控制器、provider driver、store、frontdoor、Caddy。
- `internal/nodeagent/`
  - worker 节点注册、heartbeat、workload 执行和失败诊断。
- `internal/lab/`
  - 真实云 lab 的 bootstrap/install/destroy 编排。
- `proto/`
  - gRPC 协议定义。
- `web/`
  - React 控制台。
- `deploy/compose/`
  - 本地开发和集成测试依赖。
- `deploy/terraform/`
  - 真实云 bootstrap 底座。
- `deploy/lab/`
  - 真实云 lab 配置和说明。

## 开发验证

快速检查：

```bash
make check
```

常用分项：

```bash
go test ./...
go vet ./...
npm --prefix web run check
```

构建部署用二进制：

```bash
make build-release
```

生成 proto：

```bash
make proto
```

本地 Postgres 集成测试：

```bash
./scripts/test-integration.sh
```

## 真实云 lab

准备配置：

```bash
cp deploy/lab/lab.yaml.example deploy/lab/lab.yaml
cp deploy/terraform/lab/terraform.tfvars.example deploy/terraform/lab/tencent.tfvars
cp deploy/terraform/lab/terraform.tfvars.example deploy/terraform/lab/aliyun.tfvars
```

完整流程：

```bash
go run ./cmd/labctl bootstrap --config deploy/lab/lab.yaml
make build-release
go run ./cmd/labctl install --config deploy/lab/lab.yaml
go run ./cmd/labctl destroy --config deploy/lab/lab.yaml
```

`deploy/lab/lab.yaml`、Terraform var file、token 和云账号密钥只保存在本地，不提交。示例里的 `myserver-control`、`myserver-tencent`、`myserver2` 只是占位名；每个 cloud-plane 入口机必须是独立 host，control-plane 可以单独部署，也可以和某个入口机同机部署。

更多 lab 细节见 [deploy/lab/README.md](/home/what/myproject/mini-cloud/deploy/lab/README.md)。
