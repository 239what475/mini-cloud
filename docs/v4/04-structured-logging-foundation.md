# v4/04 Structured Logging Foundation

这一章先做一件很具体的事：

- 把 `mini-cloud` 里最关键的运行日志
  先统一成一套可以串起来看的结构化字段

这里先明确边界：

- 这一章不做日志聚合
- 不接 `Loki`
- 不改 access log / audit event 的数据库 schema
- 也不为了日志去改 provider / store 的核心接口

也就是说，
`v4/04`
要先解决的是：

- 一条请求进来以后，
  control-plane、fleet、agent、gateway、runtime worker 这些日志，
  能不能靠统一字段串起来看

而不是：

- 日志最终存到哪里
- 怎么全文检索
- 怎么做 retention

这些留给：

- `v4/05`

## 先分清三类记录

这章很容易学乱，
因为仓库里现在本来就有三类“看起来都像日志”的东西。

### 1. 进程运行日志

这是这章的主角。

它们来自：

- `control-plane`
  的 `slog`
- `agent`
  的 `slog`
- `gateway`
  的 `slog`
- runtime worker / Docker runtime 相关的 `slog`

虽然当前默认输出是：

- `key=value`
  的文本格式

但它本质上已经是：

- 结构化字段日志

不是手拼的一整句字符串。

可以直接看这两个入口：

- [main.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/cmd/control-plane/main.go)
- [main.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/cmd/agent/main.go)

### 2. gateway access log

这类记录会落到数据库里的：

- `gateway_request_events`

它描述的是：

- 某次入口请求最终打到了哪个 backend
- 返回了什么状态码
- 花了多少时间

相关代码在：

- [gateway_handler.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/gateway_handler.go)
- [observability_store.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/store/observability_store.go)

### 3. 操作审计事件

这类记录会落到数据库里的：

- `operation_events`

它回答的是：

- 谁
- 对哪个资源
- 做了什么动作

相关代码在：

- [operation_audit.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/operation_audit.go)
- [operation_history_store.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/store/operation_history_store.go)

这里先记住：

- `v4/04`
  主要统一的是：
  - 进程运行日志
- access log 和 audit event
  先继续保持现有模型
- 但文档里会把它们和运行日志的关系讲清楚

## 这章真正统一了哪些字段

这章没有要求：

- 每条日志都同时带上所有业务 ID

因为那样会出现大量空字段，
反而更乱。

这里更合理的做法是分两层。

### 第一层：关联字段

这层字段负责把一条链路串起来：

- `request_id`

有一个字段要单独说明：

- `plane_id`

它不是“所有本地日志都一定会带的全局平台 ID”。

在这一章里，
`plane_id`
只用于：

- fleet 相关日志里指代“当前正在操作的受管 plane”

而本地 control-plane / agent 自己的稳定身份，
我们改成单独用：

- `platform_name`

这里的 `request_id`
要单独强调一下：

- 它表示“一次 HTTP 请求的关联键”
- 不是某条数据库事件自己的主键

### 第二层：资源上下文字段

这层字段负责说明这条日志在说哪个对象：

- `project_id`
- `app_id`
- `deployment_id`
- `node_id`
- `worker_id`
- `execution_id`

注意这里有两个容易混的词：

- `node_id`
  是平台里的节点对象 ID
- `worker_id`
  是 runtime worker 记录 ID

它们不是一回事。

## 这一章怎么实现

这章新增了一层很小的日志上下文工具：

- [logctx.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/common/logctx/logctx.go)

它只做两件事：

1. 在 `context.Context` 里保存这些关联字段
2. 根据上下文字段生成带固定字段的 `slog.Logger`

这层故意做得很小，
因为这章的目标不是再造一套日志框架，
而是先把：

- `request_id`
- `plane_id`
- 资源 ID

真正穿进现有链路。

## `request_id` 是怎么进来的

control-plane 现在会在最外层 HTTP middleware 做这件事：

1. 先看请求头里有没有：
   - `X-Request-ID`
2. 如果有：
   - 直接沿用
3. 如果没有：
   - 自动生成一个
4. 把它回写到响应头：
   - `X-Request-ID`
5. 再把它塞进请求上下文，
   让后面的 handler / service / gateway 日志都能拿到

相关代码在：

- [router.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/router.go)

这一层同时还补了原来缺的：

- `status_code`

所以现在最外层请求日志至少会统一带上：

- `request_id`
- `method`
- `path`
- `host`
- `remote_addr`
- `status_code`
- `duration_ms`

而像：

- `platform_name`
- `provider`
- `region`

这类“本地平台本身的静态身份字段”，
则由进程启动时构造的基础 logger 统一带上。

## `plane_id` 在这一章怎么理解

这一章把两个概念拆开了：

- `platform_name`
  表示“当前这套本地 mini-cloud control-plane 自己叫什么”
- `plane_id`
  表示“fleet 里那条受管 plane 记录的 ID”

这样做是为了避免一个字段同时表示两种对象：

- 本地平台自己
- 远端受管 plane

现在的规则是：

- 本地 control-plane / agent / 常规 HTTP 请求日志
  主要带：
  - `platform_name`
- fleet register / sync / incident 这类明确在操作某个受管 plane 的日志
  再额外带：
  - `plane_id`

## 关键链路 1：HTTP 请求到 control-plane

这条链路现在是：

```text
HTTP request
  -> requestLogger 注入 request_id
  -> auth / handler
  -> service
  -> 统一的 http request 日志
```

关键代码在：

- [router.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/router.go)
- [log_helpers.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/log_helpers.go)

