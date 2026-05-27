# 07 腾讯云 Terraform Platform Module

这一章先不碰：

- control-plane 自动安装
- provider 绑定落盘
- runtime worker 动态创建

而是先把问题收窄成一件事：

- `05`
  收出来的单 provider bootstrap contract
- 能不能先在腾讯云这边落成一套干净的 Terraform 平台底座

也就是说，
`07`
要证明的不是：

- 腾讯云 runtime 已经跑通

而是：

- 第二家 provider 的“底座层”
  已经能接进现有结构
- 并且不需要把 `05`
  的共享层再拆一次

## 上一章检查点

- `v3/06`
  - `e7c4f62d798282535f67ea67b5887200a24b07af`

## 这一章的官方依据

这一章的设计与真实验证主要基于下面这些官方资料：

- 腾讯云 Terraform Provider 配置：
  - https://cloud.tencent.com/document/product/1653/82873
- 腾讯云 Terraform 本地使用与环境变量鉴权：
  - https://cloud.tencent.com/document/product/1653/82868
- 腾讯云地域、可用区与资源位置说明：
  - https://cloud.tencent.com/document/product/213/6091
- 腾讯云子网创建与跨可用区语义：
  - https://cloud.tencent.com/document/product/215/15782
  - https://cloud.tencent.com/document/product/215/36517
- 腾讯云安全组接口语义：
  - https://cloud.tencent.com/document/product/215/43279
- 腾讯云 EIP 绑定语义：
  - https://cloud.tencent.com/document/product/215/16700
- 腾讯云实例角色说明：
  - https://cloud.tencent.com/document/product/213/47668
- 腾讯云 Linux 自定义数据说明：
  - https://cloud.tencent.com/document/product/213/17525
- 腾讯云云服务器预设策略与权限配置：
  - https://cloud.tencent.com/document/product/213/58790
- 腾讯云标签访问管理支持的授权粒度：
  - https://cloud.tencent.com/document/product/598/67492
- 腾讯云访问管理预设策略说明：
  - https://cloud.tencent.com/document/product/598/11107
- 腾讯云 Terraform Provider 官方仓库中的资源文档：
  - `tencentcloud_instance`
    - https://raw.githubusercontent.com/tencentcloudstack/terraform-provider-tencentcloud/master/website/docs/r/instance.html.markdown
  - `tencentcloud_eip_association`
    - https://raw.githubusercontent.com/tencentcloudstack/terraform-provider-tencentcloud/master/website/docs/r/eip_association.html.markdown
  - `tencentcloud_security_group_rule_set`
    - https://raw.githubusercontent.com/tencentcloudstack/terraform-provider-tencentcloud/master/website/docs/r/security_group_rule_set.html.markdown

## 先把几个腾讯云特性看清楚

如果不先把这些特性看清楚，
后面很容易把阿里云的心智模型直接硬套过来。

### 1. 腾讯云 Terraform 的正式入口就是 Provider + 环境变量

腾讯云官方 Terraform 文档明确给了：

- `source = "tencentcloudstack/tencentcloud"`
- `provider "tencentcloud" { region = "ap-guangzhou" }`

以及环境变量鉴权：

- `TENCENTCLOUD_SECRET_ID`
- `TENCENTCLOUD_SECRET_KEY`

所以 `07`
这里不再设计任何：

- `Go bootstrap CLI`
- “外面再包一层 Terraform”

而是和 `05`
的边界保持一致：

- 直接 `terraform init / plan / apply / destroy`

### 2. 腾讯云资源的“地域/可用区”边界和阿里云不完全一样

腾讯云官方文档里这几个点很关键：

- `SSH 密钥`
  - 全地域可用
- `CVM 实例`
  - 单可用区可用
- `EIP`
  - 单地域多可用区可用
- `安全组`
  - 单地域多可用区可用
- `VPC`
  - 单地域多可用区可用
- `子网`
  - 单可用区可用

这意味着 `07`
的输入模型应该是：

- 显式输入 `region_id`
- 显式输入 `zone_id`
- 平台主机落到这个 `zone`
- 子网也落到这个 `zone`
- `VPC / 安全组 / EIP`
  只需要跟着 `region`

而不是把所有资源都当成“同一层级的 zonal 资源”。

### 3. 同一 VPC 下可以有跨可用区子网，而且默认内网互通

腾讯云子网文档明确写了：

- 子网具有可用区属性
- 同一私有网络下可以创建不同可用区的子网
- 同一私有网络下不同可用区的子网默认可以内网互通

这对后面的 `08`
和 `09`
很重要，
因为它说明腾讯云这条线也天然支持：

- 先一个 control-plane 子网
- 后面再加 worker 子网
- 甚至跨可用区扩 worker

但 `07`
先不把问题做大，
这里只做：

