# 01 Fleet Manager 与资源模型

`v4`
的第一章不急着做：

- 多 plane 自动部署
- 全局调度
- 日志聚合
- 告警联动

而是先把最容易在后面越做越乱的东西定住：

- fleet 到底在管什么
- plane 到底是什么
- 哪些对象是静态资源
- 哪些对象是动态视图
- 哪些对象属于运维流

如果这一步不先做清楚，
后面很容易把：

- plane 注册
- 全局 inventory
- incident
- logs / metrics
- deploy 下发

都揉成一团。

所以这一章只做一件事：

- 把 fleet 层最小资源模型真正落到代码、数据库和 HTTP API 里

## 这一章先收哪几个对象

按照 `v4/ROADMAP`
里定下来的边界，
这一章先把下面四类对象做实：

1. `plane`
   - 代表一套被 fleet 纳管的独立 control-plane
   - 是静态注册对象
2. `plane_status`
   - 代表 plane 当前健康状态
   - 是动态状态对象
3. `capacity_snapshot`
   - 代表某一时刻的容量摘要
   - 也是动态状态对象
4. `incident`
   - 代表运维和故障处理里的正式对象
   - 是运维流对象

这里还有一个你在路线图里点名的对象：

- `operation_event`

这一章的处理方式不是再新建一张 fleet 专用事件表，
而是：

- 直接复用已有平台级 `operation_events`

也就是说，
这一章先把 fleet 的动作事件收进现有平台级操作历史，
比如：

- `fleet.plane.create`
- `fleet.plane.delete`
- `fleet.plane.status.update`
- `fleet.capacity_snapshot.record`
- `fleet.incident.create`
- `fleet.incident.resolve`

这样做的原因很直接：

- 第一版先让 fleet 事件真正可见
- 不重复造第二套“事件历史系统”
- 到 `v4/09`
  再继续把 incident / audit / ops 做厚会更自然

## 为什么 `plane` 和 `plane_status` 要拆开

这点很关键。

`plane`
回答的是：

- 这套 plane 叫什么
- 它属于哪家 provider
- 在哪个 region
- fleet 要通过哪个 `apiBaseURL`
  去访问它

这些信息比较像：

- 注册资料
- 静态元数据

而 `plane_status`
回答的是：

- 这套 plane 当前是不是：
  - `registering`
  - `ready`
  - `degraded`
  - `offline`
- 最近一次 heartbeat 是什么时候
- 最近一次 snapshot 同步是什么时候
- 当前状态说明是什么

这些信息明显是会变的。

所以这两者不应该混成一张“什么都塞进去的大对象”。

## 为什么 `capacity_snapshot` 不直接塞进 `plane`

因为容量视图不是“当前一个整数”这么简单。

这一章先把它单独建模成快照，
至少记录：

- `nodesTotal`
- `nodesReady`
- `appsTotal`
- `deploymentsTotal`
- `cpuMilliCapacity`
- `cpuMilliAllocated`
- `memoryMiCapacity`
- `memoryMiAllocated`
- `capturedAt`

这样后面有两个好处：

1. `v4/03`
   做全局 inventory 时，
   你已经有“最新一份容量视图”
2. 后面如果想做趋势或对比，
   也不需要再把“当前值”硬重构成“历史快照”

## 为什么 `incident` 现在就要进入主线

因为 `v4`
不是只做“多 plane 看板”。

你已经明确要求强化：

- 日志
- 监控
- 告警
- 运维

那 incident 迟早会变成主线对象。

如果现在不先给它一个正式形状，
后面很容易退化成：

- 只是日志里打一条错误
- 或只是告警系统里亮红

这都不够。

系统里应该明确存在：

- 这次故障是什么
- 它属于哪套 plane
- 当前严重级别是什么
- 是不是已经恢复
- 关联 runbook 是什么

所以这章先收一个最小 incident 模型：

- `warning` / `critical`
- `open` / `resolved`
- `summary`
- `description`
- `resolution`
- `runbookURL`

## 这一章的数据库落点

