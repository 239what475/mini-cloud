# 06 真实阿里云端到端验收

这一章不再继续加新能力，
而是把 `05` 重构后的主链重新放回真实阿里云里，
做一次完整闭环验收。

也就是说，
`06`
要回答的问题不是：

- “还能不能再写一个新功能”

而是：

- `05`
  做完大重构以后
- 阿里云这条单 provider 主链
  是否依然真的成立

## 上一章检查点

- `v3/05`
  - `87dde76ea388b6c15c174269b33e55af6d8bd056`

这章真正验收的是下面这条链：

1. 本地准备真实阿里云参数和凭证
2. `Terraform apply`
   建出平台底座
3. `cloud-init`
   把 control-plane 装起来
4. 平台 API 可用
5. 创建应用并生成首个 revision
6. 运行时自动创建 worker
7. worker 自动注册回来
8. 应用成功进入：
   - `running`
9. 平台主动 teardown
10. runtime worker 被平台回收
11. `Terraform destroy`
    最终把底座销毁

## 为什么这一章改成独立 Go 程序

这里没有继续用：

- `go test`

而是改成了一套单独的可执行测试项目。

原因很直接：

1. 这是真实云资源实验
   - 不是纯本地无副作用测试
2. 整体流程比较长
   - `terraform apply`
   - 远端 `cloud-init`
   - 镜像拉取
   都可能要很多分钟
3. 这里更需要“顺序日志 + 明确步骤”
   - 而不是测试框架输出
4. 真实实验入口应该一眼就能看出来

所以现在的真实验收入口收成了：

- [tests/main.go](../../tests/main.go)

对应目录结构是：

- [tests](../../tests)
  - 独立的真实环境黑盒测试项目
- [tests/aliyun/config.go](../../tests/aliyun/config.go)
  - 读取场景配置
- [tests/aliyun/credentials.go](../../tests/aliyun/credentials.go)
  - 读取 `~/.aliyun/config.json`
- [tests/aliyun/prepare.go](../../tests/aliyun/prepare.go)
  - 准备二进制、`OSS`、`Terraform apply`
- [tests/aliyun/run.go](../../tests/aliyun/run.go)
  - 串起整条端到端流程

## 当前配置入口

这套真实测试现在分成两类输入。

### 1. 场景配置

场景配置放在：

- [tests/aliyun/config.json.example](../../tests/aliyun/config.json.example)

本地实际使用时复制成：

- `projects/mini-cloud/tests/aliyun/config.json`

这里主要放的是：

- `bucketName`
- `regionId`
- `zoneId`
- `imageId`
- `keyPairName`
- `instanceType`
- `runtimeWorkerInstanceType`
- `dockerRegistryMirror`

也就是说：

- 哪个地域
- 哪个可用区
- 用什么镜像
- 用什么实例规格
- 把二进制传到哪个 `OSS bucket`

这些是场景输入。

### 2. 阿里云凭证

凭证不再单独写进项目配置里，
而是直接读取：

- `~/.aliyun/config.json`

当前实现会读取阿里云 CLI 当前 profile，
并使用里面的：

- `access_key_id`
- `access_key_secret`
- `security_token` / `sts_token`

也就是说，
真实实验现在复用的是你本机已经在用的阿里云 CLI 凭证。

## 真实实验现在到底跑了什么

这条链不是“只测一个 healthz”，
而是完整跑了下面这些步骤。

### 1. 自动探测当前公网 IPv4

测试程序会先探测当前本机公网 IPv4，
然后自动写成：

- `allowed_admin_cidrs = ["x.x.x.x/32"]`

对应代码在：

- [tests/aliyun/prepare.go](../../tests/aliyun/prepare.go)

这一步很重要，
因为控制面安全组不是固定对所有来源开放，
而是只放通当前测试机来源。

### 2. 本地编译 Linux 二进制

测试程序会在临时目录里构建：

- `control-plane-linux-amd64`
- `agent-linux-amd64`

也就是说，
真实实验不是依赖仓库里预先放好的旧产物，
而是拿当前工作树现编现测。

### 3. 上传二进制到 OSS 并生成临时下载链接

程序会：

1. 确认 bucket 存在
2. 上传 control-plane / agent 二进制
3. 生成带时效的签名下载链接

这两个 URL 会被写进临时生成的 Terraform var 文件，
给平台主机的 `cloud-init` 使用。

### 4. 直接跑 Terraform

现在真实实验不再通过：

- `Go CLI -> shell -> terraform`

这种旧结构间接调用，
而是测试程序直接执行：

- `terraform init`
- `terraform apply`
- `terraform output`
- `terraform destroy`

这和 `05`
要建立的边界是一致的：