- 一个 VPC
- 一个子网
- 一台 platform CVM

### 4. 腾讯云 EIP 应该和实例解耦

腾讯云 EIP API 文档写得很清楚：

- `AssociateAddress`
  是“把 EIP 绑定到实例或网卡”
- 绑定到 CVM 时，
  本质上是绑到主网卡主内网 IP

而官方 provider 文档也明确提示：

- 如果用 `tencentcloud_eip_association`
- 就不要在 `tencentcloud_instance`
  里再声明 `allocate_public_ip`

所以 `07`
这里不走：

- “创建实例时顺手分配公网 IP”

而是明确拆成两步：

1. `tencentcloud_instance`
2. `tencentcloud_eip`
3. `tencentcloud_eip_association`

这样更符合腾讯云自己的资源模型，
也更适合后面做销毁、替换和漂移排查。

### 5. 安全组规则不能继续用旧的 lite_rule

腾讯云 provider 官方文档里已经把：

- `tencentcloud_security_group_lite_rule`

标记为：

- deprecated

并明确建议改用：

- `tencentcloud_security_group_rule_set`

所以 `07`
不会再学旧资源，
直接用新的：

- `tencentcloud_security_group`
- `tencentcloud_security_group_rule_set`

而且 `rule_set`
文档还特别强调：

- 一个安全组只能由这一份 rule set 独占管理

这正好符合我们现在的模块设计习惯：

- `network-base`
  自己负责创建并完整管理自己的安全组规则

### 6. 腾讯云自定义数据只在首次启动执行，而且必须 Base64

腾讯云 Linux 自定义数据文档明确写了：

- 仅限首次启动云服务器时执行
- 文本必须经过 Base64 编码
- 启动时执行这些任务会增加实例启动时间

provider 文档也对应提供了：

- `user_data`
  - Base64 编码
- `user_data_raw`
  - 明文
- `user_data_replace_on_change`

这意味着 `07`
这里有两个设计约束：

1. 现在先不把 control-plane 安装脚本塞进来
2. `platform-host` 模块要预留 `user_data` 输入，
   但本章默认不启用

这样 `08`
再接 `cloud-init`
时，
不会推翻 `07`
的底座结构。

### 7. 腾讯云实例角色是存在的，但这一章先只预留接口

腾讯云官方文档说明：

- 腾讯云这里没有阿里云那种叫：
  - `RAM`
  的产品名
- 对应的是：
  - `CAM`
    - `Cloud Access Management`
    - 中文通常叫“访问管理”
- CVM 可以绑定 `CAM` 角色
- 实例内可以通过 `STS` 临时密钥访问其他云服务
- 实例角色要求：
  - 角色载体包含 `cvm.qcloud.com`
  - 实例网络类型必须为 VPC
  - 一台实例一次只能授予一个角色

provider 文档也给了：

- `cam_role_name`
- `key_ids`

这些字段。

在这套 root 入口里，
它们不会作为用户手填字段直接暴露出来，
而是：

- 你提供本机 `SSH` 公钥
- Terraform 自动在腾讯云导入 key pair
- Terraform 自动创建 `CVM` 可承担的 `CAM` 角色
- Terraform 自动把角色绑定到 platform `CVM`

这样做更符合统一入口的目标：

- provider 细节留在 module 内部
- root 只保留最小教学输入

## 这一章要做成什么

做完以后，
腾讯云这条线先得到一套和阿里云同构的 Terraform 目录：

```text
deploy/terraform/
  providers/
    tencent/
      modules/
        network-base/
        platform-host/
  stacks/
    lab/
```

并且：

- `terraform init`
- `terraform plan`
- `terraform apply`
- `terraform output`
- `terraform destroy`

已经可以独立创建和销毁腾讯云版平台底座。

这里的“平台底座”先只包括：

- `VPC`
- `subnet`
- `security group`
- 云侧登录 key pair
- platform `CAM` 实例角色
- platform `CVM`
- `EIP`
- `EIP association`

## 这一章明确不做什么

这一章刻意不做：

- control-plane 自动安装
- `platform-config.json` 落盘
- provider 绑定文件
- 腾讯云 runtime worker 扩容
- 腾讯云 SDK 调用
- COS 产物分发
- 真实腾讯云端到端黑盒测试

这些都留给：

- `08`
- `09`

## Terraform 结构设计

### `modules/network-base`

这个模块只负责“网络底座”。

计划包含的资源：

- `tencentcloud_vpc`
- `tencentcloud_subnet`
- `tencentcloud_security_group`
- `tencentcloud_security_group_rule_set`

#### 为什么只放这些

因为这些资源都属于：

- region / zone 相关的网络底座

并且它们和 platform host 自身生命周期相比，
复用价值更高。

以后如果：

