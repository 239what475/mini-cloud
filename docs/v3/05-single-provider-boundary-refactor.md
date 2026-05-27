# 05 单 Provider 边界重构

这一章不是继续加功能，
而是对 `mini-cloud v3` 做一次明确的 `breaking refactor`。

这里的原则很直接：

- 不保兼容
- 不继续给旧结构打补丁
- 不再保留“先兼容旧合同，后面再慢慢收”的做法
- 直接把控制面、provider、runtime、Terraform 入口重排成后续可长期演进的形态

这里也要把章节边界说清楚：

- `01-04`
  保留各自章节完成当时的历史快照
- `05`
  才是这次 `breaking refactor` 的正式落点
- 也就是说：
  - 不把这次目录和边界重排倒灌回前几章
  - 当前主线结构以这一章描述为准

## 上一章检查点

- `v3/04`
  - `706688fe4019d33ced7c687eea9b5cc2eb286b8c`

## 为什么这一章必须先做

`04` 结束以后，
阿里云单 provider 主链已经能跑，
但它还明显带着“第一家 provider 先跑起来再说”的痕迹。

最主要的问题有这几类：

1. 共享层和阿里云实现还没有真正切开
2. HTTP handler 里还混着运行时编排和回收状态机
3. control-plane 启动时还要读两份相互交叉的 provider 合同
4. Terraform 和 cloud-init 名义上在传“共享合同”，实际上仍然是阿里云形状
5. bootstrap 这条旧 Go CLI 主线还留在仓库里，和 Terraform 正式入口并存
6. runtime 层虽然已经有 Docker SDK 版本，但旧的 Docker CLI 实现还在目录里

如果现在直接去接第二家 provider，
大概率会变成：

- 到处加 `switch provider`
- 到处补默认值
- 到处保留旧字段兼容
- 到处在 HTTP 层补条件分支

这不是可扩展设计，
只是把第一家的特殊性继续向后拖。

## 本章的核心目标

这一章要把平台收成下面这套明确模型：

1. 一套 control-plane 只绑定一个 provider
2. 平台自有基础设施只允许由 `Terraform` 创建和销毁
3. 平台运行时 worker 只允许由 control-plane 通过 provider `SDK` 动态创建和回收
4. control-plane 启动时只读取一份统一的 `platform-config.json`
5. provider 差异只允许留在：
   - `internal/provider/<name>/`
   - `deploy/terraform/<name>/`
6. HTTP 层只做协议转换，不再承担业务编排
7. runtime 层只保留 Docker SDK 实现
8. `destroy` 必须先清理 runtime，再销毁平台底座

## 本章明确采用的重构原则

### 1. 不保兼容

这一章不做下面这些事：

- 不兼容旧的 `provider-binding.json`
- 不兼容旧的 `provider-runtime-profile.json`
- 不兼容旧的 `/v1` 路由前缀
- 不兼容旧的 Go bootstrap 主入口
- 不保留 Docker CLI runtime 作为后门

也就是说，
从这一章开始，
旧合同和旧入口可以直接失效。

### 2. 共享层不再解释云厂商字段

共享层只知道：

- 平台是谁
- 当前绑定的是哪一家 provider
- 平台自己的域名、端口、二进制地址
- worker 通用默认参数

共享层不再直接理解这些字段：

- `instanceType`
- `imageId`
- `vSwitchId`
- `securityGroupId`
- `systemDiskCategory`
- `systemDiskSizeGiB`

这些字段都应该只属于 provider 自己的 runtime spec。

### 3. HTTP 层必须变薄

handler 只做三件事：

1. 解析请求
2. 调用 service
3. 返回响应

下面这些流程都不再留在 handler 里：

- deployment 创建和状态推进
- scale-out 判定
- runtime worker reclaim
- platform teardown
- operation history 写入

### 4. Terraform 不是被 Go CLI 包一层的执行后端

从这一章开始，
平台底座的正式入口就是：

