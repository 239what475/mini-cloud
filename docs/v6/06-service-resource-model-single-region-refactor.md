# 06 Service Resource Model Single Region Refactor

这一章做两件彼此绑定的事：

- 把 `service` 收回成单 provider、单 region 的长期在线服务
- 把当前有效设计里已经失去独立含义的 `fleet` 命名彻底移除

这不是两个分离任务。

前半章解决的是资源模型漂移，后半章解决的是语义漂移。
如果 `service` 已经不再是“跨地域服务模板”，那么 northbound API、控制面权限、指标、测试和脚本也不应该继续把自己叫做 `fleet`。

## 本章完成后的平台语义

到这一章结束，平台主语应该重新变得简单：

- `project`
  - 项目隔离与配额边界
- `service`
  - 单 provider
  - 单 region
  - 长期运行
  - 自己携带 placement / rollout / readiness / health
- `front door`
  - 独立的入口对象
  - 引用多个 `service` backend
  - 负责入口路由与健康门控
- `control`
  - control-plane 对全局 plane、inventory、incident、operation history 的 northbound 管理语义

也就是说：

- 阿里云北京的一套服务，是一个 `service`
- 腾讯云北京的一套服务，是另一个 `service`
- 如果它们要共用入口，那是 `front door` 的职责
- control-plane 对多个 cloud-plane 的全局管理接口，统一叫 `control`

## 为什么现在要去掉 `fleet`

随着 `v6/02-05` 的重构推进，当前实现里已经明确只剩下这几层：

- `control-plane`
- `cloud-plane`
- `node` / `agent`
- `service`
- `front door`

这时再保留 `fleet` 会出现三个问题：

- 它不再对应一个独立资源层，只是旧命名残留
- 用户会误以为还有一层单独的 “fleet object” 或 “fleet service”
- northbound API 和内部包名会继续泄漏已经被抛弃的旧模型

所以这一章把它收掉，统一成更直观的词：

- `fleet service` -> `service`
- `fleet service controller` -> `service controller`
- `fleet plane` -> `plane`
- `fleet inventory` -> `control inventory`
- `fleet incidents` -> `control incidents`
- `/api/v1/fleet/*` -> `/api/v1/control/*`
- `control.read` / `control.write` 作为 control-plane 管理权限

## 本章追加任务

除了前面已经完成的单地域 `service` 模型收敛，这一章再明确补做下面这组清扫任务：

1. 清掉当前 Go 包和类型中的 `fleet`

- `internal/controlplane/service/`
- `internal/controlplane/servicecontroller/`
- `internal/controlplane/plane/`
- `internal/controlplane/inventory/`
- `internal/controlplane/incident/`

2. 清掉 northbound 管理语义中的 `fleet`

- 管理路由统一挂到 `/api/v1/control/*`
- 指标端点统一为 `/metrics/control`
- 权限常量统一为 `control.read` / `control.write`
- auth scope 统一为 `control`

3. 清掉控制面测试、脚本、模板里的 `fleet`

- 集成测试名称
- 双云 E2E 日志和断言
- user-data 注释
- 运维脚本注释

4. 保持数据库迁移风险受控

- 本章不把旧 SQL 表名和旧 migration 文件名一起改掉
- 否则这一章会从“语义和资源模型重构”膨胀成“数据库重命名迁移章”
- 当前要求是：对外语义、Go 代码语义、API 语义、权限语义不再暴露 `fleet`

## 这一章影响的代码面

- [service.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/service/service.go)
- [controller.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/servicecontroller/controller.go)
- [reconcile.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/servicecontroller/reconcile.go)
- [service_handler.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/api/service_handler.go)
- [router.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/api/router.go)
- [auth.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/api/auth.go)
- [control_handler.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/api/control_handler.go)
- [inventory.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/inventory/inventory.go)
- [plane.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/plane/plane.go)
- [plane_store.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/store/plane_store.go)
- [service_store.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/store/service_store.go)
- [run.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/tests/dual/run.go)

## 这一章结束后的 northbound 契约

用户面继续围绕 `project -> service`：

- `POST /api/v1/projects/{projectID}/services`
- `PUT /api/v1/projects/{projectID}/services/{serviceID}`
- `DELETE /api/v1/projects/{projectID}/services/{serviceID}`

control-plane 管理面统一围绕 `control`：

- `GET /api/v1/control/planes`
- `GET /api/v1/control/inventory`
- `GET /api/v1/control/incidents`
- `GET /api/v1/control/operations`

这样 northbound 契约就不会再同时出现：

- 一套 `project/service`
- 另一套语义不清的 `fleet`

## 这一章完成后的理解方式

做到这里以后，平台应该可以这样理解：

- `service` 是部署和发布主语
- `front door` 是入口与流量主语
- `control` 是 control-plane 的全局管理主语
- `plane` 是被 control-plane 纳管的云内控制面

这比继续保留 `fleet` 更干净，也更符合后面继续演进多云 CaaS 时的语义边界。

## 检查点

- 提交：`7b56b9c828fd69e2f9d0ea4384bb14f35a800fc9`
