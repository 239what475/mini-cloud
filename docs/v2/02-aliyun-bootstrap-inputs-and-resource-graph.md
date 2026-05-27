# 02 Aliyun Bootstrap Inputs And Resource Graph

这一章先不写创建代码，
先把 `bootstrap`
真正依赖的输入和资源图定下来。

这是 `v2`
里一个很关键的“冻结点”，
因为后面的：

- `03-bootstrap-resource-provisioning`
- `04-platform-host-provisioning-with-instance-role-and-eip`
- `05-managed-worker-registration-and-platform-topology`

都会直接依赖这里的约定。

如果这一章不先定清楚，
后面最容易出现的问题就是：

- 一边写创建逻辑
- 一边改输入格式
- 一边改资源命名

最后整个 `bootstrap`
很难做到真正可重复执行。

## 这一章到底要回答什么

这一章主要回答六个问题：

1. `bootstrap` 到底运行在哪
2. 最小输入参数有哪些
3. 平台自有资源图长什么样
4. 需要哪些 `RAM` 权限
5. 资源命名、标签和幂等键怎么设计
6. 销毁时的依赖顺序怎么理解

## `bootstrap` 运行在哪

`bootstrap`
不是 control-plane 自己跑出来的，
它必须运行在一个“平台之外”的执行位置。

当前 `v2`
允许两种执行位置：

### 1. 本机

适合：

- 你在本地直接发起安装
- 本机已经有阿里云凭据

### 2. seed `ECS`

适合：

- 你想把整个 `bootstrap`
  放到阿里云里执行
- seed 机器已经绑定了合适的 `RAM Role`

这里要特别强调：

- seed `ECS`
  - 不是 `mini-cloud` 的平台节点
- 它只是：
  - `bootstrap runner`

这样做的好处是：

- 平台自己和安装器职责分离
- 平台损坏时，外部仍然保留一处可以重装和恢复的平台入口

## 最小拓扑先定成什么样

为了让 `v2`
既能体现“自动创建云资源”，
又不至于一开始就铺得太大，
当前最小拓扑先定成：

```text
[seed runner]
    |
    | bootstrap
    v
[VPC]
  |
  +-- [vSwitch]
        |
        +-- [platform ECS]  <- 跑 control-plane / web / gateway / postgres
        |
        +-- [first worker ECS] <- 跑 worker agent，承接应用
  |
  +-- [SecurityGroup]
  |
  +-- [EIP] -> 绑定到 platform ECS
```

这个拓扑里有两个关键判断：

1. platform `ECS`
   - 和 first worker `ECS`
   - 先分开
2. 入口公网地址先通过：
   - `EIP`
   - 暴露到 platform `ECS`

第 1 点很重要，
因为如果 platform 和 worker 还是一台机，
那就又会退回“all in one”味道太重的问题。

第 2 点也很重要，
因为前面云实验里我们已经明确踩过这个边界：

- `EIP`
  - 是独立资源
  - 生命周期不能和实例想当然地绑死

## 最小输入参数应该有哪些

`bootstrap`
的输入不能太少，
否则真实安装时不够用；
也不能太多，
否则每次启动都会像在手工填控制台。

所以当前建议先收成下面这组最小输入。

### 1. 执行环境输入

- `BootstrapRegionId`
  - 这次平台资源准备在哪个地域
- `BootstrapRunnerMode`
  - `local` 或 `seed-ecs`
- `PreferredZoneId`
  - 可选
  - 如果你想固定落到某个可用区，可以显式给它
  - 不填时，`bootstrap`
    - 会先从 `DescribeZones`
    - 里挑一个 zone 给当前阶段使用

### 2. 平台身份输入

- `PlatformName`
  - 这套平台的逻辑名字
- `Environment`
  - 例如：
  - `lab`
  - `dev`
  - `staging`
- `Owner`
  - 当前平台归谁管理

### 3. 地域和规格约束

这一组输入直接继承我们前面阿里云实验已经定下来的限制，
避免 `v2`
自己又绕出另一套规则。

允许的地域先固定为：

