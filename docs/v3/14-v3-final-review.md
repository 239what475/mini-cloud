# 14 v3 Final Review

到这一章，
`mini-cloud v3`
就不再继续往前补新功能了。

这一章要做的事情只有四件：

- 回看 `v3`
  到底真正做成了什么
- 明确 `v3`
  现在的边界是什么
- 说明这些结论不是纸面设计，
  而是已经经过哪些真实验证
- 给 `v4`
  留下一个更清楚的起点

如果不做这一步，
前面的十几章很容易在脑子里变成：

- 阿里云一段
- 腾讯云一段
- Terraform 一段
- 测试一段

但 `v3`
真正的价值，
不是这些散点能力，
而是它已经把它们收成了一条完整的平台线。

## 上一章检查点

- `v3/13`
  - `4c02bba0b3231718f3e77804b77d41c0cc2b7647`

## 先用一句话总结 v3

如果说：

- `v2`
  解决的是：
  - “平台能不能先把自己装到单一云上，并在真实环境里跑起来”

那么：

- `v3`
  解决的就是：
  - “平台能不能把 provider 边界正式收口，并同时支持阿里云和腾讯云这两条单 provider 主线”

所以 `v3`
最核心的关键词不是：

- 多云统一调度
- 多 control-plane 高可用
- 很重的产品面

而是：

- 单 control-plane 只绑定一个 provider
- 统一 Terraform bootstrap 入口
- 统一 provider 抽象
- 两家云厂商的真实端到端闭环
- mini-cloud 自己的 Terraform provider

## v3 最终做成了什么

现在可以把 `v3`
真正做成的东西分成五块看。

### 1. 平台底座的 Terraform 入口已经统一

从 `10`
开始，
平台底座对外正式入口统一成：

- `deploy/terraform/lab`

切换阿里云还是腾讯云，
不再靠：

- 进不同目录
- 跑不同脚本

而是靠同一个 root 里的：

- `provider_name`

来表达。

也就是说，
`v3`
结束时，
平台底座这层已经稳定成：

- 同一套 Terraform root
- 不同 provider 配置
- 同一套 output 合同

这让：

- Terraform apply
- Terraform destroy

真正变成同一种入口，
而不是“表面都叫 Terraform，
但底层组织方式完全不同”。

### 2. control-plane 的 provider 边界已经正式成立

`05`
做的最关键事情，
不是再补一个云功能，
而是把控制面和云厂商 SDK 的边界真正切开。

到 `v3`
结束时，
control-plane 已经不是直接散着写：

- 阿里云逻辑一套
- 腾讯云逻辑一套

而是已经有了更明确的：

- provider 接口
- runtime worker provision / reclaim
- 平台 teardown
- provider 绑定校验

这意味着：

- 平台主逻辑
  不再直接依赖某一家云的实现细节
- 阿里云 / 腾讯云
  都能在同一套平台语义里成立

但这里也要明确：

- `v3`
  还不是“一个 control-plane 同时调度多家云”
- 它是：
  - 一个 control-plane
  - 启动时选定一家 provider
  - 然后整个平台都只绑定这一家 provider

这正是 `v3`
故意保留的边界。

### 3. 两条真实单 provider 主线都已经跑通

`v3`
最重要的成果之一，
不是“代码看起来支持两家云”，
而是：

- 阿里云真实端到端跑通了
- 腾讯云真实端到端也跑通了

而且两边都不是只验证：

- 首次安装

而是验证了完整平台主链：

- Terraform bootstrap
- cloud-init 首装
- control-plane 启动
- API 可用
- 创建 app
- 触发 runtime worker 申请
- worker 自动注册
- app 进入 running
- 平台 teardown
- Terraform destroy

这让 `v3`
已经不再只是：

- “抽象设计成立”

而是：

- “两家真实云都能各自作为一个单 provider 后端把整条平台链路跑完”

### 4. mini-cloud 自己的 Terraform provider 已经落地

`11`
和 `13`
连起来，
把这件事情真正做实了：

- 先把平台资源 API 收成 Terraform 友好的形状
- 再实现真正的 provider 插件

