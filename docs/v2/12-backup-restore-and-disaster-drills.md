# 12 Backup, Restore, and Disaster Drills

到了这一章，
`mini-cloud v2`
已经不只是：

- 能起平台
- 能纳管 worker
- 能发版
- 能回滚
- 能看操作历史
- 能看可靠性快照

还必须补上一件真正的运维底线：

- 如果 control-plane 宿主机或数据库丢了，
  我们到底怎么把平台状态拿回来

这一章先不做“全自动远程灾备平台”。

它先把当前项目最小但真实的一条恢复链补上：

1. 能打出一份真正可恢复的备份 bundle
2. 能把数据库状态导回来
3. 能把数据库外的重要配置快照一起拿回来
4. 能跑一轮真正的灾难恢复演练

## 先记一句话

这章最重要的结论是：

- 只备份数据库还不够

因为当前 `mini-cloud`
里有两类东西：

| 类别 | 例子 | 在不在数据库里 |
| --- | --- | --- |
| 平台运行状态 | project、app、release、deployment、secret set、registry credential、operation history | 在 |
| 平台运行配置 | `MINICLOUD_ADMIN_TOKEN`、域名配置、bootstrap 输入、platform inventory | 不一定在 |

所以真正能恢复平台的，
不是一份单独的 SQL dump，
而是：

- 数据库 dump
+ 外部配置快照

## 这一章新增了什么

当前新增了三条脚本：

| 脚本 | 作用 |
| --- | --- |
| `./scripts/backup-platform.sh` | 打出最小可恢复备份 bundle |
| `./scripts/restore-platform.sh` | 从 bundle 恢复数据库，并把配置快照拷回指定目录 |
| `./scripts/disaster-drill.sh` | 跑一轮从造数据到毁坏再到恢复的完整演练 |

同时：

- `./scripts/check.sh`
  现在也会额外做一轮：
  - `bash -n ./scripts/*.sh`

也就是说，
从这章开始，
项目里不只是“有运维脚本”，
这些脚本本身也开始进入日常检查。

## 现在的备份 bundle 里到底有什么

当前 bundle 至少包含一份：

- `Postgres` 逻辑备份

也就是：

- `postgres/mini-cloud.sql`

另外还可以按需把这些文件快照一起收进去：

- control-plane 环境文件
- bootstrap 配置
- platform inventory

生成后目录大致长这样：

```text
backup-bundle/
├── manifest.json
├── postgres/
│   └── mini-cloud.sql
└── snapshots/
    ├── control-plane.env
    ├── bootstrap-config.json
    └── platform-inventory.json
```

其中：

- `manifest.json`

会记录：

- 生成时间
- dump 相对路径
- dump `sha256`
- dump 大小
- 哪些配置快照被一起收进来了

这有两个好处：

1. 备份目录不再是“一堆不知道是什么的文件”
2. 恢复时能明确知道 bundle 里到底包含了哪些内容

## 为什么当前先用逻辑备份

这里没有直接去做：

- volume snapshot
- 文件系统级别冷拷贝
- 对象存储归档

而是先用：

- `pg_dump`

做逻辑备份。

原因很现实：

- 当前本地实验环境就是一套 `docker compose` 里的 `Postgres`
- 逻辑备份更容易看清楚“恢复链条到底依赖什么”
- 对教学来说，
  它比直接拷 Docker volume 更容易理解和验证

当然，
这也意味着它不是最终形态。

后面如果做更正式的生产化版本，
还会继续补：

- 定时备份
- 异地存储
- 保留周期
- 恢复点策略

但这些都不是这一章的重点。

## 哪些状态在数据库里

这一章最容易混淆的地方是：

- “secret 是不是在数据库里？”
- “token 是不是在数据库里？”
- “域名绑定是不是在数据库里？”

当前可以先直接这么记：

数据库里已经有：

- `projects`
- `apps`
- `releases`
- `deployments`
- `deployment_executions`
- `nodes`
- `placement_decisions`
- `app_domains`
- `gateway_request_events`
- `project_secret_sets`
- `project_registry_credentials`
- `project_api_tokens`
- `operation_events`

所以：

- 项目级 secret set
- registry credential
- app / release / deployment 状态
- 操作历史

这些都跟着数据库 dump 一起走。

## 哪些东西不在数据库里

但下面这些并不天然跟着数据库走：

- `MINICLOUD_ADMIN_TOKEN`
- `MINICLOUD_PLATFORM_DOMAIN`
- `MINICLOUD_APP_BASE_DOMAIN`
- `MINICLOUD_GATEWAY_REDIRECT_HTTP_TO_HTTPS`
- 你真正用于 bootstrap 的输入配置
- 你在某次安装后产出的 platform inventory

所以如果你只恢复数据库，
可能会出现这种情况：

- 平台状态回来了
- 但 control-plane 用什么 token 启动
- 对外域名是什么
- 这台平台机当初是按什么输入装出来的

这些信息却没跟着回来

这就是这一章为什么明确要求：

- 数据库外配置也要做快照

## `backup-platform.sh` 在做什么

当前最小用法是：

