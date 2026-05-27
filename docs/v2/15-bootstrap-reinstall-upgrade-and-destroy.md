# 15 Bootstrap Reinstall Upgrade and Destroy

`14`
已经证明了：

- `mini-cloud v2`
  可以在真实阿里云里
  - 创建平台资源
  - 安装平台
  - 纳管第一台 worker
  - 发布真实应用
  - 最后把资源清理掉

但真实运维不会只发生一次“首次安装”。

更常见的是：

- 想重跑一次安装，确认流程是不是幂等
- 本地代码更新了，想把新版本发到现有平台上
- 平台宿主机上的软件状态已经脏了，想保留云资源但重装平台
- 实验结束后，想把整套云资源彻底删掉

这一章就是把这四件事区分清楚。

## 这一章现在新增了什么

真实阿里云实验脚本：

- `./scripts/real-aliyun-lab.sh`

现在除了：

- `install`
- `status`
- `demo`
- `cleanup`

又新增了：

- `upgrade`
- `reinstall`
- `destroy`

其中：

- `destroy`
  只是 `cleanup` 的别名

## 先把四个动作区分清楚

| 动作 | 复用云资源 | 复用平台数据 | 典型用途 |
| --- | --- | --- | --- |
| `install` | 是 | 是 | 首次安装，或者把已有环境再 reconcile 一次 |
| `upgrade` | 是 | 是 | 上传当前代码编译出的新二进制并重启服务 |
| `reinstall` | 是 | 否 | 保留 `VPC/ECS/EIP`，但把平台软件和数据库卷清空后重装 |
| `cleanup` / `destroy` | 否 | 否 | 把这章实验用到的云资源和本地状态文件一起删除 |

这里最容易混淆的是：

- `install`
  不是“每次都新建一套云资源”
- `reinstall`
  不是“销毁后重新申请云资源”

`install`
和 `reinstall`
都默认会先重跑：

- `bootstrap apply --phase network`
- `bootstrap apply --phase platform`

区别只在于：

- 远端宿主机上的平台软件和数据要不要保留

## 为什么重跑 install 不会重复建资源

原因有两层。

第一层是：

- `bootstrap apply`
  本身就是 discover-first 的

它会先去发现当前地域里已经存在的受管资源，
再决定是：

- `reuse`
- `start`
- `wait`
- 还是真的 `create`

所以当你第二次执行：

```bash
./scripts/real-aliyun-lab.sh install \
  --ssh-key ~/.ssh/<your-private-key.pem>
```

只要前一次创建的资源还在，
这次通常会复用已有的：

- `VPC`
- `vSwitch`
- `security group`
- 平台 `ECS`
- `EIP`
- 平台实例角色

第二层是：

- 远端安装阶段也做成了幂等

也就是说：

- 已有的 systemd unit 会被覆盖为当前版本
- 已有的 control-plane / agent 二进制会被替换
- 已有的 `mini-cloud-postgres` 容器会被复用或重新拉起
- 已有的 `mini-cloud-postgres-data` volume 会继续保留

所以重跑 `install`
更像是：

- “把现有平台重新收敛到当前本地代码和当前配置”

而不是：

- “再造一套新平台”

## upgrade：保留数据，只替换程序

`upgrade`
适合在这种时候用：

- 你改了本地代码
- 想把新的 `control-plane` 和 `agent`
  发到现有平台上
- 但不想丢掉平台数据

命令是：

```bash
./scripts/real-aliyun-lab.sh upgrade
```

如果本地状态文件不在默认位置，
可以显式指定：

```bash
./scripts/real-aliyun-lab.sh upgrade \
  --config ./deploy/bootstrap/bootstrap-config.json \
  --network-inventory ./deploy/bootstrap/inventory.json \
  --inventory ./deploy/bootstrap/platform-inventory.json \
  --state ./deploy/bootstrap/real-lab-state.json
```

这一步会做这些事：

1. 重跑 `bootstrap apply network/platform`
2. 重新生成最新的 `inventory`
3. 用当前本地代码重新编译：
   - `cmd/control-plane`
   - `cmd/agent`