这里的重点不是“多打一条日志”，
而是：

- 同一个 HTTP 请求里的 handler 和 service
  现在能共享同一个 `request_id`

## 关键链路 2：fleet register / sync

`fleet` 相关日志最重要的资源字段是：

- `plane_id`

这章把它补到了这些关键路径：

- [fleet_handler.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/fleet_handler.go)
- [service.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/planesync/service.go)

这样一来，
你在看：

- fleet API handler 错误
- fleet sync service 错误

时，
至少能先回答：

- 到底是哪一个 plane 出了问题

## 关键链路 3：app deployment 到 runtime scale-out

这一章还把发布链路上的资源字段补齐了。

重点是：

- `project_id`
- `app_id`
- `deployment_id`
- `worker_id`
- `node_id`

关键代码在：

- [app_handler.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/app_handler.go)
- [service.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/deployment/service.go)
- [service.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/runtimeworker/service.go)

这样当一次发布触发自动扩容时，
日志已经能更自然地回答：

- 是哪个项目的哪个 app
- 哪次 deployment
- 创建了哪个 runtime worker
- 最终回到了哪个 node

## 关键链路 4：agent 到 control-plane

如果只有 control-plane 自己生成 `request_id`，
还有一个问题没解决：

- agent 侧日志和 control-plane 侧日志还是串不起来

所以这章又补了一步：

- agent 发起 HTTP 请求时，
  也会主动带上：
  - `X-Request-ID`

这样控制面就能直接沿用 agent 侧带来的请求关联键。

相关代码在：

- [client.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/agentclient/client.go)
- [managed.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/cmd/agent/managed.go)

同时，
worker 启动脚本也会把本地平台名字传给 agent，
这样 agent 自己的运行日志也会稳定带上：

- `platform_name`

相关代码在：

- [provisioner.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/provider/aliyun/provisioner.go)
- [provisioner.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/provider/tencent/provisioner.go)

## 关键链路 5：agent 到 Docker runtime

agent 在执行 work item 时，
现在也会把这些字段继续往下传：

- `node_id`
- `app_id`
- `deployment_id`
- `execution_id`

所以 Docker runtime 自己的日志不再只是：

- 拉了哪个镜像
- 建了哪个容器

而是还能知道：

- 这是哪次 execution 在做这件事

关键代码在：

- [managed.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/cmd/agent/managed.go)
- [docker_engine.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudworker/runtime/docker_engine.go)

## 这章特意没做什么

这章故意没做下面这些事：

- 不改 `gateway_request_events` 表结构
- 不改 `operation_events` 表结构
- 不把 bootstrap shell 日志也硬塞进同一套模型
- 不把云厂商 SDK 调用层也硬塞进这一章
- 不接 `Loki`
- 不做日志查询 API

原因是现在更重要的是先把：

- 运行链路里的结构化字段

打通。

如果这一层都还没稳定，
后面再做聚合和检索只会更乱。

这也意味着当前：

- 对于同一次 HTTP 请求，
  `request_id`
  已经能把这条请求里的进程运行日志串起来
- 但像 agent 的 work poll、readiness check、execution report
  这种跨多次 HTTP 请求的长链路，
  更稳定的关联键仍然是：
  - `node_id`
  - `app_id`
  - `deployment_id`
  - `execution_id`
- 但数据库里的 access log / audit event
  还没有把 `request_id` 持久化进去

这是这一章刻意保留的边界，
不是遗漏。

## 一个小但重要的顺手修正

control-plane 启动日志里，
这章顺手去掉了直接打印：

- `database_url`

因为这类字段不适合继续留在默认启动日志里。

相关代码在：

- [main.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/cmd/control-plane/main.go)

## 手动试一下

先验证：

- 请求头里的 `X-Request-ID`
  会被沿用并回写到响应头

```bash
curl -i http://127.0.0.1:8080/api/healthz \
  -H 'X-Request-ID: req-demo-001'
```

你应该能在响应头里看到：

- `X-Request-ID: req-demo-001`

如果不手动传，
服务端也会自动生成一个：

```bash
curl -i http://127.0.0.1:8080/api/healthz
```

如果是在真实 worker 上看 agent 日志，
可以直接看：

```bash
journalctl -u mini-cloud-agent -n 50 --no-pager
```

## 本章涉及的主要文件

- [logctx.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/common/logctx/logctx.go)
- [router.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/router.go)
- [log_helpers.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/log_helpers.go)
- [gateway_handler.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/gateway_handler.go)
- [app_handler.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/app_handler.go)
- [fleet_handler.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/fleet_handler.go)
- [node_handler.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/node_handler.go)
- [runtime_worker_handler.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/runtime_worker_handler.go)
- [service.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/planesync/service.go)
- [service.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/deployment/service.go)
- [service.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/runtimeworker/service.go)
- [client.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/agentclient/client.go)
- [managed.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/cmd/agent/managed.go)
- [docker_engine.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudworker/runtime/docker_engine.go)
- [client_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/agentclient/client_test.go)
- [httpapi_integration_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/httpapi_integration_test.go)

## 本章检查点

- 本地 control-plane 与 agent 的基础日志统一带上了：
  - `platform_name`
- HTTP 请求日志统一带上了：
  - `request_id`
  - `status_code`
- fleet 相关日志在操作受管 plane 时会显式补上：
  - `plane_id`
- agent 发往 control-plane 的请求会透传：
  - `X-Request-ID`
- access log / audit event 仍保持原有数据库 schema，
  不在这一章扩表
