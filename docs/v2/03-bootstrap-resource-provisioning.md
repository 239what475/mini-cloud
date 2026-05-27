# 03 Bootstrap Resource Provisioning

这一章开始不再只是谈设计，
而是把 `v2 bootstrap`
真正落成一段可以执行的代码。

不过这里我刻意没有一口气把：

- `VPC`
- `vSwitch`
- `SecurityGroup`
- `EIP`
- platform `ECS`
- worker `ECS`

全部塞进同一章。

原因很简单：

- 如果 `03`
  - 同时做共享网络
  - 同时做 platform 机器
  - 同时做安装
- 那么后面的：
  - `04`
  - `05`
- 就会变得边界很虚

所以这一章先把：

- 共享网络底座
- inventory 基础
- `bootstrap` CLI 骨架

真正做出来。

这样后面的承接就很清晰：

- `03`
  - 先把阿里云网络底座准备好
- `04`
  - 再在这张网络里准备 platform `ECS`
  - 实例 `RAM` 角色
  - `EIP`
- `05`
  - 再创建 first worker
  - 接进平台拓扑

## 这一章到底落了什么

这一章新增了三块代码：

| 位置 | 作用 |
| --- | --- |
| `cmd/bootstrap` | 提供 `plan` / `apply` 两个入口 |
| `internal/bootstrap` | 负责配置、校验、命名、标签、阿里云 client、discover-first、network ensure |
| `deploy/bootstrap/bootstrap-config.json.example` | 提供教学配置模板 |

当前 `03`
已经真正支持：

- 读取 `bootstrap` 配置文件
- 校验地域、实例规格、CIDR、端口、管理来源地址
- 统一生成平台资源名和标签
- 连接阿里云 `ECS` / `VPC` SDK
- 查询现有受管资源
- 不存在时创建：
  - `VPC`
  - `vSwitch`
  - `SecurityGroup`
  - 目标安全组规则
- 输出一份可复用的 network inventory

这里还没做的事情，
故意留给后续章节：

- 创建 platform `ECS`
- 创建 first worker `ECS`
- 分配并绑定 `EIP`
- 远程安装 `Docker` / `compose`
- 启动平台服务

## 为什么这一章先只做网络底座

这一版拆法不是为了“少做一点”，
而是为了让真实工程顺序更清楚。

因为 platform `ECS`
和 worker `ECS`
都依赖这些共享输入：

- `VpcId`
- `VSwitchId`
- `SecurityGroupId`
- `ZoneId`

如果这些基础网络对象都还没有稳定 inventory，
后面安装和纳管阶段就会一直夹着“顺手创建网络”的逻辑，
代码会很快变乱。

所以这一章先把：

- discover-first
- create-if-missing
- inventory output

在网络这一层做扎实。

## CLI 现在怎么用

当前入口是：

```bash
go run ./cmd/bootstrap plan \
  --config ./deploy/bootstrap/bootstrap-config.json.example
```

如果你已经准备好了自己的真实配置文件，
并且当前执行环境具备阿里云凭据，
可以再运行：

```bash
go run ./cmd/bootstrap apply \
  --config ./deploy/bootstrap/bootstrap-config.json \
  --inventory-out ./deploy/bootstrap/inventory.json
```

这里先记两点：

1. `bootstrap-config.json.example`
   - 只是模板
   - 需要复制成你自己的：
   - `bootstrap-config.json`
2. `inventory.json`
   - 是运行产物
   - 不应该提交进仓库

所以这一章同时也加了：

- `deploy/bootstrap/.gitignore`

来忽略这两个本地文件。

## `plan` 和 `apply` 的区别

这里我没有做成“默认就直接写云资源”，
而是先明确区分两种模式：

| 模式 | 作用 |
| --- | --- |
| `plan` | 读取配置，查现有资源，给出下一步动作计划，但不写资源 |
| `apply` | 在同样的 discover-first 逻辑上，真正去创建缺失资源 |

这样做的好处是：

- 先看清楚这次准备动什么
- 再决定是否真正写入云资源
- 后面做重试、升级、销毁时，CLI 结构也更容易扩展

## 当前固定了哪些命名和标签

这一章把受管资源名统一成了：

| 资源 | 名字规则 |
| --- | --- |
| `VPC` | `<platform-name>-vpc` |
| `vSwitch` | `<platform-name>-vsw` |
| `SecurityGroup` | `<platform-name>-sg` |
| `EIP` | `<platform-name>-eip` |
| platform `ECS` | `<platform-name>-platform` |
| first worker `ECS` | `<platform-name>-worker-01` |

虽然 `EIP` 和实例还没在这一章真正创建，
但名字规则已经先冻结下来了。

统一标签也先固定成：

| key | value |
| --- | --- |
| `managed-by` | `mini-cloud-bootstrap` |
| `project` | `mini-cloud` |
| `platform-name` | `<PlatformName>` |
| `environment` | `<Environment>` |
| `owner` | `<Owner>` |

后面做：

- 资源复用
- 销毁
- inventory 展示

都直接依赖这套标签。

## 安全组规则现在怎么设计

这一章里，
安全组规则先按“平台管理面 + 平台入口面”拆开。

