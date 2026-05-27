# v5/02 Service Spec And Management API

`v5/01`
已经把产品边界冻结住了：

- `mini-cloud`
  是一个多云 `CaaS`
- 只做长期在线 `service`
- `revision / deployment / execution`
  不应该继续直接长成用户主对象

所以这一章要做的事情很明确：

- 把用户看到的北向管理 API
  正式收成 `service`
- 把 `service spec`
  和 `service status`
  分开
- 把 `revision / deployment`
  压回内部实现和只读历史视图
- 把当前 `cloud-plane`
  的 management API
  真正切到：
  - `proto`
  - `gRPC`
  - `grpc-gateway`
- 把 transport
  分层收干净：
  - `server`
  - `statichttp`
  - `gateway`
  - `api`
- 把 `service`
  生命周期编排
  从 `api`
  下沉到：
  - `servicelifecycle`
- 让 Terraform provider
  也同步消费这套契约

这一章不做：

- 改 rollout engine
  的核心语义
- 改 scheduler
  和 scale-out
  语义
- 改用户流量入口
  这条数据面能力

也就是说：

- 这章不只把 northbound
  契约收成 `service`
- 还把 `cloud-plane`
  整条 management API
  收成单轨
- 同时把 service
  相关 application/usecase
  从 transport
  里抽出来
- 但内部执行机制本身先保持不动

这里的 management API
明确指的是：

- `cloud-plane`
  当前暴露给：
  - Web
  - Terraform provider
  - 实验测试程序
    的 northbound
    接口
- `fleet -> plane`
  的 plane
  管理接口
- `plane -> worker`
  的 worker
  管理接口

## 这一章先解决什么问题

在这一章之前，
cloud-plane 的 northbound
和其他 management
接口本质上还是：

- 路径叫 `apps`
- 响应包裹还是旧的资源对象
- 创建 / 更新结果里直接带：
  - `revision`
  - `deployment`
  - `placementDecision`

这会导致两个问题：

1. 用户对象不稳定
   - 旧主对象
     混合了：
     - 期望规格
     - 当前 promoted revision 指针
     - 运行状态
2. 发布实现细节泄露
   - `revision`
   - `deployment`
   - `execution`
     都直接露给用户接口
3. management API
   的路径所有权不够硬
   - public HTTP
   - gateway data plane
   - management API
     没有彻底分层
   - 容易出现：
     - 路由写了
     - 但实际上不生效
4. `api`
   包里混了共享业务编排
   - create
   - update
   - apply
   - retry
   - rollback
     这些 service usecase
     不该继续挂在
     transport
     下面

`v5/02`
的目标就是把这条边界收回来。

## 这一章之后，用户主对象是什么

从这一章开始，
用户主对象只保留：

- `service`

一个 `service`
对外只分两块：

1. `spec`
2. `status`

### `spec`

`spec`
表示用户声明的期望状态。

当前第一版最小字段是：

- `region`
- `replicas`
- `instanceClass`
- `image`
- `command`
- `args`
- `defaultPort`
- `readinessPath`
- `env`
- `secretSetID`
- `registryCredentialID`

### `status`

`status`
表示平台当前观测到的服务状态。

当前第一版最小字段是：

- `phase`
- `healthy`
- `message`
- `currentRelease`

这里的：

- `currentRelease`

只暴露：

- `id`
- `label`

它只表示：

- 当前已经 promoted 的发布快照摘要

它不再直接把整份 `revision`
对象塞进主响应里。

## 什么继续保留在内部

这一章并没有否认：

- `revision`
- `deployment`
- `execution`

这些对象的重要性。

相反，
它们仍然是当前运行时真实依赖的内部机制：

- `revision`
  是不可变发布快照
- `deployment`
  是一次 rollout / reconcile 尝试
- `execution`
  是节点上的实际执行记录

但从这一章开始，
它们不再是 `service` 主响应里的默认字段。

更具体地说：

- `GET /api/v1/services/{serviceID}`
  不再直接返回完整 `revision / deployment / execution`
- `POST /api/v1/projects/{projectID}/services`
  不再直接返回：
  - `revision`
  - `deployment`
  - `placementDecision`

如果用户需要看发布历史或部署历史，
仍然可以走只读子资源：

- `GET /api/v1/services/{serviceID}/revisions`
- `GET /api/v1/services/{serviceID}/deployments`

这就把：

- 主对象
  和
- 执行内部细节

分开了。

## 新的 management API 契约形态

这一章之后，
cloud-plane 的 management API
不再以“手写 REST”
作为正式边界，
而是统一改成：

- `proto`
  定义：
  - `ProjectService`
  - `ServiceService`
  - `PlaneService`
  - `WorkerControlService`
- `gRPC`
  作为正式 RPC
