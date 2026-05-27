# v5/11 Full Platform Bootstrap And E2E

`v5/10`
已经把：

- fleet `service`
- `front door route`
- 外部统一入口
  的声明模型

这条线收出来了。

但如果还没有一章把前面这些能力真正串成：

- 控制面
- 双云 `service cell`
- 全局项目资源
- 发布
- 回滚
- 故障演练
- 清理

那 `v5`
还是停留在“分章节功能验证”，
不是一个完整平台结果。

所以这一章只做一件事：

- 把前面所有核心能力收成一条真实可执行的 operator 流程

## 这一章先把三层模型收清楚

这一章里，
`mini-cloud`
正式按下面这套关系理解：

- `control-plane`
  - 只负责全局管理面
  - 维护全局项目、全局服务、全局入口、plane 注册和跨 plane 放置决策
- `cloud-plane`
  - 一套只属于单个云环境的执行面
  - 它跑在该云的一台固定入口机上
  - 负责本地服务编排、入口收敛、runtime node 扩缩和本地状态
- `agent`
  - 跑在每一台可承载工作负载的节点上
  - 负责节点注册、心跳、拉取工作和回报执行结果

真正的连接方向也要说清楚：

- 人和 Terraform
  -> `control-plane`
- `control-plane`
  -> `cloud-plane`
- `agent`
  -> `cloud-plane`

不是：

- `cloud-plane`
  主动连所有 `agent`

也就是说，
`cloud-plane`
逻辑上控制一组节点，
但物理连接上仍然是：

- 节点上的 `agent`
  主动向上接入它所属的 `cloud-plane`

这套三层关系在当前代码里也要有直接可见的边界：

- [internal/controlplane](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane)
  - 只放全局管理面逻辑
  - 包括：
    - plane 注册与同步
    - 全局项目与服务视图
    - 全局入口编排
- [internal/cloudplane](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane)
  - 只放单个云环境里的执行面逻辑
  - 其中：
    - [server](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/server)
      负责把 northbound / plane southbound / node-agent southbound 三组接口装配到同一个进程里
    - [gateway](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/gateway)
      负责本云入口收敛，不再继续用过于宽泛的 `dataplane` 命名
- [internal/agent](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/agent)
  - 只放节点侧执行逻辑
  - 包括：
    - `cloud-plane` southbound client
    - 容器运行时封装
    - 节点侧应用日志采集

## 固定入口机为什么也要跑 agent

这一章里，
每个真实云环境至少有一台固定入口机。

这台机器上会同时跑：

- `cloud-plane`
- `cloud-plane`
  驱动的数据面 `Caddy`
- 一个资源受限的基础 `node`

这里最容易犯错的地方是：

- 只把 `cloud-plane`
  拉起来
- 却没有让这台入口机以 `agent`
  身份注册回本地 `cloud-plane`

如果这样做，
fleet 虽然能把这个 plane
同步成：

- `ready`

但它看到的仍然会是：

- `nodesReady = 0`
- `cpuFree = 0`
- `memoryFree = 0`

于是第一次跨 plane 放置就会失败。

所以这一章的真实 bootstrap
要求是：

- 固定入口机先启动 `cloud-plane`
- 再启动本机 `agent`
- 并且等 southbound snapshot
  里真的出现：
  - 至少 1 个 ready node
  - 大于 0 的可调度余量
  才算这套 `cloud-plane`
  真正完成启动

这里还有一个实现细节：

- 固定入口机虽然在调度视角里注册成 `node`
- 但它不会把整机资源全报成空闲

因为它本身还要跑：

- `cloud-plane`
- `Postgres`
- `Caddy`
- `Docker`

所以这章会给平台宿主机 node
预留一部分：

- `CPU`
- 内存

让 fleet 看到的是更保守的初始可调度容量，
而不是“整机满额可用”的假象。

## 这一章最后收成什么

这一章完成后，
`mini-cloud`
在 `v5`
里的完整结果是：

- 一个本地启动的 fleet `control-plane`
- 两个真实云上的 `cloud-plane`
  - 阿里云
  - 腾讯云
- 每个云里各自独立的：
  - 固定入口机
  - `cloud-plane`
  - 小 `node`
  - 按需拉起的 runtime `node`