到 `v3`
结束时，
mini-cloud 已经可以被 Terraform 直接加载，
而不是再包一层：

- shell
- Go CLI
- 假入口

当前第一版 provider 已经明确支持：

- `minicloud_service`

这里的边界也很清楚：

- Terraform 直接管理：
  - `service`
- 服务端自动生成并回读：
  - `project`
  - `revision`
  - `deployment`
  - `execution`

这个边界很重要，
因为它说明：

- `v3`
  已经不再把“过程对象”误当成“声明式资源”

### 5. 测试入口已经分层，不再全靠手工实验

`12`
把这件事情收得比较清楚。

到 `v3`
结束时，
验证至少已经分成下面几层：

1. 本地日常检查
   - `bash ./scripts/check.sh`
2. 本地集成
   - `bash ./scripts/test-integration.sh`
3. 本地主链 smoke
   - `bash ./scripts/smoke.sh`
4. 真实阿里云黑盒
   - `cd tests && go run .`
5. 真实腾讯云黑盒
   - `cd tests && go run . tencent`
6. Terraform provider 黑盒
   - `go test ./internal/terraformprovider/... ./cmd/terraform-provider-mini-cloud`

这说明：

- `v3`
  虽然还没有把所有测试统一编排成一个超重总入口
- 但也已经不是“只能手动点几条命令碰碰运气”

## 现在可以怎样理解 v3 的整体结构

如果把 `v3`
压缩成最值得记住的一张脑图，
可以直接理解成三层。

### 第一层：平台底座层

负责：

- 建平台自己的云资源
- 首次装起 control-plane
- 销毁平台底座

对应入口是：

- `deploy/terraform/lab`

### 第二层：平台运行时层

负责：

- 接收 `project` / `app`
  这类平台资源
- 按当前绑定的 provider
  去申请 / 回收 runtime worker
- 推进：
  - `revision`
  - `deployment`
  - `execution`

对应代码主线在：

- `internal/provider/*`
- `internal/httpapi/*`
- `internal/runtimeworker/*`
- `internal/service/*`

### 第三层：平台资源 Terraform 层

负责：

- 让外部用户用 Terraform 直接管理平台资源

对应入口是：

- `cmd/terraform-provider-mini-cloud`
- `internal/terraformprovider/*`

这三层现在已经各自有清楚的边界：

- 平台底座层
  不负责管理 app
- 平台运行时层
  不负责自己创建 Terraform root
- 平台资源 Terraform 层
  不直接碰云厂商 SDK

这就是 `v3`
最大的架构收获之一。

## v3 明确没有做什么

回看版本时，
不仅要说做成了什么，
也要说故意没做什么。

### 1. 还没有做“一个 control-plane 同时管理多家云”

现在支持的是：

- 阿里云单 provider 模式
- 腾讯云单 provider 模式

不是：

- 同一个 control-plane
  同时把 workload 下到阿里云和腾讯云

这件事没有做，
不是漏掉了，
而是故意留到更后面的版本。

### 2. 还没有做 control-plane 高可用

当前仍然是：

- 单 control-plane
- 单数据库

这已经足够支撑：

- provider 边界
- 真实端到端
- Terraform 入口

但还不适合拿来讨论：

- 多副本控制面
- 故障切换
- 强恢复能力

### 3. 还没有把更多平台对象做成 Terraform 资源

当前 provider 第一版只做到：

- `project`
- `app`

还没有继续往前做：

- `secret_set`
- `registry_credential`
- data source
- 更复杂的状态回读对象

这是因为：

- `v3`
  先要证明 provider 插件这条线本身成立
- 不是一上来就把所有平台对象都塞进 Terraform

### 4. 还没有把 bootstrap 变成完整产品化安装器

虽然：

- `deploy/terraform/lab`
  已经统一

但 `v3`
依然是一个工程学习项目，
不是一个已经完全产品化的安装器。

例如：

- 更完整的配置校验
- 更友好的用户输入界面
- 更自动化的发布制品体系

这些都还有继续整理的空间。

## v3 的真实验证现状