- `terraform init`
- `terraform plan`
- `terraform apply`
- `terraform destroy`

而不是：

- `Go CLI -> shell 调 terraform`

Go 代码不再承担“平台底座 bootstrap orchestrator”的角色。

## 本章要删除什么

这一章不是只新增目录，
还要明确删除一批已经不适合继续保留的结构。

计划删除的主要内容如下：

- 旧 bootstrap 主链
  - `cmd/bootstrap/`
  - `internal/bootstrap/`
  - `deploy/bootstrap/`
- 旧的双 provider 合同
  - `internal/platformbinding/`
  - `internal/runtimeprofile/`
- 旧的 Docker CLI runtime
  - `internal/runtime/docker_cli.go`
- `/v1` 兼容路由
- 所有“未配置时默认回退到 aliyun”这类逻辑
- 所有“从现有节点反推出 provider”这类隐式逻辑

## 重构后的目标目录结构

这一章结束时，
代码结构要朝下面这套形态收：

```text
projects/mini-cloud/
  cmd/
    agent/
    control-plane/
  internal/
    appconfig/
    platformconfig/
    provider/
      registry.go
      aliyun/
        driver.go
        runtime_spec.go
      tencent/
        driver.go
        runtime_spec.go
    runtime/
      docker/
        runtime.go
    service/
      app/
      deployment/
      node/
      platform/
      runtimeworker/
    httpapi/
      server.go
      middleware.go
      routes_apps.go
      routes_nodes.go
      routes_platform.go
      routes_projects.go
      routes_runtime.go
    store/
    ...
  deploy/
    terraform/
      aliyun/
        modules/
          network-base/
          platform-host/
        stacks/
          lab/
      tencent/
        modules/
        stacks/
  scripts/
  docs/
```

这里最重要的不是名字本身，
而是边界：

- provider 差异收进 provider 目录和对应 Terraform 目录
- service 从 `httpapi` 包中迁出
- runtime 下只保留 Docker SDK 实现

## 统一配置合同：`platform-config.json`

这一章要把现在两份交叉的 provider 合同，
合并成一份统一启动合同：

- `platform-config.json`

control-plane 启动时只读取这一份文件。

建议形状如下：

```json
{
  "platform": {
    "name": "mini-cloud-lab",
    "environment": "lab",
    "owner": "what"
  },
  "provider": {
    "name": "aliyun",
    "regionId": "cn-beijing",
    "zoneId": "cn-beijing-h"
  },
  "network": {
    "platformDomain": "console.example.com",
    "appBaseDomain": "apps.example.com",
    "redirectHttpToHttps": true
  },
  "artifacts": {
    "agentBinaryUrl": "http://10.x.x.x:8080/internal/artifacts/agent-linux-amd64",
    "agentBinarySha256": "..."
  },
  "workerDefaults": {
    "namePrefix": "mini-cloud-worker",
    "heartbeatIntervalSeconds": 15,
    "workIntervalSeconds": 5
  },
  "providerRuntimeSpec": {
    "instanceType": "ecs.u1-c1m1.large",
    "imageId": "m-xxx",
    "vSwitchId": "vsw-xxx",
    "securityGroupId": "sg-xxx",
    "systemDiskCategory": "cloud_essd",
    "systemDiskSizeGiB": 40,
    "dockerRegistryMirror": "..."
  }
}
```

这个合同要满足四条规则：

1. 共享层只理解：
   - `platform`
   - `provider`
   - `network`
   - `artifacts`
   - `workerDefaults`
2. provider-specific 字段全部留在：
   - `providerRuntimeSpec`
3. 共享层不再暴露第二份独立 binding/profile 文件
4. control-plane 如果读不到合法合同，直接启动失败

## provider 边界如何重排

这一章之后，
provider 层要清楚分成两部分：

1. 共享注册和装配
2. provider 自己的实现

共享层负责：

