# 04 Platform Host Provisioning with Instance Role and EIP

这一章不再只准备共享网络，
而是开始把 `mini-cloud`
自己的第一台平台主机真正拉起来。

这里先聚焦三件最关键的云侧事情：

1. 给平台 `ECS`
   准备实例 `RAM` 角色
2. 创建或复用平台 `ECS`
3. 给这台机器分配并绑定 `EIP`

这一步做完以后，
平台主机就已经具备了：

- 固定的云侧身份
- 固定的内网位置
- 固定的公网入口

后面继续做真正的服务安装、
worker 纳管、
发布和回滚时，
都会以这台机器为基础继续往前走。

## 这一章解决的核心问题

很多人一开始会把两件事混在一起：

| 角色 | 真实职责 |
| --- | --- |
| 运行 `bootstrap` 的本机或 seed `ECS` 上的阿里云凭据 | 负责“创建资源” |
| 新创建的平台 `ECS` 的实例 `RAM` 角色 | 负责“这台机器以后以什么身份访问阿里云 API” |

也就是说，
这里不是把一个 `RAM` 用户“绑到 ECS 上”，
而是：

- `bootstrap`
  - 先用你当前已有的阿里云凭据
  - 调 `CreateRole`
  - 调 `AttachPolicyToRole`
  - 调 `RunInstances`
- 然后在 `RunInstances`
  - 里通过 `RamRoleName`
  - 让新机器出生时就带上实例 `RAM` 角色

真实关系可以直接理解成下面这张表：

| 阶段 | 谁在调用 API | 调什么 |
| --- | --- | --- |
| `bootstrap plan/apply` 执行时 | 你当前环境里的阿里云凭据 | 查网络、建角色、建 `ECS`、分配 `EIP` |
| 平台 `ECS` 创建完成后 | 这台 `ECS` 自己的实例 `RAM` 角色 | 以后在机器里继续访问阿里云 API |

所以：

- 当前登录环境的凭据
  - 是“安装工”
- 实例 `RAM` 角色
  - 是“机器自己的长期身份”

## 这一章新增了什么

当前 `bootstrap`
已经新增：

- `--phase platform`
- 平台 `RAM` 角色的 discover-first / ensure
- 平台 `ECS` 的 discover-first / create / start / wait-running
- 平台 `EIP` 的 discover-first / allocate / associate
- 一份 `platform inventory`

也就是说，
当前已经不只是：

- 能查一查云资源

而是已经能把：

- `03` 产出的共享网络底座
- `04` 需要的平台主机资源

真正串起来。

## 现在的执行顺序

这一章的真实执行顺序是：

1. 先回读 `03`
   创建出来的：
   - `VPC`
   - `vSwitch`
   - `SecurityGroup`
2. 确认平台实例 `RAM` 角色是否存在
3. 确认目标系统策略是否已经挂到角色上
4. 确认平台 `ECS` 是否已经存在
5. 如果不存在：
   - 在已有 `vSwitch`
     里创建平台 `ECS`
   - 创建时直接带上 `RamRoleName`
6. 如果实例已存在但没带对角色：
   - 再补一次 `AttachInstanceRamRole`
7. 确认平台 `EIP`
   是否已经存在
8. 如果不存在：
   - 分配新的 `EIP`
9. 如果 `EIP`
   还没绑到平台实例：
   - 再做 `AssociateEipAddress`
10. 输出一份新的 `platform inventory`

这里最重要的点是：

- `04`
  - 不是自己再去发明一套网络逻辑
- 它是站在 `03`
  - 已有的受管网络资源之上继续往前走

## CLI 现在怎么用

先看计划：

```bash
go run ./cmd/bootstrap plan \
  --phase platform \
  --config ./deploy/bootstrap/bootstrap-config.json.example
```

真正写资源时：

```bash
go run ./cmd/bootstrap apply \
  --phase platform \
  --config ./deploy/bootstrap/bootstrap-config.json \
  --inventory-out ./deploy/bootstrap/platform-inventory.json
```

这里要注意两点：

1. `platform` 阶段不会替你补跑 `03`
   - 如果共享网络底座还没准备好
   - 这里会直接告诉你：
   - 先去跑 `phase network`
2. `platform-inventory.json`
   - 是运行产物
   - 不应该提交进仓库

## 配置里多了哪些字段

这一章给 `bootstrap-config.json.example`
补了这些字段：

| 字段 | 作用 |
| --- | --- |
| `PlatformRoleName` | 平台 `ECS` 要使用的实例 `RAM` 角色名 |
| `PlatformRolePolicyNames` | 这一版先给平台主机挂上的系统策略列表 |
| `PlatformSystemDiskSizeGiB` | 平台实例系统盘下限 |
| `PlatformEIPBandwidthMbps` | 平台 `EIP` 带宽上限 |
| `PlatformEIPInternetChargeType` | 平台 `EIP` 计费方式，当前默认 `PayByTraffic` |

其中：

- `KeyPairName`
  - 现在对 `platform` 阶段是必填
  - 因为我们要保证后续可以直接 `SSH` 到这台机器

## 为什么这里先用系统策略

当前默认挂的是：

- `AliyunECSFullAccess`
- `AliyunVPCFullAccess`