```bash
./scripts/backup-platform.sh \
  --output-dir /tmp/mini-cloud-backup \
  --env-file /path/to/control-plane.env \
  --bootstrap-config /path/to/bootstrap-config.json \
  --inventory-file /path/to/platform-inventory.json
```

这里有一个很重要的设计取舍：

- 脚本会等数据库 ready
- 但不会替你偷偷决定“真正要备份哪份运行配置”

所以：

- 数据库 dump
  - 是必做
- 环境文件、bootstrap 配置、inventory
  - 是显式由你传入

这样更符合真实运维习惯。

因为不同机器、不同部署方式下，
这些文件的位置本来就可能不同。

## `restore-platform.sh` 在做什么

当前最小用法是：

```bash
./scripts/restore-platform.sh \
  --backup-dir /tmp/mini-cloud-backup \
  --restore-config-dir /tmp/mini-cloud-restored-config
```

它当前会做两件事：

1. 启动本地 compose `Postgres`
2. 强制重建 `mini_cloud` 数据库并导入 dump

如果你还传了：

- `--restore-config-dir`

它还会把 bundle 里的配置快照拷到这个目录。

这里要特别注意：

- 它不会自动替你重启 control-plane

这是有意的。

因为不同环境里 control-plane 的启动方式可能完全不同：

- 本地 `go run`
- systemd
- docker compose
- 远程平台机上的安装目录脚本

所以这一步仍然留给运维自己显式做。

## 一轮最小恢复顺序应该怎么理解

当前最小恢复顺序可以直接记成：

1. 停 control-plane
2. 恢复数据库
3. 恢复环境文件 / bootstrap 配置 / inventory 快照
4. 再重新启动 control-plane
5. 用只读接口回看关键状态

这里顺序不能反。

尤其不能：

- control-plane 还连着旧库
- 就直接往同一个库里导恢复 dump

不然很容易把恢复过程和在线写入混在一起。

## `disaster-drill.sh` 真正验证了什么

这一章最关键的是这条脚本：

```bash
./scripts/disaster-drill.sh
```

它不是在“空数据库上简单导入一份 SQL”。

它会真正做下面这些动作：

1. 起本地 `Postgres`
2. 启动 control-plane
3. 创建：
   - 一个 project
   - 一个 secret set
   - 一个 registry credential
   - 一个 project API token
   - 一个 ready worker
   - 一个 app
   - 一个 release
4. 打出 backup bundle
5. 停 control-plane
6. 直接删掉本地 `Postgres` volume
7. 从 bundle 恢复数据库和配置快照
8. 用恢复后的环境文件重新启动 control-plane
9. 回读并验证：
   - project
   - secret set
   - registry credential
   - project API token 元数据
   - app / release / deployment
   - managed domain
   - operation history
   - reliability 快照

也就是说，
这一章已经不是“讲恢复”。

而是真的把：

- 备份
- 毁坏
- 恢复
- 回读验证

四段主线连起来了。

## 为什么这里还要顺手验证 operation history 和 reliability

因为如果只验证：

- 项目还在
- app 还在

那其实还不够。

当前平台前面已经补上的：

- 操作历史
- 平台可靠性快照

也都是控制面的一部分。

如果恢复以后这些视图全断了，
那恢复就还是不完整。

所以这章的演练里，
会顺手确认：

- `operation history`
  还能读
- `/api/v1/platform/reliability`
  还能生成快照

## 这一章还没做什么

当前这章依然没有去做：

- 定时任务自动备份
- 备份上传到对象存储
- 多份历史保留策略
- point-in-time recovery
- 自动重装整台平台机
- 一键远程切换到新 host

所以它更准确的定位是：

- 最小可执行恢复链

而不是：

- 完整灾备系统

## 建议怎么自己跑

### 1. 只打备份

如果你已经有一套正在运行的本地环境，
可以先只打 bundle：

```bash
./scripts/backup-platform.sh \
  --output-dir /tmp/mini-cloud-backup \
  --env-file /path/to/control-plane.env
```

### 2. 只做恢复

```bash
./scripts/restore-platform.sh \
  --backup-dir /tmp/mini-cloud-backup \
  --restore-config-dir /tmp/mini-cloud-restored-config
```

### 3. 直接跑完整演练

```bash
./scripts/disaster-drill.sh
```

对当前这条教学主线来说，
第三种最有价值。

因为它验证的不是某个命令能不能执行，
而是：

- 这套平台在“数据库和控制面都丢了”的场景下，
  还能不能按我们定义的最小步骤把关键状态拿回来

## 这一章做完后，平台多了什么

到这里，
`mini-cloud v2`
就不只是会：

- 部署
- 回滚
- 审计
- 看指标

还第一次具备了：

- 主状态能打包
- 主状态能导回
- 数据库外关键配置能一起带走
- 一轮恢复演练能真正跑完

这对后面的：

- 完整模拟环境
- 真实阿里云端到端实验
- reinstall / upgrade / destroy

都是基础前提。

## 本章检查点

- 提交：
  - `943bd2bdc8a2874b5a5f794845def8df871a6dfd`
- 状态：
  - 备份 bundle、恢复脚本和最小灾难演练闭环已经落地
