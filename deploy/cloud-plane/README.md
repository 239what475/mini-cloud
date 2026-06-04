# cloud-plane 长期运行部署资产

## 当前入口模型

cloud-plane 只监听内部 gRPC 地址，只注册 `ControlPlaneSnapshotService、ControlPlaneProjectService、ControlPlaneWorkloadService` 和 `NodeAgentService`。它不提供 northbound HTTP API、grpc-gateway、静态 UI、`/api/healthz`、`/metrics` 或 node-agent 二进制下载入口。

v7 起 ingress/egress 数据面统一外置：

- Caddy 作为 ingress reverse proxy，接收 CDN/用户回源流量并反代到 `node.privateIP:hostPort`。
- Tinyproxy 作为 egress forward proxy，承接 runtime node bootstrap、Docker daemon 和 workload HTTP(S) 出公网。
- cloud-plane 只生成并应用 Caddy 配置，不内嵌 Caddy，也不承载业务 HTTP 流量。

配置模型已经收敛为单文件：

- cloud-plane 通过 `--config` 指定唯一配置文件路径
- 配置内容全部来自 YAML 文件，不读取 `MINICLOUD_*` 环境变量
- 命令行只承载配置文件路径，不承载业务配置项
- 不再拆分 `cloud-plane.env` 和 `platform-config.json`

需要对用户或平台管理员开放的 HTTP API 应走 control-plane，而不是 cloud-plane。

## 目录里的文件

- `cloud-plane.aliyun.yaml.example`
  - Aliyun 长期运行 cloud-plane 的完整配置模板。
- `cloud-plane.tencent.yaml.example`
  - Tencent 长期运行 cloud-plane 的完整配置模板。
- `node-agent.yaml.example`
  - 本机固定 node-agent 的独立配置模板。
- `start-agent.sh.example`
  - 本机固定 node-agent 启动包装脚本。
- `systemd/mini-cloud-cloud-plane.service.example`
  - cloud-plane 的 systemd 单元模板。
- `systemd/mini-cloud-node-agent.service.example`
  - 本机固定 node-agent 的 systemd 单元模板。

## 推荐主机布局

- 二进制：
  - `/opt/mini-cloud/cloud-plane/bin/cloud-plane`
  - `/opt/mini-cloud/cloud-plane/bin/node-agent`
  - `/opt/mini-cloud/cloud-plane/bin/start-agent.sh`
- 配置文件：
  - `/etc/mini-cloud/cloud-plane/cloud-plane.yaml`
  - `/etc/mini-cloud/cloud-plane/node-agent.yaml`
  - `/etc/mini-cloud/ingress/Caddyfile`
- systemd 单元：
  - `/etc/systemd/system/mini-cloud-cloud-plane.service`
  - `/etc/systemd/system/mini-cloud-node-agent.service`
- 运行状态：
  - `/var/lib/mini-cloud/node-agent/projected-files/`
  - `/var/lib/mini-cloud/persistent-dirs/`
- 日志：
  - `/var/log/mini-cloud/cloud-plane.log`
  - `/var/log/mini-cloud/node-agent.log`

## 最小启动顺序

### 1. 准备实际配置

```bash
# 按 provider 二选一，最终文件名必须是 cloud-plane.yaml。
cp deploy/cloud-plane/cloud-plane.aliyun.yaml.example deploy/cloud-plane/cloud-plane.yaml
# cp deploy/cloud-plane/cloud-plane.tencent.yaml.example deploy/cloud-plane/cloud-plane.yaml

cp deploy/cloud-plane/node-agent.yaml.example deploy/cloud-plane/node-agent.yaml
cp deploy/cloud-plane/start-agent.sh.example /tmp/start-agent.sh
cp deploy/cloud-plane/systemd/mini-cloud-cloud-plane.service.example /tmp/mini-cloud-cloud-plane.service
cp deploy/cloud-plane/systemd/mini-cloud-node-agent.service.example /tmp/mini-cloud-node-agent.service
```

然后按真实主机信息修改：

- `cloud-plane.yaml`
  - `server.listenGRPCAddr`
  - `database.url`
  - `plane.identity.*`
  - `controlPlane.auth.bearerToken`
  - `nodeAgent.connectEndpoint`
  - `nodeAgent.auth.*`
  - `nodeAgent.artifact.*`
  - `nodeAgent.defaults.*`
    - `hostPortRange` 必须和 runtime node 安全组放行的 hostPort 范围一致。
  - `infrastructure.*`
  - `runtimeProvisioning.*`
  - `observability.*`
- `node-agent.yaml`

cloud-plane 尚未正式发布，数据库 schema 以 `00001_init_schema.sql` 单一开发期 baseline 为准。
如果本机或 lab 里保留了旧 cloud-plane 数据库，不支持原地迁移，必须先重建数据库再启动新版 cloud-plane。