这一章新增了一条 migration：

- [00017_create_fleet_planes_and_incidents.sql](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/store/migrations/00017_create_fleet_planes_and_incidents.sql)

它会创建四张表：

1. `fleet_planes`
2. `fleet_plane_statuses`
3. `fleet_plane_capacity_snapshots`
4. `fleet_incidents`

这里的结构有三个关键判断：

1. `fleet_planes`
   - 只存静态注册信息
2. `fleet_plane_statuses`
   - 每个 plane 一行当前状态
3. `fleet_plane_capacity_snapshots`
   - 按时间追加
   - 不是覆盖当前值

## 这一章新增的领域对象

代码里新增了两个新包：

- [fleetplane.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/plane/fleetplane.go)
- [fleetincident.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/incident/fleetincident.go)

这里的职责边界是：

- `fleetplane`
  - 管 plane、status、capacity snapshot 的模型和校验
- `fleetincident`
  - 管 incident 的模型和校验

这意味着后面不管是：

- store
- httpapi
- fleet sync

都要遵守这一层的输入约束，
而不是各处自己随便解释 JSON。

## 这一章新增的 store 能力

这一章在：

- [fleet_store.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/store/fleet_store.go)

里新增了下面这些最小能力：

- `CreateFleetPlane`
- `ListFleetPlanes`
- `GetFleetPlane`
- `DeleteFleetPlane`
- `UpdateFleetPlaneStatus`
- `RecordFleetPlaneCapacitySnapshot`
- `ListFleetPlaneCapacitySnapshots`
- `CreateFleetIncident`
- `ListFleetIncidents`
- `ResolveFleetIncident`

这里最重要的不是“接口数量”，
而是它们对应了四类完全不同的动作：

1. 注册静态资源
2. 更新动态状态
3. 追加容量快照
4. 推进运维事件流

这就是 fleet 这层和单个 plane 内部对象的差异。

## 这一章新增的 HTTP API

这一章在：

- [fleet_handler.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/fleet_handler.go)
- [router.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/router.go)

里接入了第一版 fleet API：

### planes

- `POST /api/v1/fleet/planes`
- `GET /api/v1/fleet/planes`
- `GET /api/v1/fleet/planes/{planeID}`
- `DELETE /api/v1/fleet/planes/{planeID}`

### plane status

- `PUT /api/v1/fleet/planes/{planeID}/status`

### capacity snapshots

- `POST /api/v1/fleet/planes/{planeID}/capacity-snapshots`
- `GET /api/v1/fleet/planes/{planeID}/capacity-snapshots`

### incidents

- `POST /api/v1/fleet/incidents`
- `GET /api/v1/fleet/incidents`
- `POST /api/v1/fleet/incidents/{incidentID}/resolve`

### operation events

- `GET /api/v1/fleet/operations`

这一条当前继续复用已有平台级操作历史表，
但接口层已经只返回：

- `action` 以 `fleet.`
  开头的记录

也就是说，
`fleet/operations`
不会把：

- `project.*`
- `app.*`
- `node.*`

这类非 fleet 动作混进来。

底层存储仍然没有另起一张新表，
只是语义上从已有操作历史里筛出 fleet 事件。

所以它不是“再造第二套事件系统”，
而是：

- 先让 fleet 事件真正成一条干净可读的流

底层并没有新建 fleet 专用表，
而是复用：

- `operation_events`

这也是为什么本章里说：

- `operation_event`
  第一版先复用已有平台级操作历史

## 现在先用“手工写入状态”是为什么

这一章你会看到一个很明显的特征：

- `plane`
  可以手工创建
- `plane_status`
  可以手工更新
- `capacity_snapshot`
  也可以手工写入

这不是因为最终形态想一直手工维护，
而是因为：

- `v4/01`
  只负责把模型和边界立住
- `v4/02`
  才负责让 plane 自动注册、心跳和同步

所以这里现在做的是：

- 先把“目标资源模型”做真
- 下一章再把“谁来自动写这些对象”补上