- control-plane 主机要重建
- 实例规格要换
- 镜像要换

这层网络底座通常不应该跟着一起重建。

#### 安全组规则怎么设计

这一章先保持最小但完整的一组规则：

- 入站：
  - `22/tcp`
    - 仅允许 `allowed_admin_cidrs`
  - control-plane HTTP 端口
    - 仅允许 `allowed_admin_cidrs`
  - `80/tcp`
    - 允许 `0.0.0.0/0`
  - `443/tcp`
    - 允许 `0.0.0.0/0`
- 出站：
  - 全放通到 `0.0.0.0/0`

这里的判断是：

- `80/443`
  虽然本章暂时还没有真正服务
  但它们就是后续平台入口的保留端口
- control-plane HTTP 端口
  仍然只对管理 CIDR 开放

### `modules/platform-host`

这个模块只负责“平台主机本体”。

计划包含的资源：

- `tencentcloud_instance`
- `tencentcloud_eip`
- `tencentcloud_eip_association`

#### 为什么 EIP 也放在这里

因为对于平台主机来说：

- 公网入口
- 主机实例

是一起表达“平台宿主机”的。

虽然 `EIP`
是独立资源，
但从模块职责上看，
它更像 platform host 的一部分，
而不是公共网络底座。

#### `tencentcloud_instance` 先怎么配

本章会显式输入：

- `availability_zone`
- `image_id`
- `instance_type`
- `vpc_id`
- `subnet_id`
- `orderly_security_groups`
- `system_disk_type`
- `system_disk_size`

其中：

- `key_ids`
  - 仍然是腾讯云 provider 的正式字段
  - 但现在由 module 把“自动创建出来的 key pair id”
    组装成单元素列表再传进去

默认计费模型先用：

- `POSTPAID_BY_HOUR`
  - 这是腾讯云 `CVM` 的按量付费模式名
  - 不是“固定买 1 小时”
  - 更准确地说是：
    - 按秒计费
    - 按小时结算
    - 随时创建、随时释放

因为腾讯云 `RunInstances`
官方文档里这就是默认值，
而且最适合教学实验的可回收模式。

#### `user_data` 怎么处理

`platform-host`
模块从这一章开始就预留：

- `user_data`
- `user_data_replace_on_change`

但 `07`
默认不写安装脚本。

也就是说：

- 这一章先证明 Terraform 底座结构成立
- `08`
再把真正的 `cloud-init`
装机逻辑塞进来

#### `cam_role_name` 在这里怎么来

这里的：

- `cam_role_name`

直接由 `network-base`
里自动创建一份可被 `CVM` 承担的 `CAM` 角色，
再把角色名输出给 `platform-host`。

也就是说：

- root 不暴露这个 provider 细节
- module 内部仍然沿用腾讯云 provider 的正式字段
- `08`
  讲的重点会变成：
  - control-plane 怎样消费这份实例角色

### `stacks/lab`

这个 root stack 负责：

- provider 声明
- region 选择
- module 装配
- outputs
- `terraform.tfvars.example`
- `.terraform.lock.hcl`
- 本地 state 忽略规则

这一层继续坚持和阿里云一样的原则：

- 显式输入
- 不自动猜
- 不在 Go 代码里做“帮你选一个”

## 这一章的输入合同

`07`
我建议输入先收成下面这组。

### 共享输入

- `platform_name`
- `environment`
- `owner`
- `ssh_public_key_path`
- `control_plane_http_port`
- `gateway_http_port`
- `gateway_https_port`

### 腾讯云网络输入

- `region_id`
- `zone_id`
- `vpc_cidr_block`
- `subnet_cidr_block`
- `allowed_admin_cidrs`

### 腾讯云主机输入

- `instance_type`
- `image_id`
- `system_disk_type`
- `system_disk_size_gib`

### 腾讯云公网输入

- `eip_internet_charge_type`
- `eip_internet_max_bandwidth_out`

### 为 `08` 预留但本章先不用的输入

- `user_data`
- `user_data_replace_on_change`

## 开工前，先用 tccli 把真实输入摸清楚

这一章很适合先用：

- `tccli`

把当前账号、当前地域里真实可用的：

- 可用区
- 实例规格
- 公共镜像

先查出来。

这是因为腾讯云这几类数据都不是“看教程猜一个就一定可用”：

- 不同地域不一样
- 不同时间可能变化
- 账号状态、售卖状态、资源上下架也会影响结果

下面这些命令和输出，
是我在：

- `2026-04-13`
- `ap-beijing`

对你当前账号实际查询到的结果。

### 1. 先看 CLI 版本

命令：

```bash
tccli --version
```

输出：

```text
3.1.71.1
```

### 2. 看北京地域当前真实可用区

命令：

