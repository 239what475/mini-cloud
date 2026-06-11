# mini-cloud lab deployment

`deploy/lab` 是真实云 lab 的本地编排入口。

Terraform 只负责云资源底座：

- runtime VPC / subnet / security group
- 复用已有入口机：阿里云 ECS 或腾讯云轻量应用服务器
- security groups
- SSH key
- existing Tencent CCN，用于打通轻量应用服务器和 runtime CVM VPC

平台进程安装、入口机清理、Lighthouse CCN 补齐和 Lighthouse 防火墙补齐由 `labctl` 负责。

## Config

先准备 Terraform 变量：

```bash
cp deploy/terraform/lab/terraform.tfvars.example deploy/terraform/lab/terraform.tfvars
```

再准备 lab 配置：

```bash
cp deploy/lab/lab.yaml.example deploy/lab/lab.yaml
```

`deploy/lab/lab.yaml` 不提交。token 写在这个本地配置文件里。

如果使用腾讯云，`install` 只读取 `provider.tencentCredentialFile` 指向的 JSON 凭据文件，并把凭据写进远端 `cloud-plane.yaml`。不要通过环境变量提供平台 token 或腾讯云 AK/SK。

腾讯云凭据文件格式固定为：

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

`bootstrap` 会执行 `deploy/terraform/lab` 的 `terraform init` 和 `terraform apply`。默认参数来自 `lab.yaml`：

```yaml
terraform:
  applyArgs: ["-auto-approve"]
```

如果 `platform_mode = "existing_lighthouse"`，`bootstrap` 还会：

- 把当前腾讯云轻量应用服务器关联到 `existing_ccn_id` 指定的 CCN
- 自动接受 Lighthouse 发起的 pending VPC 关联
- 等待关联状态变成 `ACTIVE`
- 给 Lighthouse 防火墙补齐 runtime subnet 到 cloud-plane gRPC、tinyproxy 和 artifact server 的规则

如果 lab 需要本地 Caddy 入口路由，在 `lab.yaml` 里配置托管 service 域名：

```yaml
install:
  ingressBaseDomain: apps.whatcloud.cn
```

`bootstrap` 不再创建 wildcard CDN/DNS。cloud-plane 当前只读取 `ingress.baseDomain` 并把 `<service-name>.<ingressBaseDomain>` 路由应用到入口机上的 Caddy；service 级 CDN/DNS frontdoor 后续由 cloud-plane 管理。

## Install

```bash
go run ./cmd/labctl install --config deploy/lab/lab.yaml
```

`install` 默认通过 Terraform output 获取入口机信息，再通过 SSH 安装：

- Docker
- Postgres
- Caddy
- Tinyproxy
- `control-plane`
- `cloud-plane`

入口机不运行 `node-agent`，也不承接 workload。`install` 会把本地 `node-agent` 安装到 `/opt/mini-cloud/artifacts/node-agent-linux-amd64`，并让 Caddy 在入口机私网 `18082` 端口提供静态下载；动态创建出来的 runtime node 会从这个内网 URL 下载并启动 `node-agent`。

默认 artifact URL 形如：

```text
http://<platform-private-ip>:18082/node-agent-linux-amd64
```

Terraform 会让 runtime 安全组访问入口机 artifact 端口；复用腾讯云轻量应用服务器时，`bootstrap` 会同步创建 Lighthouse 防火墙规则。

如果不使用 Terraform output，可以在 `lab.yaml` 显式指定入口机：

```yaml
ssh:
  host: myserver
```

## Destroy

```bash
go run ./cmd/labctl destroy --config deploy/lab/lab.yaml
```

`destroy` 会先卸载复用入口机上的平台进程，再用云厂商 CLI 清理当前平台名下的 runtime 节点，最后执行 `terraform destroy` 删除 Terraform 管理的 runtime 基础设施。默认参数来自 `lab.yaml`：

```yaml
terraform:
  destroyArgs: ["-auto-approve"]
```

如果当前 lab 使用 `platform_mode = "existing_lighthouse"`，`destroy` 会在 `terraform destroy` 前：

- 停止并删除入口机上的 lab 安装件
- 删除 Lighthouse 防火墙里由 bootstrap 补齐的 runtime 访问规则
- 解除 Lighthouse 与当前 CCN 的关联

入口机会清理：

- `mini-cloud-control-plane.service`
- `mini-cloud-cloud-plane.service`
- `mini-cloud-caddy`
- `mini-cloud-postgres`
- `mini-cloud-postgres-data`
- `/etc/mini-cloud`
- `install.root`
- `tinyproxy`

如果 Terraform state 已经被清掉但入口机还有残留，可以在 `lab.yaml` 显式指定入口机后继续执行 destroy：

```yaml
ssh:
  host: myserver
```

## Requirements

本机需要安装：

- Terraform
- Go
- SSH / SCP
- Aliyun: `aliyun`
- Tencent Cloud: `tccli`
