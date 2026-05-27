# 09 真实腾讯云端到端验收

这一章不再继续补腾讯云新能力，
而是把：

- `07`
  的 Terraform 平台底座
- `08`
  的 control-plane 首装和 runtime scale-out

重新放回真实腾讯云里，
做一次完整闭环验收。

## 上一章检查点

- `v3/08`
  - `72fbb25c55c00a123c845060bb6bf6add1a6eb97`

## 这一章的官方依据

- 腾讯云 `DescribeImages`
  - https://cloud.tencent.com/document/product/213/15715
- 腾讯云 `RunInstances`
  - https://cloud.tencent.com/document/product/213/15730
- 腾讯云实例自定义数据
  - https://cloud.tencent.com/document/product/213/17525
- 腾讯云实例角色
  - https://cloud.tencent.com/document/product/213/47668
- 腾讯云 Docker 镜像加速器
  - https://cloud.tencent.com/document/product/1207/45596
- 腾讯云 `CAM`
  预设策略里，`QcloudCVMFullAccess + QcloudCVMFinanceAccess`
  才对应“创建、管理、下单支付”
  - https://cloud.tencent.com/document/product/598/11093
- 腾讯云 `QcloudCVMFinanceAccess`
  用来补：
  - `finance:*`
  - https://cloud.tencent.com/document/product/457/43416

## 这一章要验收的主链

这章真正要证明的是：

1. 本地读取 `tccli` 凭证
2. 自动准备 `COS bucket` 上传二进制
3. `Terraform apply`
   建出腾讯云平台底座
4. platform host 首启通过 `cloud-init`
   装起：
   - `Postgres`
   - `control-plane`
   - `agent`
5. 平台 API 可用
6. 创建应用并生成首个 revision
7. 平台通过腾讯云 `SDK`
   自动创建 runtime worker
8. worker 自动注册回来
9. 应用进入：
   - `running`
10. 平台 teardown
    回收 runtime worker
11. `Terraform destroy`
    最终销毁平台底座

## 为什么这里明确切到 Ubuntu 公共镜像

上一次真实腾讯云实验失败，
不是因为：

- Terraform 目录有问题
- provider contract 有问题
- CAM 临时角色自动化有问题

而是因为当时选到的公共镜像是：

- `img-6n21msk1`
  - `TencentOS Server 4 for x86_64`

但当前腾讯云平台首装脚本和运行时 worker 首装脚本，
都明确按 Ubuntu / `apt-get` 这条链来写。

对应位置在：

- [user-data.sh.tftpl](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/terraform/providers/tencent/templates/user-data.sh.tftpl)
- [provisioner.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/provider/tencent/provisioner.go)

所以这一章先把真实实验镜像切到：

- `img-487zeit5`
  - `Ubuntu Server 22.04 LTS 64位`

这里不是去扩展多发行版兼容，
而是先把真实端到端主链收稳。

## 当前真实测试入口

真实腾讯云验收入口在：

- [main.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/tests/main.go)

执行方式是：

```bash
cd projects/mini-cloud/tests
go run . tencent
```

这里没有继续用：

- `go test`

原因和 `06`
一样：

1. 这是真实云资源实验
2. 时长长
3. 更需要顺序日志
4. 更适合作为独立黑盒入口

## 当前配置入口

### 1. 场景配置

腾讯云场景配置模板在：

- [config.json.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/tests/tencent/config.json.example)

本地实际使用的是：

- `projects/mini-cloud/tests/tencent/config.json`

当前关键字段包括：

- `regionId`
  - 例如：
    - `ap-beijing`
- `zoneId`
  - 例如：
    - `ap-beijing-6`
- `imageId`
  - 当前改成：
    - `img-487zeit5`
- `instanceType`
- `runtimeWorkerInstanceType`
- `dockerRegistryMirror`
  - 当前改成：
    - `https://mirror.ccs.tencentyun.com`
- `bucketURL`
  - 可留空，由程序自动推导
- `camRoleName`
  - 可留空，由程序自动复用或临时创建

### 2. 腾讯云凭证

