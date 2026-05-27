# 11 Real Control Plane And First Production Plane

这一章把
`mini-cloud`
从：

- 真实实验环境

再往前推一步，
收成：

- 第一台长期运行平台机

这里当前明确采用：

- 腾讯云 `tencent-platform-host`
  承载：
  - `control-plane`
  - 首个 Tencent
    `cloud-plane`
  - 本机固定 `node agent`

## 这一章要解决什么问题

到 `10`
结束时，
平台虽然已经能做真实双云实验，
但主要还是靠：

- Terraform `cloud-init`
- 临时 `go run`
- 本地测试脚本

去把环境拉起来。

这对“能不能做实验”是够的，
但对“能不能长期运行”还不够。

缺口主要有三类：

1. 长期运行部署资产不对称
   - `control-plane`
     已经有正式
     `systemd + env file + binary`
     资产
   - `cloud-plane`
     还主要埋在
     `cloud-init`
2. shared host
   基础设施没有收干净
   - 如果把
     `control-plane`
     和
     `cloud-plane`
     放同一台主机，
     数据库、目录、恢复入口怎么统一
3. 首个长期运行 plane
   的接入链还不够正式
   - 需要一个清晰的：
     - plane create
     - plane register
     - 首个固定 node ready
     验证入口

## 这一章的正式目标

这一章结束后，
仓库必须具备下面这些正式能力：

1. `control-plane`
   可以继续按单活正式方式部署
2. `cloud-plane`
   也有对称的正式部署资产
3. 一台 shared host
   可以用一套共享
   `Postgres`
   承载这两个面
4. 本机固定 `node agent`
   可以跟着
   `cloud-plane`
   一起长期运行
5. 首个生产 plane
   可以用仓库内脚本完成：
   - 创建
   - 注册
   - 首次验证

## 这一章明确不做什么

这一章刻意不做：

- 第二个长期运行 plane
- 阿里云正式入口机
- 域名 / CDN / 公网 TLS
- operator
  可观测性产品栈
- 更复杂的管理入口
  和认证方式

这些能力后面几章再收。

## 为什么这章不继续用 cloud-init 作为正式路径

`cloud-init`
在这个项目里很有价值，
但它更适合：

- 一次性引导
- lab 首装
- Terraform
  配合的自动初始化

它不适合承载：

- 长期升级
- 显式替换二进制
- 人工校验配置
- 主机重启后的标准运维动作

所以这一章开始，
正式长期运行主机要改成：

- Terraform
  只负责主机和网络底座
- 进程安装、
  配置落盘、
  `systemd`
  生命周期、
  plane 注册
  走显式 deploy 资产

## 这一章新增的正式部署资产

### `cloud-plane` 正式部署目录

新增：

- [deploy/cloud-plane/README.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/cloud-plane/README.md)
- [cloud-plane.env.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/cloud-plane/cloud-plane.env.example)
- [agent.env.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/cloud-plane/agent.env.example)
- [platform-config.tencent.json.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/cloud-plane/platform-config.tencent.json.example)
- [start-agent.sh.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/cloud-plane/start-agent.sh.example)
- [mini-cloud-cloud-plane.service.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/cloud-plane/systemd/mini-cloud-cloud-plane.service.example)
- [mini-cloud-agent.service.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/cloud-plane/systemd/mini-cloud-agent.service.example)

这组文件把原来散在
`cloud-init`
里的这些东西收成了正式资产：

- `cloud-plane`
  环境变量
- 首个固定
  `node agent`
  独立环境变量
- `cloud-plane`
  平台配置
- 本机固定 `node agent`
  启动包装
- `systemd`
  单元模板

### shared host 基础设施目录

新增：

- [deploy/platform-host/README.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/platform-host/README.md)
- [docker-compose.postgres.yml.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/platform-host/docker-compose.postgres.yml.example)
- [01-create-databases.sql](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/platform-host/initdb/01-create-databases.sql)

这一层专门负责：

- shared host
  上的共享
  `Postgres`
- 两个数据库：
  - `mini_cloud_control_plane`
  - `mini_cloud_cloud_plane`

这让：