- 平台底座的正式入口就是 Terraform 自己

### 5. 等 control-plane healthz 就绪

平台主机起来后，
测试程序会等待：

- `/api/healthz`

变成正常，
然后继续回读：

- `/api/v1/platform/config`
- `/api/v1/platform/gateway-config`

确认当前平台确实绑定到了：

- `aliyun`

并且读到的：

- `regionId`
- `zoneId`

和场景配置一致。

### 6. 走一遍平台 API 主链

healthz 通过以后，
测试程序会继续调用平台 API：

1. 创建 project
2. 做 usage preview
3. 创建 app
4. 因为当前没有 ready worker，等待首次 deployment 触发 scale-out

也就是说，
这已经不是“只验证安装成功”，
而是开始验证平台业务主链。

### 7. 创建应用触发运行时扩容

这一章最关键的验证点在这里。

当前场景会故意让平台一开始没有 ready worker，
然后通过创建 app 触发：

- control-plane 走阿里云 `SDK`
  创建 runtime worker

之后程序会等待：

- runtime worker inventory 变成：
  - `ready`

再继续等应用进入：

- `running`

这就把下面这条链一起验证掉了：

- `create app -> scale out -> worker register -> app running`

### 8. teardown 与 destroy 也一起验

这一章不是在 app 跑起来以后就直接结束。

测试程序还会继续：

1. 调：
   - `POST /api/v1/platform/teardown`
2. 等所有 runtime worker 进入：
   - `reclaimed`
3. 再执行：
   - `terraform destroy`

这一步很重要，
因为 `04`
和 `05`
补的关键责任边界之一就是：

- runtime worker 不在 Terraform state 里
- 所以 destroy 之前必须先由平台自己清场

这一章等于把这个设计重新在真实阿里云里证明了一次。

## 一个很重要的细节：本机公网 IP 变化

真实云实验里有一个很烦但必须处理的问题：

- 你的本机公网 IP 可能在实验过程中变化

如果：

- `apply`
  时放行的是旧 IP
- `destroy`
  时你已经换了新 IP

那最后就会出现：

- 控制面还活着
- 但测试机已经连不进去
- destroy 也可能受影响

所以当前测试程序在 destroy 前会再探测一次公网 IP。

如果发现 CIDR 变了，
它会先重写 Terraform var 文件里的：

- `allowed_admin_cidrs`

然后先补做一次：

- `terraform apply`

把安全组更新到新 IP，
再继续 destroy。

这个逻辑已经做进了：

- [tests/aliyun/prepare.go](../../tests/aliyun/prepare.go)

## 当前运行入口

现在真实阿里云验收的正式入口就是：

```bash
cd projects/mini-cloud/tests
go run .
```

它会自动完成：

- 凭证读取
- 本地公网 IP 探测
- 二进制构建
- `OSS` 上传
- `Terraform apply`
- 平台 API 验证
- create app / deployment / scale-out / running
- teardown
- `Terraform destroy`

## 当前这条真实验收覆盖了什么，没覆盖什么

当前已经覆盖的是：

- 阿里云底座创建
- control-plane 自动安装
- provider 绑定回读
- 平台 API 主链
- 单次 app 创建触发的 runtime scale-out
- worker 自动注册
- app 进入：
  - `running`
- runtime worker reclaim
- 整体 destroy 收尾

当前还没有刻意扩大的范围是：

- 一次场景里重复扩很多台 worker
- 多 region 切换
- 腾讯云 provider

也就是说，
`06`
现在证明的是：

- 阿里云单 provider 主链在 `05` 重构后仍然成立

但还不是：

- 所有 provider / 所有扩缩容组合
  都已经全面验完

## 真实运行时目前已观察到的现象

这条真实实验里，
初始化控制面的阶段可能会比较慢。

当前最常见的原因不是平台逻辑卡死，
而是平台主机第一次启动时要拉：

- `docker.io/library/postgres:17`

所以如果你看到：

- `terraform apply`
  已经完成
- 但 healthz 还没立刻起来

先优先怀疑：

- 远端机器还在装包、拉镜像、跑 `cloud-init`

而不是先怀疑控制面代码一定错了。

## 当前结论

这一章做完以后，
`mini-cloud v3`
在阿里云这条线上，
已经不只是：

- 代码结构被 `05` 重构干净

而且还已经在真实云里重新跑通了：

- `terraform apply`
- `cloud-init install`
- `platform api`
- `runtime scale-out`
- `teardown`
- `terraform destroy`

也就是说，
`06`
已经把：

- “重构后主链还是否成立”

这个问题回答清楚了。

## 本章检查点

- `v3/06`
  - `e7c4f62d798282535f67ea67b5887200a24b07af`