```bash
tccli cvm DescribeZones --region ap-beijing
```

输出：

```json
{
  "TotalCount": 4,
  "ZoneSet": [
    {
      "Zone": "ap-beijing-3",
      "ZoneName": "北京三区",
      "ZoneId": "800003",
      "ZoneState": "AVAILABLE"
    },
    {
      "Zone": "ap-beijing-6",
      "ZoneName": "北京六区",
      "ZoneId": "800006",
      "ZoneState": "AVAILABLE"
    },
    {
      "Zone": "ap-beijing-7",
      "ZoneName": "北京七区",
      "ZoneId": "800007",
      "ZoneState": "AVAILABLE"
    },
    {
      "Zone": "ap-beijing-8",
      "ZoneName": "北京八区",
      "ZoneId": "800008",
      "ZoneState": "AVAILABLE"
    }
  ]
}
```

这里最值得注意的点是：

- 官方总文档会告诉你“北京有哪几个可用区”
- 但真正给 Terraform 填值前，
  更应该信你当前账号实际 API 返回的：
  - `AVAILABLE`

对 `07`
来说，
这意味着目前最自然的候选值就是：

- `zone_id = ap-beijing-6`

或者：

- `ap-beijing-3`
- `ap-beijing-7`
- `ap-beijing-8`

### 3. 看北京某个可用区里有哪些小规格实例

原始命令：

```bash
tccli cvm DescribeInstanceTypeConfigs --region ap-beijing
```

这个原始输出非常大，
所以教学里更建议先做一次筛选。

命令：

```bash
tccli cvm DescribeInstanceTypeConfigs --region ap-beijing \
  | jq '[.InstanceTypeConfigSet[]
    | select(.Zone=="ap-beijing-6" and .CPU <= 16 and .Memory <= 64)
    | {Zone, InstanceType, CPU, Memory, InstanceFamily}]
    | sort_by(.CPU, .Memory, .InstanceType)'
```

输出片段：

```json
[
  {
    "Zone": "ap-beijing-6",
    "InstanceType": "S5.SMALL1",
    "CPU": 1,
    "Memory": 1,
    "InstanceFamily": "S5"
  },
  {
    "Zone": "ap-beijing-6",
    "InstanceType": "SA2.SMALL1",
    "CPU": 1,
    "Memory": 1,
    "InstanceFamily": "SA2"
  },
  {
    "Zone": "ap-beijing-6",
    "InstanceType": "S5.SMALL2",
    "CPU": 1,
    "Memory": 2,
    "InstanceFamily": "S5"
  },
  {
    "Zone": "ap-beijing-6",
    "InstanceType": "S9.MEDIUM4",
    "CPU": 2,
    "Memory": 4,
    "InstanceFamily": "S9"
  },
  {
    "Zone": "ap-beijing-6",
    "InstanceType": "S9e.MEDIUM4",
    "CPU": 2,
    "Memory": 4,
    "InstanceFamily": "S9e"
  },
  {
    "Zone": "ap-beijing-6",
    "InstanceType": "M9.MEDIUM16",
    "CPU": 2,
    "Memory": 16,
    "InstanceFamily": "M9"
  },
  {
    "Zone": "ap-beijing-6",
    "InstanceType": "S9.LARGE8",
    "CPU": 4,
    "Memory": 8,
    "InstanceFamily": "S9"
  },
  {
    "Zone": "ap-beijing-6",
    "InstanceType": "S9.LARGE16",
    "CPU": 4,
    "Memory": 16,
    "InstanceFamily": "S9"
  },
  {
    "Zone": "ap-beijing-6",
    "InstanceType": "M9.LARGE32",
    "CPU": 4,
    "Memory": 32,
    "InstanceFamily": "M9"
  },
  {
    "Zone": "ap-beijing-6",
    "InstanceType": "S9.2XLARGE32",
    "CPU": 8,
    "Memory": 32,
    "InstanceFamily": "S9"
  }
]
```

这里要学会看三件事：

- `Zone`
  - 同一个规格不一定每个可用区都卖
- `InstanceType`
  - 这是 Terraform 里最终要填的规格字符串
- `CPU / Memory`
  - 单位分别是核和 `GiB`

对 `07`
这种“先起一台平台主机”的场景，
教学上我更倾向先从：

- `S5.MEDIUM4`
- `S9.MEDIUM4`
- `S9.LARGE8`

这类清楚、保守的小规格里选，
而不是一开始就选太大的机器。

### 4. 看当前地域里的公共镜像

原始命令：

```bash
tccli cvm DescribeImages --region ap-beijing
```

为了更适合学习，
先只截取前几项公共官方镜像：

命令：

```bash
tccli cvm DescribeImages --region ap-beijing \
  | jq '[.ImageSet[]
    | {ImageId, ImageName, OsName, ImageType, ImageSource}]
    | .[:20]'
```