## 一条最小演示链

这组 fleet API 当前都走：

- admin 鉴权

也就是说，
如果 control-plane 启动时配置了：

- `MINICLOUD_ADMIN_TOKEN`

那下面这些请求都需要带：

- `Authorization: Bearer <admin-token>`

为了简化命令，
先约定：

```bash
AUTH_HEADER='Authorization: Bearer <admin-token>'
```

先创建一个 plane：

```bash
curl -sS -X POST http://127.0.0.1:8080/api/v1/fleet/planes \
  -H "$AUTH_HEADER" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "aliyun-bj-primary",
    "displayName": "Aliyun Beijing Primary",
    "provider": "aliyun",
    "region": "cn-beijing",
    "apiBaseURL": "https://plane-a.example.com/api"
  }'
```

把它的状态改成 `ready`：

```bash
curl -sS -X PUT http://127.0.0.1:8080/api/v1/fleet/planes/<plane-id>/status \
  -H "$AUTH_HEADER" \
  -H 'Content-Type: application/json' \
  -d '{
    "status": "ready",
    "message": "heartbeat is healthy"
  }'
```

再写一条容量快照：

```bash
curl -sS -X POST http://127.0.0.1:8080/api/v1/fleet/planes/<plane-id>/capacity-snapshots \
  -H "$AUTH_HEADER" \
  -H 'Content-Type: application/json' \
  -d '{
    "nodesTotal": 3,
    "nodesReady": 2,
    "appsTotal": 5,
    "deploymentsTotal": 6,
    "cpuMilliCapacity": 12000,
    "cpuMilliAllocated": 4000,
    "memoryMiCapacity": 24576,
    "memoryMiAllocated": 8192
  }'
```

然后读回 plane 详情：

```bash
curl -sS \
  -H "$AUTH_HEADER" \
  http://127.0.0.1:8080/api/v1/fleet/planes/<plane-id>
```

你会看到：

- 静态 `plane` 信息
- 当前 `status`
- 最新一条 `latestCapacitySnapshot`

如果这套 plane 要退役或注册错了，
现在也可以直接删掉：

```bash
curl -sS -X DELETE \
  -H "$AUTH_HEADER" \
  http://127.0.0.1:8080/api/v1/fleet/planes/<plane-id>
```

最后再创建并关闭一条 incident：

```bash
curl -sS -X POST http://127.0.0.1:8080/api/v1/fleet/incidents \
  -H "$AUTH_HEADER" \
  -H 'Content-Type: application/json' \
  -d '{
    "planeID": "<plane-id>",
    "severity": "critical",
    "summary": "plane lost heartbeat",
    "description": "no heartbeat for 5 minutes",
    "runbookURL": "https://runbooks.example.com/fleet/plane-offline"
  }'
```

```bash
curl -sS -X POST http://127.0.0.1:8080/api/v1/fleet/incidents/<incident-id>/resolve \
  -H "$AUTH_HEADER" \
  -H 'Content-Type: application/json' \
  -d '{
    "resolution": "heartbeat stream recovered"
  }'
```

## 这章最重要的边界结论

这一章做完以后，
你应该明确记住下面四句话：

1. `plane`
   是静态注册对象
2. `plane_status`
   和 `capacity_snapshot`
   是动态状态对象
3. `incident`
   是运维流对象
4. `operation_event`
   第一版先复用已有平台级操作历史

这四句就是 `v4`
后面所有章节的基础边界。

## 本章涉及的核心文件

- [fleetplane.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/plane/fleetplane.go)
- [fleetincident.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/incident/fleetincident.go)
- [fleet_store.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/store/fleet_store.go)
- [fleet_handler.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/fleet_handler.go)
- [router.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/router.go)
- [00017_create_fleet_planes_and_incidents.sql](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/store/migrations/00017_create_fleet_planes_and_incidents.sql)
- [store_integration_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/store/store_integration_test.go)
- [httpapi_integration_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/httpapi_integration_test.go)

## 本章检查点

- 提交：
  - 待本章完成后补充