- 根据 `platform-config.json` 里的 `provider.name` 选择 driver
- 创建统一的 provider registry
- 在启动阶段完成一次 provider driver 初始化

provider 自己负责：

- 解释自己的 `providerRuntimeSpec`
- 创建云厂商 SDK client
- provision worker
- reclaim worker
- 生成 provider 自己的 worker bootstrap 内容

共享层不再做：

- `switch provider` 后直接 new 阿里云 client
- 根据旧 JSON 字段判断是不是阿里云
- 读取 provider-specific map 后在共享层做字段校验

## 应用装配和 service 边界如何重排

这一章之后，
要把“配置”“依赖对象”“业务编排”这三层彻底拆开。

### 1. 配置对象只保存静态配置

新的配置层只负责：

- 进程监听地址
- 数据库连接串
- UI 目录
- admin token
- `platform-config.json` 路径

配置对象不再保存：

- `WorkerProvisioner`
- `PlatformTeardown`
- 已初始化的 runtime 依赖

### 2. 应用装配层显式创建依赖

应该新增一层明确的 app builder，
负责把这些依赖组装起来：

- store
- runtime
- provider driver
- services
- HTTP server

也就是说，
`main.go`
不再自己一边 load config，
一边在启动流程里手写 provider/new service 的拼装细节。

### 3. service 从 `httpapi` 包中迁出

下面这些逻辑都应该迁到 `internal/service/` 下：

- deployment 编排
- release 提交主链
- runtime worker reclaim
- platform teardown

handler 里不再重复实现：

- `load -> check -> execute -> map error`

这种状态机。

## HTTP API 如何收口

这一章之后，
HTTP 层只保留：

- `/api/v1`

旧的：

- `/v1`

直接删除。

这样做的目的不是“追求风格统一”，
而是把一切历史兼容债直接清空，
避免后面 provider 重构期间还要同时维护双前缀。

此外，
`router.go`
也不应该继续维持一个巨大的总入口文件。

目标结构是：

- 按资源拆 route 文件
- `server.go`
  只做路由组装
- inline handler 尽量消失

## runtime 层如何收口

这一章之后，
runtime 层明确只保留 Docker SDK 实现。

也就是说：

- `internal/runtime/docker_engine.go`
  这一条继续保留并整理
- `internal/runtime/docker_cli.go`
  直接删除

原因很明确：

- Docker CLI 更适合教学实验和临时脚本
- 但不适合继续作为平台运行时主实现
- SDK 接口更稳定
- 错误处理更标准
- 后续如果接入别的 runtime，也更容易保持接口一致

## Terraform 和 cloud-init 如何重排

这一章之后，
Terraform 目录已经从平铺结构收成：

- `deploy/terraform/providers/aliyun/modules/`
- `deploy/terraform/lab/`

阿里云当前应该先落成：

- `modules/network-base`
- `modules/platform-host`
- `stacks/lab`

后面的腾讯云也按同一层次组织。

这里要特别强调一件事：

- 不再假装存在“共享 cloud-init 模板”

阿里云就是阿里云自己的模板，
腾讯云以后有腾讯云自己的模板。

这样反而更清楚，
因为：

- metadata 地址
- 实例身份获取方式
- 初始化约束

本来就是 provider-specific 的。

## destroy 路径如何定型

这一章之后，
销毁路径必须明确成下面这条严格主链：

1. `terraform destroy` 先触发 cleanup hook
2. cleanup hook 调 control-plane 的：
   - `/api/v1/platform/teardown`
3. control-plane 只根据数据库里的 runtime worker inventory 回收运行时资源
4. 所有 runtime worker 回收成功后，
   Terraform 才继续删除平台底座
5. 如果 runtime teardown 失败，
   destroy 直接失败

这里不再采用：

- “尽量继续”
- “失败了先记日志，底座照删”

这种模糊策略。

## 本章明确不做什么

这一章故意不做这些：

