# platform-host 共享基础设施资产

这一组文件最初给 `v6/11` 的“单机长期运行平台”使用；在 v7 外置数据面重构后，platform host 的职责已经扩展。

这一章当前明确采用：

- 一台腾讯云长期在线主机
- 同机承载：
  - `control-plane`
  - 首个 Tencent `cloud-plane`
  - 本机固定 `node agent`
  - 外置 `Caddy` ingress reverse proxy
  - 外置 `Tinyproxy` egress forward proxy

所以这里单独收一层：

- shared host
  基础设施

避免把：

- `control-plane`
  正式部署资产
- `cloud-plane`
  正式部署资产
- 它们共用的
  `Postgres`

混在一个目录里。

## 目录里的文件

- [docker-compose.postgres.yml.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/platform-host/docker-compose.postgres.yml.example)
  - 共享 `Postgres`
    容器模板
- [01-create-databases.sql](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/platform-host/initdb/01-create-databases.sql)
  - 首次初始化时创建：
    - `mini_cloud_control_plane`
    - `mini_cloud_cloud_plane`

## 为什么这里改成“一容器两库”

这一章不再采用：

- `control-plane`
  一个 `Postgres`
- `cloud-plane`
  再单独一个 `Postgres`

原因很直接：

- `tencent-platform-host`
  只有 `2c4g`
- 当前还没上：
  - 中央观测栈
  - 外层 front door
  - 第二个 plane

所以这一步更务实的做法是：

- 一台 `Postgres`
  容器
- 两个数据库

这样能把：

- 资源占用
- 目录布局
- 备份恢复入口

都先收干净。

## v7 数据面补充

v7 当前架构要求 ingress/egress 数据面都外置：

- Caddy 负责 `CDN / 用户 -> runtime node privateIP:hostPort`。
- Tinyproxy 负责 `runtime node -> Internet`。
- cloud-plane 只生成 Caddyfile 并执行 reload，不内嵌 Caddy。

本目录里的 Postgres compose 只覆盖共享数据库资产；真实 v7 platform host 还必须通过 Terraform lab 或手工 systemd/docker 步骤安装并启动 Caddy/Tinyproxy。

## 推荐主机布局

- shared host
  工作目录：
  - `/opt/mini-cloud/platform-host`
- compose 文件：
  - `/opt/mini-cloud/platform-host/docker-compose.postgres.yml`
- initdb 文件：
  - `/opt/mini-cloud/platform-host/initdb/01-create-databases.sql`

## 最小启动方式

### 1. 准备实际 `compose` 文件

```bash
cp deploy/platform-host/docker-compose.postgres.yml.example deploy/platform-host/docker-compose.postgres.yml
```

然后把整个目录同步到主机：

- `docker-compose.postgres.yml`
- `initdb/01-create-databases.sql`

### 2. 启动共享 `Postgres`

```bash
docker compose -f /opt/mini-cloud/platform-host/docker-compose.postgres.yml up -d
```

### 3. 健康检查

```bash
docker exec mini-cloud-platform-postgres \
  pg_isready -h 127.0.0.1 -p 5432 -U mini_cloud -d mini_cloud_control_plane

docker exec mini-cloud-platform-postgres \
  pg_isready -h 127.0.0.1 -p 5432 -U mini_cloud -d mini_cloud_cloud_plane
```

## 这层和上面两组资产怎么组合

shared host
起来以后，
再分别接：

- [deploy/control-plane/README.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/control-plane/README.md)
- [deploy/cloud-plane/README.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/cloud-plane/README.md)

也就是说，
这一层只负责：

- 共享数据库
- shared host
  目录与容器布局

不直接负责：

- 注册 plane
- 启动 `agent`
- 对外管理入口

## 备份与恢复怎么接

当前不再维护仓库级备份恢复脚本。
shared host
部署时直接对共享
Postgres
容器做逻辑备份：

```bash
backup_dir=/var/backups/mini-cloud/control-plane/$(date +%Y%m%d-%H%M%S)
mkdir -p "$backup_dir"
docker exec mini-cloud-platform-postgres \
  pg_dump -U mini_cloud -d mini_cloud_control_plane -Fc \
  > "$backup_dir/control-plane.dump"
```

```bash
backup_dir=/var/backups/mini-cloud/cloud-plane/$(date +%Y%m%d-%H%M%S)
mkdir -p "$backup_dir"
docker exec mini-cloud-platform-postgres \
  pg_dump -U mini_cloud -d mini_cloud_cloud_plane -Fc \
  > "$backup_dir/cloud-plane.dump"
```

恢复时先停掉对应进程，
再用
`pg_restore`
导回目标数据库。

只是容器名都改成：

- `mini-cloud-platform-postgres`