- 一个可执行的外部 front door
  运行时
- 一条完整的 operator 主流程：
  - 注册 plane
  - 绑定项目
  - 创建全局 `config/secret`
  - 创建 fleet service
  - 创建外部 route
  - 发布新 revision
  - rollback
  - maintenance / offline 演练
  - 观测验证
  - teardown / destroy

## 这一章的真实拓扑

这次验收不是“全部本地模拟”，
也不是“所有组件都上真实云厂商 API”。

最终收的是下面这条拓扑：

### 1. fleet control-plane 先在本地启动

这一章里，
fleet `control-plane`
仍然跑在本地。

原因很简单：

- 我们这章要验证的是：
  - fleet 对多 plane 的管理语义
  - 项目资源与 service 的全局抽象
  - front door route 的收敛
  - rollback / incident / cleanup 的操作链
- 不是先把 fleet 自己也做成一套复杂的云上高可用控制面

也就是说，
这一章的重点还是：

- “平台能力是否闭环”

不是：

- “fleet 自己怎么做多副本高可用”

### 2. cloud-plane 用真实双云环境

这次不是只起本地假 plane。

真实环境里会准备：

- 一套阿里云 `cloud-plane`
- 一套腾讯云 `cloud-plane`

每套 plane
都通过各自已有的 Terraform / bootstrap 流程拉起来，
再由 fleet 统一注册。

### 3. 外部 front door 先用可执行代理替代真实 CDN 厂商 API

这里要特别说明：

这一章虽然在概念上已经是：

- 外部统一前门

但第一版执行链路里，
并没有直接接：

- 阿里云 CDN
- 腾讯云 CDN

之类的真实厂商 front door。

这里用的是：

- 一个本地可执行的 front door proxy

它承担的职责是：

- 接收 fleet 下发的 route snapshot
- 按：
  - `externalHost`
  - `pathPrefix`
  做最长前缀匹配
- 再把请求转发到目标 plane 的：
  - `publicOrigin`
  - `publishedHost`

这里有一个容易踩的点：

- `publicOrigin`
  来自 plane 对外入口
- `publishedHost`
  来自 service 在该 plane 上的受管域名

而 `publishedHost` 能不能算出来，
取决于这个 plane 是否配置了：

- `serviceBaseDomain`

如果真实环境里没给 `serviceBaseDomain`，
那么 service 即使已经 `running`，
fleet 的 front door route 仍然会因为
`publishedHost` 为空而停在：

- `syncStatus=blocked`
- `syncMessage=placed service does not have a published host yet`

这一章对应的真实双云 E2E
现在会在 Terraform 输入里显式补上：

- `platformDomain`
- `serviceBaseDomain`

避免这里再次卡住。

为什么这一章先这样做：

1. 这能把：
   - fleet route 解析
   - front door snapshot 推送
   - path routing
   - route 覆盖/不覆盖
   这些核心语义先收干净
2. 不会把章节结果绑死在：
   - 某个 CDN 厂商 API
   - 证书签发传播时间
   - DNS / CDN 缓存延迟
3. 后面如果要接真实 `CDN`
   API，
   只需要替换 front door adapter，
   不需要推翻 fleet 的对象模型

所以这一章的定位非常明确：

- 外部前门语义是正式的
- 真实厂商 front door provider
  还不是这一章的重点

## 这一章新补上的两个关键闭环

前面 10 章以后，
最关键的两个缺口其实是：

1. fleet 还没有自己的项目级资源层
2. fleet 还没有自己的 operator rollback 入口

这一章就是把这两个点补齐。

### 1. fleet project resource 正式成立

现在 fleet `project`
下面正式有三类全局资源：

- `config set`
- `secret set`
- `registry credential`

这里最重要的设计点是：

- fleet `service spec`
  里保存的是：
  - fleet resource ID
- 不是某个具体 plane
  的本地 resource ID

这意味着：

- 用户只在 fleet 视角建一次
  `config/secret/registry`
- 真正投放到具体 plane 时，
  再由 southbound `ensure`
  过程把它们物化成该 plane
  的本地资源

所以这一层的边界是：

- fleet 管“全局声明”
- plane 管“本地实际对象”
- 二者之间用：
  - `resource binding`
  做映射

### 2. rollback 现在有 fleet 侧 operator 入口

