# mini-cloud demo flow

这份文档用于面试或项目展示。它描述 `mini-cloud` 当前真实云 demo 的完整链路，不包含 token、云账号密钥、真实资源 ID 等敏感信息。

## Demo Goal

用一条真实 e2e 流程证明：

- control-plane 可以作为统一 Web/API 入口。
- Tencent 和 Aliyun 两个 cloud-plane 可以同时接入。
- service 显式部署到指定 cloud-plane。
- cloud-plane 会自动创建 worker node 并启动 workload。
- service 可以通过公网域名访问。
- control-plane 可以独立 update。
- 实验资源可以自动回收。

## Topology

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

## One Command Demo

```bash
go run ./cmd/minictl e2e --config deploy/ops/config.yaml
```

`e2e` 会自动执行：

1. `build`: 构建 control-plane、cloud-plane、node-agent 和 Web UI。
2. `bootstrap`: 为每个 cloud-plane 准备 worker node 所需的云基础设施。
3. `install`: 部署 control-plane，并安装每个 cloud-plane entry host。
4. `full web flow`: 通过真实 Web UI 创建和删除 service。
5. `update`: 只更新 control-plane/SCF。
6. `smoke`: 更新后验证 Web/API 仍可用。
7. `destroy`: 回收实验资源。

## Expected Output Shape

输出中应该能看到这些关键阶段：

```text
[mini-cloud ops] e2e deploy
[mini-cloud ops] build release binaries and web assets
[mini-cloud ops] bootstrap infrastructure for plane mini-cloud-tencent-ops (tencent)
[mini-cloud ops] bootstrap infrastructure for plane mini-cloud-aliyun-ops (aliyun)
[mini-cloud ops] control-plane URL: http://control.apps.example.com
[mini-cloud ops] e2e run full web flow
[ops-e2e] create service e2e-ui-tx-... on pln_mini_cloud_tencent_ops
[ops-e2e] wait public HTTP 200 for e2e-ui-tx-....apps.example.com
[ops-e2e] create service e2e-ui-ali-... on pln_mini_cloud_aliyun_ops
[ops-e2e] wait public HTTP 200 for e2e-ui-ali-....apps.example.com
[mini-cloud ops] e2e update control-plane
[mini-cloud ops] e2e run post-update web smoke
[mini-cloud ops] destroy ops resources
```

最后 Playwright 应该显示：

```text
1 passed
```

## What The Demo Proves

### Multi-cloud backend

同一个 control-plane 同时读取 Tencent 和 Aliyun 两个 cloud-plane snapshot，并且可以把 service 下发到指定 plane。

### Automatic worker lifecycle

创建 service 后，cloud-plane 会创建 worker node，等待 node-agent 注册，然后把 workload 下发给 node-agent。删除 service 后，cloud-plane 会回收对应 worker node。

### Real ingress path

service 暴露不是本地 mock。真实路径是：

```text
service name
  -> DNSPod CNAME
  -> provider CDN domain
  -> cloud-plane Caddy
  -> worker host port
  -> container port
```

e2e 会对公网 service domain 发起 HTTP 请求并等待 `200`。

### Stateless control-plane update

`update` 只更新 control-plane 容器镜像和 SCF，不重装 cloud-plane，不重启 entry host，不修改 worker node。更新后 smoke 仍然能登录 Web UI、读取 planes 和 services。

### Cleanup discipline

`e2e` 最后会执行 `destroy`。它会回收：

- cloud-plane entry host 上的 mini-cloud 服务和本地 artifact。
- service 产生的 worker node。
- service 产生的 CDN domain。
- service 产生的 DNSPod CNAME。
- Terraform bootstrap 资源。
- SCF control-plane 和 control-plane CNAME。

## Manual Demo Checklist

如果不跑完整 e2e，可以按下面步骤手动展示：

```bash
go run ./cmd/minictl check --config deploy/ops/config.yaml
go run ./cmd/minictl deploy --config deploy/ops/config.yaml
```

然后打开 control-plane Web：

1. 使用 admin token 登录。
2. 确认 Tencent 和 Aliyun plane 都是 ready。
3. 创建一个 `nginx:alpine` service，plane 选择 Tencent。
4. 等待 service 变成 `ready / running`。
5. 访问生成的 service 域名。
6. 再创建一个 service，plane 选择 Aliyun。
7. 验证两个 service 分别走对应 cloud-plane。
8. 删除 service。
9. 执行回收：

```bash
go run ./cmd/minictl destroy --config deploy/ops/config.yaml
```

## Demo Notes

- Tencent Lighthouse 接入 CVM VPC 的 CCN attachment 可能需要控制台手动同意。
- DNS/CDN 生效有延迟，e2e 会等待，但云控制台展示可能更慢。
- 真实云测试会产生云资源和费用，测试结束必须确认 `destroy` 成功。
- `deploy/ops/config.yaml`、`deploy/ops/state/`、Terraform var file、token 和云账号密钥不能提交。