输出片段：

```json
[
  {
    "ImageId": "img-6n21msk1",
    "ImageName": "TencentOS Server 4 for x86_64",
    "OsName": "TencentOS Server 4 for x86_64",
    "ImageType": "PUBLIC_IMAGE",
    "ImageSource": "OFFICIAL"
  },
  {
    "ImageId": "img-7rqxtnh9",
    "ImageName": "TencentOS Server 3.3 (TK4)",
    "OsName": "TencentOS Server 3.3 (TK4)",
    "ImageType": "PUBLIC_IMAGE",
    "ImageSource": "OFFICIAL"
  },
  {
    "ImageId": "img-j5e5hadz",
    "ImageName": "OpenCloudOS Server 9",
    "OsName": "OpenCloudOS Server 9",
    "ImageType": "PUBLIC_IMAGE",
    "ImageSource": "OFFICIAL"
  },
  {
    "ImageId": "img-028qly2h",
    "ImageName": "OpenCloudOS Server 8",
    "OsName": "OpenCloudOS Server 8",
    "ImageType": "PUBLIC_IMAGE",
    "ImageSource": "OFFICIAL"
  }
]
```

这里最值得注意的现象是：

- 我这次直接按：
  - `Ubuntu`
  去过滤
- 当前返回是空集

也就是说，
在真正写 `terraform.tfvars`
之前，
不要先脑补“北京一定有 Ubuntu 公共镜像可以直接拿来用”，
而应该先以当前 API 返回为准。

对 `07`
来说，
更稳妥的起步方式是先选一张：

- `PUBLIC_IMAGE`
- `OFFICIAL`

的腾讯云官方 Linux 镜像，
例如：

- `img-6n21msk1`
  - `TencentOS Server 4 for x86_64`

### 5. 用 tccli 查输入时，建议的顺序

如果你后面自己重新查一遍，
我建议固定用这个顺序：

1. `DescribeZones`
   - 先确定 `zone_id`
2. `DescribeInstanceTypeConfigs`
   - 先确认“这个规格存在”
3. `DescribeZoneInstanceConfigInfos`
   - 再确认“这个 zone 里现在到底还有没有库存”
4. `DescribeImages`
   - 最后确定 `image_id`
5. 检查你本机已有哪把 `SSH` 公钥
   - 例如：
     - `~/.ssh/id_ed25519.pub`

这样做的原因是：

- 先知道 zone
- 再知道哪些规格理论上存在
- 最后确认哪些规格当前真的还能卖
- 再选一张当前地域里真实可用的镜像
- 最后确认你准备导入哪把本机 `SSH` 公钥

比上来就凭印象写：

- `zone_id`
- `instance_type`
- `image_id`
- `ssh_public_key_path`

要稳得多。

### 6. 实例规格表不等于库存表

这次真实实验里，
我就踩到了一个很典型的坑：

- `DescribeInstanceTypeConfigs`
  只能告诉你：
  - 这个规格在规格表里存在
- 但它并不能保证：
  - 你现在真正下单时还有库存

也就是说，
像：

- `S9.LARGE8`
- `S9.MEDIUM4`

虽然都能在规格查询里看到，
真实 `terraform apply`
依然可能报：

- `ResourceInsufficient.SpecifiedInstanceType`

所以腾讯云这里更有操作价值的接口其实是：

```bash
tccli cvm DescribeZoneInstanceConfigInfos --region ap-beijing \
  --cli-unfold-argument \
  --Filters.0.Values POSTPAID_BY_HOUR \
  --Filters.0.Name instance-charge-type \
  --Filters.1.Values ap-beijing-6 \
  --Filters.1.Name zone
```

为了更适合学习，
我把这次和 `07`
直接相关的几个规格摘出来：

```json
[
  {
    "InstanceType": "S5.MEDIUM4",
    "Cpu": 2,
    "Memory": 4,
    "Status": "SELL",
    "StatusCategory": "EnoughStock",
    "SoldOutReason": ""
  },
  {
    "InstanceType": "S6.MEDIUM4",
    "Cpu": 2,
    "Memory": 4,
    "Status": "SELL",
    "StatusCategory": "EnoughStock",
    "SoldOutReason": ""
  },
  {
    "InstanceType": "S9.LARGE8",
    "Cpu": 4,
    "Memory": 8,
    "Status": "SOLD_OUT",
    "StatusCategory": "WithoutStock",
    "SoldOutReason": "ResourcesSoldOut.SpecifiedInstanceType"
  },
  {
    "InstanceType": "S9.MEDIUM4",
    "Cpu": 2,
    "Memory": 4,
    "Status": "SOLD_OUT",
    "StatusCategory": "WithoutStock",
    "SoldOutReason": "ResourcesSoldOut.SpecifiedInstanceType"
  },
  {
    "InstanceType": "SA5.MEDIUM4",
    "Cpu": 2,
    "Memory": 4,
    "Status": "SELL",
    "StatusCategory": "EnoughStock",
    "SoldOutReason": ""
  }
]
```

