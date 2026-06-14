# mini-cloud ops deployment

`deploy/ops` 是 mini-cloud 的真实云部署入口。当前部署工具以 multi backend 和 serverless control-plane 为目标：

- control-plane 打包成容器并部署到腾讯云 SCF HTTP 函数
- 每个 cloud-plane 对应一个 `planes[]` 配置
- 每个 plane 使用独立 Terraform workspace
- service 必须显式指定 `planeID`
- 公开 service 使用 `<service-name>.<ingressBaseDomain>`，control-plane 写 DNSPod 记录，CNAME 指向对应云厂商 CDN

## Config

准备 ops 配置：

```bash
cp deploy/ops/config.yaml.example deploy/ops/config.yaml
```

`deploy/ops/config.yaml` 不提交。token 写在这个本地配置文件里。

每个 plane 单独准备 Terraform var file，例如：

```bash
cp deploy/terraform/ops/terraform.tfvars.example deploy/terraform/ops/tencent.tfvars
cp deploy/terraform/ops/terraform.tfvars.example deploy/terraform/ops/aliyun.tfvars
```

`planes[].region`、`provider_name`、`platform_name`、入口机、VPC、地域和规格等字段必须按 plane 分别填写。不要让两个 plane 共用同一个 Terraform workspace。

`controlPlane.scf` 配置 SCF 函数和 TCR 镜像仓库。`planes[].ssh.host` 是本机 SSH alias 或地址；每个 `planes[]` 必须使用独立入口机，入口机不承接 workload。

腾讯云凭据只从 `provider.tencentCredentialFile` 指向的 JSON 文件读取，并写入远端腾讯云 cloud-plane 配置。不要通过环境变量提供平台 token 或腾讯云 AK/SK。

```json
{
  "secretId": "replace-with-secret-id",
  "secretKey": "replace-with-secret-key",
  "token": ""
}
```

## Check

```bash
go run ./cmd/minictl check --config deploy/ops/config.yaml
```

`check` 会检查配置、本机工具、腾讯云凭据文件、Terraform var file 和 plane SSH 连通性，不修改云资源。

## Deploy

```bash
go run ./cmd/minictl deploy --config deploy/ops/config.yaml
```

`deploy` 会构建项目、准备基础设施并安装 control-plane/cloud-plane：

- `terraform init`
- 选择或创建 `planes[].terraform.workspace`
- `terraform apply`
- 腾讯云 Lighthouse 模式下补齐 CCN 和防火墙规则
- control-plane 配置渲染成本地快照
- control-plane 二进制、Web UI 和配置快照打进容器镜像
- 镜像推送到 `controlPlane.scf.image`
- 创建或更新 SCF HTTP 函数
- Docker
- Postgres
- Caddy
- Tinyproxy
- cloud-plane
- node-agent artifact server

入口机不运行 `node-agent`，也不承接 workload。动态创建出来的 worker node 会从对应 cloud-plane 入口机的内网 artifact URL 下载并启动 `node-agent`。

## Destroy

```bash
go run ./cmd/minictl destroy --config deploy/ops/config.yaml
```

`destroy` 会逐个 plane 回收：

- cloud-plane 入口机上的 cloud-plane、Caddy、Tinyproxy 和 artifact
- 当前 plane 创建的 worker node
- 当前 plane 创建的 CDN 域名
- Terraform 管理的 node 网络资源
- 腾讯云 Lighthouse 模式下的防火墙规则和 CCN 关联

`destroy` 也会按 provider 兜底删除实验域名下属于当前 plane CDN 的 DNSPod CNAME，避免真实云实验留下入口记录。所有 plane 回收完成后，`destroy` 再删除 SCF control-plane 函数。

## Requirements

本机需要安装：

- Terraform
- Go
- SSH / SCP
- Aliyun: `aliyun`
- Tencent Cloud: `tccli`
