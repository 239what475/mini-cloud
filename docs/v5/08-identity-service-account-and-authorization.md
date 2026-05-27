# v5/08 Identity Service Account And Authorization

`v5/07`
已经把：

- 应用日志
- 指标
- trace

这条应用侧观测链收下来了。

但平台如果还说不清：

- 到底是谁在调用平台
- 人类用户怎么进入平台
- 机器身份怎么进入平台
- 平台管理员和项目拥有者怎么区分

那后面的：

- 项目治理
- 外部前门
- 完整平台验收

都会继续建立在一套模糊的身份语义上。

所以这一章要收的不是“再加一个登录页”，
而是把：

- 身份主体
- 多管理员模型
- 单 owner project 模型
- 平台级和项目级授权边界

一次收清楚。

## 这一章只收什么

这一章只收：

- `human_user`
  的进入方式
- `service_account`
  的统一模型
- `break_glass`
  的正式定位
- 多管理员模型
- `project.owner_user`
  这条项目归属关系
- 平台角色和项目归属之间的授权边界

这一章明确不做：

- 自助注册
- 团队 / 组织
- 多人共享同一个 `project`
- `project membership`
- 人类用户的项目内多档角色

`v5`
当前先故意把模型收窄，
避免平台在还没做厚之前，
就先把租户关系做复杂。

## 这一章最后收出的主体模型

这一章把 northbound 主体统一成 4 类：

- `anonymous`
  - 未认证请求
- `break_glass`
  - bootstrap 和 emergency 通道
- `human_user`
  - 人类用户
  - 由外部认证层给出稳定身份
- `service_account`
  - 机器身份
  - 用于 AI agent、CI、自动化脚本

这里最重要的一条收口是：

- 原来的项目级 `project token`
  不再被当成另一套独立身份体系
- 在平台内部统一表现为：
  - `service_account(scope=project)`

也就是说，
平台内部只认：

- `human_user`
- `service_account`
- `break_glass`

而不会再额外维护一套：

- “项目 token 世界”
- “service account 世界”

## 认证和授权的边界

这一章把边界固定成一句话：

- `human_user`
  认证由上游完成
- `service_account`
  和 `break_glass`
  由 `mini-cloud`
  自己校验
- 所有主体的授权统一由
  `mini-cloud`
  决定

人类用户认证不直接塞进
`mini-cloud`
内部做，
而是采用：

- 上游认证代理
- 受信认证代理头

也就是：

1. 上游认证代理先完成人类登录
   - `v5`
     当前冻结选择是：
     - `Authelia`
2. 再把稳定身份信息透传给
   `mini-cloud`
3. `mini-cloud`
   只做用户 upsert、
   平台角色判断、
   项目归属判断、
   机器身份判断

当前这条链的核心语义仍然是：

- 上游负责 `human_user`
  的“你是谁”
- 平台自己校验：
  - `service_account`
  - `break_glass`
- 平台统一负责所有主体的
  “你能做什么”

## 多管理员模型

这一章明确支持多管理员，
而不是只有一个超级管理员。

平台级角色固定为：

- `platform_owner`
  - 最高权限
  - 管身份危险变更
  - 管平台级角色授予
  - 管项目 owner 归属调整
- `platform_admin`
  - 日常平台管理
  - 管项目生命周期
  - 管平台级 service account
  - 可以处理大多数平台运维事务
- `platform_operator`
  - 运维型管理员
  - 适合 AI agent
  - 管发布、回滚、扩缩、节点运维、观测
- `platform_auditor`
  - 平台只读
  - 适合审计和观察

这里要刻意分清 3 件事：

- 认证系统自己的管理员
  - 管认证系统本身
- `platform_*`
  - 管 `mini-cloud`
    平台
- `break_glass`
  - 只做初始化和应急

所以：

- `break_glass`
  不是日常管理员角色
- `platform_admin`
  也不是认证系统管理员

## 项目归属模型

这一章不再采用：

- `project membership`
- 旧的人类项目三级角色模型

`v5`
当前先固定成下面这套更窄的关系：

```text
human_user --< project --< service

project --< service_account(scope=project)
platform --< service_account(scope=platform)

actor = human_user | service_account
owner = project.owner_user
```

