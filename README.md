# mini-cloud

`mini-cloud` 是一个面向运维体验的 CaaS demo。它不复刻 Kubernetes，只聚焦一个清晰场景：

把一个单容器 service 部署到明确指定的 cloud-plane，并自动准备 worker node、入口流量、基础状态展示和实验环境回收。

## 架构

### control-plane

control-plane 是用户入口、全局 DNS owner 和多 cloud-plane 聚合门户。当前部署形态是腾讯云 SCF HTTP 函数，配置和 Web UI 打包进容器镜像。

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
- 生成本 plane 内部 execution。
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

## 网络语义

- 用户访问 service：公网用户 -> DNS/CDN -> cloud-plane 入口机 Caddy -> worker node host port。
- control-plane 到 cloud-plane：跨云 gRPC，使用私有 CA 生成的 TLS 证书保护。
- node-agent 到 cloud-plane：同云内网 gRPC。
- worker 下载 node-agent：从 cloud-plane 入口机内网 artifact server 下载。
- workload 出公网：worker 上的业务容器和 Docker daemon 通过入口机上的 Tinyproxy，也就是 `workloadProxy`。

worker node 默认不分配公网 IP。Tinyproxy 不是控制链路依赖，而是 workload 的受控公网出口。

## 产品边界

当前保留：

- Aliyun + Tencent 两套 backend。
- service 显式指定 cloud-plane。
- 自动扩缩 worker node。
- service name 自动生成三级域名。
- Caddy + provider CDN + DNSPod 的入口链路。
- 简单 readiness、service 状态、节点状态、事件和基础 metrics。
- 真实云 ops 的 `check`、`deploy`、`e2e`、`destroy`。

当前不做：

- 多副本 service。
- Docker Compose runtime。
- 跨 cloud-plane 自动调度。
- 用户自带域名。
- release、灰度、权重流量。
- 自建 Prometheus、Loki、Jaeger、Grafana。
- 复杂 RBAC、多租户、incident、runbook、SLO 派生视图。

## 目录

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

## 本地开发

常用质量门禁：

```bash
make check
```

常用分项：

```bash
go test ./...
go vet ./...
npm --prefix web run check
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

## 真实云流程

准备本地配置：

```bash
cp deploy/ops/config.yaml.example deploy/ops/config.yaml
cp deploy/terraform/ops/terraform.tfvars.example deploy/terraform/ops/tencent.tfvars
cp deploy/terraform/ops/terraform.tfvars.example deploy/terraform/ops/aliyun.tfvars
```

执行流程：

```bash
go run ./cmd/minictl check --config deploy/ops/config.yaml
go run ./cmd/minictl build --config deploy/ops/config.yaml
go run ./cmd/minictl bootstrap --config deploy/ops/config.yaml
go run ./cmd/minictl install --config deploy/ops/config.yaml
go run ./cmd/minictl update --config deploy/ops/config.yaml
go run ./cmd/minictl update-control-plane --config deploy/ops/config.yaml
go run ./cmd/minictl update-cloud-plane --config deploy/ops/config.yaml
go run ./cmd/minictl deploy --config deploy/ops/config.yaml
go run ./cmd/minictl e2e --config deploy/ops/config.yaml
go run ./cmd/minictl destroy --config deploy/ops/config.yaml
```

`deploy` 是组合命令，等价于 `build + bootstrap + install`。如果只改了 Web UI，通常执行 `build + update-control-plane`；如果改了 cloud-plane/node-agent，执行 `build + update-cloud-plane`。两者都不需要重新 bootstrap 云基础设施。

`deploy/ops/config.yaml`、`deploy/ops/state/`、Terraform var file、token 和云账号密钥只保存在本地，不提交。

更多真实云部署细节见 [deploy/ops/README.md](/home/what/myproject/mini-cloud/deploy/ops/README.md)。
