# 10 统一 Terraform Bootstrap 入口

这一章不再增加新的云能力，
而是把 `v3` 到目前为止已经长出来的 Terraform 入口再收一刀。

前一轮我们已经做到了：

- 阿里云可以真实创建平台底座
- 腾讯云也可以真实创建平台底座
- 两边都有真实 E2E 验收

但入口侧还留着两层冗余：

1. 目录层次还是偏深
   - `deploy/terraform/stacks/lab`
2. root 输入和输出还是有些偏胖
   - 例如旧的 `platform = { ... }`
   - 以及一批不该由统一 root 公开暴露的 provider 细节

这一章就是把这两点收掉。

## 上一章检查点

- `v3/09`
  - `efe78509dde4f1ff6c3485b0457cedda59ed3ec0`

## 这一章的目标

这一章只做三件事：

1. 统一入口目录直接落到：
   - `deploy/terraform/lab`
2. 把 root 的平台元信息拍平成：
   - `provider_name`
   - `platform_name`
   - `environment`
   - `owner`
3. 把 root outputs 缩到最小公开合同

对应文件现在是：

- [main.tf](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/terraform/lab/main.tf)
- [variables.tf](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/terraform/lab/variables.tf)
- [outputs.tf](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/terraform/lab/outputs.tf)
- [versions.tf](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/terraform/lab/versions.tf)
- [terraform.tfvars.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/terraform/lab/terraform.tfvars.example)
- [destroy-runtime-cleanup.sh](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/terraform/lab/destroy-runtime-cleanup.sh)

现在推荐的进入方式就是：

```bash
terraform -chdir=projects/mini-cloud/deploy/terraform/lab init
terraform -chdir=projects/mini-cloud/deploy/terraform/lab apply
```

## 现在统一的是什么

这里统一的是：

1. Terraform root 入口
2. root 输入变量形状
3. root 对外公开的 outputs
4. 阿里云 / 腾讯云真实 E2E 指向的 root 路径

这里没有做的事是：

- 抹平 provider 差异
- 把所有 provider 资源写成一套无差别实现

provider 差异仍然留在各自模块里：

- 阿里云：
  - [network-base](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/terraform/providers/aliyun/modules/network-base)
  - [platform-host](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/terraform/providers/aliyun/modules/platform-host)
- 腾讯云：
  - [network-base](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/terraform/providers/tencent/modules/network-base)
  - [platform-host](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/terraform/providers/tencent/modules/platform-host)

也就是说：

- 统一的是“入口”
- 不是“把 provider 差异假装成不存在”

## 为什么 `stacks/lab` 要继续收成 `lab`

用户真正要进入的地方只有一个：

- 当前教学实验真正执行 `terraform init / apply / destroy` 的 root

既然这里已经是唯一公开入口，
那继续保留：

- `stacks/lab`

这层中间目录就没有教学价值了。

它只会带来两件事：

1. 路径更长
2. 让读者误以为后面还有很多 stack 层级设计

所以这一章直接把入口收成：

- `deploy/terraform/lab`

## 为什么把 `platform = { ... }` 拍平

旧写法的问题不是“不能用”，
而是对当前这个 root 来说没有必要。

原来像这样：

```hcl
platform = {
  provider    = "aliyun"
  name        = "mini-cloud-v3-lab"
  environment = "lab"
  owner       = "student"
}
```

现在直接改成：

```hcl
provider_name = "aliyun"
platform_name = "mini-cloud-v3-lab"
environment   = "lab"
owner         = "student"
```

这里有一个 Terraform 语法细节要顺手记住：

- root input 不能直接叫 `provider`

因为这个名字在 Terraform 里是保留的，
放进 `variable "provider"` 会直接报错。

所以这轮最终选的是：

- `provider_name`

原因很直接：

1. root 元信息只有四个字段，没必要再包一层对象
2. E2E 测试写 `tfvars.json` 更直观
3. 读 `terraform.tfvars.example` 时一眼就能看懂
4. 少一层对象，就少一层“这层抽象到底值不值得”的疑问

而像：

- `control_plane_binary_url`
- `agent_binary_url`

这类发布期产物，
现在也故意不再放进主 `terraform.tfvars.example`，
而是默认交给发布脚本生成的：

- `terraform.binary.auto.tfvars.json`

这样主变量文件就只保留“你自己要维护的配置”，
不会把临时制品地址也混进去。

provider-specific 的字段仍然保留在：

- `aliyun`
- `tencent`

两个对象里。

这才是合理的分层：

- 统一 root 元信息拍平
- 真正有差异的 provider 参数留在各自块里

## 现在怎样选择 provider

核心开关现在就是：

- `provider_name`

例如阿里云：

```hcl
provider_name = "aliyun"
platform_name = "mini-cloud-v3-lab"
environment   = "lab"
owner         = "student"
ssh_public_key_path = "~/.ssh/id_ed25519.pub"

aliyun = {
  region_id     = "cn-beijing"
  zone_id       = "cn-beijing-f"
  instance_type = "ecs.u1-c1m1.large"
  image_id      = "replace-with-image-id"
}
```

