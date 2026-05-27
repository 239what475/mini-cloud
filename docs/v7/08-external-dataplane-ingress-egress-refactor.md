# v7/08 External Dataplane Ingress/Egress Refactor

## 背景

v7 前半段把 cloud-plane 收敛成纯内部 gRPC 控制面时，连同历史 gateway/domain 模型一起删除了 embedded Caddy 数据面。这个删除过度：cloud-plane 不应该继续提供 northbound HTTP API 或静态资源入口，但 public service 仍然需要一条清晰的 ingress 数据面链路。

同时，runtime node 默认无公网 IP 后，bootstrap、镜像拉取和 workload HTTP(S) 出站也需要统一的 egress 数据面。Tinyproxy 不能内嵌到 cloud-plane；如果 Caddy 继续 embedded，而 Tinyproxy 外置，数据面边界会再次变得不统一。

因此本次重构把 ingress 和 egress 全部定义为外置数据面组件，由 cloud-plane 只负责控制和配置。

## 目标架构

```text
control-plane
  -> cloud-plane gRPC southbound services

node-agent
  -> cloud-plane gRPC NodeAgentService

用户 / CDN
  -> platform host Caddy
  -> runtime node privateIP:hostPort
  -> Docker container

runtime node / workload
  -> platform host Tinyproxy
  -> Internet
```

组件职责：

| 组件 | 职责 | 是否承载业务流量 |
|---|---|---|
| cloud-plane | gRPC 控制面、本地 reconciler、生成并应用 ingress 配置、下发 egress 配置 | 否 |
| Caddy | ingress reverse proxy，接收 CDN/用户回源流量并反代到 runtime node 私网地址 | 是 |
| Tinyproxy | egress forward proxy，承接 runtime node 出公网 HTTP(S) 流量 | 是 |
| node-agent | 在 runtime node 上运行 workload，并上报 hostPort | 否 |

## 非目标

- 不恢复 cloud-plane northbound HTTP API、grpc-gateway、静态 UI、`/metrics` 或 artifact 下载入口。
- 不恢复旧 `gateway` 命名和 gateway request event/SLO 表结构。
- 不让 cloud-plane 进程内嵌 Caddy 或 Tinyproxy。
- 不给 runtime node 分配公网 IP 或公网带宽。

## Ingress 设计

### 路由来源

cloud-plane 后台 ingress reconciler 周期性读取本地状态：

1. 列出 `exposure=public` 的 service。
2. 找到 service 当前 promoted deployment。
3. 列出该 deployment 下 running 且有 `hostPort` 的 execution。
4. 读取 execution 所在 node 的 `privateIP`。
5. 生成 `host -> backends` 的 route snapshot。

默认 managed host 规则为：

```text
<service-name>.<project-name>.<ingress.baseDomain>
```

例如：

```text
api.team-a.apps.example.com
```

### Caddy 配置应用

cloud-plane 只写外置 Caddy 的配置文件并执行 reload 命令：

```yaml
ingress:
  enabled: true
  baseDomain: apps.example.com
  caddy:
    listenHTTPAddr: 0.0.0.0:80
    configPath: /etc/mini-cloud/ingress/Caddyfile
    reloadCommand:
      - docker
      - exec
      - mini-cloud-caddy
      - caddy
      - reload
      - --config
      - /etc/caddy/Caddyfile
      - --adapter
      - caddyfile
```

Caddy 是外置进程。cloud-plane 挂掉时，Caddy 继续使用最后一份有效配置服务已有入口。

代码分层：

- `control/ingress` 只负责从本地 observed 状态构造 `Route` 快照，并调用 `Sink`。
- `infra/ingress/caddy` 只负责 Caddyfile 渲染、原子写文件和 reload 命令。
- `control.Manager` 只启动名为 `ingress` 的后台 loop，不感知 Caddy 细节。

长期运行部署必须保证 cloud-plane 进程同时具备两项权限：

- 可写 `ingress.caddy.configPath`。
- 可执行 `ingress.caddy.reloadCommand`。如果使用示例中的 `docker exec mini-cloud-caddy ...`，运行 cloud-plane 的用户必须有 Docker socket 访问权限；也可以改用更受限的 `systemctl reload caddy` 或 sudoers 白名单命令。

## Egress 设计

runtime node 不依赖公网 IP、EIP 或云厂商公网带宽。出公网统一使用 platform host 上的 Tinyproxy：

