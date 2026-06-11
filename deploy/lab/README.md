# mini-cloud lab deployment

`deploy/lab` 是真实云 lab 的本地编排入口。当前 lab 以 multi backend 为目标：

- control-plane 安装一次
- 每个 cloud-plane 对应一个 `planes[]` 配置
- 每个 plane 使用独立 Terraform workspace
- service 必须显式指定 `planeID`
- 公开 service 使用 `<service-name>.<ingressBaseDomain>`，DNSPod CNAME 指向对应云厂商 CDN

## Config

准备 lab 配置：

```bash
cp deploy/lab/lab.yaml.example deploy/lab/lab.yaml
```

`deploy/lab/lab.yaml` 不提交。token 写在这个本地配置文件里。

每个 plane 单独准备 Terraform var file，例如：

```bash
cp deploy/terraform/lab/terraform.tfvars.example deploy/terraform/lab/tencent.tfvars
cp deploy/terraform/lab/terraform.tfvars.example deploy/terraform/lab/aliyun.tfvars
```

`provider_name`、`platform_name`、入口机、VPC、地域和规格等字段必须按 plane 分别填写。不要让两个 plane 共用同一个 Terraform workspace。

腾讯云凭据只从 `provider.tencentCredentialFile` 指向的 JSON 文件读取，并写入远端腾讯云 cloud-plane 配置。不要通过环境变量提供平台 token 或腾讯云 AK/SK。

```json
{
  "secretId": "replace-with-secret-id",
  "secretKey": "replace-with-secret-key",
  "token": ""
}
```

## Bootstrap

```bash
go run ./cmd/labctl bootstrap --config deploy/lab/lab.yaml
```

`bootstrap` 会对每个 plane 执行：

- `terraform init`
- 选择或创建 `planes[].terraform.workspace`
- `terraform apply`
- 腾讯云 Lighthouse 模式下补齐 CCN 和防火墙规则

## Install

```bash
go run ./cmd/labctl install --config deploy/lab/lab.yaml
```

`install` 会先在 `controlPlane.ssh.host` 安装 control-plane，然后逐个在 plane 入口机安装 cloud-plane：

- Docker
- Postgres
- Caddy
- Tinyproxy
- cloud-plane
- node-agent artifact server

入口机不运行 `node-agent`，也不承接 workload。动态创建出来的 runtime node 会从对应 cloud-plane 入口机的内网 artifact URL 下载并启动 `node-agent`。

## Destroy

```bash
go run ./cmd/labctl destroy --config deploy/lab/lab.yaml
```

`destroy` 会逐个 plane 回收：

- cloud-plane 入口机上的 cloud-plane、Caddy、Tinyproxy 和 artifact
- 当前 plane 创建的 runtime node
- 当前 plane 创建的 CDN 域名和 DNSPod 记录
- Terraform 管理的 runtime 网络资源
- 腾讯云 Lighthouse 模式下的防火墙规则和 CCN 关联

所有 plane 回收完成后，`destroy` 再卸载 control-plane。

## Requirements

本机需要安装：

- Terraform
- Go
- SSH / SCP
- Aliyun: `aliyun`
- Tencent Cloud: `tccli`