- 华北6（乌兰察布）`cn-wulanchabu`
- 华南2（河源）`cn-heyuan`
- 华东1（杭州）`cn-hangzhou`
- 华北2（北京）`cn-beijing`
- 美国（弗吉尼亚）`us-east-1`

允许自动选择的实例规格先固定为：

- `ecs.e-c1m1.large`
- `ecs.e-c1m2.large`
- `ecs.e-c1m4.large`
- `ecs.e-c1m2.xlarge`
- `ecs.e-c1m4.xlarge`
- `ecs.e-c1m2.2xlarge`
- `ecs.u1-c1m1.large`
- `ecs.u1-c1m2.large`
- `ecs.u1-c1m2.xlarge`
- `ecs.u1-c1m2.2xlarge`
- `ecs.u2a-c1m1.large`
- `ecs.u2a-c1m2.large`
- `ecs.u2a-c1m2.xlarge`
- `ecs.u2a-c1m2.2xlarge`
- `ecs.u2i-c1m1.large`
- `ecs.u2i-c1m2.large`
- `ecs.u2i-c1m2.xlarge`
- `ecs.u2i-c1m2.2xlarge`

这意味着：

- `bootstrap`
  - 不是“想买哪种实例就买哪种”
- 它仍然要遵守我们前面真实实验已经验证过的约束

### 4. 镜像与登录输入

- `ImageStrategy`
  - 例如：
  - `ImageOwnerAlias=system`
  - `OSType=linux`
- `KeyPairName`
  - 或者：
  - `SSHPublicKeyPath`

这一组输入很重要，
因为 `04`
章要真正连到新创建的 platform `ECS`
去做安装。

也就是说，
`bootstrap`
不能只会创建机器，
还必须有办法安全地进机器。

### 5. 网络输入

- `VPCCidrBlock`
- `VSwitchCidrBlock`
- `AllowedAdminCIDRs`

这里的：

- `AllowedAdminCIDRs`

用来表达：

- 哪些来源可以通过 `22`
  - 访问 platform `ECS`

这也意味着：

- 默认不应该把 `22`
  - 对全世界开放

### 6. 平台安装输入

- `ControlPlaneHTTPPort`
- `GatewayHTTPPort`
- `GatewayHTTPSPort`
- `PlatformDomain`
  - 可以先为空
- `ComposeProjectName`
- `InstallRoot`

这些输入不一定全部在 `02`
就实现，
但现在应该先把它们列成正式输入位，
避免 `04`
又临时发明一套新的安装参数。

## 推荐的输入文件长什么样

这里先给一个教学用的最小例子：

```json
{
  "BootstrapRegionId": "cn-beijing",
  "BootstrapRunnerMode": "seed-ecs",
  "PreferredZoneId": "",
  "PlatformName": "mini-cloud-v2-lab",
  "Environment": "lab",
  "Owner": "student",
  "AllowedRegionIds": [
    "cn-wulanchabu",
    "cn-heyuan",
    "cn-hangzhou",
    "cn-beijing",
    "us-east-1"
  ],
  "AllowedInstanceTypes": [
    "ecs.e-c1m1.large",
    "ecs.e-c1m2.large",
    "ecs.e-c1m4.large",
    "ecs.e-c1m2.xlarge",
    "ecs.e-c1m4.xlarge",
    "ecs.e-c1m2.2xlarge",
    "ecs.u1-c1m1.large",
    "ecs.u1-c1m2.large",
    "ecs.u1-c1m2.xlarge",
    "ecs.u1-c1m2.2xlarge",
    "ecs.u2a-c1m1.large",
    "ecs.u2a-c1m2.large",
    "ecs.u2a-c1m2.xlarge",
    "ecs.u2a-c1m2.2xlarge",
    "ecs.u2i-c1m1.large",
    "ecs.u2i-c1m2.large",
    "ecs.u2i-c1m2.xlarge",
    "ecs.u2i-c1m2.2xlarge"
  ],
  "ImageStrategy": {
    "ImageOwnerAlias": "system",
    "OSType": "linux"
  },
  "KeyPairName": "mini-cloud-bootstrap",
  "VPCCidrBlock": "10.66.0.0/16",
  "VSwitchCidrBlock": "10.66.1.0/24",
  "AllowedAdminCIDRs": [
    "203.0.113.10/32"
  ],
  "ControlPlaneHTTPPort": 8080,
  "GatewayHTTPPort": 80,
  "GatewayHTTPSPort": 443,
  "PlatformDomain": "",
  "ComposeProjectName": "mini-cloud",
  "InstallRoot": "/opt/mini-cloud"
}
```

