# control-plane 单活部署资产

这一组文件是给 `v6/08`
准备的：

- `control-plane`
  正式单活部署布局
- `control-plane`
  最小恢复路径

这里故意不做多活，
也不把它和
`deploy/compose`
的本地实验辅助栈混在一起。

## 这一组文件的角色

- [control-plane.env.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/control-plane/control-plane.env.example)
  - `control-plane`
    进程环境文件模板
- [docker-compose.postgres.yml.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/control-plane/docker-compose.postgres.yml.example)
  - 单活 `Postgres`
    依赖模板
- [mini-cloud-control-plane.service.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/control-plane/systemd/mini-cloud-control-plane.service.example)
  - `systemd`
    单元模板

根据仓库约定，
这些文件如果要在本机真正使用：

- 先复制一份
- 去掉 `.example`
- 再按机器环境修改

这也是为什么本目录自带：

- [/.gitignore](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/control-plane/.gitignore)

来忽略本机实际文件。

如果你现在要做的是
`v6/11`
里那种：

- shared host
  同机承载
  `control-plane`
  和
  `cloud-plane`

那么这一组文件继续负责：

- `control-plane`
  自己的正式部署

共享 `Postgres`
和 shared host
目录布局则改看：

- [deploy/platform-host/README.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/platform-host/README.md)

## 推荐的主机布局

这一章推荐按下面这套单活布局来理解：

- 二进制：
  - `/opt/mini-cloud/control-plane/bin/control-plane`
- 工作目录：
  - `/opt/mini-cloud/control-plane`
- 环境文件：
  - `/etc/mini-cloud/control-plane/control-plane.env`
- 日志目录：
  - `/var/log/mini-cloud/control-plane.log`
- `systemd`
  单元：
  - `/etc/systemd/system/mini-cloud-control-plane.service`
- `Postgres`
  容器名：
  - `mini-cloud-control-plane-postgres`
- 恢复 bundle：
  - `/var/backups/mini-cloud/control-plane/<timestamp>`

这套布局背后的取舍是：

- `control-plane`
  自己保持成一个普通 Go 进程
- 进程配置收进单独 env 文件
- 数据库存储先保持成单机单实例
  `Postgres`

## 最小启动方式

### 1. 准备实际文件

```bash
cp deploy/control-plane/control-plane.env.example deploy/control-plane/control-plane.env
cp deploy/control-plane/docker-compose.postgres.yml.example deploy/control-plane/docker-compose.postgres.yml
cp deploy/control-plane/systemd/mini-cloud-control-plane.service.example /tmp/mini-cloud-control-plane.service
```

然后按你的机器环境改：

- `control-plane.env`
- `docker-compose.postgres.yml`
- `mini-cloud-control-plane.service`

## 2. 构建二进制

```bash
go build -o /opt/mini-cloud/control-plane/bin/control-plane ./cmd/control-plane
```

## 3. 启动本机 `Postgres`

```bash
docker compose -f deploy/control-plane/docker-compose.postgres.yml up -d
```

## 4. 安装并启动 `control-plane`

```bash
sudo install -d -o minicloud -g minicloud /var/log/mini-cloud
sudo install -o minicloud -g minicloud -m 0640 deploy/control-plane/control-plane.env /etc/mini-cloud/control-plane/control-plane.env
sudo cp /tmp/mini-cloud-control-plane.service /etc/systemd/system/mini-cloud-control-plane.service
sudo systemctl daemon-reload
sudo systemctl enable --now mini-cloud-control-plane
```

## 5. 健康检查

```bash
curl -fsS http://127.0.0.1:18080/api/healthz
```

如果这台
`control-plane`
还需要直接提供：

- `GET /api/v1/control/logs`

这样的聚合日志查询，
那它自己的 env
里还必须显式配置：

- `MINICLOUD_LOKI_URL`

通常同机 center
部署时，
就直接写：

- `MINICLOUD_LOKI_URL=http://127.0.0.1:3100`

如果有租户隔离，
再继续补：

- `MINICLOUD_LOKI_TENANT_ID`

否则最直观的现象就是：

- `control-plane`
  健康检查正常
- 但
  `GET /api/v1/control/logs`
  直接返回：
  - `503`
  - `log query backend is not configured`

## 恢复链路怎么接这套布局

当前仓库不再维护单独的备份恢复脚本。
生产环境恢复链路应由部署系统或运维平台封装，
这里保留最小命令形态，方便理解边界。

先确认环境变量：

```bash
export MINICLOUD_CONTROL_PLANE_ENV_FILE=/etc/mini-cloud/control-plane/control-plane.env
export MINICLOUD_CONTROL_PLANE_POSTGRES_CONTAINER=mini-cloud-control-plane-postgres
export MINICLOUD_CONTROL_PLANE_COMPOSE_FILE=/opt/mini-cloud/control-plane/deploy/control-plane/docker-compose.postgres.yml
```

备份数据库：

```bash
backup_dir=/var/backups/mini-cloud/control-plane/$(date +%Y%m%d-%H%M%S)
mkdir -p "$backup_dir"
docker exec "$MINICLOUD_CONTROL_PLANE_POSTGRES_CONTAINER" \
  pg_dump -U mini_cloud -d mini_cloud_control_plane -Fc \
  > "$backup_dir/control-plane.dump"
cp "$MINICLOUD_CONTROL_PLANE_ENV_FILE" "$backup_dir/control-plane.env"
```

如果数据库容器已经由别的方式拉起来了，
恢复时先停掉
`control-plane`
进程，
再重建数据库并导入：

```bash
backup_dir=/var/backups/mini-cloud/control-plane/<timestamp>
docker exec "$MINICLOUD_CONTROL_PLANE_POSTGRES_CONTAINER" \
  psql -U mini_cloud -d postgres -v ON_ERROR_STOP=1 \
  -c "DROP DATABASE IF EXISTS mini_cloud_control_plane;" \
  -c "CREATE DATABASE mini_cloud_control_plane;"
docker exec -i "$MINICLOUD_CONTROL_PLANE_POSTGRES_CONTAINER" \
  pg_restore -U mini_cloud -d mini_cloud_control_plane \
  < "$backup_dir/control-plane.dump"
```

## 这一章的正式验收入口

恢复验收不再依赖仓库脚本。
建议按下面三步手动验收：

- `control-plane`
  的主状态能恢复回来
- plane registration
  仍然成立
- 不重新手工 register
  `cloud-plane`
  的前提下，
  后台 sync
  能把最新快照重新拉回