### 1.1 准备外置数据面

如果 `cloud-plane.yaml` 中启用了 `ingress.enabled` 或 `runtimeProvisioning.egress.proxy.enabled`，需要先在 platform host 上准备外置数据面：

- Caddy：监听 `ingress.caddy.listenHTTPAddr`，并让 `ingress.caddy.reloadCommand` 可以成功 reload 当前 Caddyfile。
- Tinyproxy：监听 `runtimeProvisioning.egress.proxy.endpoint` 中的端口，只允许 runtime node 私网网段访问。

Terraform lab 会自动安装并启动这两个组件。手工部署时必须自行安装，否则 public service 入口和 runtime node 出公网代理都不会生效。

如果沿用示例 `reloadCommand: ["docker", "exec", "mini-cloud-caddy", ...]`，`minicloud` 用户必须能访问 Docker socket。示例 systemd 单元通过 `SupplementaryGroups=docker` 表达这个权限；生产环境也可以改成受限的 `systemctl reload caddy` 或专用 sudoers 命令，但必须保证 cloud-plane 能写 `ingress.caddy.configPath` 并触发 reload。

### 2. 构建 Linux 二进制

```bash
make build-release
```

默认输出在 `dist/release/linux-amd64/`。

### 3. 安装目录和权限

```bash
sudo useradd --system --home /opt/mini-cloud --shell /usr/sbin/nologin minicloud || true
sudo install -d -o minicloud -g minicloud /opt/mini-cloud/cloud-plane/bin
sudo install -d -o minicloud -g minicloud /etc/mini-cloud/cloud-plane
sudo install -d -o minicloud -g minicloud /etc/mini-cloud/ingress
sudo install -d -o minicloud -g minicloud /var/lib/mini-cloud/node-agent
sudo install -d -o minicloud -g minicloud /var/lib/mini-cloud/node-agent/projected-files
sudo install -d -o minicloud -g minicloud /var/lib/mini-cloud/persistent-dirs
sudo install -d -o minicloud -g minicloud /var/log/mini-cloud
```

### 4. 安装二进制和配置

```bash
sudo install -m 0755 dist/release/linux-amd64/cloud-plane /opt/mini-cloud/cloud-plane/bin/cloud-plane
sudo install -m 0755 dist/release/linux-amd64/node-agent /opt/mini-cloud/cloud-plane/bin/node-agent
sudo install -m 0755 /tmp/start-agent.sh /opt/mini-cloud/cloud-plane/bin/start-agent.sh
sudo install -o minicloud -g minicloud -m 0640 deploy/cloud-plane/cloud-plane.yaml /etc/mini-cloud/cloud-plane/cloud-plane.yaml
sudo install -o minicloud -g minicloud -m 0640 deploy/cloud-plane/node-agent.yaml /etc/mini-cloud/cloud-plane/node-agent.yaml
sudo install -o minicloud -g minicloud -m 0644 /dev/null /etc/mini-cloud/ingress/Caddyfile
```

### 5. 安装并启动 systemd 单元

```bash
sudo install -m 0644 /tmp/mini-cloud-cloud-plane.service /etc/systemd/system/mini-cloud-cloud-plane.service
sudo install -m 0644 /tmp/mini-cloud-node-agent.service /etc/systemd/system/mini-cloud-node-agent.service
sudo systemctl daemon-reload
sudo systemctl enable --now mini-cloud-cloud-plane.service
sudo systemctl enable --now mini-cloud-node-agent.service
```

cloud-plane 的 systemd 单元不再引用 `EnvironmentFile`。配置文件路径由 `--config /etc/mini-cloud/cloud-plane/cloud-plane.yaml` 显式指定。

## 验证本 plane

readiness 使用 TCP/gRPC 探测内部 gRPC 端口：

```bash
# 例：检查本机 18081 端口是否可连
bash -c '</dev/tcp/127.0.0.1/18081'
```

首次注册到 control-plane 时，control-plane 通过配置中的 `controlPlane.auth.bearerToken` 访问 cloud-plane 的 `ControlPlaneSnapshotService、ControlPlaneProjectService、ControlPlaneWorkloadService`。

## runtime scale-out 的关系

这组长期运行资产只负责：

- 启动 cloud-plane
- 启动首个固定 node-agent
- 提供 node-agent 连接、下载和 runtime node bootstrap 默认参数

cloud-plane 固定启用云上动态扩容，不再支持手动扩容模式。`cloud-plane.yaml` 必须填写 `runtimeProvisioning.providerSpec`，该字段是 provider 专属结构，由 cloud-plane 按 `infrastructure.provider` 延迟解析。

## 这组文件刻意不做什么

- 域名和公网 TLS
- CDN / front door 外层入口
- Loki / Tempo / Grafana 持久化观测栈
- 跨 plane 服务切换、双活或全局退役编排
