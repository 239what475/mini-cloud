# mini-cloud lab deployment

`deploy/lab` 是真实云 lab 的脚本入口。

Terraform 只负责云资源底座：

- VPC / subnet
- platform host，或复用已有腾讯云轻量应用服务器
- EIP
- security groups
- SSH key
- provider role
- existing Tencent CCN，用于打通轻量应用服务器和 runtime CVM VPC

平台进程安装和销毁前清理由这里的脚本负责。

## Bootstrap

```bash
deploy/lab/bootstrap.sh
```

该脚本执行 `deploy/terraform/lab` 的 `terraform init` 和 `terraform apply`。
默认使用 `-auto-approve`，如果需要改回 Terraform 交互确认，可以设置：

```bash
TERRAFORM_APPLY_ARGS= deploy/lab/bootstrap.sh
```

如果 `platform_mode = "existing_lighthouse"`，脚本还会把当前腾讯云轻量应用服务器关联到 `existing_ccn_id` 指定的 CCN，自动接受 Lighthouse 发起的 pending VPC 关联，并等待关联状态变成 `ACTIVE`。

如果 lab 需要临时 HTTP 泛解析，可以让 bootstrap 顺手创建 DNSPod CNAME 记录：

```bash
LAB_DNS_DOMAIN=whatcloud.cn \
LAB_DNS_SUBDOMAIN='*.apps' \
LAB_DNS_VALUE=tx-origin.whatcloud.cn. \
deploy/lab/bootstrap.sh
```

如果同名记录已经存在且值不同，脚本会拒绝覆盖。

## Install

```bash
CONTROL_PLANE_ADMIN_TOKEN=replace-with-token \
CONTROL_PLANE_SOUTHBOUND_TOKEN=replace-with-token \
NODE_AGENT_BOOTSTRAP_TOKEN=replace-with-token \
deploy/lab/install.sh
```

`install.sh` 默认通过 Terraform output 获取入口机信息，再通过 SSH 安装：

- Docker
- Postgres
- Caddy
- Tinyproxy
- `control-plane`
- `cloud-plane`

入口机不运行 `node-agent`，也不承接 workload。`install.sh` 会把本地 `node-agent` 安装到 `/opt/mini-cloud/artifacts/node-agent-linux-amd64`，并让 Caddy 在入口机私网 `18082` 端口提供静态下载；动态创建出来的 runtime node 会从这个内网 URL 下载并启动 `node-agent`。

默认 artifact URL 形如 `http://<platform-private-ip>:18082/node-agent-linux-amd64`。Terraform 会让 runtime 安全组访问入口机 artifact 端口；复用腾讯云轻量应用服务器时，`bootstrap.sh` 会同步创建 Lighthouse 防火墙规则。

如果不使用 Terraform output，也可以直接指定 `PLATFORM_HOST`，并显式提供 cloud-plane 创建 runtime node 所需的 provider 配置：

```bash
PLATFORM_HOST=myserver \
PLATFORM_NAME=mini-cloud-lab-tencent \
PROVIDER=tencent \
REGION_ID=ap-guangzhou \
ZONE_ID=ap-guangzhou-6 \
SUBNET_CIDR_BLOCK=10.1.0.0/24 \
RUNTIME_PROVIDER_SPEC_JSON='{"provider":"tencent","instanceType":"S5.MEDIUM2","imageId":"img-xxx","keyIds":["skey-xxx"],"vpcId":"vpc-xxx","subnetId":"subnet-xxx","securityGroupIds":["sg-xxx"],"systemDiskType":"CLOUD_PREMIUM","systemDiskSizeGiB":50}' \
CONTROL_PLANE_BINARY_PATH=dist/release/linux-amd64/control-plane \
CLOUD_PLANE_BINARY_PATH=dist/release/linux-amd64/cloud-plane \
CONTROL_PLANE_ADMIN_TOKEN=replace-with-token \
CONTROL_PLANE_SOUTHBOUND_TOKEN=replace-with-token \
NODE_AGENT_BOOTSTRAP_TOKEN=replace-with-token \
deploy/lab/install.sh
```

复用腾讯云轻量应用服务器时，推荐走 Terraform 的 `platform_mode = "existing_lighthouse"`。这会创建 runtime CVM VPC，并把 runtime VPC 和 Lighthouse 都关联到 `existing_ccn_id` 指定的已有 CCN。Lighthouse 和 runtime VPC 的 CIDR 不能冲突。

可选环境变量：

- `PLATFORM_HOST`
- `SSH_USER`
- `SSH_KEY`
- `SSH_OPTS`
- `INSTALL_ROOT`
- `CONTROL_PLANE_BINARY_PATH`
- `CLOUD_PLANE_BINARY_PATH`
- `NODE_AGENT_BINARY_PATH`
- `ARTIFACT_HTTP_PORT`
- `INGRESS_BASE_DOMAIN`
- `REGISTRY_MIRROR`
- `WORKLOAD_LOG_LOKI_URL`
- `WORKLOAD_LOG_LOKI_TENANT_ID`
- `WORKLOAD_OTLP_ENDPOINT`

## Destroy

```bash
deploy/lab/destroy.sh
```

该脚本先用云厂商 CLI 清理当前平台名下的 runtime 节点，再执行 `terraform destroy` 删除 Terraform 管理的基础设施。
默认使用 `-auto-approve`，如果需要改回 Terraform 交互确认，可以设置：

```bash
TERRAFORM_DESTROY_ARGS= deploy/lab/destroy.sh
```

如果当前 lab 使用 `platform_mode = "existing_lighthouse"`，脚本会在 `terraform destroy` 前解除 Lighthouse 与当前 CCN 的关联。
脚本还会先停止并删除入口机上的 lab 安装件，避免 control-plane/cloud-plane 在销毁过程中继续创建 runtime 节点：

- `mini-cloud-control-plane.service`
- `mini-cloud-cloud-plane.service`
- `mini-cloud-caddy`
- `mini-cloud-postgres`
- `mini-cloud-postgres-data`
- `/etc/mini-cloud`
- `/opt/mini-cloud`
- `tinyproxy`

如果 Terraform state 已经被清掉但入口机还有残留，可以显式指定入口机继续清理：

```bash
PLATFORM_HOST=myserver deploy/lab/destroy.sh
```

如果 bootstrap 创建过临时 DNS 记录，destroy 使用同一组参数会删除它；设置 `LAB_DNS_VALUE` 时，只有记录值匹配才会删除：

```bash
LAB_DNS_DOMAIN=whatcloud.cn \
LAB_DNS_SUBDOMAIN='*.apps' \
LAB_DNS_VALUE=tx-origin.whatcloud.cn. \
deploy/lab/destroy.sh
```

需要本机安装对应 provider CLI：

- Aliyun: `aliyun`
- Tencent Cloud: `tccli`
