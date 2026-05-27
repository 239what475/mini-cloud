# 12 v3 测试与加固

这一章不继续给 `v3` 增加新能力，
而是把 `11` 刚刚收口出来的新模型先稳住。

也就是说，
这一章的目标不是：

- 再接一个 provider
- 直接写 Terraform provider

而是：

- 把现在已经存在的主链补成“可持续回归”的状态

## 上一章检查点

- `v3/11`
  - `a663ae8f1ac9b0499a406f2dff2ea46d4e2dd13b`

## 这一章为什么现在就要做

`11`
刚做完一轮很大的资源模型收口：

- `project`
- `app`
- `revision`
- `deployment`
- `actions/*`

这时候最容易发生的问题不是“功能完全没有”，
而是：

- 接口看起来已经对了
- 但过几章一改代码，
  很容易又把边界打回去

所以 `12`
要先把下面这几条线锁住：

1. `project` 是完整 CRUD 资源
2. `app` 是声明式资源
3. `revision / deployment`
   是服务端自动生成和回读的状态对象
4. `retry / rollback / probe / issue-token`
   是动作接口
5. 本地日常检查入口要覆盖：
   - 根模块
   - 独立 tests 子项目
   - web 前端

## 这一章补了什么

### 1. `check.sh` 现在覆盖面更完整了

之前的：

- [check.sh](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/scripts/check.sh)

只覆盖了根模块的 Go 代码和 shell 语法。

这一章把它补成了真正的“日常快速检查”入口，
现在会依次跑：

1. 根模块：
   - `go test ./...`
2. 根模块：
   - `go vet ./...`
3. shell 脚本语法检查：
   - `bash -n ./scripts/*.sh`
4. 根模块：
   - `staticcheck ./...`
5. 独立真实云 tests 子项目：
   - `cd tests && go build ./...`
6. web 前端：
   - `cd web && npm run check`
   - `cd web && npm run build`

这里的意义很直接：

- 以前你只改 control-plane，
  可能不会顺手发现 `tests/`
  或 `web/`
  已经编译坏了
- 现在一条：
  - `./scripts/check.sh`
  就会把这三块一起扫一遍

### 2. 补了 `project` 完整生命周期回归

这一章新增了 `project` 的 CRUD 集成测试，
明确验证：

- `POST /api/v1/projects`
- `GET /api/v1/projects/{projectID}`
- `PUT /api/v1/projects/{projectID}`
- `DELETE /api/v1/projects/{projectID}`

这条测试不是为了“多一个接口示例”，
而是为了把 `11` 里定下来的边界锁住：

- `project`
  现在就是一个真正的声明式资源

对应测试在：

- [httpapi_integration_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/httpapi_integration_test.go)

### 3. 补了“元数据更新不触发 rollout”的回归

`11` 之后，
`app` 已经是完整期望资源。

但这并不意味着：

- 每一次 `PUT /api/v1/apps/{appID}`
  都必须生成新的 `revision`
  和新的 `deployment`

这一章新增了一条专门的回归测试，
验证的是：

- 如果只是更新不会影响运行规格的字段，
  例如：
  - `displayName`
- 那么响应里的：
  - `rolloutTriggered`
  应该是：
  - `false`
- 并且：
  - `revision`
  - `deployment`
  都不应重新生成

这条测试的意义很重要，
因为它直接锁住了：

- “声明式资源更新”
  和
- “需要 rollout 的规格变化”

之间的边界。

### 4. 补了 `actions/probe` 的集成测试

`11`
把探测动作正式收成了：

- `POST /api/v1/apps/{appID}/actions/probe`

但如果没有真正的集成测试，
后面很容易又把它改坏。

所以这一章补了一条完整验证：

1. 先创建 app
2. 在 backend 还没 ready 时，
   调 probe
   应返回：
   - `503`
3. 再让 execution 上报：
   - `running`
4. 把 backend 指到测试里临时起的：
   - `127.0.0.1:<port>/healthz`