这里最值得记住的点是：

- `Status = SELL`
  说明当前可售
- `StatusCategory`
  还能继续告诉你：
  - `EnoughStock`
  - `NormalStock`
  - `UnderStock`
  - `WithoutStock`

所以如果你想少踩“规格存在但下单售罄”的坑，
`DescribeZoneInstanceConfigInfos`
比单纯的规格目录更值得先看。

### 7. 这一章的 SSH 登录密钥怎么准备

命令：

```bash
tccli cvm DescribeKeyPairs --region ap-beijing
```

输出：

```json
{
  "TotalCount": 0,
  "KeyPairSet": [],
  "RequestId": "4bd9abc1-b202-45d4-963b-fecbb2d179a3"
}
```

这个现象现在更适合拿来说明：

- 你当前账号在 `ap-beijing`
  下面还没有任何现成密钥对

root 入口里不会让你手填：

- `key_ids`

你只需要提供：

- `ssh_public_key_path`

Terraform 会读取这把本机公钥，
自动在腾讯云导入 key pair，
再把生成出来的 key id 传给：

- `tencentcloud_instance.key_ids`

所以这里的“密钥”
指的是：

- 用于 `SSH` 登录实例的公钥 / 私钥对

它不是：

- `CAM` 身份
- `SecretId / SecretKey`
- control-plane 的业务 token

## 跑这一章前，子账户至少要有什么 CAM 权限

这一章只做腾讯云平台底座，
不做：

- control-plane 安装
- COS 制品分发
- runtime worker 动态创建
- CAM 角色创建和绑定

所以这里需要的权限，
先只围绕这些资源：

- `VPC`
- `subnet`
- `security group`
- `CVM`
- `EIP`

### 最稳妥的授权方式

如果你的目标是：

- 先把 `07` 跑通
- 不想一开始就在自定义最小策略上卡权限

那最稳妥的官方授权组合就是：

- `QcloudCVMFullAccess`
- `QcloudVPCFullAccess`
- `QcloudTAGReadOnlyAccess`

如果后面创建计费资源时又被支付相关权限拦住，
再补：

- `QcloudCVMFinanceAccess`
- `QcloudVPCFinanceAccess`

### 为什么这里建议同时给 CVM 和 VPC 两套权限

因为 `07`
虽然表面上只是在建：

- `CVM`
- `VPC`
- `subnet`
- `security group`
- `EIP`

但腾讯云权限动作的产品边界没有你想得那么整齐。

例如腾讯云官方安全组和 `EIP`
的自定义策略示例里，
就能看到不少动作名放在：

- `cvm:*`

下面。

再加上 Terraform 自己还会调用很多：

- `Describe*`

去做资源查询、状态轮询和销毁前校验，
所以如果一开始只给其中一套权限，
很容易在执行过程中遇到：

- `AccessDenied`

### 真实 `apply` 暴露出的一个额外权限点

这不是纸上谈兵，
而是这次真实腾讯云执行里已经碰到的问题。

我在：

- `2026-04-13`
- `ap-beijing`

对这套 `07`
代码跑真实：

- `terraform plan`
  - 成功
- `terraform apply`
  - 部分成功后失败

失败时暴露出来的关键动作名是：

- `tag:DescribeResourceTagsByResourceIds`

也就是说，
即使你已经给了：

- `QcloudCVMFullAccess`
- `QcloudVPCFullAccess`

Terraform provider 在资源创建后的读回、查询或校验阶段，
仍然可能额外调用腾讯云标签服务的只读接口。

这就是为什么 `07`
这里我现在把推荐权限组合正式改成：

- `QcloudCVMFullAccess`
- `QcloudVPCFullAccess`
- `QcloudTAGReadOnlyAccess`

而不是只写前两项。

这件事在当前实验里已经验证过：

- 补齐 `QcloudTAGReadOnlyAccess`
  之后
- `terraform apply`
  就不再卡在标签读权限上了

对学习来说，
这也是一个很典型的云上真实现象：

- 你以为自己在建：
  - `VPC`
  - `subnet`
  - `security group`
  - `EIP`
  - `CVM`
- 但 Terraform provider 为了做状态回读，
  还会顺手打到：
  - 标签服务

所以“资源所属产品”和“真正需要的权限产品边界”
经常不是一回事。

### 这一章暂时不需要哪些权限

`07`
暂时不需要：

- `QcloudCamFullAccess`
- COS 相关权限
- CLS / 监控 / 告警相关权限
- 角色创建与绑定权限

