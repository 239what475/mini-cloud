# 14 Real Aliyun End-to-End Lab

这一章开始，
`mini-cloud v2`
第一次把真实阿里云实验收成一个完整闭环：

- 本章自己创建实验资源
- 本章自己完成远端安装
- 本章自己做发布验收
- 本章自己清理资源

也就是说，
这一章不应该依赖：

- `03`
  或 `04`
  之前“恰好还留在阿里云里的旧资源”

前面章节提供的是：

- 配置结构
- 资源建模
- `bootstrap` 能力

而不是：

- 必须长期保留一套已经创建好的网络和平台主机

这点很重要，
因为如果真实实验依赖旧资源常驻，
那就会变成：

- 流程不自洽
- 排障边界不清楚
- 还会白白持续计费

## 这一章现在验证什么

这一章验证的是下面这一条真实主链：

```text
本机执行 install
  -> bootstrap 创建 network / platform
  -> 上传 control-plane 和 agent
  -> 远端启动 Postgres / control-plane / managed worker
  -> 本机回读 healthz 和 node ready
  -> demo 发布 nginx
  -> gateway 回读业务页面
  -> cleanup 释放云资源
```

这套流程对应的脚本是：

- `./scripts/real-aliyun-lab.sh`

它现在提供四个子命令：

| 子命令 | 作用 |
| --- | --- |
| `install` | 先执行 `bootstrap apply network/platform`，再完成远端安装，并把本次实验状态写到本地 |
| `status` | 回读真实平台 `healthz`、节点列表，以及远端 `systemd` 服务状态 |
| `demo` | 创建 demo project / app / release，并通过 gateway 回读业务页面 |
| `cleanup` | 执行 `bootstrap destroy platform/network`，并清理本地实验产物 |

## 这一章的前提

跑这一章时，
真正需要的是这些东西：

1. 本地有：
   - `./deploy/bootstrap/bootstrap-config.json`
2. 这个配置里对应的 `RAM` 身份有权限去创建和删除：
   - `VPC`
   - `vSwitch`
   - `security group`
   - `ECS`
   - `EIP`
   - 实例角色
3. 你手里有平台 `KeyPairName` 对应的私钥
4. `AllowedAdminCIDRs`
   已经允许你当前的来源地址
5. 平台镜像是 Debian / Ubuntu 系
   - 因为远端安装阶段会用 `apt-get`

这里再记一个很常见的坑：

- 如果你中途切换了网络
- 当前出口公网 IP 变了
- 但 `AllowedAdminCIDRs` 还没同步更新

那么这章通常会卡在：

- `SSH`
- 或 `healthz` 回读

所以真实实验前，
最好先确认一下：

- 本地现在的出口地址
- 和 `bootstrap-config.json`
  里的管理白名单

是不是同一套。

注意这里没有：

- “必须先手动跑完 `03/04` 并保留资源”

因为 `install`
自己就会创建本章需要的资源。

## 第一步：执行 install

最常用的命令是：

```bash
./scripts/real-aliyun-lab.sh install \
  --ssh-key ~/.ssh/<your-private-key.pem>
```

如果想显式指定输入输出路径，
可以写完整一点：

```bash
./scripts/real-aliyun-lab.sh install \
  --config ./deploy/bootstrap/bootstrap-config.json \
  --network-inventory ./deploy/bootstrap/inventory.json \
  --inventory ./deploy/bootstrap/platform-inventory.json \
  --state-out ./deploy/bootstrap/real-lab-state.json \
  --ssh-key ~/.ssh/<your-private-key.pem>
```

这一步实际会做下面这些事：

1. 本地执行：
   - `bootstrap apply --phase network`
2. 本地执行：
   - `bootstrap apply --phase platform`
3. 生成本次实验用的：
   - `inventory.json`
   - `platform-inventory.json`
4. 本地交叉编译：
   - `cmd/control-plane`
   - `cmd/agent`
5. 上传二进制和远端安装脚本到新建的平台 `ECS`
6. 在远端确保：
   - `docker`
   - `curl`
   可用
7. 在远端启动：
   - `mini-cloud-postgres`
   - `mini-cloud-control-plane.service`
   - `mini-cloud-agent.service`
8. 本机等待：
   - `healthz` 可访问
   - managed worker 进入 `ready`
9. 把本次实验状态写到：
   - `real-lab-state.json`

这里要注意，
`platform-inventory.json`
在这一章里已经变成：

- `install` 过程中自动生成的本地运行产物

不再是：

- 你必须事先手工准备好的前置输入

## 第二步：查看平台状态

安装完成后，
先回读一次状态：

```bash
./scripts/real-aliyun-lab.sh status
```

它会做三件事：

1. 回读：
   - `/api/healthz`
2. 回读：
   - `/api/v1/platform/nodes`
3. 通过 `SSH`
   查看远端：
   - `mini-cloud-control-plane.service`
   - `mini-cloud-agent.service`
   是否是 `active`

这一步的目的很直接：