5. 再调用 probe，
   应返回：
   - `successful = true`
   - `statusCode = 200`

这条测试把下面这条链锁住了：

- `current deployment`
- `current execution`
- `ResolveCurrentAppRoute`
- `actions/probe`

### 5. 这次真实回归确实抓出了集成环境问题

这一章不是只把脚本接起来。

把：

- `./scripts/test-integration.sh`

真正跑起来之后，
这次还实际暴露了几类只会在真实 Postgres 集成链路里出现的问题：

1. `00016`
   里的多个 `DO $$ ... $$;`
   语句需要加上：
   - `-- +goose StatementBegin`
   - `-- +goose StatementEnd`
   否则 goose 在真实迁移时会解析失败
2. `CreateApp`
   的 SQL 插入语句少了一个 `status` 占位，
   在真实数据库里会直接出错
3. 一些 `httpapi` 集成用例原来先建 app、后补 worker，
   这和现在已经收口好的调度语义不一致，
   所以测试场景也一起调整成了：
   - 先让可调度 worker ready
   - 再验证 app / revision / probe / gateway 这条主链

这也是为什么：

- `check.sh`
  不能替代
- `test-integration.sh`

前者能发现编译和静态问题，
后者才能把：

- migration
- SQL
- handler 到 store 的真实持久化链路

一起压一遍。

## 现在本地验证入口怎么分层

这一章之后，
`v3`
本地开发时可以先按下面这三层理解。

### 第一层：日常快速检查

最常用入口：

```bash
cd projects/mini-cloud
./scripts/check.sh
```

它现在负责：

- 根模块 Go 回归
- shell 脚本语法
- `tests/` 子项目编译
- `web/` 前端检查和构建

这条命令应该作为：

- 改完代码后的第一层默认检查

### 第二层：真实数据库集成测试

如果改动已经影响到：

- store
- migration
- HTTP API 持久化行为

继续跑：

```bash
cd projects/mini-cloud
./scripts/test-integration.sh
```

它会起本地 compose 里的 Postgres，
再跑：

- `internal/store`
- `internal/httpapi`

里的 `TestIntegration*`

### 第三层：本地主链 smoke

如果改动已经影响到：

- app / revision / deployment 主链
- node / work / execution 主链
- runtime 行为

继续跑：

```bash
cd projects/mini-cloud
./scripts/smoke.sh
```

## 本章实际验证

这一章完成时，
实际跑过的是：

```bash
cd projects/mini-cloud
./scripts/check.sh
./scripts/test-integration.sh
./scripts/smoke.sh
```

这条脚本会从零起一条最小闭环：

- Postgres
- control-plane
- worker register / heartbeat
- create project
- create app
- agent 领取 work
- app / deployment / execution 回读

## 这章没有做什么

这里也要明确边界。

这一章虽然名字叫：

- `tests-and-hardening`

但它没有去做下面这些事：

1. 还没有写真正的 Terraform provider
2. 还没有把真实阿里云 / 腾讯云黑盒测试重新封装成统一一键入口
3. 还没有做更重的全量回归脚本编排

这一章做的是更基础的一层：

- 先把当前已经存在的代码和接口边界锁住
- 让后面的 `13`
  在做 Terraform provider 时，
  不会踩着一套不稳定 API 往前走

## 本章涉及的文件

- [check.sh](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/scripts/check.sh)
- [httpapi_integration_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/httpapi_integration_test.go)

## 本章结论

`12`
做完以后，
`v3`
至少在下面这条线上更稳了：

- `project`
  的资源生命周期
- `app`
  的声明式更新边界
- `actions/probe`
  的真实路由与行为
- `check.sh`
  对根模块、tests 子项目、web 前端的统一快速检查

所以这章的价值不是“多了一个大功能”，
而是：

- 把 `11`
  刚收好的资源模型先锁住
- 给下一章的真正 Terraform provider 准备更稳定的地基

## 本章检查点

- `v3/12`
  - `3409e99d42185ed9dfdb3e58fd1b7d21308c99bd`