这里的 `203.0.113.10/32`
只是教学占位地址，
不是要写真实公网 IP。

## 平台自有资源图应该怎样理解

这里先把 `v2`
要自动创建的资源图固定下来：

```text
[seed runner]
    |
    +--> Create/Find VPC
             |
             +--> Create/Find vSwitch
             |
             +--> Create/Find SecurityGroup
             |
             +--> Create/Find platform ECS
             |       |
             |       +--> Allocate/Associate EIP
             |
             +--> Create/Find first worker ECS
```

这张图里有两个要点。

### 1. 先查再建

也就是说每一层都应该是：

- 先 `Describe`
- 能复用就复用
- 不能复用才 `Create`

这个模式我们在前面的阿里云实验里已经反复验证过，
它是把真实写资源流程做成“可重复执行”的基础。

### 2. `EIP` 是独立资源

不要把它理解成：

- “实例上的一个公网字段”

而应该理解成：

- 独立的公网地址资源

所以它必须被单独纳入：

- 创建流程
- 回读流程
- 销毁流程

## `RAM` 权限应该怎么给

这里要分成两个层次来看。

### 1. 教学和开发阶段的省事做法

如果你想先把 `v2`
尽快跑起来，
最省事的方式依然是：

- `AliyunVPCFullAccess`
- `AliyunECSFullAccess`

原因很简单：

- `bootstrap`
  - 当前要碰到的核心资源
  - 就集中在：
  - `VPC`
  - `SecurityGroup`
  - `ECS`
  - `EIP`

### 2. 后面再收缩成最小动作权限

等 `v2`
流程稳定后，
再把它收缩成更细的动作集更合理。

当前最值得先记住的动作大致有这些：

#### `VPC` / 网络侧

- `vpc:CreateVpc`
- `vpc:DescribeVpcs`
- `vpc:DeleteVpc`
- `vpc:CreateVSwitch`
- `vpc:DescribeVSwitches`
- `vpc:DeleteVSwitch`
- `vpc:AllocateEipAddress`
- `vpc:DescribeEipAddresses`
- `vpc:AssociateEipAddress`
- `vpc:UnassociateEipAddress`
- `vpc:ReleaseEipAddress`

#### `ECS` 侧

- `ecs:RunInstances`
- `ecs:DescribeInstances`
- `ecs:DeleteInstance`
- `ecs:StartInstance`
- `ecs:StopInstance`
- `ecs:DescribeZones`
- `ecs:DescribeImages`
- `ecs:DescribeInstanceTypes`
- `ecs:CreateSecurityGroup`
- `ecs:DescribeSecurityGroups`
- `ecs:DescribeSecurityGroupAttribute`
- `ecs:AuthorizeSecurityGroup`
- `ecs:RevokeSecurityGroup`
- `ecs:DeleteSecurityGroup`

这里先不追求一次就把所有动作列到最细，
但方向要明确：

- 前期先用托管策略把主链跑通
- 后面再收缩成真正的最小权限

## 平台资源怎么命名更合适

资源名要满足两个目标：

1. 让人一眼看出它是谁的
2. 让程序稳定复用和清理它

所以推荐把命名统一成：

- `<platform-name>-vpc`
- `<platform-name>-vsw`
- `<platform-name>-sg`
- `<platform-name>-eip`
- `<platform-name>-platform`
- `<platform-name>-worker-01`

例如：

- `mini-cloud-v2-lab-vpc`
- `mini-cloud-v2-lab-vsw`
- `mini-cloud-v2-lab-sg`
- `mini-cloud-v2-lab-eip`
- `mini-cloud-v2-lab-platform`
- `mini-cloud-v2-lab-worker-01`

## 标签应该怎么打

光靠名字还不够，
因为后面：

- 发现资源
- 清理资源
- 做 inventory