- 第二家 provider 真正接入并跑通
- 一个 control-plane 同时管理多家云
- control-plane 高可用
- MQ / 异步任务系统
- 新的用户功能
- 更复杂的租户权限模型

这一章的目标不是加能力，
而是为后面的能力留出干净结构。

## 本章的实施顺序

因为这是一次不保兼容的重构，
所以实施顺序也要明确。

建议严格按下面这条顺序推进：

1. 先销毁当前旧版 `v3` 的实验环境
2. 删除旧 bootstrap 主链和旧双合同
3. 引入统一的 `platform-config.json`
4. 重写 provider registry 和阿里云 driver 装配路径
5. 把 service 从 `httpapi` 包中迁出
6. 删除 `/v1` 和所有兼容回退逻辑
7. 删除 Docker CLI runtime
8. 重排 Terraform 目录和 cloud-init 模板
9. 跑本地测试
10. 跑真实阿里云端到端验收

这里故意不采用“边改边兼容”的顺序，
因为那样虽然表面更稳，
但会让旧结构在过渡期继续污染新设计。

## 本章的最终验收标准

这一章做完时，
至少要满足下面这些条件：

1. control-plane 只读取：
   - `platform-config.json`
2. 启动路径里已经没有：
   - `platformbinding`
   - `runtimeprofile`
3. handler 不再重复实现 runtime reclaim / teardown 状态机
4. `/v1` 路由已经完全消失
5. runtime 目录里已经没有 Docker CLI 主实现
6. Terraform 阿里云目录已经改成：
   - `modules + stacks`
7. `terraform destroy`
   能先触发平台 runtime teardown，
   再销毁底座
8. `go test ./...`
   通过
9. `go vet ./...`
   通过
10. 真实阿里云主链重新验收通过

## 本章涉及的核心文件范围

本章实际重点改动了这些区域：

- `cmd/control-plane/`
- `internal/config/`
- `internal/httpapi/`
- `internal/provider/`
- `internal/runtime/`
- `internal/service/`
- `internal/platformconfig/`
- `deploy/terraform/providers/`
- `deploy/terraform/lab/`
- `scripts/smoke.sh`
- `scripts/disaster-drill.sh`
- `scripts/simulated-env.sh`
- `scripts/backup-platform.sh`

已经删除或替换的旧区域：

- `cmd/bootstrap/`
- `internal/bootstrap/`
- `deploy/bootstrap/`
- `internal/platformbinding/`
- `internal/runtimeprofile/`
- `internal/runtime/docker_cli.go`

## 当前落地进度

这一章现在已经落下来的部分是：

- control-plane 只读取：
  - `platform-config.json`
- 旧双合同已经删除：
  - `platformbinding`
  - `runtimeprofile`
- provider 装配已经收成：
  - `internal/provider/`
  - `internal/provider/aliyun/`
  - `internal/provider/local/`
- 本地验证不再依赖云凭据，
  而是显式走：
  - `local provider`
- deployment 编排和 runtime worker reclaim / teardown
  已经迁到：
  - `internal/service/deployment/`
  - `internal/service/runtimeworker/`
- `httpapi`
  里已经不再保留这两块状态机实现
- Docker CLI runtime 已经删除，
  只保留 Docker SDK 路径
- 旧 bootstrap 主线已经删除
- 本地脚本已经统一改成读取：
  - `platform-config.json`

这一章当前已经补齐的部分还有：

- Terraform 阿里云目录已经改成：
  - `modules/`
  - `lab/`
  - `lab/main.tf`
    现在显式引用：
    - `../../modules/network-base`
    - `../../modules/platform-host`
- 发布脚本默认写入的本地工作文件路径
  也已经切到：
  - `deploy/terraform/lab/terraform.binary.auto.tfvars.json`

这一章还待继续的部分是：

- 这次破坏式重构后的真实阿里云主链重新验收

## 本章检查点

- `v3/05`
  - `87dde76ea388b6c15c174269b33e55af6d8bd056`
