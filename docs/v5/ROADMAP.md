# Mini Cloud v5 路线图

回到总路线图：

- [projects/mini-cloud/docs/ROADMAP.md](../ROADMAP.md)

## v5 现在的出发点

做完 `v4`
以后，
`mini-cloud`
已经有了：

- `fleet control-plane`
- 多套独立 `cloud-plane`
- `agent`
- 多云纳管
- 全局观察和审计
- 基础下发和放置

但与此同时，
也暴露出了两个问题：

1. 当前产品边界还是太散
   - workload 类型开始变多
   - 平台能力开始摊得过宽
2. 当前基础组件还是自己搭得太多
   - 入口网关
   - HTTP API
   - 观测入口

所以：

- `v5`
  不再继续摊大能力清单
- `v5`
  也不再继续自己搭这些通用基础件

而是先把重点收回到：

- 多云 `CaaS`
  产品化
- 长期在线服务
  主线
- 成熟开源组件
  替代基础设施

## v5 的一句话定位

如果只用一句话描述 `v5`，
我建议就是：

- 基于多云 `service cell`
  拓扑，
  用成熟开源组件把 `mini-cloud`
  做成一个只支持长期在线服务的多云 `CaaS`

## v5 的明确目标

`v5`
现在要正式收成的是：

- 一个围绕：
  - 服务定义
  - 服务暴露
  - 发布
  - 回滚
  - 扩容
  - 观测
  组织起来的多云 `CaaS`
- 一个最后可以完整搭起来的平台结果：
  - 全局 `control-plane`
  - 双云 `service cell`
  - 每云固定入口机
  - 外部统一入口
    - `CDN`
      前门

这里的“多云 `CaaS`”
明确指的是：

1. 只支持：
   - 容器
   - 长期在线服务
2. 每个云都是一个：
   - `service cell`
3. 每个 `service cell`
   至少包含：
   - 一台固定入口机
   - `cloud-plane`
   - `Caddy`
   - 一个小 `node`
4. 本云资源不够时，
   由本云 `cloud-plane`
   扩更多 `runtime node`
5. 全局：
   - `fleet control-plane`
     只做管理面
   - 不进用户流量热路径
6. 对外还需要有一个：
   - 统一公网入口
   - 在 `v5`
     里先收成：
     - `CDN`
       前门接入

## v5 的核心设计

### 1. 每个云是一个 `service cell`

`v5`
不再把一个云只看成“若干 runtime node”，
而是把它正式看成一个：

- `service cell`

这个 `cell`
里有：

- `cloud-plane`
- `Caddy`
- 小 `node`
- 按需扩出来的更多 `runtime node`

### 2. 用户对象只保留 `service`

`v5`
不再继续扩：

- `job`
- `cron`
- `oneoff`

这些 workload 类型。

这一版只收一个核心对象：

- `service`

它表示：

- 一个长期在线的容器服务

### 3. 管理流量和用户流量必须分开

管理路径：

- `CLI / Web UI`
  -> fleet `control-plane`
    `HTTP API`
  -> `gRPC`
  -> `cloud-plane`
  -> `gRPC`
  -> `node-agent`

这里要特别说明：

- `cloud-plane`
  的 northbound 管理接口
  在这一版里收成：
  - `gRPC`
  - `grpc-gateway`
- fleet `control-plane`
  的 northbound 接口
  在这一版里仍然保留：
  - `HTTP JSON`

用户流量路径：

- 用户
  -> 外部接入层
    - 可以是：
      - `CDN`
      - `DNS`
  -> 本云 `gateway`
  -> 本云服务实例

这里要特别强调：

- `fleet control-plane`
  不进入用户流量热路径
- `control-plane`
  挂了以后，
  现网服务仍然应该能继续跑
- 这里提到的：
  - `CDN / DNS`
    不是“全局流量编排主线”，
    但最小的 front door 接入
    仍然属于 `v5`
    完整平台闭环的一部分

### 4. 用成熟组件替代通用基础件

`v5`
直接建议收成下面这套：

- 应用入口网关：
  - `Caddy`
- `cloud-plane`
  northbound API：
  - `gRPC`
  - `grpc-gateway`
- fleet `control-plane`
  northbound API：
  - `HTTP JSON`
- `proto`
  和生成：
  - `buf`
- 数据库：
  - `Postgres`
- 数据访问：
  - `sqlc`
- 迁移：
  - `goose`
- 运行时控制：
  - `Docker SDK`
- 观测入口：
  - `OTel Collector`
- 指标 / 日志 / 看板：
  - `Prometheus`
  - `Loki`
  - `Grafana`
- 本地客户端：
  - `Cobra`

这里也要明确：