之前 cloud-plane
已经有自己的：

- revision
- rollback

但 fleet 还没有统一入口。

这一章补上的语义是：

- 操作员不直接跳进某个 plane
  去手调 rollback
- 而是通过 fleet：
  - 找到当前放置的 remote service
  - 读取远端 revision 列表
  - 自动选出“当前版本之前的那个 revision”
  - 下发 rollback
  - 再把 rollback 后的结果回写到 fleet `service`
    的声明状态里

这点非常关键。

因为如果 rollback
只发生在远端 plane，
但 fleet 仍然保留着“更新后的新 spec”，
那下一次再 apply
又会把服务推回错误版本，
整个模型会立刻失真。

这一章的实现里，
rollback 之后：

- remote service
  会回到旧 revision
- fleet `service`
  的 spec
  也会回到与这个旧 revision
  一致的声明

这样 fleet 仍然是：

- 真正的全局真相源

## 这一章的 operator 主流程

这次真实验收走的是：

### 1. 启动本地 front door proxy

这一步先起一个真正能转发请求的 front door 进程，
而不是只做内存假对象。

它会暴露两类地址：

- admin snapshot endpoint
  - 给 fleet 下发 route snapshot
- public endpoint
  - 给测试流量真正访问

### 2. 启动本地 fleet control-plane

启动时会同时带上：

- fleet sync loop
- front door reconcile loop
- front door adapter URL

所以 front door 的下发已经不是：

- 手工一次性触发

而是：

- 后台周期收敛
- 写请求触发收敛

### 3. 准备真实阿里云 / 腾讯云 cloud-plane

这一章复用前面已有的单云 bootstrap 能力，
分别准备：

- 阿里云 plane
- 腾讯云 plane

这里复用的其实是同一套：

- 单个 `cloud-plane cell`
  Terraform 栈

只是分别对：

- 阿里云
- 腾讯云

各执行一次。

每套环境都会：

- 上传二进制
- Terraform apply
- 等待 `healthz`
- 暴露：
  - `baseURL`
  - `publicIP`
  - `adminToken`
    - 这里只用于当前章节里的调试和显式 `platform teardown`
    - 它不是 `control-plane -> cloud-plane -> agent`
      主控制链路的一部分
  - `fleetBootstrapToken`

### 4. 注册两套 plane

fleet 会依次：

- 创建 plane 记录
- 用 bootstrap token 做 register
- 再等待：
  - `status=ready`
  - `registration.registered=true`

注册完成后，
还会验证：

- fleet inventory
- fleet metrics

确保：

- 两套 plane
  都进入统一视图

### 5. 创建一个全局 project 并绑定到两套 plane

这一步之后，
同一个全局 `project`
会拿到：

- 阿里云 remote project
- 腾讯云 remote project

注意这里的 project binding
只是在不同 plane
里建立“同一全局项目”的对应关系。

### 6. 创建全局 config / secret

这一章真实双云验收里，
至少会创建：

- 一个 `config set v1`
- 一个 `secret set v1`

后面在发布新 revision
时，
还会再创建：

- `config set v2`
- `secret set v2`

这样我们就能验证：

- fleet service
  引用的是全局资源 ID
- 真正下发到 remote plane
  时会被翻译为该 plane
  自己的本地 resource ID
- rollback
  时又能从 remote ID
  重新恢复到 fleet 的全局 ID

### 7. 创建两个 fleet service

这次会创建：

- 一个落在阿里云的 public service
- 一个落在腾讯云的 public service

其中阿里云这条服务会显式引用：

- `config set`
- `secret set`

这样我们能验证：

- project resource 的 global -> remote 映射链路

### 8. 创建 front door route 并验证统一入口

这一步会把：

- `/aliyun`
  路由到阿里云 service
- `/tencent`
  路由到腾讯云 service

要让这一步真正收敛成 `synced`，
前面两个 cloud-plane 不能只做到
“服务已经能跑起来”，
还必须已经具备：

- 非空的 `publicOrigin`
- 非空的 `publishedHost`

其中 `publishedHost`
依赖 `serviceBaseDomain`，
所以真实 E2E 的 Terraform 输入里
也必须把这个字段带上。

然后真正从统一入口发请求，
验证：

