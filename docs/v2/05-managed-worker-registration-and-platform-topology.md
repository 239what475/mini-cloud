# 05 Managed Worker Registration and Platform Topology

这一章先不急着碰远程 `SSH` 安装。

先把更底层、也更关键的一层做扎实：

- platform 节点和 worker 节点在平台里到底怎么分工
- worker 为什么不能靠一次性 `register` 命令就算“纳管完成”
- agent 进程怎样在重启后继续认出自己
- heartbeat 为什么不能把 control-plane 已经保留的资源视图覆盖掉

如果这些问题没有先收住，
后面即使把机器真的装上去，
平台拓扑也还是假的。

## 这一章解决的核心问题

前面 `04`
已经把平台宿主机的云侧资源准备好了：

- `VPC`
- `vSwitch`
- `SecurityGroup`
- platform `ECS`
- 实例 `RAM` 角色
- `EIP`

但这还不等于“平台拓扑已经成立”。

真正的平台拓扑至少还要再补三件事：

| 问题 | 这一章怎么处理 |
| --- | --- |
| platform 节点能不能直接接业务 workload | 不行。现在通过 `node.role` 显式区分，只允许 `worker` 参与调度 |
| worker 重启以后怎么继续认出自己 | `cmd/agent run` 会把 `nodeID` 持久化到本地 `state file` |
| heartbeat 会不会把平台已经保留的资源清零 | 不会。现在 heartbeat 只会“抬高”已分配资源视图，不会把更高的保留值覆盖掉 |

所以这一章真正补的是：

- 逻辑拓扑
- 持续纳管
- 调度边界

## 节点角色现在怎么理解

现在节点有了明确角色：

| 角色 | 作用 |
| --- | --- |
| `platform` | 运行 control-plane、网关、数据库等平台自有组件 |
| `worker` | 真正承接应用 workload |

这不是文档上的约定，
而是已经落到代码里：

- `nodes` 表新增了 `role`
- `register` 时可以显式传 `role`
- scheduler 只会从 `worker` 节点里选目标

也就是说，
从这一章开始：

- platform 节点
  - 可以是 `ready`
  - 也可以持续 heartbeat
  - 但不会被业务发布选中

## scheduler 现在多了一层过滤

现在调度顺序不再只是：

1. 过滤 provider
2. 过滤状态
3. 过滤 schedulable
4. 过滤 region
5. 比容量

而是先多了一步：

1. 过滤 provider
2. 过滤 `role=worker`
3. 再过滤状态 / schedulable / region / capacity

这意味着：

- 就算 platform 节点也是 `ready`
- 就算它资源很多
- 它也不会接到应用发布

这一点已经通过集成测试验证过：

- 同时注册一个 `platform` 节点和一个 `worker` 节点
- 创建 release
- placement 会稳定落到 `worker`

## `cmd/agent run` 现在在干什么

这一章给 `cmd/agent`
补了一个新的长跑模式：

```bash
go run ./cmd/agent run \
  --server http://127.0.0.1:18080 \
  --role worker \
  --region cn-beijing \
  --name worker-a \
  --private-ip 10.0.0.12 \
  --instance-id i-worker-a \
  --instance-type ecs.u1-c1m2.large \
  --cpu-milli-capacity 2000 \
  --memory-mi-capacity 4096 \
  --state-file /tmp/mini-cloud-agent-state.json
```

它启动后会一直循环做这几件事：

1. 如果本地还没有 `nodeID`
   - 先调用 `register`
2. 把拿到的 `nodeID`
   - 写进本地 `state file`
3. 周期性发 heartbeat
4. 周期性 poll work
5. 如果 control-plane 告诉它：
   - 这个 `nodeID` 不存在了
   - 它会清掉本地状态
   - 然后重新注册

所以它不是简单地把三条命令拼起来，
而是在补一条真正能长期跑的 agent 主链。

## 为什么要有 `state file`

如果没有 `state file`，
那 agent 每次重启都会重新 `register`，
平台里就会不断出现新的 node 记录。

这会直接带来两个问题：

1. 同一台机器
   - 看起来像很多台不同节点
2. 旧的 placement / execution / heartbeat 历史
   - 会和新的 node 记录断开