4. 上传到平台 `ECS`
5. 覆盖远端：
   - 二进制
   - `control-plane.env`
   - systemd unit
6. 保留：
   - `mini-cloud-postgres-data`
   - 平台数据库里的项目、应用、发布记录
   - 远端 agent state
7. 重启：
   - `mini-cloud-control-plane.service`
   - `mini-cloud-agent.service`

这次真实阿里云验收里，
这里还暴露了一个非常具体的问题：

- 首次发布 `nginx:1.27-alpine`
  时
- worker 侧第一次真正去拉业务镜像
- 原来的默认：
  - `docker-timeout=30s`
  在真实公网环境里偏短
- 结果是：
  - agent 先报
    `docker run failed: context deadline exceeded`
  - 但 Docker daemon 后面其实还把镜像拉下来了
  - 容器甚至已经在宿主机上跑起来了

所以这次真实验收后，
把 agent 默认的：

- `docker-timeout`

从：

- `30s`

提高到了：

- `2m`

这样做不是为了“掩盖 bug”，
而是因为真实云环境里第一次冷拉镜像，
本来就可能明显慢于本地实验。

这里有一个很重要的点：

- `upgrade`
  默认会复用旧的 `admin token`

原因很直接：

- 这是一套“原地升级”
- 不是“换一套全新的平台身份”

另外，
`control-plane`
启动时会自动执行数据库 migration，
所以这里的 `upgrade`
不只是“替换一个二进制文件”，
还包括：

- 当前版本需要的 schema 升级

升级后，
最少应该再跑一次：

```bash
./scripts/real-aliyun-lab.sh status
```

如果想再验证一遍主链，
也可以继续跑：

```bash
./scripts/real-aliyun-lab.sh demo
```

## reinstall：保留云资源，但清空平台软件和数据

`reinstall`
解决的是另一个问题：

- 云上的平台 `ECS` 还想继续用
- `EIP` 也想继续用
- 共享网络底座也想继续用
- 但宿主机上的平台状态已经不想保留了

比如：

- 你想验证“从干净机器重装平台”这条链
- 你怀疑宿主机上的软件状态已经脏了
- 你想保留同一套云资源继续实验

命令是：

```bash
./scripts/real-aliyun-lab.sh reinstall
```

这一条和 `upgrade`
最大的区别是，
它在远端会先清掉：

- `INSTALL_ROOT`
  默认是：
  - `/opt/mini-cloud`
- agent 本地状态目录：
  - `/var/lib/mini-cloud-agent`
- 旧的业务容器：
  - `mini-cloud-dep_*`
- `mini-cloud-postgres` 容器
- `mini-cloud-postgres-data` volume

也就是说，
`reinstall`
之后保留的是：

- `VPC`
- `vSwitch`
- `security group`
- 平台 `ECS`
- `EIP`

但不保留的是：

- 平台数据库里的项目、应用、发布记录
- 旧的 agent state
- 远端旧版本程序文件

所以它更接近：

- “保留同一台云主机和同一个公网入口，再做一次干净安装”

而不是：

- “在旧状态上继续升级”

`reinstall`
完成后，
建议先跑：

```bash
./scripts/real-aliyun-lab.sh status
```

再重新跑一次：

```bash
./scripts/real-aliyun-lab.sh demo
```

因为这时平台数据已经是空白的了。

## cleanup / destroy：真正把云资源删掉

如果你不想再保留这章实验环境，
最后执行：

```bash
./scripts/real-aliyun-lab.sh cleanup
```

或者：

```bash
./scripts/real-aliyun-lab.sh destroy
```

这两条命令等价。

它会做两件事：

1. 执行：
   - `bootstrap destroy --phase platform`
   - `bootstrap destroy --phase network`
2. 删除本地产物：
   - `real-lab-state.json`
   - `platform-inventory.json`
   - `inventory.json`

所以：

- `reinstall`
  是保留云资源、重装平台
- `cleanup/destroy`
  才是释放云资源、结束实验

## 失败后应该怎么重试

这一章真正要学会的，
不是只有“命令怎么敲”，
还包括：

