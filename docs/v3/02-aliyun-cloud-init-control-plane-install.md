# 02 阿里云 Cloud-Init 安装 Control-Plane

这一章开始，
`mini-cloud v3`
不再把“平台主机安装”放到：

- 本地脚本 `ssh` 上去推文件
- 本地脚本再远程执行安装命令

而是改成：

- `Terraform`
  创建平台主机
- `ECS user_data`
  作为首次启动输入
- 由实例上的 `cloud-init`
  在第一次开机时自己完成安装

这才更像一个正式平台的 bootstrap 入口。

## 为什么这里要改成 cloud-init

前一阶段如果继续走：

- 本地 `Terraform` 建机
- 再本地 `ssh` 上去安装

会有两个明显问题：

1. 平台主机“创建完成”和“安装完成”是分裂的
2. 本地机器要一直在线，安装过程才继续得下去

而 `cloud-init`
的优势很直接：

- 实例第一次启动时就能自动跑初始化逻辑
- 平台安装逻辑可以直接跟着实例一起声明
- `Terraform apply`
  到“control-plane 已就绪”这条链更闭环

这里要注意一个概念：

- 这一章虽然说“用 cloud-init 安装”
- 但阿里云 Terraform 侧真正传进去的还是：
  - `user_data`

阿里云官方文档说明了两件事：

1. Linux 实例会用 `cloud-init` 处理用户数据
2. Terraform 的 `alicloud_instance.user_data`
   支持传入 `Base64` 编码内容

参考：

- ECS 用户数据：
  https://help.aliyun.com/zh/ecs/user-guide/manage-the-user-data-of-linux-instances
- Terraform `alicloud_instance`：
  https://help.aliyun.com/zh/terraform/alicloud-instance

所以这一章的实现方式是：

- `templatefile(...)`
  先渲染一个首启脚本
- `base64encode(...)`
  再把脚本喂给 `alicloud_instance.user_data`

## 这章把 Terraform 目录怎么拆

`01`
里我们还是一个：

- `platform-base/`

从这章开始，
为了把“网络底座”和“主机安装”分开，
目录改成：

- `deploy/terraform/providers/aliyun/modules/network-base/`
- `deploy/terraform/providers/aliyun/modules/platform-host/`
- `deploy/terraform/lab/`

可以这样理解：

### `network-base`

只负责平台自己的网络和身份底座：

- `VPC`
- `vSwitch`
- `security group`
- platform `RAM role`

### `platform-host`

只负责那台真正跑平台的主机：

- platform `ECS`
- `EIP`
- `user_data`

### `lab`

这里仍然是你真正执行：

- `terraform init`
- `terraform plan`
- `terraform apply`
- `terraform destroy`

的地方。

它负责把：

- `network-base`
- `platform-host`

拼成一套完整实验。

## 这一章的安装逻辑到底做了什么

平台主机第一次启动时，
`cloud-init` 会执行一段首启脚本。

这段脚本当前做的事情是：

1. 安装基础依赖
   - `docker.io`
   - `curl`
2. 配置 Docker 镜像加速
3. 下载 `control-plane` 二进制
4. 如果你提供了 `SHA256`
   - 先做完整性校验
5. 启动本地 `Postgres` 容器
6. 写入 provider binding 文件
7. 写入 control-plane 环境变量文件
8. 写入并启动：
   - `mini-cloud-control-plane.service`
9. 在本机轮询：
   - `http://127.0.0.1:8080/api/healthz`

也就是说，
这章的目标不是“把所有平台组件一次装完”，
而是先把最核心的最小闭环装起来：

- 数据库
- control-plane
- provider binding

worker / 自动扩容
留给后续章节。

## 这里为什么改成二进制下载地址

这一章的安装不是 SSH 推文件，
那就必须给实例一个“程序从哪里来”的来源。

当前这章选的是：

- 让实例自己下载现成的 `control-plane` 二进制
- 由 `cloud-init` 直接安装到：
  - `/opt/mini-cloud/bin/control-plane`

这里要区分两层输入来源：

1. 主 `lab/terraform.tfvars`
   里你通常手工维护的是：
   - 镜像、密钥、CIDR 这些平台参数
   - 如果你真想固定管理口令，
     也可以显式写 `admin_token`
2. 二进制下载地址这组输入
   - `control_plane_binary_url`
   - `control_plane_binary_sha256`
   - `agent_binary_url`
   - `agent_binary_sha256`
   默认不手填在主 `terraform.tfvars` 里，
   而是由发布脚本生成到本地 `auto tfvars`

为了不手工反复填二进制地址，
这一章现在配了一条辅助脚本：

- [publish-control-plane-to-oss.sh](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/scripts/publish-control-plane-to-oss.sh)

它会：

1. 本地编译 `linux/amd64` 的 `control-plane`
2. 上传到你指定的 `OSS object`
3. 生成本地：
   - `terraform.binary.auto.tfvars.json`

默认输出位置是：

- `deploy/terraform/lab/terraform.binary.auto.tfvars.json`

这个文件已经被：