凭证不写进项目配置，
而是直接读取本机：

- `~/.tccli/default.credential`
- `~/.tccli/default.configure`

对应代码在：

- [credentials.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/tests/tencent/credentials.go)

也就是说，
真实实验直接复用你已经配置好的：

- `tccli secretId / secretKey`
- 默认 region

## 这章和 08 相比，多补了什么

`08`
已经证明：

- 腾讯云底座能建
- 腾讯云版 control-plane 能装起来
- 创建应用时能触发 runtime scale-out

但 `08`
主要还是实现章。

`09`
补的是：

1. 一套真正独立的腾讯云黑盒真实验收入口
2. `COS bucket URL`
   自动推导
3. `CAM role`
   自动复用或临时创建
4. Docker 拉取默认走腾讯云官方镜像加速地址
5. 完整：
   - `apply -> 安装 -> API 验证 -> scale-out -> teardown -> destroy`
6. 销毁结束后把临时 `CAM role`
   一起清掉
7. 测试结束后把本次上传到 `COS` 的二进制对象删掉

## 自动化处理了哪两个腾讯云特有输入

### 1. `bucketURL`

如果：

- `tests/tencent/config.json`
  里没有手动写 `bucketURL`

程序会先调用：

- `tccli cam GetUserAppId`

拿到当前账号 `AppId`，
再自动拼成：

- `https://<bucket-name>.cos.<region>.myqcloud.com`

对应代码在：

- [prepare.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/tests/tencent/prepare.go)

### 2. `camRoleName`

如果：

- `tests/tencent/config.json`
  里没有手动写 `camRoleName`

程序会先尝试：

1. 枚举现有角色
2. 找是否存在唯一一个：
   - 非 `service_linked`
   - 信任主体里包含：
     - `cvm.qcloud.com`

如果找到，
就直接复用。

如果没找到，
就自动创建临时角色，
并附加：

- `QcloudCVMFullAccess`
- `QcloudCVMFinanceAccess`

测试结束后，
再自动：

- `DetachRolePolicy`
- `DeleteRole`

对应代码在：

- [cam.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/tests/tencent/cam.go)

## 当前这章的真实现象记录

### 1. 首次失败现象

第一次真实腾讯云实验里，
在 `Terraform apply`
阶段直接失败。

报错是：

```text
InvalidParameterValue.InvalidImageForGivenInstanceType
The specified image `img-moo8dyuz` is not available for the given instance type.
```

当时 platform host 用的是：

- `img-moo8dyuz`
  - `Ubuntu Server 22.04 LTS (TK4) 64位`

但它和当前规格：

- `S5.MEDIUM4`

并不兼容。

也就是说，
这次失败还没走到：

- `cloud-init`
- `healthz`
- runtime scale-out

而是在：

- “镜像和实例规格的匹配关系”

这一步就被腾讯云挡住了。

### 2. 这章的修正

这章把真实实验镜像统一切到：

- `img-487zeit5`
  - `Ubuntu Server 22.04 LTS 64位`

并且继续沿着当前已经存在的：

- `apt-get`
  首装链

把真实腾讯云主链跑通。

### 3. 第二次失败现象

切到兼容的 Ubuntu 公共镜像以后，
平台首装已经成功，
`/api/healthz`
也正常了。

但第二次真实运行又在：

- `create app -> runtime scale-out`

这里失败。

平台 API 回读到的部署失败原因是：

```text
auto scale-out failed before scheduling could continue:
RunInstances failed: sdk code=UnauthorizedOperation
message=无支付权限，无法完成支付，请开通后支付权限后再试
```

这里要特别注意：

- 这不是说 control-plane 没去调 `RunInstances`
- 而是：
  - 已经调了
  - 但被腾讯云按“无支付权限”拒绝了

根因是：

- 临时 `CAM role`
  当时只附加了：
  - `QcloudCVMFullAccess`

而腾讯云官方文档里，
“拥有 CVM 的所有权限”对应的是：

- `QcloudCVMFullAccess`
- `QcloudCVMFinanceAccess`

也就是说，
创建按量 `CVM`
时还会命中：