因为这一章还不会做：

- `CAM` 角色创建
- `cloud-init` 中的平台安装
- 对象存储制品分发
- 腾讯云 `SDK` 动态扩容

这些会到后面的：

- `08`
- `09`

再补。

### 如果后面要收紧成最小权限，怎么做

腾讯云官方自己给的建议很实用：

- 先执行操作
- 如果因为无权限失败
- 再根据错误信息里返回的 action
  反推最小策略

也就是说，
当前教学阶段不建议一上来就手写极小策略，
而是先用：

- `QcloudCVMFullAccess`
- `QcloudVPCFullAccess`
- `QcloudTAGReadOnlyAccess`

把主线跑通。

等 `07`
和 `08`
都落稳以后，
如果你真要做更严的子账户权限，
再单独收一章“腾讯云最小 CAM 策略设计”会更自然。

## 为什么 root 入口里不是直接写 `key_ids`

腾讯云 provider 的 `tencentcloud_instance`
文档里，
推荐字段是：

- `key_ids`

而且腾讯云官方地域与可用区文档还特别说明：

- `SSH 密钥`
  是全地域可用资源

这里的分层是：

- root 输入：
  - `ssh_public_key_path`
- module 内部：
  - 自动创建 key pair
  - 再把生成出来的 id 组装成：
    - `key_ids`

也就是说，
对齐 provider 正式字段这件事没有变，
只是它不该再暴露给最终用户手填。

哪怕当前教学里通常只会导入一把，
module 内部也仍然保持：

- `key_ids`

这个形状。

## 这一章的输出合同

现在回看这一章，
root 对外公开的输出已经刻意收窄成最小集合：

- `platform_public_ip`
- `control_plane_base_url`
- `admin_token`

如果需要，
也可以把：

- `platform_provider`

当成一个轻量回读字段，
用来确认这次 root 选中的就是腾讯云。

其中：

- `admin_token`
  - 默认同样是自动生成的敏感输出
  - 不是要求你先手填在主变量文件里的固定值

而像：

- `vpc_id`
- `subnet_id`
- `security_group_id`
- `platform_private_ip`
- `platform_instance_id`

这些更底层、更 provider-specific 的信息，
现在仍然存在于模块内部和 Terraform state 里，
但不再作为统一 root 的公开输出。

这样做的原因是：

- 统一入口只需要负责“怎么进入平台”
- 不需要把所有底座细节都暴露成固定教学接口
- 以后 provider 扩展时，
  root 的公开合同也能保持更稳定

## 当前代码已经落到哪里

这一章现在已经不是停留在“设计稿”阶段了，
而是已经把腾讯云 Terraform 目录真正落出来了：

```text
deploy/terraform/
  providers/
    tencent/
      modules/
        network-base/
        platform-host/
  lab/
```

其中：

- `modules/network-base`
  - 负责 `VPC / subnet / security group / rule set`
- `modules/platform-host`
  - 负责 `CVM / EIP / EIP association`
- `lab`
  - 负责 provider、变量装配和对外 outputs

### 本地 Terraform 校验结果

我已经实际跑过：

```bash
terraform -chdir=projects/mini-cloud/deploy/terraform/lab init -backend=false -no-color
terraform -chdir=projects/mini-cloud/deploy/terraform/lab validate -no-color
```

其中当前最新一次 `validate`
结果是：

```text
Success! The configuration is valid.
```

也就是说：

- 模块目录结构已经成立
- provider 语法和资源拼装当前是可通过 Terraform 校验的

### 真实腾讯云执行记录

这一章现在已经完成了一次完整的真实云生命周期验证，
而且中间两个关键坑都已经被证实并写清楚了：

1. 第一轮真实 `apply`
   - 暴露缺少：
     - `tag:DescribeResourceTagsByResourceIds`
2. 补齐：
   - `QcloudTAGReadOnlyAccess`
   之后
   - 这个权限阻塞解除
3. 随后又碰到：
   - 某些机型虽然在规格表里存在
   - 但当前可用区实际已经售罄
4. 通过：
   - `DescribeZoneInstanceConfigInfos`
   看库存状态后
   - 把实例规格收敛到：
     - `S5.MEDIUM4`
5. 最终真实：
   - `apply`
   - `output`
   - `DescribeInstances`
   - `22/tcp` 连通性检查
   - `destroy`
   全部跑通

这次成功跑通时，
真实创建出来的资源是：

- `VPC`
  - `vpc-6g8a3ruw`
- `subnet`
  - `subnet-82bm29xx`
- `security group`
  - `sg-rrrj8gfp`
- `platform CVM`
  - `ins-m9vqagen`
- `EIP`
  - `eip-9210may1`
  - `81.70.174.172`