都更适合基于标签。

所以平台自有资源建议统一至少带上：

- `managed-by=mini-cloud-bootstrap`
- `project=mini-cloud`
- `platform-name=<PlatformName>`
- `environment=<Environment>`
- `owner=<Owner>`

实例类资源还建议再带：

- `topology-role=platform`
  - 或
- `topology-role=worker`

这样后面做：

- 资源发现
- 资源销毁
- 平台拓扑展示

都会更直观。

## 幂等到底靠什么保证

这一节很重要，
因为前面阿里云实验里我们已经真实踩过：

- `ClientToken`
  - 跨删除重建周期长期复用

导致命中旧幂等结果的问题。

所以这里要把三个概念分清：

### 1. 资源名字和标签

它们负责表达：

- “我想管理的是哪一组平台资源”

这是长期稳定的。

### 2. `ClientToken`

它负责表达：

- “这一次写请求的幂等键”

它应该：

- 在同一次真实写请求的重试里保持稳定
- 但不应该跨很多天、跨删除重建周期永久写死

### 3. discovery-first 模式

它负责表达：

- “在真的写之前，先查当前是否已经存在可复用资源”

所以 `bootstrap`
的正确模式应该是：

1. 先按名字和标签查资源
2. 已存在就复用
3. 不存在才创建
4. 真正创建时再给这一次写请求分配新的 `ClientToken`

这样做的好处是：

- 保留云端写请求幂等
- 同时避免命中旧资源历史结果

## `bootstrap` 应该输出什么 inventory

`bootstrap`
不能只在终端打印一堆日志，
它还应该输出一份稳定的 inventory。

这份 inventory 至少应该包含：

- `VpcId`
- `VSwitchId`
- `SecurityGroupId`
- `EipAllocationId`
- `EipAddress`
- `PlatformInstanceId`
- `PlatformPrivateIP`
- `PlatformPublicIP`
- `WorkerInstanceId`
- `WorkerPrivateIP`
- `RegionId`
- `ZoneId`
- `GeneratedAt`

这份 inventory 的价值很大，
因为后面这些动作都要依赖它：

- 安装
- 回读
- 验收
- 重装
- 销毁

## 销毁顺序要先想清楚

`v2`
既然要做 `bootstrap`,
就不能只想创建，
必须同时把销毁顺序想清楚。

根据前面真实云资源释放实验的经验，
这里要提前接受一个事实：

- 删资源不是“反向按一条命令删完就没事”
- 很多时候还要等依赖真正释放

当前最小删除顺序建议先理解成：

1. 删除 worker `ECS`
2. 删除 platform `ECS`
3. 解绑并释放 `EIP`
4. 删除安全组
5. 删除 `vSwitch`
6. 如有必要，先关闭 `VPC` 上的相关属性
7. 删除 `VPC`

如果中间遇到：

- `DependencyViolation`
- `DependencyViolation.NetworkInterface`

这一类错误，
不应该立刻把它当成逻辑错，
而应该先理解成：

- 云端底层依赖还没释放干净

也就是说，
`destroy`
也必须带：

- 等待
- 重试
- 再回读确认

## 做完这一章后，后面三章分别在干什么

看到这里，
后面的 `03`、`04`、`05`
其实就已经很清楚了：

- `03`
  - 先按这里定义的命名、标签和幂等规则
    - 把共享网络底座真正创建出来
  - 也就是：
    - `VPC`
    - `vSwitch`
    - `SecurityGroup`
    - 安全组规则
- `04`
  - 再拿着 `03` 产出的 inventory
    - 去准备 platform `ECS`
    - 绑定 `EIP`
    - 真正把平台装到机器上
- `05`
  - 再把 first worker 创建出来、纳管进平台，并建立最小平台拓扑

所以这一章的真正作用不是“介绍几种资源”，
而是：

- 把 `v2 bootstrap`
  - 的边界、输入和目标资源图真正冻结下来

## 本章检查点

- 提交：
  - `fb327ee6874616360c5aa6e521c1522a2df518be`
- 状态：
  - `bootstrap` 的输入字段、命名约定、资源边界和目标资源图已经固定下来