这是一个教学上的有意简化。

原因是：

- `v2`
  - 现在还处在“先把平台完整装起来”的阶段
- 这时最怕的不是权限太宽，
  - 而是权限缺了一块，
  - 导致后面的平台安装和节点操作一直被细碎权限问题打断

所以这一步先把：

- 实例角色
- 角色策略挂载
- `RunInstances.RamRoleName`

这条主链打通。

后面如果要收紧权限，
再回头把系统策略换成更细的自定义策略即可。

## 平台实例现在怎么选

这一章没有让你自己手填一个实例规格，
而是保持和前面阿里云实验一致的思路：

1. 先固定：
   - 地域
   - `vSwitch` 所在 `zone`
   - 允许使用的实例规格白名单
2. 再调用：
   - `DescribeAvailableResource`
3. 只在当前 `zone`
   里挑：
   - 状态为 `Available`
   - 且在白名单里的规格
4. 最后按：
   - `AllowedInstanceTypes`
     的顺序选第一个

也就是说，
这里不是“查规格目录里有什么”，
而是：

- 在当前网络真实落地的 `zone`
  里看哪些规格现在真能开

## 镜像现在怎么选

镜像也不是随便抓一个：

1. 调 `DescribeImages`
2. 限定：
   - `ImageOwnerAlias=system`
   - `OSType=linux`
   - 目标 `InstanceType`
3. 优先排除：
   - `gpu`
   - `cuda`
   - `nvidia`
   - `hpc`
   这类明显偏专项场景的系统镜像
4. 在剩下的候选里挑最小的镜像

系统盘大小则按：

- `max(PlatformSystemDiskSizeGiB, 镜像大小)`

来决定。

所以这一章延续的还是前面那条思路：

- 先尽量选“小而通用”的系统镜像
- 再保证系统盘不会比镜像本身还小

## `platform inventory` 里现在有什么

`apply`
成功后，
当前会输出一份平台主机 inventory。

里面最重要的是这些字段：

| 字段 | 作用 |
| --- | --- |
| `NetworkBase` | 平台实例实际落到哪张 `VPC` / `vSwitch` / `SecurityGroup` |
| `Role` | 平台实例真正使用的实例 `RAM` 角色 |
| `Instance` | 平台 `ECS` 的 `InstanceId`、规格、内网地址、运行状态 |
| `EIP` | 平台实例对外暴露的公网地址 |
| `SSHCommandHint` | 后续手工登录和排障时的直接提示 |
| `ControlPlaneBaseURL` | 后面管理面要访问的基础地址 |

也就是说，
到这一章结束时，
平台“宿主机”这一层已经有了稳定 inventory，
后面继续做：

- 远程安装依赖
- 启动 control-plane
- 纳管 worker

都不需要再重新查一遍云资源。

## 这章和“真正安装平台服务”是什么关系

这里要说清楚：

这一章当前打通的是：

- 平台主机的云侧准备

具体包括：

- 身份
- 主机
- 公网入口

它还不是最终意义上的：

- control-plane 全套服务已经装进机器里并对外提供功能

所以你可以把这一章理解成：

- 先把“平台要住的那台房子”
  - 建好、编号、通电、挂门牌
- 后面的章节
  - 再把真正的平台服务搬进去

这样拆的好处是：

- 云资源问题
  - 和平台安装问题
  - 不会糊成一团

## 这一章跑了哪些检查

当前我已经跑过：

```bash
go test ./...
./scripts/check.sh
go run ./cmd/bootstrap plan \
  --phase platform \
  --config ./deploy/bootstrap/bootstrap-config.json.example
go run ./cmd/bootstrap apply \
  --phase platform \
  --config ./deploy/bootstrap/bootstrap-config.json
```

其中：

- `plan`
  - 已经成功走到真实阿里云读路径
- `apply`
  - 已经在真实阿里云里创建并回读：
  - 实例 `RAM` 角色
  - 角色系统策略挂载
  - 平台 `ECS`
  - 平台 `EIP`
  - `EIP -> ECS`
    绑定关系

这次真实 `apply`
还额外验证了两件关键事情：

1. 新导入的本机公钥
   - 确实可以作为 `KeyPair`
     给新平台实例登录
2. 新平台实例已经可以从当前机器直接 `SSH`
   登录

同时，
这次真实验证也暴露了一个结果回读层的小问题，
现在已经修掉了：

- `AssociateEipAddress`
  成功后，
  不能只看“已经绑上实例且拿到公网 IP”
  - 还要继续等 `Status=InUse`
  - 否则结果里会过早显示成 `Associating`

## 这一章的结论

`04`
真正建立起来的是：

- `mini-cloud`
  - 第一次拥有了“自己的云侧平台主机”
  - 这台主机有固定实例身份
  - 有固定公网入口
  - 有可复用的 platform inventory

也就是说，
从这一章开始，
`v2`
已经不只是“能在阿里云里建出几张网络资源”，
而是开始具备：

- 把平台自己落到云上

这条真正的主线了。

## 本章检查点

- 提交：
  - `ec02c8b560b830f83eee0f5d1197220d6598a2fc`
- 状态：
  - platform `ECS`、实例角色、`EIP` 和 platform inventory 已经形成正式主机准备链路
