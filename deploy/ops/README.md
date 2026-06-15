# mini-cloud ops

`deploy/ops` 是 mini-cloud 的真实云部署入口。它负责检查本机和云账号权限、准备基础设施、安装 control-plane/cloud-plane、运行真实 Web e2e，并在实验结束后回收资源。

当前目标形态：

- control-plane 打包成容器并部署到腾讯云 SCF HTTP 函数。
- control-plane Web UI、二进制和配置快照都内置在镜像中。
- 每个 cloud-plane 对应一个 `planes[]` 配置和一个 Terraform workspace。
- 每个 cloud-plane 使用独立入口机，入口机不运行 workload。
- service 必须显式指定 `planeID`。
- 公开 service 使用 `<service-name>.<ingressBaseDomain>`。
- control-plane 写 DNSPod 记录，CNAME 指向对应云厂商 CDN。

## 本机依赖

需要提前安装：

- Go
- Node.js / npm
- Docker
- Terraform
- SSH / SCP
- Tencent Cloud CLI: `tccli`
- Aliyun CLI: `aliyun`

`tccli`、`aliyun` 和 Docker registry 登录状态必须能在当前 shell 中直接使用。

## 配置

准备 ops 配置：

```bash
cp deploy/ops/config.yaml.example deploy/ops/config.yaml
```

`deploy/ops/config.yaml` 不提交。这里保存 token、SCF 配置、TCR 镜像地址、入口域名、二进制路径、Web dist 路径、cloud-plane 列表和腾讯云凭据文件路径。

`deploy/ops/state/` 也不提交。这里保存本地部署状态，例如 control-plane 和 cloud-plane 之间的私有 TLS CA/证书。`install` 会复用已有证书，不会因为只更新 Web UI 就隐式轮换 TLS 信任链。

每个 plane 单独准备 Terraform var file：

```bash
cp deploy/terraform/ops/tencent/terraform.tfvars.example deploy/terraform/ops/tencent/terraform.tfvars
cp deploy/terraform/ops/aliyun/terraform.tfvars.example deploy/terraform/ops/aliyun/terraform.tfvars
```

要求：

- 每个 `planes[]` 使用独立入口机。
- 每个 `planes[]` 使用独立 Terraform workspace。
- `planes[].terraform.dir` 指向对应云厂商的 Terraform root，例如 `deploy/terraform/ops/tencent` 或 `deploy/terraform/ops/aliyun`。
- `planes[].ssh.host` 是本机可用的 SSH alias 或地址。
- `install.ingressBaseDomain` 是 service 生成域名的根，例如 `apps.whatcloud.cn`。
- `controlPlane.scf.publicDomain` 是 control-plane Web/API 域名，例如 `control.apps.whatcloud.cn`。
- `tokens.controlPlaneAdmin`、`tokens.controlPlaneSouthbound`、`tokens.nodeAgent` 必须填写。

腾讯云 cloud-plane 需要腾讯云 AK/SK，因为它要管理腾讯云 runtime node 和 CDN。凭据只从 `provider.tencentCredentialFile` 指向的本地 JSON 文件读取，不从环境变量读取：

```json
{
  "secretId": "replace-with-secret-id",
  "secretKey": "replace-with-secret-key",
  "token": ""
}
```

阿里云 cloud-plane 使用入口机实例角色访问阿里云 API。

## Terraform 边界

Terraform 只负责 bootstrap 资源：

- worker node 所在网络基础资源。
- node security group。
- cloud-plane 入口机需要的基础安全组规则。
- 腾讯云 Lighthouse + CVM VPC 连通所需的 CCN attachment。

Terraform 不负责：

- service。
- runtime worker node。
- CDN service domain。
- DNSPod service CNAME。
- Caddy route。

这些运行态资源由 cloud-plane/control-plane 在 service 生命周期内管理。`destroy` 会额外做实验兜底清理，确保真实云测试后不留资源。

## Check

```bash
go run ./cmd/minictl check --config deploy/ops/config.yaml
```

`check` 只检查，不修改云资源。它会检查：

- 本机工具是否存在。
- ops 配置是否完整。
- Terraform var file 是否存在。
- 腾讯云凭据文件是否可读。
- 每个 plane 的 SSH 是否可连。

## Deploy

```bash
go run ./cmd/minictl deploy --config deploy/ops/config.yaml
```

`deploy` 是组合命令，按顺序执行 `build`、`bootstrap`、`install`。

## Build

```bash
go run ./cmd/minictl build --config deploy/ops/config.yaml
```