- `grpc-gateway`
  只做平台管理接口
- 它不是用户业务流量入口
- 用户业务流量入口由：
  - `Caddy`
    承担

## v5 的非目标

`v5`
现在明确不做下面这些事情：

- `job / cron / oneoff`
- 双 `control-plane`
  对等同步
- 正式多活 `control-plane`
  高可用
- 全局流量代理层
- `DNS`
  级主备切流编排
- 复杂 `CDN`
  流量策略系统
- `service mesh`
- 第三家 provider
- 有状态服务主线
- 托管数据库、对象存储等新产品线
- `Kubernetes`
  运行底座迁移
- 余额 / 账单 / 支付系统
- 更复杂的租户体系

这些方向不是不做，
而是：

- 现在还不该压过
  `v5`
  的长期在线服务主线
- 它们会明显提高架构和叙事复杂度

## 新的章节顺序

`v5`
仍然不单独保留总览章。

原因和前面一样：

- `docs/v5/README.md`
  已经承担导航和总说明作用

所以正式章节仍然从：

- `01`

开始。

### `01-caas-foundation-and-service-cell`

- 把原来分散的：
  - 产品边界
  - `service cell`
    拓扑
  - 组件栈选择
  一次收成一个完整基础章
- 这一章至少讲清：
  - `v5`
    只做长期在线服务
  - 每个云是一个 `service cell`
  - 入口机上跑什么
  - 管理流量和用户流量怎么分
  - 为什么选：
    - `Caddy`
    - `gRPC`
    - `grpc-gateway`
    - `OTel Collector`
- 这一章结束时，
  下面这些东西必须冻结：
  - 产品边界
  - 拓扑边界
  - 组件栈边界

### `02-service-spec-and-management-api`

- 定义：
  - `service`
    的资源模型
  - `service`
    的期望状态
- 至少收出：
  - 镜像
  - 端口
  - 副本
  - 健康状态
  - 当前 release
- 同时把：
  - northbound
  - plane southbound
  - node-agent southbound
    这三类 management API
    统一到：
    - `proto`
    - `gRPC`
    - `grpc-gateway`
  - 并把：
    - `server`
    - `statichttp`
    - `gateway`
    - `api`
      这几层 transport
      边界收干净
  - 再把：
    - `service`
      生命周期用例
      从 `api`
      下沉到：
      - `servicelifecycle`
- 这一章结束时，
  用户应该第一次可以：
  - 提交一个正式的 `service`
    定义
  - 通过统一的 management API
    去管理它

### `03-config-secret-and-image-access`

- 补齐服务真正运行需要的输入
- 至少收出：
  - env
  - config
  - secret
  - registry credential
- 让“提交一个服务规格”不再只是空壳

### `04-managed-gateway-exposure-with-caddy`

- 用：
  - `Caddy`
    取代当前最小自研 `gateway`
- 补齐：
  - 域名绑定
  - `TLS`
  - `public / private`
    暴露语义
  - 路由下发
- 这一章结束时，
  一个服务要能被真正访问到

### `05-health-release-and-rollback`

- 把长期在线服务真正做成可运维能力
- 至少收出：
  - `readiness`
  - `liveness`
  - `startup`
  - graceful shutdown
  - rolling update
  - pause / resume
  - rollback
- 这里不再拆出更多 workload 类型，
  只围绕：
  - `service`
    组织发布语义

### `06-scaling-placement-and-worker-expansion`

- 把这条本地执行链做实：
  - 入口机兼小 `node`
  - 本云资源不够时扩更多 `runtime node`
- 至少补齐：
  - 服务副本扩容
  - 副本级放置与执行槽位
  - 失败副本重试
  - 容量判断
  - 本云 `runtime node`
    扩容
  - 入口机资源保护
- 这一章先不收：
  - `scale-down`
  - `runtime node`
    回收
  - 多副本滚动发布

### `07-app-observability-with-otel`

- 这一章明确只做：
  - 应用侧观测
- 平台日志和平台级指标
  - 继续沿用前面章节
  - 不在这里重做
- 这里要收两条链：
  - 应用日志：
    - `agent`
      兜底采集受管应用容器的 `stdout / stderr`
    - 直接写入 `Loki`
    - 通过 `service`
      作用域查询
  - 应用指标和链路：
    - 应用自愿埋点
    - 平台注入标准 `OTEL_*`
      环境变量
    - `OTel Collector`
      统一接收
    - 指标进 `Prometheus`
    - trace 进 `Tempo`
    - 最后统一在 `Grafana`
      查看
- 这一章明确不做：
  - 宿主机全量日志采集
  - `docker daemon`
    日志采集
  - 任意容器扫描
  - `docker compose`
    运行模型
  - 用户自带 `sidecar`
    日志方案
