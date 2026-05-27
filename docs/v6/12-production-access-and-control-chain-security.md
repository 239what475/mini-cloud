# 12 Production Access And Control Chain Security

这一章不接第二个生产
`plane`。

这一章先把第一套真实环境的“谁能进、
拿什么进、
什么事必须走 break-glass”
收干净。

到 `11`
结束时，
`tencent-platform-host`
上已经有了真实长期运行的：

- `control-plane`
- `cloud-plane`
- 首个固定 `agent`

但管理入口仍然偏教学期：

- 日常操作和紧急操作都还围着同一个
  `admin token`
- `control-plane`
  平台级主体只有：
  - break-glass
    管理口
  - `project token`
- 哪些接口属于“日常 operator”
  能做，
  哪些接口必须保留给
  break-glass，
  还没有被正式写死

所以这一章要做的，
不是接第二套真实环境，
而是先把第一套真实环境的访问链收成正式模型。

## 这一章要解决什么问题

当前最危险的点有三个：

1. 日常操作和紧急恢复没有分层
   - 如果长期把
     `ADMIN_TOKEN`
     当日常口令用，
     break-glass
     就失去意义
2. `control-plane`
   缺少平台级“日常主体”
   - `project token`
     只适合项目自动化
   - 不适合做平台 operator
     的长期入口
3. 控制链里的高风险动作没有单独隔离
   - 比如：
     - 签发新的平台级口令
     - 给
       `plane`
       写入新的 southbound
       token

这些动作都应该和普通的：

- 看控制面状态
- 改项目
- 推服务
- 处理 incident

明显分开。

## 这一章的正式目标

这一章结束后，
平台访问链固定成下面三层：

1. break-glass admin token
   - 只放在
     `control-plane`
     主机的正式配置里
   - 只负责高风险入口
2. platform service account
   - 由 break-glass
     管理口签发
   - 用于平台日常 operator
     访问
3. project token
   - 继续只服务项目级自动化

同时要明确固定：

- 哪些接口属于 break-glass
- 哪些接口属于平台级日常入口
- 哪些接口仍然是项目级入口

## 这一章明确不做什么

这一章不做：

- 第二个生产
  `plane`
- 更复杂的人类登录系统
- `OIDC / SSO`
- `mTLS / PKI`
- 前端权限中心

这些能力后面再说。

这一章先把当前已经真实跑起来的平台，
收成一套可信的最小生产访问模型。

## 这章采用的新访问模型

### 1. break-glass admin token

break-glass
token
继续存在，
但从这章开始，
它不再被当成日常操作凭证。

它只负责：

- 签发平台级
  `service account`
- 删除平台级
  `service account`
- 查看平台级
  `service account`
  清单
- 给某个
  `plane`
  写入 /
  轮转 southbound
  token

也就是说，
它正式变成：

- 紧急恢复口
- 高风险控制口

而不是“所有人平时都拿来调接口的总钥匙”。

### 2. platform service account

这章新增了
`control-plane`
自己的平台级
`service account`
模型。

它的特点是：

- secret
  只在签发时返回一次
- 数据库存的是 hash，
  不是明文
- 请求命中后会更新
  `last_used_at`
- `whoami`
  会明确返回：
  - 主体类型
  - 角色
  - `tokenPrefix`
  - 最近使用时间

这一层就是日常 operator
访问入口。

### 3. 角色边界

当前先固定四个角色：

- `platform_owner`
- `platform_admin`
- `platform_operator`
- `platform_auditor`

这章实际先把下面两类用清楚：

- `platform_owner`
  - 平台级高权限日常主体
  - 可以做：
    - 项目 owner
      变更
    - 项目自动化 token
      发放
- `platform_operator`
  - 可做跨项目运行态操作
  - 例如看控制面、
    看项目、
    发服务、
    回滚和排障
  - 不能碰 break-glass
    接口
  - 也不能做：
    - 项目创建 /
      删除
    - 项目 owner
      变更
    - 项目自动化 token
      发放
- `platform_auditor`
  - 只读
  - 能看控制面和项目面
  - 不能做写操作

`platform_admin`
在当前实现里介于
`owner`
和
`operator`
之间：