也就是说：

- 一个 `project`
  固定只属于一个 `human_user`
- 一个 `project`
  可以有多个 `service`
- `service`
  属于 `project`
  不直接属于某个 `user`
- 项目级自动化身份继续存在，
  但它是：
  - `service_account(scope=project)`

这里最关键的一点是：

- `project owner`
  不是“项目内角色表里的一行”
- 它是 `project`
  资源本身的一条归属字段

也就是：

- `project.owner_user_id`

## 人类用户在项目内的权限怎样收口

既然不再做人类用户的旧三级项目角色

那么项目内的人类权限就收成非常直接的一条：

- 这个人是不是该项目的 `owner_user`

如果是，
它就能管理这个项目下的：

- 服务
- 发布
- 回滚
- 配置
- 密钥
- 镜像凭据
- 域名
- 项目级自动化身份

如果不是，
那它默认就没有这个项目的人类用户权限。

也就是说，
`v5`
当前的人类项目权限没有中间档位：

- 要么你是这个项目的 owner
- 要么你不是

## 平台角色怎样作用到项目

平台角色不需要再额外绑定项目，
因为它们天然就是跨项目能力。

当前默认边界收成下面这套：

- `platform_owner`
  - 可以管理任意项目
  - 可以调整项目 owner
  - 可以做身份危险变更
- `platform_admin`
  - 可以管理任意项目
  - 可以处理大多数跨项目平台运维工作
  - 不负责最高风险的身份边界变更
- `platform_operator`
  - 可以做跨项目的运行态操作
  - 例如发布、回滚、扩缩、排障、查看观测
  - 但不应该修改：
    - 项目 owner
    - 身份危险变更
    - 高敏感项目凭据边界
- `platform_auditor`
  - 可以跨项目只读
  - 看状态、日志、事件、观测
  - 不做变更

所以这一章收出的授权模型要按主体拆开理解：

- `human_user`
  - 看：
    - `platform_role`
    - `project.owner_user`
- `service_account`
  - 看：
    - `scope=platform`
    - `scope=project`
- `break_glass`
  - 只用于 bootstrap 和 emergency

## 项目级自动化身份为什么还要保留

虽然人类用户不再走项目成员关系，
但项目级自动化身份仍然必须保留。

原因很简单：

- 发布机器人
- CI
- AI agent
- 外部自动化脚本

都需要一个：

- 只绑定某个 `project`
- 不直接冒充人类用户

的机器身份。

所以这一章继续保留：

- `service_account(scope=project)`

而且项目级 `token`
在平台内部统一解释成：

- 项目级 `service_account`

这样后面的授权和审计语义就都干净了：

- 人类操作是 `human_user`
- 机器操作是 `service_account`
- 应急操作是 `break_glass`

## 人类用户怎样进入平台

当前流程应该固定成：

1. 上游认证代理先完成人类登录
2. 代理把受信身份头转发给
   `mini-cloud`
3. `mini-cloud`
   根据：
   - `issuer`
   - `subject`
   做用户 upsert
4. 首次进入平台的人类用户，
   默认只会被“识别”
5. 后续是否有平台权限，
   取决于：
   - 是否被授予 `platform_*`
6. 后续是否有项目权限，
   取决于：
   - 是否是某个 `project.owner_user`

所以“首登即有权限”
不是默认行为。

首登只会完成两件事：

- 让平台认识这个人是谁
- 让后续角色授予或项目归属绑定
  有稳定目标

## 首个管理员怎样绑定

平台初始化流程固定成：

1. 平台第一次启动时配置
   `MINICLOUD_ADMIN_TOKEN`
2. 配好上游认证代理
3. 让第一个人类管理员先登录一次
4. 用 `break_glass`
   查询待认领用户
5. 用 `break_glass`
   把这个人绑定成：
   - `platform_owner`
     或
   - `platform_admin`
6. 从这一步开始，
   日常管理就改走：
   - 人类用户
   - 平台级 `service_account`

所以：

- `break_glass`
  是 bootstrap 和 emergency 通道
- 不是正常管理入口

## 项目 owner 应该怎样绑定