- 失败时应该重跑哪一步

可以按下面这套思路记。

### 1. install 半路失败

如果失败发生在：

- `bootstrap apply`
- 上传 bundle
- 远端安装

通常直接重跑同一条：

```bash
./scripts/real-aliyun-lab.sh install \
  --ssh-key ~/.ssh/<your-private-key.pem>
```

就行。

因为目标状态仍然是：

- 这套云资源存在
- 平台软件在这台宿主机上完成安装

### 2. upgrade 半路失败

最常见的做法就是：

- 再跑一次 `upgrade`

因为它的目标状态很清楚：

- 云资源继续复用
- 数据继续保留
- 当前代码重新发布到现有宿主机

### 3. reinstall 半路失败

如果已经开始清数据了，
那最合理的恢复动作通常也是：

- 再跑一次 `reinstall`

因为这条命令追求的本来就是：

- “把宿主机重新收敛成一台干净的平台主机”

### 4. cleanup 失败

`cleanup`
底层走的是：

- `bootstrap destroy`

而 `bootstrap destroy`
内部已经处理了一部分真实云环境里的等待和重试，
比如：

- 实例删除后，
  `ENI`
  或安全组引用还没立刻释放

所以如果这一步失败，
优先做的事情通常也是：

- 再跑一次 `cleanup`

## 建议的最小验收顺序

这一章建议至少按下面顺序跑一遍：

```bash
bash -n ./scripts/real-aliyun-lab.sh
./scripts/check.sh
./scripts/real-aliyun-lab.sh install --ssh-key ~/.ssh/<your-private-key.pem>
./scripts/real-aliyun-lab.sh status
./scripts/real-aliyun-lab.sh demo
./scripts/real-aliyun-lab.sh upgrade
./scripts/real-aliyun-lab.sh status
./scripts/real-aliyun-lab.sh reinstall
./scripts/real-aliyun-lab.sh status
./scripts/real-aliyun-lab.sh demo
./scripts/real-aliyun-lab.sh cleanup
```

这里：

- 前两条
  是本地检查
- 中间几条
  是真实平台的重跑、升级、重装验证
- 最后一条
  是结束实验并释放成本

## 这一章真正证明了什么

到这里，
`mini-cloud v2`
在真实阿里云里已经不只是：

- “能安装一次”

而是开始具备了最基础的平台运维闭环：

- 首次安装
- 幂等重跑
- 原地升级
- 保留云资源的重装
- 最终销毁

这意味着下一章可以继续处理的，
就不再只是“有没有这条链”，
而是：

- 真实环境里还剩哪些毛刺
- 哪些说明还需要整理
- 哪些脚本还要继续收口和加固

## 这次真实验收额外确认到的细节

这次不是只跑了本地检查，
而是真的把这一章在阿里云上完整跑了一遍。

真实链路里确认到的点有：

- `install`
  可以从零创建：
  - `VPC`
  - `vSwitch`
  - `security group`
  - 平台 `ECS`
  - `EIP`
  - RAM role
- 第一次 `demo`
  暴露了真实环境里的冷拉镜像超时问题
- 修正默认 `docker-timeout` 后，
  `upgrade`
  可以把新 agent 二进制推到现有平台上
- 修正后第二次 `demo`
  成功
- `reinstall`
  会保留云资源，
  但清空平台软件、数据库卷和旧 workload 容器
- `reinstall`
  之后再跑一次 `demo`
  也成功
- 最后 `cleanup`
  已经真正释放了：
  - 平台 `ECS`
  - `EIP`
  - `security group`
  - `vSwitch`
  - `VPC`
  - RAM role

这说明：

- 这一章现在不只是“脚本设计上说得通”
- 而是“真实跑过，并且把真实环境里的一个超时问题顺手修掉了”

## 本章检查点

- 提交：
  - `d361e1ab1f564c46b55de939a7d4d729e444b1d9`
- 状态：
  - `real-aliyun-lab.sh` 已支持 `install / status / demo / upgrade / reinstall / cleanup / destroy`
  - 这一章的真实阿里云验收已经完整跑通