这一版最应该记住的不是“理论上能测什么”，
而是已经真的测过什么。

### 本地验证

已经反复使用并收口到脚本里的包括：

- `bash ./scripts/check.sh`
- `bash ./scripts/test-integration.sh`
- `bash ./scripts/smoke.sh`

### 真实阿里云验证

正式入口是：

```bash
cd projects/mini-cloud/tests
go run .
```

这条线会做：

- 准备制品
- Terraform apply
- 等待平台就绪
- 调平台 API
- 触发扩容
- teardown
- destroy

### 真实腾讯云验证

正式入口是：

```bash
cd projects/mini-cloud/tests
go run . tencent
```

它和阿里云一样，
也是完整闭环，
不是只验某一个局部动作。

### Terraform provider 验证

正式入口是：

```bash
cd projects/mini-cloud
go test ./internal/terraformprovider/... ./cmd/terraform-provider-mini-cloud
```

这里不是只做单元测试，
而是真的会编出 provider 二进制，
再驱动 Terraform CLI
做黑盒验证。

## v3 最重要的工程判断

如果只挑最值得记住的几件事，
我会把 `v3`
的判断压缩成下面四条。

### 1. 多 provider 的第一步，不是多云调度，而是边界收口

`v3`
已经很清楚地说明：

- 真正困难的第一步
  不是“同时调两家云”
- 而是：
  - 先让平台对云厂商的依赖边界收口
  - 先让单 provider 模式成立

如果这一步没做好，
后面越往前推，
只会越乱。

### 2. Terraform 在这里应该分两层理解

在 `mini-cloud`
里，
Terraform 现在已经不是一个单点能力，
而是两层不同入口：

- 底座 Terraform
  - 建平台自己的云资源
- 平台资源 Terraform provider
  - 管用户自己项目里的 `service`

这两层分开以后，
整个架构会清楚很多。

### 3. 单 provider 真实闭环，比空谈“多云”更有学习价值

`v3`
已经证明：

- 把两家云各自做成完整单 provider 闭环

比一开始就去做：

- 多云统一调度
- 很大的抽象层

更稳，
也更符合工程推进顺序。

### 4. provider 插件成立后，平台入口终于开始像“云”

当 `minicloud_service`
能被 Terraform 直接加载时，
这个项目的感觉就已经和前面不一样了。

因为这意味着：

- 你不只是搭了一个 control-plane
- 你开始给这个 control-plane 提供：
  - 像云产品一样的资源入口

这正是 `v3`
很关键的一步。

## v4 应该从哪里开始

`v3`
结束以后，
`v4`
最自然的起点，
不是再继续堆零散能力，
而是沿着下面三条线往前走。

### 1. 更完整的平台资源面

优先可能会继续做：

- 更多 Terraform 资源
- data source
- 更清楚的状态只读模型

也就是继续把：

- mini-cloud provider

这条线做厚。

### 2. 更高阶的控制面能力

更可能进入主线的是：

- 多 control-plane
- control-plane 高可用
- 更清晰的认证与审计能力

这些东西放到 `v4`
会比在 `v3`
硬塞进去更自然。

### 3. 更正式的多 provider / 多 control-plane 形态

如果未来继续往前推，
更值得做的是：

- 一个统一入口
- 后面挂多个独立 control-plane
- 每个 control-plane
  各自绑定自己的 provider

而不是直接让：

- 一个 control-plane
  同时管理所有云厂商

这会更符合前面已经建立起来的边界。

## 本章结论

`v3`
做完以后，
`mini-cloud`
已经不再只是：

- “能在某个云上跑起来的控制面 demo”

而是已经具备了下面这条完整结构：

- Terraform 建平台底座
- cloud-init 装起 control-plane
- control-plane 通过单 provider 后端调云资源
- 平台资源可以再通过 mini-cloud Terraform provider 管理

而且这条结构现在已经同时在：

- 阿里云
- 腾讯云

两条真实单 provider 主线上成立。

这就是 `v3`
真正完成的事情。

## 本章检查点

- `v3/14`
  - `602a11874f95623da46d723ecd859a56cce4df22`