- `grpc-gateway`
  从同一份 `proto`
  暴露 `HTTP/JSON`

也就是说，
HTTP 路径还会继续存在：

- `GET /api/v1/projects/{projectID}/services`
- `POST /api/v1/projects/{projectID}/services`
- `GET /api/v1/services/{serviceID}`
- `PUT /api/v1/services/{serviceID}`
- `DELETE /api/v1/services/{serviceID}`
- `GET /api/v1/services/{serviceID}/domains`
- `POST /api/v1/services/{serviceID}/domains`
- `GET /api/v1/services/{serviceID}/revisions`
- `GET /api/v1/services/{serviceID}/deployments`
- `POST /api/v1/services/{serviceID}/actions/retry`
- `POST /api/v1/services/{serviceID}/actions/rollback`
- `POST /api/v1/services/{serviceID}/actions/probe`
- `GET /api/plane/v1/snapshot`
- `POST /api/plane/v1/projects:ensure-binding`
- `POST /api/plane/v1/services:apply`
- `POST /api/worker/v1/register`
- `POST /api/worker/v1/workers/{workerID}/heartbeat`
- `GET /api/worker/v1/workers/{workerID}/work`
- `POST /api/worker/v1/workers/{workerID}/executions/{executionID}/report`

但这些路径的来源已经变成：

- `proto`
  注解
- `grpc-gateway`
  生成

而不是：

- 再为 `project / service`
  主链手写一套 northbound REST handler

配额预览接口也同步改成：

- `POST /api/v1/projects/{projectID}/usage/preview-service`

不过这条配额预览接口在本章里仍然保留为手写 HTTP。

原因是：

- 它属于辅助校验接口
- `v5/02`
  当前先把：
  - `project`
  - `service`
    这条主 northbound 链做完

更重要的是，
management API
现在不是靠运行时前缀劫持来接管的，
而是由顶层装配显式挂载：

- `/api/v1/`
- `/api/plane/v1/`
- `/api/worker/v1/`

这意味着：

- 路径所有权
  写在装配层
- public HTTP
  不会再偷偷拥有这些管理前缀
- `grpc-gateway`
  真的成为这些路径的唯一管理入口

## 为什么这一章最后还要改 transport 分层

如果只是把 northbound
换成 `proto + gRPC + grpc-gateway`，
但包结构还是混的，
后面很快还会继续脏掉。

所以这章最后也把
`cloud-plane`
的 transport
分成了四层：

1. `server`
   - 只做顶层装配
   - 显式挂：
     - `/api/v1/`
     - `/api/plane/v1/`
     - `/api/worker/v1/`
2. `statichttp`
   - 只做：
     - `/`
     - `/assets/*`
     - `/api/healthz`
     - `/metrics`
     - `agent artifact`
3. `gateway`
   - 只做用户流量数据面反代
   - 不负责管理 API
4. `api`
   - 只做：
     - gRPC service
     - grpc-gateway
     - auth
     - proto 转换
     - transport 级错误映射

这样收完以后，
至少有一条边界是稳定的：

- public HTTP
  不再混进 management API
- gateway
  不再混进 northbound 语义
- `api`
  不再承担静态入口或数据面职责

## 为什么这一章还要下沉 `servicelifecycle`

这章一开始只是想把
`service`
主对象和 northbound
契约收干净，
但真正改下去以后会发现：

- `create`
- `update`
- `apply`
- `retry`
- `rollback`
- `probe`
- `view`

这些 service
生命周期用例
其实已经被：

- northbound gRPC
- plane southbound

共同复用了。

如果这些逻辑继续留在：

- `api`

包里，
那 `api`
就还是：

- transport
  +
- 共享业务编排

的混合层。

所以这一章最后又新收了一层：

- `servicelifecycle`

它负责：

- `service`
  生命周期相关 usecase
- runtime state
  汇总
- 默认域名绑定
- revision / deployment
  启动
- apply / retry / rollback
- domain / revision / deployment
  查询
- probe

这件事的意义是：

- `api`
  退回 transport adapter
- `servicelifecycle`
  承担 application/usecase
- `service`
  和 `ingress`
  继续只保留领域模型与输入校验

## 创建 service 的请求形状

这一章最关键的变化之一是：

- 请求体不再把所有字段拍平在顶层
- 而是把规格字段收进 `spec`

示例：

```json
{
  "name": "hello",
  "displayName": "Hello",
  "spec": {
    "region": "cn-beijing",
    "replicas": 1,
    "instanceClass": "small",
    "image": "nginx:1.27-alpine",
    "defaultPort": 8080,
    "readinessPath": "/healthz"
  }
}
```

这件事很重要，
因为之后再往里加：

- env
- secret ref
- registry credential ref

都会自然挂在：

- `spec`