- 重点是：
  - 开发者怎样看自己服务的日志、指标、trace
  - 而不是继续摊大平台观测总线

### `08-identity-service-account-and-authorization`

- 这一章不再只是“人类登录章”
- 而是把整个平台的：
  - 身份主体
  - 外部登录
  - 管理员模型
  - 项目归属关系
  - 平台与项目两层授权边界
  一次收成一个统一基础章
- 明确采用：
  - `Authelia`
    作为外部身份与人类登录入口
- 但同时保持：
  - `mini-cloud`
    自己继续掌握授权边界
- 这一章会统一 northbound principal：
  - `human_user`
    来自 `Authelia`
  - `service_account`
    给 AI agent、CI、自动化脚本
  - `break_glass`
    也就是当前 `admin token`
    的收口形态
  - `anonymous`
    未认证请求
- 这一章会把当前：
  - `project_token`
    收编成：
    - `service_account(scope=project)`
  - 也就是说，
    后面不再把 `project_token`
    当成一套独立的身份世界观
- 这一章明确区分：
  - `Authelia admin`
    只管理身份系统本身
  - `platform_owner`
    管平台最高权限和危险变更
  - `platform_admin`
    管平台日常管理
  - `platform_operator`
    管部署、回滚、扩缩和观测
    适合 AI agent
  - `platform_auditor`
    管平台只读与审计
  - `break_glass`
    只用于初始化和应急
- 总原则是：
  - `Authelia`
    负责认证
  - `mini-cloud`
    负责授权
  - `break_glass`
    不参与日常管理员模型
- 这一章至少收出：
  - 人类用户登录入口
  - 外部身份到平台主体的映射
  - `issuer + subject`
    作为 `human_user`
    稳定标识
  - `service_account`
    的平台级和项目级作用域
  - 首登后的待认领用户识别
  - 首个 `platform_owner`
    或首个 `platform_admin`
    的绑定流程
  - `principal -> platform_role`
    映射
  - `project -> owner_user`
    归属关系
  - `WhoAmI`
    返回主体类型、
    平台角色和项目归属信息
  - 审计里区分：
    - `human_user`
    - `service_account`
    - `break_glass`
- 这一章还要把这些现有能力正式接入“单 owner project”授权：
  - `service / revision / deployment / domain`
  - `config-set / secret-set / registry-credential`
  - 项目级 `service_account`
    或原 `project_token`
  - `service logs`
  - `operation audit`
- 默认权限语义先收成：
  - `project.owner_user`
    管这个项目下的：
    - 服务
    - 敏感资源
    - 域名
    - 项目级自动化身份
  - `platform_*`
    管平台和跨项目能力
- 这一章会把初始化流程收成：
  1. 平台首次启动时保留 `break_glass`
  2. 配置 `Authelia`
     作为外部身份入口
  3. 先完成人类用户首次登录，
     让平台拿到待认领用户的
     `issuer + subject`
  4. 再用 `break_glass`
     绑定首个 `platform_owner`
     或 `platform_admin`
  5. 之后人类管理员都通过 `Authelia`
     登录，再由 `mini-cloud`
     按映射授予权限
  6. AI agent 和自动化流程
     统一改用 `service_account`
  7. `break_glass`
     之后仅保留作应急恢复
- 这一章明确不做：
  - 多人共享同一个 `project`
  - `project membership`
  - 人类用户的项目内多档角色
  - 自助注册
  - 组同步
  - `SCIM`
  - 复杂企业目录集成
  - 多个身份系统同时接入
  - 把项目授权下沉给外部身份系统

### `09-project-guardrails-and-capacity-admission`

- 做最小治理能力
- 这一章不再负责定义身份语义
- 而是在 `08`
  已经明确“谁能做什么”之后，
  再把项目级护栏收清楚
- 至少收出：
  - 项目级配额：
    - `maxServices`
    - `cpuMilli`
    - `memoryMi`
  - 项目级当前 usage / remaining 视图
  - 创建前 preview
  - 创建和扩容时的 quota reject reasons
  - `project.owner_user`
    和 `platform_*`
    分别能看什么、改什么
- 这一章放后面，
  避免打断主路径
- 这一章明确不做：
  - 成本视图
  - 预算
  - 余额
  - 支付
  - 域名数量限制
  - 独立 `maxReplicas`
    quota

### `09` 之后要固定的关系模型

做完 `09`
以后，
后面的平台主线，
先固定成下面这套简化关系：

```text
human_user --< project --< service

project --< service_account(scope=project)
platform --< service_account(scope=platform)

actor = human_user | service_account
owner = project.owner_user
```