- `control-plane`
  的正式部署资产
- `cloud-plane`
  的正式部署资产

可以继续各自保持边界清晰，
而 shared host
本身的基础设施也有单独落点。

### 正式工程入口

新增项目根目录
`Makefile`
入口：

- `make build-release`
  - 统一构建：
    - `control-plane`
    - `cloud-plane`
    - `agent`
    的 Linux 发布二进制

plane
注册不再依赖仓库脚本，
直接使用
`control-plane`
API
或 operator TUI。

## 这一章采用的主机布局

推荐把 `tencent-platform-host`
理解成：

- shared platform host

它的推荐布局是：

- `/opt/mini-cloud/control-plane/`
- `/opt/mini-cloud/cloud-plane/`
- `/opt/mini-cloud/platform-host/`
- `/etc/mini-cloud/control-plane/`
- `/etc/mini-cloud/cloud-plane/`
- `/var/lib/mini-cloud/cloud-plane/`
- `/var/lib/mini-cloud/agent/`

这样做的目的是：

- `control-plane`
  和
  `cloud-plane`
  各自有明确工作目录
- shared host
  自己的 compose /
  initdb
  也有独立位置

## 首次引导顺序

这章把首次引导顺序收成下面这条固定链：

1. shared host
   启动共享
   `Postgres`
2. 安装
   `control-plane`
   二进制、
   env、
   `systemd`
3. 安装
   `cloud-plane`
   二进制、
   env、
   agent env、
   platform-config、
   `systemd`
4. 启动
   `cloud-plane`
5. 启动本机固定
   `node agent`
6. 启动
   `control-plane`
7. 用
   `control-plane`
   API
   或 operator TUI
   创建并注册首个 Tencent plane
8. 回读：
   - `plane ready`
   - `nodesReady >= 1`
   - 本机 node
     容量可见

## 为什么这章不再把 provider 扩容模板和启动绑死

这章现在明确采用：

- `control-plane`
  分开启动
- `cloud-plane`
  分开启动
- `agent`
  分开启动
- 三者通过配置和注册关系显式建立连接

所以：

- `cloud-plane`
  启动本身不再强依赖
  `providerRuntimeSpec`
- 腾讯云 / 阿里云的真实扩容模板
  不再作为这章的启动前置条件

这样收的原因是：

- 首台长期运行 plane
  的核心目标是先稳定跑起来
- “未来要不要让这个 plane
  再去自动申请更多 runtime node”
  是后续单独能力

也就是说：

- 这章仍然可以给
  `tencent-platform-host`
  配置 Tencent
  凭证链，
  方便后续扩容章节继续复用
- 但这已经不再是
  `cloud-plane`
  本身能否启动的硬前置

## 这一章影响的代码与资产

- [deploy/control-plane/README.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/control-plane/README.md)
- [control-plane.env.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/control-plane/control-plane.env.example)
- [deploy/cloud-plane/README.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/cloud-plane/README.md)
- [deploy/platform-host/README.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/platform-host/README.md)
- [Makefile](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/Makefile)
- [README.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/README.md)

## 这一章的验证重点

这一章重点验证下面这几件事：

1. shared host
   共享
   `Postgres`
   能同时服务两套数据库
2. `control-plane`
   和
   `cloud-plane`
   都能通过正式
   `systemd`
   方式长期运行
3. 主机重启后，
   两个面和本机固定
   `node agent`
   都会自动恢复
4. `control-plane`
   API
   或 operator TUI
   能把首个 Tencent plane 正式接入
5. `control-plane`
   已经能看到：
   - 这个 plane
   - 这个 plane
     的首个固定 node
   - 基础容量快照

## 这一章结束后的平台理解方式

做到这里以后，
`mini-cloud`
不能再只被理解成：

- 可以做真实实验的多云平台

而要开始被理解成：

- 已经有第一台正式长期运行平台机的多云平台

这一步虽然还没把：

- 访问入口
- 观测栈
- 外层域名
- 第二个长期 plane

全部收进来，
但它已经把“正式运行的底座”
收出来了。

## 检查点

- 待本章提交时回填 commit hash。