- [lab/.gitignore](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/terraform/lab/.gitignore)

忽略，
所以它只是本地工作文件，不会进仓库。

这里最重要的是：

- `control_plane_binary_url`
  必须是平台主机第一次启动时就能直接下载到的地址
- `control_plane_binary_sha256`
  不是强制的
  但非常建议填写
  因为这样实例在首启时就能做完整性校验

否则实例首启时下载不到二进制，
`cloud-init`
就会失败。

这也是为什么我现在认为“直接下二进制”
比“下源码再云上编译”更合理：

- 不需要在云主机上安装 Go
- 不需要拉源码和依赖
- 首启更快
- 失败面更小
- 更符合正式发布链路

## provider binding 在这章里是什么意思

从 `v3`
开始，
我们明确规定：

- 一套 control-plane
- 只绑定一个 provider

这一章先把这个约束正式落成磁盘文件。

当前写入的位置是：

- `/opt/mini-cloud/provider-binding.json`

文件里的核心内容类似：

```json
{
  "provider": "aliyun",
  "regionId": "cn-beijing",
  "boundAt": "2026-04-12T08:00:00Z",
  "bootstrapSource": {
    "mechanism": "cloud-init",
    "binaryUrl": "https://example.com/mini-cloud/control-plane-linux-amd64",
    "binarySha256": "0123456789abcdef"
  }
}
```

control-plane 启动时会读取这个文件，
并通过环境变量：

- `MINICLOUD_PROVIDER_BINDING_PATH`

知道去哪里加载它。

然后 API 会把这份状态暴露出来：

- `/api/v1/platform/provider-binding`

这样你就能直接看见：

- 平台当前绑定到哪家云
- 绑定的是哪个地域
- 这份绑定是不是由 `cloud-init` 写进去的

## 这章为什么不再依赖 SSH 安装

这一章里，
SSH 的角色已经变成：

- 调试
- 查看 `cloud-init` 日志
- 查看 `systemd` 状态

而不是：

- 上传程序
- 远程执行安装脚本

也就是说，
如果一切正常，
这章的安装主线只需要：

- `terraform apply`

再加上少量回读命令确认结果。

## 这一章现在怎么用

先进入实验目录：

```bash
cd projects/mini-cloud/deploy/terraform/lab
```

准备本地变量文件：

```bash
cp ./terraform.tfvars.example ./terraform.tfvars
```

至少改掉这些值：

- `aliyun.image_id`
- `ssh_public_key_path`
- `allowed_admin_cidrs`

这里的 `admin_token`
默认会自动生成，
所以通常不需要手改。
只有你希望固定成自己指定的值时，
才需要在 `terraform.tfvars` 里显式加上它。

这里的 `ssh_public_key_path`
指的是：

- 你本机已有的 `SSH` 公钥文件

它不是：

- 阿里云 `RAM` 凭证
- `AccessKey`
- control-plane 的业务 token

Terraform 会读取这把公钥，
自动在云侧创建登录密钥对，
再把它挂到 platform `ECS` 上。

然后先发布一份控制面二进制到 `OSS`：

```bash
cd projects/mini-cloud

./scripts/publish-control-plane-to-oss.sh \
  --bucket <your-oss-bucket> \
  --region cn-beijing
```

这一步会自动生成本地文件：

- `deploy/terraform/lab/terraform.binary.auto.tfvars.json`

里面带上：

- `control_plane_binary_url`
- `control_plane_binary_sha256`

所以你不需要再把这些 URL 手工抄进 `terraform.tfvars`。

如果你愿意，
也完全可以把这份 `auto tfvars`
理解成：

- “发布脚本产出的本地工作文件”
- “专门承载二进制下载地址和校验值”

而主 `terraform.tfvars`
只保留你自己维护的平台参数。

这里的 bucket
最自然的做法是：

- 放到 `OSS`
- 用你已经准备好的私有 bucket
- 再通过脚本生成临时签名下载 URL

这一章采用的方式是：

- 先在本地发布二进制
- 实例通过下载地址拉取二进制并安装

初始化：

```bash
cd deploy/terraform/lab
terraform init
```

看计划：

```bash
terraform plan
```

确认后创建：

```bash
terraform apply
```

拿到最关键的两个输出：

```bash
terraform output control_plane_base_url
terraform output -raw admin_token
```

然后先看健康检查：

```bash
BASE_URL="$(terraform output -raw control_plane_base_url)"
curl -fsS "${BASE_URL}/api/healthz" | jq .
```

再看 provider binding：

```bash
ADMIN_TOKEN="$(terraform output -raw admin_token)"
curl -fsS \
  -H "Authorization: Bearer ${ADMIN_TOKEN}" \
  "${BASE_URL}/api/v1/platform/provider-binding" | jq .
```

如果你想确认它真的是“重启后仍然一致”，
可以重启服务再读一次：

```bash
ssh root@"$(terraform output -raw platform_public_ip)" \
  "systemctl restart mini-cloud-control-plane"

curl -fsS \
  -H "Authorization: Bearer ${ADMIN_TOKEN}" \
  "${BASE_URL}/api/v1/platform/provider-binding" | jq .
```

