# 11 Complete Test Suite And Smoke Tests

这一章给 `mini-cloud v1` 补上真正可复现的测试面。

前面的 `01` 到 `10`
已经把：

- control-plane
- node agent
- scheduler
- release / deployment
- runtime execution
- ingress
- observability
- failure retry
- usage / quota

这些能力一段一段串起来了。

但如果到了这个阶段还只有：

- “我手工点一遍页面看看”
- “我临时跑几个 curl 试试”

那这个项目其实还没有真正进入：

- 可回归
- 可验证
- 可收尾

的状态。

所以 `11`
这一章只做一件事：

- 把 `v1` 的测试矩阵补成一个清晰、稳定、能重复执行的三层结构

## 这一章先记一个核心取舍

这一章故意没有把所有测试都塞进：

- `go test ./...`

原因很简单：

- 纯逻辑单测应该足够轻
- 不应该强制依赖 Docker
- 不应该强制依赖本地 Postgres

所以这一章把测试拆成三层：

### 第一层：轻量检查

- `./scripts/check.sh`

这一层继续负责：

- `go test ./...`
- `go vet ./...`
- `staticcheck ./...`

它的目标是：

- 让你在日常改代码时快速获得反馈

### 第二层：真实 Postgres 集成测试

- `./scripts/test-integration.sh`

这一层会：

- 起本地 `Postgres`
- 给每个测试建临时数据库
- 跑 store / API 集成测试
- 结束后自动清理

它的目标是：

- 真正验证数据库迁移
- 验证 store 落库逻辑
- 验证 HTTP API 和 store 的组合

### 第三层：主链 smoke

- `./scripts/smoke.sh`

这一层会把最关键的一条主链完整跑通：

1. 起本地 `Postgres`
2. 启动 control-plane
3. 用 `cmd/agent` 注册节点
4. 发送 heartbeat
5. 创建项目
6. 做一次 usage preview
7. 创建 app
8. 提交 release
9. 让 agent 拉取 work 并启动容器
10. 回读 app / deployment / execution 状态
11. 回读 usage 汇总

它的目标是：

- 确认 `v1` 的关键主链真的还能从头跑到尾

## 这一章实际新增了什么

### 1. 纯逻辑测试补齐了一些关键输入面

新增了：

- `internal/common/project/project_test.go`
- `internal/cloudplane/app_test.go`

现在会显式检查这些点：

- 项目 quota 默认值
- quota 局部覆盖
- 非法 quota 拒绝
- `instanceClass -> cpu/memory` 映射
- `v1` 当前仍然禁止 `replicas > 1`

### 2. 增加了统一的 Postgres 测试辅助

新增了：

- `internal/testutil/postgres.go`

这个辅助做的事情很直接：

1. 从环境变量读：
   - `MINICLOUD_TEST_DATABASE_URL`
2. 生成一个随机测试数据库名
3. 连接管理库
4. 创建临时数据库
5. 跑 migrations
6. 把：
   - `*sql.DB`
   - `*store.Store`
   返回给测试
7. 测试结束后：
   - 终止连接
   - 删除临时数据库

所以这一章不是在共享一个“脏测试库”反复跑，
而是每次都建干净的临时库。

### 3. store 层有了真实落库测试

新增了：

- `internal/store/store_integration_test.go`

当前重点测两件事：

- `CreateProject`
  - quota 是否真的持久化到了数据库
- `CreateApp`
  - quota 超限时是否真的被后端拒绝

### 4. API 层有了真实 HTTP 集成测试

新增了：

- `internal/httpapi/httpapi_integration_test.go`

这组测试会真起一个：

- `httptest` server

然后走真实 HTTP 请求，去验证：

- 创建项目
- preview app usage
- 创建第一个 app
- usage 汇总回读
- 第二次 preview 被拒绝
- 第二次创建 app 返回 `409`

### 5. 新增了两个脚本

新增了：

- `scripts/test-integration.sh`
- `scripts/smoke.sh`

这两个脚本都做了两件很重要的事：

- 自己起本地 `docker compose` 里的 `Postgres`
- 结束时自动清理本地测试现场

所以它们不是：

- “跑完之后还要你手工收尸”

## 为什么集成测试要用环境变量显式启用

虽然 `go test ./...`
也会编译这些 integration test 文件，
但当没有设置：

- `MINICLOUD_TEST_DATABASE_URL`

时，
这些测试会直接：

- `Skip`

这样做的目的是：

- 保持日常 `go test ./...` 足够轻
- 又让真正需要时可以明确开启真实数据库测试

也就是说，
这一章之后你可以这样理解：

- `go test ./...`
  - 负责轻量回归
- `./scripts/test-integration.sh`
  - 负责真实数据库集成
- `./scripts/smoke.sh`
  - 负责关键主链冒烟

## smoke 这一章到底在证明什么

`scripts/smoke.sh`
证明的不是“每个功能都覆盖到了”，
而是：

- 最关键的一条业务主链没有断

它验证的是下面这条闭环：

- agent 可以注册节点
- heartbeat 可以把节点变成 `ready`
- 项目 quota 能生效
- app 可以先 preview 再创建
- release 提交后能生成 deployment
- scheduler 能选中节点
- agent 能领到 execution work
- runtime 能把容器拉起来
- 健康检查通过后：
  - execution 变 `running`
  - deployment 变 `running`
  - app 变 `running`
- usage 汇总会同步反映这个 app

这条链路一旦断了，
`v1` 就还不能算真的稳定。

## 一个值得注意的小现象

这一章的真实 smoke 输出里，
`nginx` 容器第一次健康检查并不是立刻成功的。

我这次实际跑出来的现象是：

- 第一次：
  - `connection reset by peer`
- 第二次：
  - `200`

这其实是很正常的：

- 容器刚启动
- 端口已经映射出来
- 但应用还在起来

所以这一章的 agent 健康检查本来就设计成：

- 多次尝试
- 中间带间隔

这比“只试一次”更接近真实平台行为。

## 这一章怎么跑

在：

- `projects/mini-cloud/`

目录下执行。

### 1. 轻量检查

```bash
./scripts/check.sh
```

### 2. 真实 Postgres 集成测试

```bash
./scripts/test-integration.sh
```

### 3. 主链 smoke

```bash
./scripts/smoke.sh
```

## 我这次实际跑通的结果

这次 `11`
我实际跑过了：

- `./scripts/check.sh`
- `./scripts/test-integration.sh`
- `./scripts/smoke.sh`

其中 smoke 的关键结果是：

- 节点成功从：
  - `registering -> ready`
- release 成功生成：
  - placement
  - deployment
  - execution
- agent 成功把：
  - `nginx:1.27-alpine`
  拉起来
- 最终回读时：
  - `app.status = running`
  - `currentDeployment.status = running`
  - `currentExecution.status = running`
- usage 汇总里：
  - `appsUsed = 1`
  - `runningApps = 1`

也就是说，
当前 `v1`
已经不只是“代码能编译”，
而是已经具备了：

- 可回归的轻量检查
- 可复现的真实数据库集成测试
- 可复现的主链 smoke

## 到这里，`v1` 的状态发生了什么变化

做完这一章以后，
`mini-cloud v1`
就从：

- “功能已经很多，但验证还偏手工”

进入到了：

- “开始具备正式收尾条件”

所以下一章 `12`
就可以更聚焦地做：

- 错误处理收口
- 小毛刺修正
- 文档统一整理

## 本章检查点

- commit:
  - `9e8c325fd8ccec6baaadf71cb736df574e27469f`
- 状态：
  - `v1` 已经具备轻量检查、真实 Postgres 集成测试和关键主链 smoke 三层验证