| 端口 | 来源 | 用途 |
| --- | --- | --- |
| `22/tcp` | `AllowedAdminCIDRs` | 只允许管理来源做 `SSH` |
| `<ControlPlaneHTTPPort>/tcp` | `AllowedAdminCIDRs` | 管理面 HTTP 入口，先只开放给管理来源 |
| `<GatewayHTTPPort>/tcp` | `0.0.0.0/0` | 对外应用网关 HTTP |
| `<GatewayHTTPSPort>/tcp` | `0.0.0.0/0` | 对外应用网关 HTTPS |

这里要注意：

- `AllowedAdminCIDRs`
  - 只控制管理入口
- 公网应用入口
  - 目前仍然默认对所有来源开放

后面如果做更正式的入口层，
这个策略还会继续收紧，
但当前这组规则足够支撑：

- 平台管理
- 网关接入

这两条基础链路。

## discover-first 在代码里怎么体现

这一章最重要的不是“会调 `CreateVpc`”，
而是把前面阿里云实验里已经验证过的做法，
真正收进项目代码：

1. 先按名字 + 标签查现有受管资源
2. 命中就直接复用
3. 查不到才真的发起创建
4. 创建请求只给本次写动作分配新的 `ClientToken`
5. 创建成功后再按返回 ID 回读一次

当前这套模式已经贯穿了：

- `VPC`
- `vSwitch`
- `SecurityGroup`
- 安全组规则

所以这一章的价值不只是“能建出资源”，
而是：

- `mini-cloud`
  - 第一次真正有了可以反复执行的云端写资源主链

## zone 现在怎么选

前一章里，
我们只明确了地域，
还没有把 zone 输入完全冻结下来。

到了真实代码这里，
这个问题就必须正面处理，
因为：

- `CreateVSwitch`
  - 必须带 `ZoneId`

所以当前实现加了一个可选输入：

- `PreferredZoneId`

它的语义是：

- 如果你明确知道自己要落在哪个可用区
  - 就显式写它
- 如果你不写
  - `bootstrap`
    - 就先调用 `DescribeZones`
    - 从返回列表里挑一个 zone 用于当前阶段

这里先刻意保持简单，
还没有引入：

- 规格可用性过滤
- 价格过滤
- 更复杂的 zone 选择策略

因为这些更像：

- platform `ECS`
- worker `ECS`

创建时才真正需要的决策。

## 当前 inventory 里有什么

`apply`
执行成功后，
当前会输出一份 network inventory。

里面最重要的是这些字段：

| 字段 | 作用 |
| --- | --- |
| `RegionId` | 当前平台网络所在地域 |
| `ZoneId` | 当前 `vSwitch` 落在哪个可用区 |
| `Vpc` | 后续实例要加入哪张专有网络 |
| `VSwitch` | 后续实例要落到哪个子网 |
| `SecurityGroup` | 后续实例要套用哪个安全组 |
| `IngressRules` | 这张安全组当前应该具备哪些规则 |

也就是说，
从这一章结束开始，
后面的 `04`
已经不需要再重新发明一套“先查网络资源”的逻辑，
而是可以直接拿这份 inventory 继续向前走。

## 配置模板为什么一定要用 `.example`

这一章新增了：

- `deploy/bootstrap/bootstrap-config.json.example`

而不是直接提交一个：

- `bootstrap-config.json`

原因和仓库里其他教学主线一致：

- 真实配置通常会带你的：
  - 地域
  - 管理 CIDR
  - 平台命名
  - 后面还会带更多真实参数
- 这些都属于你本地实际使用的内容

所以模板入库，
真实配置留在本地，
这才符合这个仓库一贯的约定。

## 这一章跑了哪些检查

当前我已经跑过：

```bash
go test ./...
./scripts/check.sh
go run ./cmd/bootstrap plan \
  --config ./deploy/bootstrap/bootstrap-config.json.example
go run ./cmd/bootstrap apply \
  --config ./deploy/bootstrap/bootstrap-config.json
```

所以这次提交至少已经过了：

- `go test`
- `go vet`
- `staticcheck`
- `bootstrap plan`
  - 已经成功走到真实阿里云读路径
  - 能返回 zone 选择和动作计划
- `bootstrap apply`
  - 已经在真实阿里云里创建并回读：
  - `VPC`
  - `vSwitch`
  - `SecurityGroup`
  - 安全组规则

这次真实 `apply`
还顺手暴露了两个读写链路里的真实问题，
现在都已经在代码里修掉了：

1. `CreateVpc`
   之后不能立刻 `CreateVSwitch`
   - 要先等 `VPC`
     进入 `Available`
2. 安全组回读里的 `IpProtocol`
   是大写 `TCP`
   - 代码里要先标准化成小写
   - 否则会误判成“规则不存在”

## 这一章的结论

`03`
真正建立起来的，
不是“几条创建网络资源的函数”，
而是一条更重要的基础能力：

- `mini-cloud`
  - 已经有了一个正式的 `bootstrap` 入口
  - 已经能把阿里云共享网络底座做成 discover-first 的受管资源
  - 已经能输出后续章节直接可复用的 inventory

这意味着从下一章开始，
我们终于可以把注意力放到：

- platform `ECS`
- 远程安装
- 首次启动

这些真正属于“平台自安装”的问题上了。

## 本章检查点

- 提交：
  - `24613314c2884695427f273a0e6ce8adaadde289`
- 状态：
  - `bootstrap` CLI、配置模板、共享网络底座 provisioning 和 network inventory 已落地
