# 01 阿里云 Terraform Platform Module

这一章把 `v3`
真正拉回到一条更干净的主线上：

- 平台底座直接用 `Terraform`
- 不再让 `Go`
  充当一个“外面再包一层的 bootstrap CLI”

也就是说，
这一章之后，
你如果要创建平台底座，
标准入口就是：

- `terraform init`
- `terraform plan`
- `terraform apply`
- `terraform output`
- `terraform destroy`

而不是：

- `go run ./cmd/bootstrap ...`

## 为什么要这样改

前一版思路的问题很明显：

1. `Terraform` 只是“被 Go 调起来的执行器”
2. zone / 镜像 / 实例规格这些关键选择
   - 被分散在 Go 代码里
3. 真正的基础设施入口不是 `Terraform`
   - 而是一个自定义 CLI

这样做短期能跑，
但长期会越来越别扭：

- `plan/apply/destroy` 不是标准心智模型
- provider 扩展时会继续把复杂度堆到 Go 里
- drift、state、module 复用这些 Terraform 本来的优势
  也会被削弱

所以 `v3/01`
现在明确改成：

- 用标准 Terraform module 描述平台底座
- 用标准 Terraform root env 承担 provider 配置和 state

## 这一章做成什么

这一章先只解决一件完整的事：

- 在阿里云上，
  用一套标准 Terraform 工程，
  创建 `mini-cloud` 的平台底座

这里的“平台底座”包括：

- `VPC`
- `vSwitch`
- `security group`
- platform `RAM role`
- platform `ECS`
- `EIP`

这章刻意不做：

- control-plane 自动安装
- provider 绑定落盘
- worker 动态创建
- 应用触发扩容

这些留给后续章节。

## 目录结构

这一章现在回看，
Terraform 目录可以更自然地理解成两层：

- `deploy/terraform/providers/aliyun/modules/`
- `deploy/terraform/lab/`

这样比：

- `modules/...`
- `envs/...`

更直白。

### `modules`

这里放真正可复用的资源定义。

它表达的是：

- “一个阿里云版 platform base 应该长什么样”

### `lab`

这里放这次实验真正执行 `terraform` 的 root 配置：

- provider 配置
- root module
- outputs
- `terraform.tfvars.example`
- `.terraform.lock.hcl`
- 本地 state 工作目录的忽略规则

也就是说：

- `providers/aliyun/modules/`
  - 是资源模板
- `lab/`
  - 是当前教学实验真正执行 `terraform` 的地方

## 为什么这里不用“自动选 zone / 镜像 / 规格”

这里我刻意没有继续走之前那条：

- 让 Go 先去阿里云查询
- 再帮你“自动挑一个”

原因很直接：

- 这会把关键决策藏进 Go 代码
- 读者很难一眼看出：
  - 到底要建到哪个地域
  - 哪个可用区
  - 哪个实例规格
  - 哪个镜像

而 Terraform module 更适合先把这些输入显式写出来。

所以这一章的 root env
要求你明确写出：

- `provider_name`
- `platform_name`
- `aliyun.region_id`
- `aliyun.zone_id`
- `aliyun.instance_type`
- `aliyun.image_id`
- `ssh_public_key_path`
- `allowed_admin_cidrs`

这反而更适合学习。

后面如果我们要做“自动发现合适镜像”或“自动选可用区”，
也应该优先考虑：

- Terraform `data source`
- 或专门的一章单独讲

而不是先把它们混进 bootstrap 主链里。

## 这一章的输入模型

本章实验目录的本地变量文件是：

- `deploy/terraform/lab/terraform.tfvars`

仓库里提交的是：

- `deploy/terraform/lab/terraform.tfvars.example`

这符合当前仓库的统一约定：

- 需要你复制后再按本机环境修改的文件
  - 统一提交 `.example`
- 本机真实文件
  - 不进仓库

其中最值得注意的几项是：

- `provider_name`
  - 统一入口里这次选哪家云
- `platform_name`
  - 平台名字前缀
- `aliyun.region_id`
  - 平台底座落在哪个地域
- `aliyun.zone_id`
  - platform `ECS` 落在哪个可用区
- `aliyun.instance_type`
  - platform `ECS` 的规格