- route 已同步
- front door 会选对目标 plane
- 新增第二条 route
  不会把第一条 route 覆盖掉

### 9. 发布一个新 revision

阿里云 service
会再做一次 fleet 级更新，
把：

- `config set`
  从 `v1`
  切到 `v2`
- `secret set`
  从 `v1`
  切到 `v2`
- `env.APP_VERSION`
  从 `v1`
  改到 `v2`

这里故意不强依赖真实云上再拉一个更大的镜像，
而是通过：

- spec 变化
- 新 revision 生成
- 远端 service detail

来验证：

- 发布链已经真的发生了

### 10. 通过 fleet 入口执行 rollback

rollback 之后会检查三件事：

1. fleet `service spec`
   回到 `v1`
2. 远端 plane
   当前运行的 service spec
   也回到 `v1`
3. front door
   仍然继续可用

也就是说，
这一章不把 rollback
做成“远端偷偷回退、fleet 还停留在错误版本”。

### 11. 继续做 targeted apply / placement / maintenance / offline drill

前面已有的这些 operator 流程仍然保留：

- targeted apply
- auto placement preview
- maintenance 状态阻塞新投放
- 故意切断某个 plane 的 southbound 访问
- fleet incident create / resolve
- plane recover 后再次 apply

这一章不是替换这些流程，
而是把它们放到一条更完整的平台主链里。

### 12. teardown / destroy

最后仍然要求：

- 先做 platform teardown
- 再做 Terraform destroy
- 再回收上传的二进制对象

这里的销毁语义要特别明确：

- `platform teardown`
  是测试程序主动发起的一步显式 API 调用
- `terraform destroy`
  只负责删除 Terraform 管理的基础设施
- 不再把 `teardown`
  塞进 Terraform 的 destroy-time hook

这样改的原因不是“风格问题”，
而是旧设计确实不幂等。

之前如果把：

- `enabled`
- `teardown URL`
- `admin token`
- `timeout`

这类值塞进 `terraform_data`
再配合 destroy-time `local-exec`，
这些输入会跟着 state 一起固化。

结果就是：

- 你后来即使改了 `tfvars`
- 甚至把开关改成 `false`

真正执行 destroy
时，
Terraform 仍然可能按旧 state
里的输入去跑历史 hook，
于是出现：

- 平台已经起不来
- `teardown API`
  根本打不通
- 但 `terraform destroy`
  反而先被这个旧 hook 卡死

现在这章收敛后的规则就是：

- 显式 `teardown`
  失败不会阻塞后续 `terraform destroy`
- `terraform destroy`
  永远可以独立执行
- 最终清理结果会把：
  - `teardown`
    错误
  - `destroy`
    错误
  一起汇总出来

这一章收的是：

- “完整闭环”

不是：

- “功能做出来但资源留在云上”

## 这一章到底验证了哪些语义

### 1. 全局资源和远端资源已经分层

这次不会再假设：

- fleet `configSetID`
  就等于某个 plane
  的本地 `configSetID`

真正验证的是：

- fleet ID
  -> southbound ensure
  -> remote ID

这条翻译链。

### 2. rollback 已经是 fleet 级语义

这不是：

- 单个 plane
  的内部调试动作

而是：

- fleet 级 operator
  的正式入口

### 3. front door 已经进入真实可执行态

虽然还不是厂商 `CDN`
provider，
但已经不是只做对象层测试。

它真的会：

- 收 snapshot
- 选 route
- 反代到目标 plane

### 4. 故障演练和平台主链已经合并

maintenance / offline / incident / recover
不再是孤立 demo，
而是和：

- service deploy
- front door
- rollback

一起构成 operator runbook

## 这一章没有故意去做什么

即使做到这里，
这一章也没有把下面这些东西一起做掉：

### 1. 真实 CDN 厂商 API 对接

这一章先不做：

- 阿里云 CDN API
- 腾讯云 CDN API

因为那会把章节重点拉到：

- 厂商差异
- 证书传播
- 缓存失效

而不是：

- front door 语义本身

### 2. 真实 TLS 证书签发和公网域名托管

概念上，
这一章已经把：

- 域名入口
- front door

这条路径收出来了。

但第一版真实验收里，
并不要求你一定已经把：