- 可以做平台和项目的一般管理变更
- 但不负责：
  - 项目 owner
    变更
  - 项目自动化 token
    发放

## 这章把哪些接口收成 break-glass

目前先把下面这些口固定为 break-glass：

- `GET /api/v1/control/service-accounts`
- `POST /api/v1/control/service-accounts`
- `DELETE /api/v1/control/service-accounts/{accountID}`
- `POST /api/v1/control/planes/{planeID}/actions/register`

这里的原则很简单：

- 只要会发新凭证、
  改控制链凭证，
  就不允许走日常 operator
  token

## 这章把哪些接口开放给平台级日常主体

除了 break-glass
口之外，
平台级主体现在可以按权限访问：

- `control.*`
  读写接口
- `project.*`
  读接口
- `operations.read`
  审计查看接口

更具体地说：

- `platform_owner`
  - 具备：
    - `project.write`
    - `project.deploy.write`
    - `project.api_token.write`
    - `project.ownership.write`
- `platform_admin`
  - 具备：
    - `project.write`
    - `project.deploy.write`
  - 不具备：
    - `project.api_token.write`
    - `project.ownership.write`
- `platform_operator`
  - 具备：
    - `project.deploy.write`
  - 不具备：
    - `project.write`
    - `project.api_token.write`
    - `project.ownership.write`
- `platform_auditor`
  - 只具备读权限

这让：

- 日常 operator
  可以继续看控制面、
  推服务、
  处理故障
- 但不能直接去发新的平台口令，
  也不能碰高敏感的项目身份边界

## 这一章的代码落点

这章新增或修改的关键代码在：

- [auth.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/api/auth.go)
  - 把平台级主体从“只有 admin token”
    收成：
    - break-glass admin
    - platform service account
    - project token
- [platform_service_account_handler.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/api/platform_service_account_handler.go)
  - 平台级
    `service account`
    的签发、
    列表、
    删除接口
- [router.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/api/router.go)
  - 把 break-glass
    路由和日常平台路由正式分开
- [operation_audit.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/api/operation_audit.go)
  - 审计里新增
    `service_account`
    actor
  - break-glass
    也会作为显式
    `break_glass`
    actor
    记入审计
- [serviceaccount.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/serviceaccount/serviceaccount.go)
  - 平台级
    `service account`
    角色和输入校验
- [platform_service_account_store.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/store/platform_service_account_store.go)
  - hash
    存储、
    列表、
    删除、
    `last_used_at`
    记录
- [00041_create_platform_service_accounts.sql](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/store/migrations/00041_create_platform_service_accounts.sql)
  - 平台级
    `service account`
    表结构

## 本地集成验证

这章新增了控制面集成验证，
重点覆盖：

- break-glass
  签发
  `platform service account`
- `platform_operator`
  的运行态权限边界
- `platform_admin`
  和
  `platform_owner`
  的高敏感权限差异
- `platform_auditor`
  的只读权限
- break-glass
  接口拒绝日常
  `service account`
- 审计里能记录
  `service_account`
  actor

对应测试：

- [integration_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/api/integration_test.go)
  - `TestIntegrationControlPlanePlatformServiceAccountAccessAndBreakGlass`

## 这章结束后的真实环境操作建议

从这章开始，
真实环境建议这样用：

1. 把 break-glass
   `ADMIN_TOKEN`
   只保留在：
   - `control-plane`
     主机的正式 env
   - 恢复文档 /
     密码库
2. 日常 operator
   先用一次 break-glass
   口签发自己的平台级
   `service account`
3. 日常脚本、
   本地运维命令、
   之后的自动化入口，
   都改用平台级
   `service account`
4. 只有在下面这些场景，
   才动用 break-glass：
   - 补发 /
     删除平台口令
   - 重写
     `plane`
     southbound
     token
   - 平台级恢复

## 这一章和后面几章的关系

这章做完以后：

- `12`
  先把入口和控制链访问边界收死
- `13`
  再继续做 destroy /
  teardown
  的可靠性
- `14`
  才去接第二个生产
  `plane`
  到阿里云

顺序不能反过来，
因为如果访问边界没收干净，
继续扩第二个真实
`plane`
只会让控制链变得更乱。

## 检查点

- 待本章提交时回填 commit hash。