既然 `project`
固定只有一个 owner，
那项目归属就必须成为一等信息。

这一章之后，
项目 owner 的来源应当只有两种：

1. 创建项目时显式指定
   `owner_user`
2. 后续由：
   - `platform_owner`
   - `platform_admin`
   做 owner 转移

而不是：

- 给某个用户补一条成员关系
- 再由成员关系解释成 owner

这里要特别强调：

- owner 归属变化
  是敏感操作

因为它不仅影响：

- 授权
- 审计归属
- 后面的项目级护栏判断

## 当前 northbound 接口应该围绕什么展开

这一章真正需要的 northbound 能力，
应该围绕下面这些最小接口展开：

- `GET /api/v1/auth/whoami`
  - 查看当前主体
- `GET /api/v1/auth/users`
  - 列出平台已识别的人类用户
- `PUT /api/v1/auth/users/{userID}/platform-role`
  - 设置人类用户的平台级角色
- `GET /api/v1/platform/service-accounts`
  - 列出平台级 `service_account`
- `POST /api/v1/platform/service-accounts`
  - 发行平台级 `service_account`

项目 owner 归属和项目级机器身份，
虽然和这一章的语义强相关，
但它们分别应当由：

- 项目资源接口
- 项目级自动化身份接口

去承载，
而不是继续靠：

- 项目成员关系接口

来兜底。

## `WhoAmI` 应该返回什么

做完这一章之后，
`WhoAmI`
至少应该稳定返回：

- 当前主体类型
- 当前主体的平台级角色
- 当前主体绑定的项目上下文
  - 对 `human_user`
    来说，
    应该是它拥有的项目列表
  - 对项目级 `service_account`
    来说，
    应该是它绑定的单个
    `projectID`
- 当前主体有效权限列表

例如项目级机器身份可以表现成：

```json
{
  "authenticationEnabled": true,
  "principal": {
    "kind": "service_account",
    "serviceAccount": {
      "id": "psa_123",
      "name": "deploy-bot",
      "scope": "project",
      "source": "project_token",
      "projectID": "prj_123"
    }
  },
  "platformRole": "",
  "permissions": [
    "project.read",
    "project.write",
    "service.read",
    "service.write",
    "service.operate",
    "operations.read"
  ]
}
```

这里最关键的是：

- `project token`
  不再在 `WhoAmI`
  里表现成独立 principal kind
- 它统一表现为：
  - `service_account`

## 这一章之后，审计里的 actor 也要收口

这一章不仅是身份识别，
也是审计语义收口。

后面的操作审计里，
应该明确区分：

- `break_glass`
- `human_user`
- `service_account`

这样后面看操作历史时，
就可以清楚区分：

- 是某个人做的
- 是某个 AI agent 做的
- 还是应急入口做的

## 这一章做完后，平台边界怎样变化

做完这一章之后，
平台终于不再只是：

- 一个带 `admin token`
  的单管理员实验品

而是正式具备了：

- 多管理员
- 人类用户
- 平台级机器身份
- 项目级机器身份
- 单 owner project 归属模型
- 平台和项目两层授权边界

同时，
它也明确放弃了 `v5`
当前不需要的复杂度：

- 多人共享项目
- 人类项目内多档角色
- 复杂租户模型

这才是后面继续做：

- 项目治理
- 外部前门
- 完整平台验收

之前应该先打牢的一层地基。

## 本章检查点

如果这一章做对了，
你现在应该已经能稳定回答下面这些问题：

1. 为什么：
   - `human_user`
   - `service_account`
   - `break_glass`
   不能混成一种 actor
2. 为什么：
   - platform scope
   和：
   - project scope
   的机器身份必须分开
3. 为什么 `project token`
   最后要被收编成：
   - `service_account(scope=project)`
4. 为什么：
   - 多管理员
   和：
   - 单 owner project
   可以同时成立
5. 为什么这一章必须真的补一条本地身份 `e2e`，
   而不是只停在：
   - API
   - store
   - 文档模型

如果这些问题都已经能稳定回答，
那 `v5/08`
的身份、授权和项目归属边界就算真正立住了。

对应提交：

- `3ddffbc9cab2a6adb549f3e62d19052c76a7bbf8`