- 真实公网域名
- 真实证书
- 真实 DNS / CDN 记录

也全自动化接上。

这不是回避问题，
而是故意把边界切清楚：

- `v5/11`
  先收平台语义和 operator 流程
- 真实公网入口供应商接入
  留到后面继续深化

### 3. 真实双云环境里强依赖私有镜像仓库

这一章会把：

- `registry credential`

正式建模进 fleet 项目资源层，
并在本地/集成链路里验证其语义。

但真实双云验收主链，
仍然优先用：

- 公共镜像

这样可以避免把真实云实验结果强行绑在：

- 私有仓库账号
- 镜像权限
- 镜像拉取额度

这类额外变量上。

## 建议的验证方式

### 1. 先跑快速本地检查

```bash
cd projects/mini-cloud
go test ./internal/cloudplane/api
go test ./internal/controlplane/api
```

这两组测试重点覆盖：

- southbound plane API
- fleet northbound API
- 项目资源
- rollback
- route / placement / auth

### 2. 再跑 tests 子项目的轻量校验

```bash
cd projects/mini-cloud/tests
go test ./dual ./internal/frontdoorproxy ./internal/localcontrolplane
```

这里主要确认：

- dual 验收入口能编译
- front door proxy 行为稳定
- 本地 fleet control-plane runner
  配置正确

### 3. 最后跑真实双云 E2E

```bash
cd projects/mini-cloud/tests
go run . dual
```

这条命令会真正执行：

- 本地 fleet control-plane
- 真实阿里云 plane
- 真实腾讯云 plane
- front door route
- service publish / rollback
- incident / recover
- teardown / destroy

## 排障时先看哪里

这一章如果失败，
最先看的不是单一日志文件，
而是先定位“卡在哪一层”。

### 1. 如果 route 不通

先看 fleet：

- `GET /api/v1/projects/{projectID}/services/{serviceID}/frontdoor-routes`

重点看：

- `syncStatus`
- `syncMessage`
- `targetPlaneID`
- `targetOrigin`
- `targetHost`

如果这里就不对，
说明问题还在：

- fleet route 解析
  或
- front door reconcile

### 2. 如果 route 已同步但访问失败

再看目标 plane：

- `GET /api/v1/services/{serviceID}`
- `GET /api/v1/services/{serviceID}/revisions`
- `GET /api/v1/services/{serviceID}/deployments`

这能分辨出：

- 是 front door 到 plane 的问题
- 还是 plane 内服务本身没有 ready

### 3. 如果 rollback 后 spec 不一致

先分别看：

- fleet service detail
- remote service detail

如果 remote 已回退，
但 fleet 没回退，
说明问题在：

- remote -> fleet
  的 spec 回写

如果 fleet 已回退，
但 remote 没回退，
说明问题在：

- southbound rollback
  或
- remote revision 选择

## 这一章的结论

做到这里，
`v5`
已经不再只是：

- 多个独立功能章节的集合

而是第一次真正收成：

- 一个能跨双云管理长期在线服务的 `CaaS`

它已经具备：

- 全局项目
- 全局服务
- 全局 `config/secret`
- 外部统一入口
- 发布
- rollback
- placement
- incident
- teardown

这些主链能力。

而且更重要的是，
这些能力现在不是停留在模型层，
而是已经有：

- 本地可执行链路
- 真实双云验收链路

两套验证方式。

## 本章检查点

如果这一章做对了，
你现在应该已经能稳定回答下面这些问题：

1. 为什么这一章的完整验收，
   必须同时包含：
   - 本地 fleet `control-plane`
   - 双云真实 `cloud-plane`
   - 双云真实 `service cell`
2. 为什么固定入口机不仅要跑：
   - `cloud-plane`
   - `Caddy`
   还必须真的注册成：
   - 一个可调度 `node`
3. 为什么：
   - 发布
   - rollback
   - placement
   - incident
   - teardown
   必须在同一条 operator 主链里一起看
4. 为什么这一章的价值，
   不是“再演示一次接口”，
   而是证明：
   - `v5`
     已经能收成完整平台结果

如果这些问题都已经能稳定回答，
那 `v5/11`
的完整平台闭环就算真正收住了。

对应提交：

- `d2a2976c67863e7f5c280176311aba191a7ee4ab`