所以这里用一个很直接的办法先收住：

- 把已注册的 `nodeID`
  - 存在本地文件里

当前默认路径是：

- `.mini-cloud-agent/state.json`

后面如果放到真实服务器上，
再把它改成例如：

- `/var/lib/mini-cloud-agent/state.json`

即可。

## heartbeat 为什么不能覆盖更高的保留资源

这一点很关键。

当前平台里其实有两套“资源视图”：

1. control-plane 自己根据 placement / execution
   - 维护的已分配资源
2. agent heartbeat 上报的节点观察值

如果 heartbeat 每次都直接把：

- `cpu_milli_allocated`
- `memory_mi_allocated`

改成：

- `capacity - free`

那只要 agent 暂时还没做出精确观测，
它就可能把 control-plane 已经保留的资源直接刷没。

所以现在改成了更稳的策略：

- heartbeat 只会把“观察到的已分配资源”
  - 往上抬
- 但不会把一个更高的保留值覆盖成更低值

可以把它理解成：

- control-plane 的保留值
  - 是调度真相
- heartbeat 的观测值
  - 目前更像一个安全下限校正

这也是为什么这一章新增了对应的数据库集成测试。

## 这一章建议怎么本地体验

这一章最适合先在本地把逻辑跑顺。

### 1. 启动本地 Postgres

```bash
docker compose -f ./deploy/compose/docker-compose.yml up -d postgres
```

### 2. 启动 control-plane

```bash
MINICLOUD_HTTP_ADDR=127.0.0.1:18080 \
MINICLOUD_DATABASE_URL='postgres://mini_cloud:mini_cloud@127.0.0.1:5432/mini_cloud?sslmode=disable' \
go run ./cmd/control-plane
```

### 3. 手工注册一个 platform 节点

```bash
go run ./cmd/agent register \
  --server http://127.0.0.1:18080 \
  --role platform \
  --region cn-beijing \
  --name platform-a \
  --private-ip 10.0.0.11 \
  --instance-id i-platform-a \
  --instance-type ecs.u1-c1m2.large \
  --cpu-milli-capacity 2000 \
  --memory-mi-capacity 4096
```

再发一次 heartbeat：

```bash
go run ./cmd/agent heartbeat \
  --server http://127.0.0.1:18080 \
  --node-id <platform-node-id> \
  --cpu-milli-free 2000 \
  --memory-mi-free 4096 \
  --status ready
```

### 4. 再启动一个持续运行的 worker agent

```bash
go run ./cmd/agent run \
  --server http://127.0.0.1:18080 \
  --role worker \
  --region cn-beijing \
  --name worker-a \
  --private-ip 10.0.0.12 \
  --instance-id i-worker-a \
  --instance-type ecs.u1-c1m2.large \
  --cpu-milli-capacity 2000 \
  --memory-mi-capacity 4096 \
  --state-file /tmp/mini-cloud-agent-state.json
```

### 5. 查看节点列表

```bash
curl -fsS http://127.0.0.1:18080/api/v1/platform/nodes | jq
```

你会看到：

- platform 节点的 `role` 是 `platform`
- worker 节点的 `role` 是 `worker`

### 6. 再创建 app / release

这时再走发布主链，
placement 就只会从 `worker` 节点里选。

## 这一章的边界

这一章还没有做这些事：

- 真实阿里云 first worker `ECS` 的自动创建
- 通过 `SSH` / systemd 把 agent 真正装到云主机上
- platform `ECS` 上的 control-plane 远程安装

这些事情都很重要，
但它们依赖的前提正是本章刚补好的这条逻辑主链：

- 节点角色
- 长跑 agent
- 重连语义
- 调度边界

所以现在的顺序是有意的：

- 先把“纳管语义”做对
- 再把它搬到真实云主机上

## 本章检查点

- 提交：
  - `9766ffafa002990042f0b8ac0f62777dfcbf79e7`
- 状态：
  - `node.role`
    - 已经进入数据模型、数据库和 API
  - scheduler
    - 已经只调度 `worker`
  - `cmd/agent run`
    - 已经能持续注册、发心跳、poll work、掉线后重连
  - heartbeat
    - 已经不会把更高的保留资源视图错误清零