例如腾讯云：

```hcl
provider_name = "tencent"
platform_name = "mini-cloud-v3-lab"
environment   = "lab"
owner         = "student"
ssh_public_key_path = "~/.ssh/id_ed25519.pub"

tencent = {
  region_id     = "ap-beijing"
  zone_id       = "ap-beijing-6"
  instance_type = "S5.MEDIUM4"
  image_id      = "img-xxxxxxxx"
}
```

这里统一收成：

- 一份 `SSH` 公钥输入
- 一套 provider 选择
- 一组最小的网络与主机参数

云侧真正的：

- key pair 资源名
- 实例角色名

都由 Terraform module 自动生成和绑定。

这更符合 `v3` 的边界：

- 一套 control-plane 只绑定一个 provider
- 但部署前只需要看一套统一入口

## 统一 root 对外现在公开什么

这一轮收口后，
root 对外只保留最小公开合同：

- `platform_provider`
- `platform_public_ip`
- `control_plane_base_url`
- `admin_token`

其中：

- `admin_token`
  - 默认自动生成
  - 需要固定值时再显式覆盖

这样做的重点是：

- root 负责给出“怎么进入平台”
- 而不是把所有 provider 底座细节都暴露出来

像这些值：

- `vpc_id`
- `subnet_id`
- `security_group_id`
- `platform_private_ip`
- `platform_instance_id`

仍然存在于 provider module / Terraform state / 云控制台里，
但不再作为统一 root 的公开教学输出。

这能明显减少两个问题：

1. root 对外接口越来越胖
2. 后面 provider 扩展时，所有章节都被迫跟着一串 provider 细节字段迁移

## destroy 前的清理钩子放在哪里

这一章没有改变 destroy 前清理 runtime worker 的语义，
只是把它也一起收进统一 root：

- [destroy-runtime-cleanup.sh](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/terraform/lab/destroy-runtime-cleanup.sh)

也就是说现在的顺序仍然是：

1. `terraform destroy`
2. destroy hook 先调用：
   - `POST /api/v1/platform/teardown`
3. control-plane 自己按 inventory 清理 runtime worker
4. Terraform 再删平台底座

只是这个钩子文件不再在 provider-specific root 下各放一份了。

## 真实测试为什么也要跟着改

如果只是 Terraform 目录挪了，
但阿里云 / 腾讯云真实测试还各自指向旧路径，
那这个“统一入口”就只是看起来统一。

所以这一章同时把两套真实 E2E 也切过来：

- [suite.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/tests/aliyun/suite.go)
- [prepare.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/tests/aliyun/prepare.go)
- [suite.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/tests/tencent/suite.go)
- [prepare.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/tests/tencent/prepare.go)

它们现在都会：

- 把 Terraform root 指到：
  - `deploy/terraform/lab`
- 写平铺后的 root 变量：
  - `provider_name`
  - `platform_name`
  - `environment`
  - `owner`

区别只剩下：

- `aliyun` 块
- `tencent` 块

这才真正说明：

- 入口统一了
- provider 差异没有被乱塞回 root 元信息层

## 发布脚本默认输出路径为什么也要改

发布二进制到 `OSS` 的脚本现在默认写到：

- [publish-control-plane-to-oss.sh](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/scripts/publish-control-plane-to-oss.sh)
  - `deploy/terraform/lab/terraform.binary.auto.tfvars.json`

这件事看起来小，
但其实很关键：

- 如果脚本默认还写旧路径，
  读者会天然以为旧目录才是正式入口

所以这种默认路径也必须一起收口。

## 当前验证方式

这一章当前至少需要两类校验。

### 1. Go 侧回归

```bash
cd projects/mini-cloud/tests
go test ./...
```

### 2. Terraform root 校验

```bash
terraform -chdir=projects/mini-cloud/deploy/terraform/lab init -backend=false -no-color
terraform -chdir=projects/mini-cloud/deploy/terraform/lab validate -no-color
```

如果要继续跑真实云黑盒验收，
入口仍然是：

```bash
cd projects/mini-cloud/tests
go run .
go run . tencent
```

只是现在它们内部都已经走统一 root 了。

## 这一章完成后的状态

这章做完以后，
`mini-cloud v3` 的 Terraform 入口应该是这个样子：

1. 对外公开入口只有一个：
   - `deploy/terraform/lab`
2. root 元信息不再多包一层 `platform = { ... }`
3. provider 选择通过：
   - `provider_name`
   表达，而不是通过目录表达
4. root 对外 outputs 缩成最小公开合同
5. 阿里云 / 腾讯云真实测试都走同一个 Terraform root

这会让后面的：

- `11`
  测试与加固
- `12`
  最终回看

建立在更干净的入口之上。

## 本章检查点

- `v3/10`
  - `f59cd87df8253481a327d5c1f3126cf13482086f`