最后清理：

```bash
terraform destroy
```

## 这次真实实验里确认过的三个细节

### 1. 当前默认确实在用 DaoCloud 镜像加速

这章里 `docker_registry_mirror`
的默认值就是：

- `https://docker.m.daocloud.io`

真实 ECS 上也已经确认：

- `/etc/docker/daemon.json`
  里写入了这个地址
- `docker info`
  也能看到这个 mirror

这里要注意一个小点：

- 你在日志里看到的仍然可能是：
  - `docker pull docker.io/library/postgres:17`

这不表示“没有用镜像加速”。

因为：

- `docker pull`
  命令本来还是会写原始镜像名
- 真正决定是否走 mirror 的是：
  - Docker daemon 的配置

### 2. 带签名参数的 OSS URL 不能直接生塞进 shell 变量

这次真实排障里，
最先踩到的坑就是：

- `control_plane_binary_url`
  是一个带签名参数的临时下载链接
- 里面会有很多：
  - `&`

如果你在 Terraform 模板里直接把：

- `jsonencode(control_plane_binary_url)`

塞进 shell 变量，
渲染出来很可能会变成：

- `\u0026`

这样实例里的 `curl`
拿到的就不是一个真正可用的 URL，
最后会导致：

- `cloud-init`
  下载二进制失败

所以现在模板里的做法不是“直接拿 JSON 字符串当 shell 值”，
而是先经过一层 JSON 解码，
把真正的 URL 还原出来再交给：

- `curl`

### 3. `user_data` 改了，不代表实例会自动重跑 cloud-init

这一章还有一个很容易误判的点：

- `cloud-init`
  处理的是实例第一次启动时拿到的 `user_data`

所以即使你后面改了：

- `user_data` 脚本内容

老实例也不会因为 Terraform 做了一次“原地更新”就重新执行首启安装。

这也是为什么这一章现在把平台主机模块改成：

- 当 bootstrap 脚本内容变化时
- 触发 ECS 重建

只有这样，
修复后的 `cloud-init`
脚本才会真的重新跑一遍。

## cloud-init 失败时先看哪里

这一章最常见的排查顺序是：

1. 先确认实例本身已经起来
2. 进机器看 `cloud-init`
3. 再看 `systemd`
4. 最后再看 control-plane API

常用命令：

```bash
ssh root@"$(terraform output -raw platform_public_ip)" \
  "cloud-init status --wait"
```

```bash
ssh root@"$(terraform output -raw platform_public_ip)" \
  "tail -n 80 /opt/mini-cloud/cloud-init-bootstrap.log"
```

```bash
ssh root@"$(terraform output -raw platform_public_ip)" \
  "journalctl -u mini-cloud-control-plane.service --no-pager -n 80"
```

如果你怀疑是二进制地址或校验值写错了，
优先看：

- `/opt/mini-cloud/cloud-init-bootstrap.log`

里面会直接记录：

- 下载二进制
- 校验 `SHA256`
- 启动 `systemd`

这几个关键阶段。

如果你看到实例已经是：

- `Running`

但公网：

- `:8080`

还打不通，
不要立刻怀疑安全组。

先确认是不是还卡在：

- Docker 拉 `postgres:17`
- `cloud-init` 还没跑完

因为这时实例虽然已经创建成功，
但 control-plane 可能还没有真正启动。

另外别忘了：

- 脚本生成的是临时签名 URL
- 如果你过了很久才执行 `terraform apply`
- 这个 URL 可能已经过期

这时重新执行一次：

- `./scripts/publish-control-plane-to-oss.sh`

生成一份新的本地 `auto tfvars`
就可以了。

## 这章之后还没做什么

这一章故意还没做：

- worker 自动创建
- worker 自动注册
- 应用触发扩容
- 完整升级 / 重装策略

因为这章只想先把一件事做扎实：

- 一台阿里云平台主机
- 不靠 SSH 安装
- 只靠 `Terraform + cloud-init`
- 就能把 control-plane 真正拉起来
- 而且安装输入已经是“二进制制品地址”
- 不再是“源码归档 + 云上编译”

## 建议的最小验收顺序

```bash
cd projects/mini-cloud
./scripts/publish-control-plane-to-oss.sh --bucket <your-oss-bucket> --region cn-beijing
cd deploy/terraform/lab
terraform init
terraform plan
terraform apply
terraform output control_plane_base_url
terraform output -raw admin_token
curl -fsS "$(terraform output -raw control_plane_base_url)/api/healthz" | jq .
terraform destroy
```

如果你要把“provider binding 可持久化”也一起验收，
再多加一步：

```bash
ssh root@"$(terraform output -raw platform_public_ip)" \
  "systemctl restart mini-cloud-control-plane"
```

然后重新请求：

- `/api/v1/platform/provider-binding`

## 本章检查点

- `v3/02`
  - `0077d97306e295445cbcd364517c73394e1ae372`