这里要特别强调：

- `service`
  属于 `project`
  不属于某个 `human_user`
- `actor`
  只负责审计和授权上下文
- `owner`
  就是这个 `project`
  的固定拥有者

也就是说，
后面的 project guardrail、
front door、
完整平台验收，
都应该建立在：

- `project.owner_user`
- `service_account(scope=project)`
- `service -> project`

这三条稳定关系之上。

### `10-external-front-door-and-cdn-attachment`

- 在保持前面主线不变的前提下，
  补上完整平台所需的外部统一入口
- 这一章只解决：
  - 外部 `front door`
    如何接到两台固定入口机
  - 平台对外 `host / path`
    规则如何组织
  - 双云 `service cell`
    怎样作为外部接入层的后端
  - 独立的
    `front door route`
    资源怎样动态映射到对应云的入口机
  - 入口机上的 `Caddy`
    再怎样把这些路径转发到本云对应容器
- 这一章明确不做：
  - 厂商级 `CDN / DNS / TLS`
    正式接入
  - 智能全局流量编排
  - 动态权重切流
  - 多活全局入口控制系统
- 这一章结束时，
  平台应该已经具备：
  - 一个对外统一入口
  - 两个云内固定入口机
  - 基础接入闭环
  - 一套清楚的两段路由模型：
    - 外部 `front door`
      按 route 规则选云入口机
    - `Caddy`
      在本云按服务路径选容器

### `11-full-platform-bootstrap-and-e2e`

- 把前面所有能力收成一个完整平台结果
- 至少包含：
  - 全局 `control-plane`
  - 双云固定入口机
  - 双云 `service cell`
  - 外部 `front door`
    前门
  - 创建项目
  - 定义服务
  - 配置 `secret`
  - 外部 route / host / path
  - 发布
  - 回滚
  - 扩容
  - 双云部署
  - 观测
  - 清理
- 这一章同时要补一套最小 operator 视角的运行流程：
  - cell 注册和对账
  - 入口机与 node 的维护模式
  - 发布失败后的回滚与人工介入边界
  - front door 和本云 `Caddy`
    配置不一致时的排障入口
- 这是 `v5`
  最核心的完整平台验收章

### `12-v5-hardening-and-boundary-review`

- 最后统一复盘：
  - 哪些能力已经是正式 `CaaS`
    能力
  - 哪些边界故意没有做
  - 哪些方向留到 `v6+`
- 让 `v5`
  最终收成一个清晰的产品版本，
  而不是一个继续扩散的实验合集

## 为什么不选其他方向当 v5 主线

### 不选“继续扩 workload 类型”

因为当前真正缺的不是：

- `job`
- `cron`
- `worker`

这种越来越多的对象种类，
而是把长期在线服务这条主链做扎实。

### 不选“继续自己搭 gateway”

因为：

- `gateway`
  不是 `v5`
  的核心创新点
- 更成熟的入口能力应该直接复用开源组件

### 不选“双 control-plane 同步”

因为这会立刻把主线拉成：

- 分布式控制面一致性

而不是：

- 多云 `CaaS`
  产品化

### 不选“把所有流量都统一成一套协议栈”

因为：

- 平台管理 API
  和用户业务流量
  不是同一类东西
- `grpc-gateway`
  适合平台 API
- 不适合做通用应用入口网关

### 不选“现在做支付 / 余额 / 账单”

因为：

- 当前真正缺的是：
  - 项目护栏
  - 外部统一入口
  - 双云完整验收
- 不是：
  - 付款主体
  - 余额系统
  - 充值
  - 账单

如果现在继续做这些能力，
会反过来把主线拉成：

- 平台内部财务模型

而不是：

- 多云 `CaaS`
  产品化

## 验证标准

`v5`
这一版如果做对了，
最后应该能比较自然地回答下面这些问题：

1. 用户是不是已经能把它当一个多云 `CaaS`
   来使用
2. 一个长期在线服务是不是已经能被：
   - 定义
   - 部署
   - 暴露
   - 发布
   - 回滚
   - 扩容
   - 观察
   - 归集到正确的项目和 owner
3. 一个服务是不是已经能同时部署到两个云的：
   - `service cell`
4. `control-plane`
   挂了以后，
   现网是不是仍然能继续跑
5. 主备切流是不是不依赖：
   - `control-plane`
6. 平台是不是已经从：
   - 自己搭很多基础件
   收敛到：
   - 核心自己写
   - 通用能力复用开源
7. 项目护栏、外部前门和双云完整验收
   是不是已经能作为同一套平台主线工作

如果这些问题都能回答“是”，
那 `v5`
就走在正确方向上了。