当时真实创建出来的关键资源和地址已经拿到，
但现在需要注意一件事：

- 这些资源 ID 仍然能通过真实云回读确认
- 只是统一 root 不再把它们全部暴露成公开 output

当前更推荐把 root 输出理解成：

- 平台公网入口地址
- 平台公网 IP
- 管理令牌

而把更细的资源 ID 回读放到：

- `terraform state`
- `tccli`
- 云控制台

当时真实跑通时，
对外最关键的访问结果可以概括成：

```text
control_plane_base_url = "http://81.70.174.172:8080"
platform_public_ip = "81.70.174.172"
platform_provider = "tencent"
```

而且这次我还额外补了两层真实回读：

- `tccli cvm DescribeInstances --region ap-beijing --cli-unfold-argument --InstanceIds ins-m9vqagen`
  - 返回：
    - `InstanceState = RUNNING`
    - `DefaultLoginPort = 22`
- 从本机执行：
  - `nc -zvw3 81.70.174.172 22`
  - 返回：
    - `Connection to 81.70.174.172 22 port [tcp/ssh] succeeded!`

这说明 `07`
承诺的第三层验证：

- CVM 已创建
- EIP 已绑定
- `22/tcp`
  从管理 CIDR 可达

在这次真实实验里已经成立。

### 成功后怎么清理

这次成功验证后，
我又立刻跑了一次完整销毁：

```bash
terraform -chdir=projects/mini-cloud/deploy/terraform/lab destroy \
  -auto-approve \
  -no-color
```

销毁结果是：

- `ins-m9vqagen`
- `eip-9210may1`
- `sg-rrrj8gfp`
- `subnet-82bm29xx`
- `vpc-6g8a3ruw`

也就是说：

- 真实验证已经做完
- 但当前没有把这批计费资源继续留在腾讯云里

另外，
前面那次因为缺少 `TAG`
读权限而半途失败时，
我也验证过：

- 如果已经落下一部分资源
- 可以用：
  - `destroy -refresh=false`
  先把残留资源从云上收回来

而在当前这次完整成功后的销毁里，
普通的：

- `terraform destroy`

就已经可以直接收干净。

现在再次执行：

```bash
terraform -chdir=projects/mini-cloud/deploy/terraform/lab state list
```

已经为空，
说明这次真实实验结束后，
Terraform 受管资源和本地状态都已经清干净了。

### 先不做自动选区、自动选镜像、自动选规格

这里我建议和阿里云 `01`
保持一致：

- 不自动帮你选
- 直接要求：
  - `region_id`
  - `zone_id`
  - `image_id`
  - `instance_type`

原因很直接：

1. 这一章重点是 provider 第二家接入
   - 不是“动态发现最优售卖资源”
2. 腾讯云和阿里云一样，
   一旦把这些选择逻辑塞进自动查询，
   教学可读性会马上下降
3. 真要做动态发现，
   也应该单独成一章

### 先不把实例角色做实

虽然腾讯云实例角色是后面平台运行时很重要的能力，
但这章我建议只做：

- 模块接口预留

不做：

- 角色创建
- 策略拼装
- 最小权限验证

这样 `07`
的边界才干净。

### 安全组直接用 `rule_set`

这一点不建议犹豫。

既然 provider 已经把：

- `security_group_lite_rule`

标记为 deprecated，
那教程就不该继续用旧资源。

## 这一章完成后怎么验

`07`
完成后，
验证目标先只收成三层：

### 第一层：Terraform 基础命令成立

- `terraform init`
- `terraform plan`
- `terraform apply`
- `terraform destroy`

### 第二层：输出合同成立

至少回读：

- `platform_public_ip`
- `control_plane_base_url`
- `platform_provider`

### 第三层：平台主机真的在腾讯云里起来了

至少确认：

- CVM 已创建
- EIP 已绑定
- `22/tcp`
  从管理 CIDR 可达

但这章先不要求：

- control-plane 进程已经启动
- API 已健康

那是 `08`
的责任。

## 这一章做完后，08 自然要接什么

如果 `07`
按这套设计落完，
那 `08`
就会很自然地只接下面几件事：

1. 在 `platform-host` 模块里启用真正的 `user_data`
2. 通过 `cloud-init`
   安装：
   - Docker
   - Postgres
   - control-plane
3. 创建并绑定用于平台运行时的 `CAM` 角色
4. 生成并落盘：
   - `platform-config.json`
5. 把 provider 名切成：
   - `tencent`
6. 再往后接腾讯云 runtime scale-out

也就是说，
`07`
如果结构对了，
`08`
就不会再去碰底座分层，
而只是在这个底座上补“平台真的装起来”。

## 本章检查点

- `v3/07`
  - `7bdae26df08d009648c31e5eac13272d9c9f357a`