- `finance:*`

### 4. 最终修正

所以这章最终把自动创建的临时 `CAM role`
改成同时附加：

- `QcloudCVMFullAccess`
- `QcloudCVMFinanceAccess`

修完以后，
真实腾讯云端到端就完整通过了。

## 常用 `tccli` 参考

这章建议先用下面几个命令观察腾讯云侧资源和镜像。

### 查看公共镜像

```bash
tccli cvm DescribeImages \
  --region ap-beijing \
  --InstanceType S5.MEDIUM4 \
  --Filters '[{"Name":"platform","Values":["Ubuntu"]},{"Name":"image-type","Values":["PUBLIC_IMAGE"]}]' \
  --Limit 20
```

这次实验里，
用：

- `--InstanceType S5.MEDIUM4`

过滤后确认到兼容的 Ubuntu 公共镜像包括：

- `img-487zeit5`
  - `Ubuntu Server 22.04 LTS 64位`
- `img-mmytdhbn`
  - `Ubuntu Server 24.04 LTS 64位`
- `img-22trbn9x`
  - `Ubuntu Server 20.04 LTS 64位`

也就是说，
这一章最终没有继续用：

- `img-moo8dyuz`

而是切到了：

- `img-487zeit5`

### 查看当前账号 AppId

```bash
tccli cam GetUserAppId
```

这一步是为了自动推导：

- `bucketURL`

### 查看当前角色列表

```bash
tccli cam DescribeRoleList --Page 1 --Rp 50
```

程序会用这一步判断：

- 是否存在可直接复用的 `CVM` 可承担角色

## 真实执行结果

最终这章已经真实跑通：

```bash
cd projects/mini-cloud/tests
go run . tencent
```

关键日志可以概括成：

```text
created temporary CAM role ... with policies QcloudCVMFullAccess, QcloudCVMFinanceAccess
control-plane healthz satisfied
runtime worker ready satisfied
app running satisfied
runtime workers reclaimed satisfied
deleted temporary CAM role ...
tencent e2e passed
```

这几行分别对应：

1. 临时角色自动创建成功
2. platform host 首装成功
3. `RunInstances`
   真的建出了 runtime worker
4. worker 注册成功并开始承载应用
5. 平台 teardown 把 worker 主动回收
6. 测试结束后临时角色也被清掉

## 清理验证

这次真实测试结束后，
我又补查了腾讯云侧资源。

### 1. CVM 已清空

```bash
tccli cvm DescribeInstances --region ap-beijing --Limit 20
```

输出结果：

```json
{
  "TotalCount": 0,
  "InstanceSet": []
}
```

### 2. VPC 已清空

```bash
tccli vpc DescribeVpcs --region ap-beijing
```

输出结果：

```json
{
  "TotalCount": 0,
  "VpcSet": []
}
```

### 3. 临时角色已清掉

```bash
tccli cam DescribeRoleList --Page 1 --Rp 50
```

最终只剩腾讯云自带的：

- `Lighthouse_QCSLinkedRoleInBasic`
- `Lighthouse_QCSLinkedRoleInDnsAndSsl`

没有残留：

- `mini-cloud-e2e-role-*`

### 4. `COS` 对象现在也会自动删

这章后面又补了一次清理收口：

- 测试程序会记录本次上传到 `COS` 的对象 key
- `Destroy`
  阶段在清完平台资源和临时 `CAM role`
  之后，
  再把这些对象逐个删除

这里故意只删：

- 本次测试上传的对象

不删：

- 整个 bucket

这样能避免误删共享 bucket，
同时把持续性的 `COS` 存储费用压到最低。

## 这一章结束后的目标状态

做完以后，
腾讯云这条线应该正式具备：

- 独立真实黑盒验收入口
- Ubuntu 公共镜像首装主链
- `COS` 上传二进制
- `CAM` 临时角色自动化
- 平台 API 验证
- runtime scale-out 验证
- teardown / destroy / 临时角色清理

## 本章检查点

- `v3/09`
  - `efe78507e655a4019248f6ee2ba63c9b7bb91714`