- `aliyun.image_id`
  - platform `ECS` 的系统镜像
- `ssh_public_key_path`
  - 本机 `SSH` 公钥文件路径
  - Terraform 会据此自动在阿里云导入一把云侧登录密钥对
- `allowed_admin_cidrs`
  - 哪些来源地址可以直接访问：
    - `22`
    - control-plane HTTP 端口
    - gateway `80/443`

### 约束是在哪里做的

这一章把两类教学约束直接写进了 module 的变量校验里：

1. 允许的地域集合
2. 允许的实例规格集合

这样如果你填了不在当前教学范围内的值，
`terraform plan`
阶段就会直接报错，
而不是等到真正创建资源时才发现。

## 这一章现在怎么用

先进入实验目录：

```bash
cd projects/mini-cloud/deploy/terraform/lab
```

复制本地变量文件：

```bash
cp ./terraform.tfvars.example ./terraform.tfvars
```

然后按你的真实环境至少改掉：

- `aliyun.image_id`
- `ssh_public_key_path`
- `allowed_admin_cidrs`

第一次初始化：

```bash
terraform init
```

这里要补一个真实实验里踩到的点：

- 阿里云 Terraform provider
  - 不能直接把 `aliyun configure list` 里那个 CLI profile 当成现成认证源来用
- 你需要提前准备 provider 能识别的认证方式
  - 例如在当前 shell 里导出：
    - `ALICLOUD_ACCESS_KEY`
    - `ALICLOUD_SECRET_KEY`

也就是说，
“本机阿里云 CLI 已经能用”
并不自动等于：

- `terraform plan`
  也一定能直接通过认证

先看计划：

```bash
terraform plan
```

确认后创建：

```bash
terraform apply
```

查看最重要的两个输出：

```bash
terraform output platform_public_ip
terraform output control_plane_base_url
```

如果你需要继续回读管理口令，
也可以显式取出：

```bash
terraform output -raw admin_token
```

这个 token 现在默认由 Terraform 自动生成。
只有你明确希望固定成某个已知值时，
才需要在 `terraform.tfvars` 里显式覆盖 `admin_token`。

回收资源时直接：

```bash
terraform destroy
```

## 为什么现在不再输出 `platform_inventory`

这轮统一入口重构以后，
root 对外只保留最小公开合同：

- `platform_public_ip`
- `control_plane_base_url`
- `admin_token`

其中：

- `admin_token`
  - 默认是自动生成的敏感输出
  - 不是要求你先手填在主变量文件里的固定值

像：

- `VPC`
- `vSwitch / subnet`
- `security group`
- 平台主机私网信息

这些 provider 细节仍然存在，
但不再作为 root 的教学主输出公开。

这样做的原因是：

- root 入口主要负责“怎么进入平台”
- provider-specific 资源细节留在 module 和 state 里
- 对外接口面更小，
  也更符合这一轮“统一入口、去掉冗余输出”的目标

## 这章真实测试修掉了什么

这章第一次真实 `apply`
并不是一次就过的，
而是现场暴露了一个很典型的阿里云细节：

- 在 `VPC` 场景里，
  `alicloud_security_group_rule.nic_type`
  不能写成：
  - `internet`
- 必须写成：
  - `intranet`

修掉这个点以后，
这一章才真正完成了：

- `plan`
- `apply`
- 读取 outputs
- `destroy`

这一轮真实测试也说明：

- 这套 module 不是只在本地 `validate` 通过
- 它已经在真实阿里云里跑过一整次生命周期

## 这一章的边界

这章结束后，
你应该建立下面这个心智模型：

1. 平台底座的入口是 `Terraform`
2. `module`
   - 负责描述“资源应该长什么样”
3. `env`
   - 负责描述“这次具体实验怎样执行”
4. 平台运行时的动态行为
   - 后面再继续由 `mini-cloud` 自己的代码负责

所以这一章回答的问题是：

- “能不能先用标准 Terraform module 把阿里云平台底座建出来”

而不是：

- “control-plane 和运行时链路都已经打通”

- “整个 `v3` 平台链路都已经打通了”

## 本章检查点

- `v3/01`
  - `9c9e062917109807f7e2e431cda8901e00b65981`
