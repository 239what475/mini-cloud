# mini-cloud 部署和运行演示

这份文档描述如何在真实云环境中部署 mini-cloud，以及部署完成后平台的行为。文档不包含 token、云账号密钥、真实资源 ID 等敏感信息。

## 部署拓扑

```text
operator
  |
  v
control-plane on Tencent SCF
  |
  +-- gRPC/TLS --> Tencent cloud-plane entry host
  |                  |
  |                  +-- create worker node
  |                  +-- run nginx container
  |                  +-- expose through Tencent CDN + Caddy
  |
  +-- gRPC/TLS --> Aliyun cloud-plane entry host
                     |
                     +-- create worker node
                     +-- run nginx container
                     +-- expose through Aliyun CDN + Caddy

public user
  -> DNSPod CNAME
  -> provider CDN
  -> cloud-plane Caddy
  -> worker host port
  -> nginx container
```

## 部署流程

### 1. 部署

```bash
make deploy
```

`deploy` 会先检查本机工具链、配置完整性、云凭据和 SSH 连通性，然后按顺序执行三个阶段：

- **release**：构建 control-plane、cloud-plane、node-agent 三个 Linux 二进制和 Web UI 前端。
- **bootstrap**：通过 Terraform 在每个 cloud-plane 对应的云账号中创建网络、安全组等基础资源。
- **install**：构建 control-plane 容器镜像并推送到腾讯云 CCR，部署到 SCF 函数；在每台 cloud-plane 入口机上安装 Docker、Postgres、Caddy、Tinyproxy、cloud-plane 服务，读取或生成本地私有 CA 和 TLS 证书。

### 2. 使用 Web UI

部署完成后，打开 control-plane 公网地址（例如 `http://control.apps.example.com`）：

1. 使用 admin token 登录。
2. 在 dashboard 确认 Tencent 和 Aliyun 两个 plane 均为 ready。
3. 创建 service：填写 name、选择镜像（如 `nginx:alpine`）、指定 plane（Tencent 或 Aliyun）。
4. 观察 service 状态变化，最终变成 `ready / running`。
5. 浏览器访问 `<service-name>.<baseDomain>`，验证 nginx 欢迎页。
6. 在另一个 plane 上再创建一个 service，确认两个 service 分别走对应云厂商的公网入口。
7. 删除 service，观察状态变为 `deleted`，对应 worker node 被自动回收。

### 3. 日常更新

当只需要更新 control-plane（例如发布新的 Web UI 或 control-plane 逻辑）：

```bash
make update
```

`update` 只重新构建 control-plane 镜像并更新 SCF 函数，不触碰 cloud-plane 入口机、不重启已运行的 worker node、不影响现有 service。

### 4. 实验回收

```bash
make destroy
```

`destroy` 按 plane 逐个回收：

- cloud-plane 入口机上的服务进程和本地 artifact
- 动态创建的 worker node
- CDN service domain
- DNSPod service CNAME
- Terraform bootstrap 资源
- SCF control-plane 函数

## 一键验证

上述整个流程可以通过一条命令自动完成：

```bash
make e2e
```

`e2e` 自动执行 build → bootstrap → install → Web UI 操作（通过 Playwright）→ update → smoke 验证 → destroy 全流程。适合验证代码变更后整个系统仍然闭环可用。

## 部署后的关键行为

### Worker 自动生命周期

创建 service 后，cloud-plane 自动创建 worker node、等待 node-agent 注册、下发 workload。删除 service 后，cloud-plane 自动回收对应 worker node。

### 公网入口链路

service 暴露走完整生产链路：

```text
service-name.baseDomain
  -> DNSPod CNAME
  -> provider CDN domain
  -> cloud-plane Caddy
  -> worker host port
  -> container port
```

### Control-plane 独立更新

`update` 只更新 control-plane 容器镜像和 SCF 函数，不重装 cloud-plane、不重启入口机、不修改 worker node。更新后 smoke 验证确认 Web / API 仍可正常使用。

## 注意事项

- DNS / CDN 生效有延迟，e2e 内置等待重试逻辑，但云控制台显示可能更慢。
- 真实云测试会产生云资源和费用，结束后务必确认 `destroy` 成功。
- `deploy/ops/config.yaml`、`deploy/ops/state/`、Terraform var file、token 和云账号密钥不能提交到仓库。