- 确认平台活着
- 确认第一台真实 worker 已经纳管
- 给真实环境排障留一个最短路径

## 第三步：发布一个真实 demo

确认状态没问题后，
再执行：

```bash
./scripts/real-aliyun-lab.sh demo
```

这一步会真正走一遍平台主链：

1. 创建 demo project
2. 创建 demo app
3. 给 app 绑定一个测试域名
4. 提交一个：
   - `nginx:1.27-alpine`
   release
5. 等待 deployment / execution / app 进入：
   - `running`
6. 再通过：
   - `curl -H "Host: ..."`
   命中 gateway

这里绑定的域名形态大致会像：

- `echo-<timestamp>.<platform-name>.lab.test`

它不是拿来做真实公网 DNS 解析的，
而是为了验证：

- 平台里的 domain binding
- gateway 路由
- worker 上的真实 execution

脚本最后会输出一条类似下面的验证命令：

```bash
curl -H "Host: <demo-host>" <base-url>/
```

你可以手动再回读一次。

## 第四步：及时 cleanup

这一章最重要的动作之一，
不是 `demo`，
而是实验结束后的：

```bash
./scripts/real-aliyun-lab.sh cleanup
```

它会按顺序做两件事：

1. 执行：
   - `bootstrap destroy --phase platform`
2. 执行：
   - `bootstrap destroy --phase network`

然后再删除本地运行产物：

- `./deploy/bootstrap/inventory.json`
- `./deploy/bootstrap/platform-inventory.json`
- `./deploy/bootstrap/real-lab-state.json`

这样这一章结束后，
不会在云上无意中留下一套：

- 空跑的 `ECS`
- 空跑的 `EIP`
- 没有继续使用价值的网络资源

如果 `install`
或 `demo`
中途失败，
也应该优先执行一次：

- `cleanup`

把现场收干净以后再重来。

## 这一章当前的实验形态

这一章仍然是刻意收敛过的版本。

当前只使用一台阿里云 `ECS`，
同时承载：

- `docker` 里的 `Postgres`
- `systemd` 托管的 control-plane
- `systemd` 托管的 managed worker

可以先把它理解成：

```text
同一台 ECS
  ├─ mini-cloud-postgres
  ├─ mini-cloud-control-plane.service
  └─ mini-cloud-agent.service
       └─ 拉起 demo app container
```

这不是说：

- platform 和 worker 的逻辑边界消失了

而是说：

- 物理上先共用一台机器
- 以最低成本把真实闭环验证出来

后面如果要拆成：

- 独立 platform host
- 独立 worker host

再继续扩展即可。

## 这一章跑完后会留下什么

如果你刚执行完 `install`
但还没 `cleanup`，
通常会看到：

### 本地

- `./deploy/bootstrap/inventory.json`
- `./deploy/bootstrap/platform-inventory.json`
- `./deploy/bootstrap/real-lab-state.json`

### 云上

- 一套本章创建出来的：
  - `VPC`
  - `vSwitch`
  - `security group`
  - `ECS`
  - `EIP`
- 远端机器上的：
  - `mini-cloud-postgres`
  - `mini-cloud-control-plane.service`
  - `mini-cloud-agent.service`
- 至少一个 demo app 容器

如果你执行完：

- `cleanup`

这些资源和本地产物都应该被移除。

## 这一章真正证明了什么

这章打通以后，
`mini-cloud v2`
第一次具备了下面这个性质：

- 给定一份真实阿里云配置
- 平台可以自己把实验环境建起来
- 平台可以把自己装上去
- 平台可以纳管第一台真实 worker
- 平台可以发布真实容器应用
- 实验结束后还能把现场收干净

这和“前面章节分别能创建一点资源”是两回事。

这一章证明的是：

- `bootstrap -> install -> register -> deploy -> verify -> destroy`

这条完整主链已经成立。

## 这章之后还没做什么

当前还没有做的事情主要有：

- 平台宿主机和 worker 物理分离
- 第二台独立 worker `ECS`
- 专门的公网入口代理层
- 真实 `TLS` 证书和 `80/443` 接入
- 更正式的重装、升级和变更策略

这些内容会继续留给下一章：

- `15-bootstrap-reinstall-upgrade-and-destroy`

## 建议的最小验收顺序

这一章建议至少按下面顺序跑一遍：

```bash
bash -n ./scripts/real-aliyun-lab.sh
./scripts/check.sh
./scripts/real-aliyun-lab.sh install --ssh-key ~/.ssh/<your-private-key.pem>
./scripts/real-aliyun-lab.sh status
./scripts/real-aliyun-lab.sh demo
./scripts/real-aliyun-lab.sh cleanup
```

这里：

- 前两条
  是本地检查
- 后四条
  才是这一章真正的阿里云闭环实验

## 本章检查点

- 提交：
  - `08bed91fe252fc3ddd69de13e3729312ba33af91`
- 状态：
  - `bootstrap -> install -> register -> deploy -> verify -> destroy` 的真实阿里云闭环已经成立