`build` 只做本地构建，不修改云资源：

- 构建 release 二进制和 Web UI。

## Bootstrap

```bash
go run ./cmd/minictl bootstrap --config deploy/ops/config.yaml
```

`bootstrap` 只准备云基础设施：

- 为每个 plane 在对应云厂商 Terraform root 中执行 `terraform init`、workspace select/new、`terraform apply`。
- 腾讯云 Lighthouse 模式下补齐 CCN 和防火墙规则。

## Install

```bash
go run ./cmd/minictl install --config deploy/ops/config.yaml
```

`install` 首次安装或修复 control-plane/cloud-plane，不执行 Terraform apply：

- 构建 control-plane 容器镜像并推送到 `controlPlane.scf.image`。
- 读取或创建本地私有 CA/TLS 证书。
- 渲染 control-plane 配置快照，并随镜像部署到 SCF。
- 在每台 cloud-plane 入口机安装或修复 Docker、Postgres、Caddy、Tinyproxy、cloud-plane 和 node-agent artifact。
- 启动 cloud-plane systemd 服务。

入口机不运行 `node-agent`，也不承接 workload。动态创建出来的 worker node 会从对应 cloud-plane 入口机的内网 artifact server 下载并启动 `node-agent`。

## Update

```bash
go run ./cmd/minictl update --config deploy/ops/config.yaml
```

`update` 是日常发布路径，不执行 Terraform apply，也不重装入口机基础服务：

- 构建 control-plane 容器镜像并更新 SCF。
- 不修改 cloud-plane 和 node-agent。
- 不重启 Docker、Caddy、Tinyproxy，也不会碰 Postgres 数据。

如果只修改了前端页面，通常执行：

```bash
go run ./cmd/minictl build --config deploy/ops/config.yaml
go run ./cmd/minictl update --config deploy/ops/config.yaml
```

这会重新构建 Web UI/release 二进制、重新打包 control-plane 镜像并更新 SCF，不会重启 cloud-plane。

## 网络

当前网络链路：

- control-plane -> cloud-plane：跨云 gRPC TLS。
- node-agent -> cloud-plane：同云内网 gRPC。
- worker -> artifact server：同云内网 HTTP。
- 用户 -> service：DNSPod CNAME -> 云厂商 CDN -> cloud-plane Caddy -> worker host port。
- workload -> 公网：业务容器和 Docker daemon 使用 cloud-plane 入口机上的 Tinyproxy，也就是 `workloadProxy`。

worker node 不分配公网 IP。Tinyproxy 是 workload 的受控公网出口，不是 control-plane/cloud-plane/node-agent 控制链路的依赖。

## E2E

```bash
go run ./cmd/minictl e2e --config deploy/ops/config.yaml
```

`e2e` 会在真实云环境中运行 Web UI 流程：

- 打开 control-plane Web。
- 登录。
- 分别在 Tencent 和 Aliyun plane 创建 service。
- 等待 runtime node 创建、node-agent 注册、service readiness 通过。
- 访问 service 域名验证公开入口。
- 删除 service。
- 验证 runtime node、CDN 和 DNS 记录被清理。
- 执行一次 control-plane `update`。
- 再运行一轮 Web UI/API smoke，验证更新后的 control-plane 仍能登录、读取 plane 和 services。
- 最后执行 `destroy` 回收实验环境。

## Destroy

```bash
go run ./cmd/minictl destroy --config deploy/ops/config.yaml
```

`destroy` 会逐个 plane 回收：

- cloud-plane 入口机上的 cloud-plane、Caddy、Tinyproxy 和 artifact。
- 当前 plane 创建的 worker node。
- 当前 plane 创建的 CDN domain。
- 当前 plane 对应的 DNSPod service CNAME。
- Terraform 管理的 node 网络资源。
- 腾讯云 Lighthouse 模式下的防火墙规则和 CCN attachment。

所有 plane 回收完成后，`destroy` 会删除 SCF control-plane 函数和 control-plane 域名 CNAME。

## 注意事项

- `deploy/ops/config.yaml`、`*.tfvars`、token、云账号密钥不能提交。
- CCN attachment 在腾讯云控制台可能需要手动同意。
- DNS/CDN 生效有延迟，e2e 会等待，但控制台显示可能继续延迟。
- 如果 `deploy` 中断，先运行 `destroy` 做兜底回收，再重新执行 `deploy`。
- 如果只想做静态检查，用 `check`，不要直接运行 `deploy`。