```yaml
runtimeProvisioning:
  imagePull:
    registryMirrors:
      - https://mirror.ccs.tencentyun.com
  egress:
    proxy:
      enabled: true
      endpoint: http://10.0.0.10:3128
      noProxy:
        - 127.0.0.1
        - localhost
        - 10.0.0.0/8
        - 172.16.0.0/12
        - 192.168.0.0/16
        - 169.254.169.254
        - 100.100.100.200
```

使用范围：

- runtime node bootstrap 的 `apt-get` / `curl`。
- Docker daemon 拉镜像。
- node-agent 进程自身出站；动态 runtime node 的 systemd unit 会注入同一组 proxy 环境变量。
- node-agent 注入 workload 容器的 `HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY` 环境变量。

注意：环境变量代理只约束遵守 HTTP proxy 约定的应用。强制阻止绕过 proxy 仍依赖 runtime node 安全组和路由策略。

## Provider 配置边界

`providerSpec` 只保留云厂商创建 runtime node 必需的字段，例如 instance type、image、subnet、安全组和系统盘。以下字段不再属于 providerSpec：

- `dockerRegistryMirror`
- `publicIpAssigned`
- `internetChargeType`
- `internetMaxBandwidthOutMbit`

这些字段要么是通用 runtime provisioning 配置，要么与“runtime node 无公网”目标冲突。

## 安全组原则

平台安全组至少允许：

- `admin_cidrs` -> SSH 端口；只用于运维登录 platform host。
- `control_plane_cidrs` -> cloud-plane gRPC 端口；用于外部 control-plane/admin 客户端访问 cloud-plane。
- `ingress_cidrs` -> Caddy HTTP 入口端口；当前 lab 示例是 `80`，TLS/443 由 CDN/front door 终止或后续 ingress 配置承接。
- runtime security group -> cloud-plane gRPC 端口；用于 runtime node 回连 cloud-plane。
- runtime security group -> Tinyproxy 3128；用于 runtime node 经 platform host 出网。

runtime node 安全组原则上只允许：

- platform host -> runtime node hostPort 范围。
- runtime node -> platform gRPC 端口。
- runtime node -> platform Tinyproxy 端口。
- runtime node -> metadata 等必要基础设施地址；DNS/NTP 是否需要显式放行取决于云厂商网络实现和后续生产环境策略，当前 lab 不把它们伪装成已完成的精细规则。

Terraform lab 已拆分 `platform` 和 `runtime` 两个安全组：

- platform host 绑定 platform security group。
- 动态 runtime node 绑定 runtime security group。
- platform security group 将外部入口按 `admin_cidrs`、`control_plane_cidrs`、`ingress_cidrs` 拆开，避免 SSH、cloud-plane gRPC 和 Caddy HTTP 共用同一批来源 CIDR。
- platform security group 从 runtime security group 接受 cloud-plane gRPC 和 Tinyproxy 端口。
- runtime security group 只从 platform security group 接受 workload hostPort 范围。

node-agent 不再让 Docker 任意分配宿主机端口，而是从 `runtime.hostPortRange` 中选择端口，并把该端口显式传给 Docker。Terraform lab 会把同一组 `runtime_host_port_min/runtime_host_port_max` 写入 cloud-plane 的 `nodeAgent.defaults.hostPortRange`，再由 provider user-data 下发到动态 runtime node 的 node-agent 配置。

这解决了“Docker 分配端口”和“安全组放行端口”不一致的问题，也避免 platform host 和 runtime node 继续复用同一个安全组。生产环境仍可继续细化 runtime egress，例如按实际 DNS/NTP/metadata 需求进一步收敛出站规则。

## Breaking changes

- cloud-plane YAML 必须使用新的 `ingress` 和 `runtimeProvisioning.imagePull/egress` 结构。
- providerSpec 不再接受历史公网字段或 `dockerRegistryMirror`。
- Tencent runtime node 固定无公网 IP。
- Aliyun runtime node 固定不申请公网出带宽。
- public service 的入口由外置 Caddy 承接，cloud-plane 不再内嵌 Caddy。

## 生效边界

`runtimeProvisioning.imagePull` 和 `runtimeProvisioning.egress.proxy` 在创建新的 runtime node 时写入 bootstrap 脚本、Docker daemon、node-agent systemd unit 和 node-agent YAML。它不是对已经运行的旧 node 做热更新；已有 node 需要重建或重新 bootstrap 才会获得新代理配置。