下面，
而不会继续把顶层结构挤乱。

## 读取 service 的响应形状

示例：

```json
{
  "service": {
    "id": "svc_001",
    "projectID": "prj_001",
    "name": "hello",
    "displayName": "Hello",
    "spec": {
      "region": "cn-beijing",
      "replicas": 1,
      "instanceClass": "small",
      "image": "nginx:1.27-alpine",
      "command": [],
      "args": [],
      "defaultPort": 8080,
      "readinessPath": "/healthz",
      "env": {}
    },
    "status": {
      "phase": "running",
      "healthy": true,
      "message": "current revision is running",
      "currentRelease": {
        "id": "rev_svc_001_1",
        "label": "r000001"
      }
    },
    "createdAt": "2026-04-16T00:00:00Z",
    "updatedAt": "2026-04-16T00:00:00Z"
  },
  "domains": [
    {
      "host": "hello.demo.apps.example.test"
    }
  ]
}
```

这里要注意：

- `phase`
  来自服务当前汇总状态
- `healthy`
  是当前可用性结论
- `message`
  是当前诊断摘要
- `currentRelease`
  只是当前已提升版本的摘要

而不是：

- rollout 全量内部对象

## Terraform provider 也同步改成同一份契约

如果 `proto/gRPC`
已经是正式契约，
而 Terraform provider
还继续手写一套独立 DTO，
那边界仍然是不干净的。

所以这一章里，
Terraform provider
虽然仍然走：

- `grpc-gateway`
  暴露出来的 `HTTP/JSON`

但它内部不再自己定义一套 northbound 资源语义，
而是要对齐：

- `proto`
  里的 `service`
  资源模型
- `grpc-gateway`
  导出的字段和路径

对用户可见的资源名也同步收成：

- `minicloud_service`

它仍然支持用熟悉的字段声明服务，
但内部回填的是：

- `service.spec`
- `service.status`

而不是继续维持一套独立的旧资源模型。

## 这一章没有做的事，要明确写清楚

这一章故意没有继续做的事情是：

### 不改 rollout / placement / scale-out 核心语义

虽然这一章最后已经把：

- northbound
- plane southbound
- worker southbound

这三类 management API
都统一到了：

- `proto`
- `gRPC`
- `grpc-gateway`

但它没有继续改变：

- rollout engine
  的状态机语义
- placement
  的决策逻辑
- runtime scale-out
  的触发条件
- 用户流量入口
  和 `Caddy`
  数据面能力

所以现在的边界是：

- management API transport:
  - 已经统一
  - 也已经分层
- service application/usecase:
  - 已经从 `api`
    下沉到：
    - `servicelifecycle`
- internal runtime:
  - 仍然保留 `revision / deployment / execution`
- rollout / placement / scale-out
  语义：
  - 还没有在这一章重做

这是这一章刻意保留下来的边界。

## 这一章完成后，收益是什么

这章做完以后，
`mini-cloud`
在 `v5`
里第一次真正具备了：

- 稳定的 `service` 主对象
- 清晰的 `spec / status` 分层
- 不再把 rollout 内部对象当成默认用户接口
- `proto/gRPC/gateway`
  一致的 management API
  主契约
- public HTTP / gateway / management API
  的显式分层
- `api`
  退回 transport adapter
- `servicelifecycle`
  成为 service
  相关 application/usecase
  层
- Web / Terraform / 测试
  以及 plane / worker
  管理链路
  都可以围绕同一份 `service`
  契约继续推进

这会直接给后面的章节打基础：

- `03`
  往 `service spec`
  里补配置和镜像访问
- `04`
  给 `service`
  增加入口和暴露
- `05`
  围绕 `currentRelease`
  做发布与回滚
- `06`
  围绕 `service spec.replicas`
  做扩容和放置

## 本章检查点

如果这一章做对了，
你现在应该已经能比较自然地回答下面这些问题：

1. 为什么 `service`
   必须只暴露：
   - `spec`
   - `status`
2. 为什么 `revision / deployment / execution`
   不能继续直接塞进主响应
3. 为什么这一章最后不只改了 northbound，
   还要把：
   - plane
   - worker
     的 management API
     一起收成单轨
4. 为什么 management API
   不能靠运行时前缀劫持接管，
   而必须显式装配
5. 为什么 `statichttp`
   `gateway`
   `api`
   必须拆开
6. 为什么 `servicelifecycle`
   不该继续留在 `api`
   包里
7. 为什么这一章虽然动了很多 transport
   和 usecase
   分层，
   但 rollout / placement / scale-out
   仍然不算被重写

如果这些问题都已经能稳定回答，
那 `v5/02`
的边界就算真的收干净了。

对应提交：

- `84fa98ee2f186ff483ca0c59aad44660fe9b281b`
