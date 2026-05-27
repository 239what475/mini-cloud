# 07 Safe Deployment and Rollback

这一章解决的是一个很实际的问题：

前面的发布链虽然已经能把新 release 跑起来，
但它还不够安全。

问题在于：

- 新 release 一提交，
  平台就过早把它当成“当前版本”
- 如果候选版本还没通过健康检查，
  或者最后一步替换旧实例失败，
  流量就可能被切到一个并不稳定的版本上

所以这一章要做的不是“再补一个按钮”，
而是先把发布语义重新收紧。

## 这一章先记住一句话

从这一章开始：

- `app.currentReleaseID`
  - 表示已经被正式提升为当前稳定版本的 release
  - 不是“刚提交上来的最新 release”

也就是说，
发布链里第一次真正出现了：

- 稳定版本
- 候选版本

这两个角色。

## 当前发布语义变成了什么

现在一条发布链按下面的顺序看：

1. 用户提交一个新 release
2. control-plane 创建一个新的 candidate deployment
3. 这时 `app.currentReleaseID`
   - 仍然保持在旧的稳定 release
4. gateway / 路由解析
   - 继续指向旧的稳定 deployment
5. agent 把 candidate 容器跑起来并做健康检查
6. 只有当健康检查通过后，
   - 才会进入“替换旧实例”和“提升新 release”这一步
7. 替换成功后，
   - control-plane 才把：
     - `app.currentReleaseID`
       更新成新的 release
8. 如果 candidate 失败，
   - 平台停在安全位置
   - 不会提前切流

这就是这一章最核心的变化。

## 为什么这里还不是完整 rolling update

这一章要明确收住边界。

当前 `mini-cloud v2/07`
做的是：

- 单实例应用的安全切流
- 最小回滚路径

它还不是：

- 多副本 rolling update
- 跨节点批次替换
- 带 stop work queue 的完整替换系统

原因很直接：

- 当前 app 仍然只支持：
  - `replicas = 1`
- agent 还是一个“主动 poll work 的客户端”
- control-plane 还没有一条正式的“去别的 node 停旧实例”的工作队列

所以这章选择了一个诚实、可落地的版本：

- 如果当前已经有稳定实例在跑，
  - 那新的 candidate 会优先复用当前活跃 node
- 这样 agent 才能在本地：
  - 先验证 candidate 健康
  - 再停掉被替换的旧容器

所以你可以把这一章理解成：

- 单实例安全切流

而不是完整的：

- 多副本滚动发布

## 这一章新增了什么语义

### 1. `currentReleaseID` 不再提前前移

以前是：

- release 一创建，
  - app 很快就会把 `currentReleaseID`
    切到这个新 release

现在是：

- 只有 candidate 真正通过健康检查并完成替换后，
  - 才会提升成新的当前 release

### 2. work item 里会带上 `supersededExecution`

当平台发现：

- 当前 app 已经有一个稳定版本正在运行
- 新 candidate 也要在同一台 node 上接管它

control-plane 在下发给 agent 的 work item 里，
会多带一段信息：

- `supersededExecution`

里面描述的是：

- 被替换的 deployment / execution
- 旧容器的 `containerID`

这样 agent 就知道：

- candidate 健康后，
  - 需要停掉哪一个旧容器

### 3. 旧 deployment / execution 会被标成 `superseded`

这一章新增了一个终态：

- `superseded`

它表达的不是“失败”，
而是：

- 这个版本之前是好的
- 只是现在已经被更新版本替换掉了

所以：

- 历史上真正失败的 deployment
  - 仍然是 `failed`
- 被成功替换掉的 deployment
  - 现在会是 `superseded`

这对后面读发布历史很重要。

## 候选失败时，平台现在怎么停在安全位置

这一章把“失败后停在哪里”也说清楚了。

### 情况 1：旧稳定版本还在正常服务

如果：

- 当前 release 仍然有一条稳定运行中的 execution
- 新 candidate 失败了

那么平台会：

- 保持 `currentReleaseID`
  - 不变
- app 状态记成：
  - `degraded`
- 路由继续走旧版本

也就是说：

- 发布失败了
- 但服务没有直接断

### 情况 2：根本没有可继续服务的旧版本

如果：

- 这是第一次发布
  - 或者
- 当前稳定版本本来就已经不可用

那 candidate 失败后，
app 就会直接进入：

- `failed`

因为这时平台已经没有旧版本可以退回去继续服务了。

## agent 现在怎样完成“最后一跳”

当 agent 收到一个带 `supersededExecution` 的 candidate work 时，
它现在会按下面顺序做：

1. `docker run`
   启动 candidate
2. 对 candidate 做本地 HTTP 健康检查
3. 如果健康检查失败：
   - 直接把 candidate 报成 `failed`
   - 旧版本继续保留
4. 如果健康检查通过：
   - 先尝试 `docker stop` 掉旧容器
5. 只有旧容器也成功停掉，
   - 才向 control-plane 报告：
     - 当前 execution = `running`
     - 旧 execution 已被 supersede
6. 如果“新容器健康，但停旧容器失败”：
   - agent 会把新 candidate 也清掉
   - 然后把这次发布报成 `failed`

这个设计很关键，
因为它保证了：

- 只有整个切换真的做完
  - control-plane 才会正式提升新 release

## 新增的最小回滚 API

这一章补了一个最小回滚入口：

```bash
POST /api/v1/apps/{appID}/operations/rollback
```

请求体最小例子：

```json
{
  "releaseID": "rel_xxx"
}
```

它的语义是：

- 不去“修改旧记录”
- 而是基于某个历史 release
  - 再创建一轮新的 candidate deployment

所以 rollback 本质上仍然是一轮新的发布，
只是目标 release 换成了旧版本。

## 这章最重要的三条规则

| 规则 | 现在怎么做 |
| --- | --- |
| 新 release 能不能一提交就切流 | 不能。先做 candidate |
| candidate 失败后会不会把流量切坏 | 不会。旧稳定版本还在时，会停在 `degraded` |
| rollback 是不是直接改旧 deployment | 不是。会新建一轮 deployment |

## 这章最值得看的代码落点

### control-plane 怎样决定“当前稳定版本”

- `internal/httpapi/app_handler.go`
- `internal/store/ingress_store.go`

### work item 怎样带上被替换的旧 execution

- `internal/store/execution_store.go`
- `internal/execution/execution.go`

### agent 怎样在健康检查后停掉旧容器

- `cmd/agent/managed.go`

### superseded 状态怎样落进历史

- `internal/deployment/deployment.go`
- `internal/store/execution_store.go`

## 本地最小实验顺序

下面这条实验链最能体现这章语义：

1. 先把 `v1` 跑成稳定版本
2. 再提交一个 `v2` candidate
3. 让 `v2` 失败一次
4. 观察路由仍然保持在 `v1`
5. 再重试 `v2`
6. 让它成功接管
7. 最后把应用 rollback 回 `v1`

### 1. 先创建第一个稳定 release

```bash
curl -fsS -X POST http://127.0.0.1:18080/api/v1/apps/<app-id>/releases \
  -H 'Content-Type: application/json' \
  -d '{
    "version": "v1",
    "image": "nginx:1.27-alpine",
    "port": 8080,
    "readinessPath": "/healthz"
  }' | jq
```

然后让 worker 取 work 并上报成功。

### 2. 再提交 `v2`

```bash
curl -fsS -X POST http://127.0.0.1:18080/api/v1/apps/<app-id>/releases \
  -H 'Content-Type: application/json' \
  -d '{
    "version": "v2",
    "image": "nginx:1.27-alpine",
    "port": 8080,
    "readinessPath": "/healthz"
  }' | jq
```

注意这时去看 app：

- `app.currentReleaseID`
  - 仍然应该是旧的 `v1`

### 3. 让 worker 取 `v2` 的 work

```bash
curl -fsS http://127.0.0.1:18080/api/v1/nodes/<worker-node-id>/work | jq
```

这时你会看到：

- work item 里多了：
  - `supersededExecution`

### 4. 让 `v2` 失败一次

```bash
curl -fsS -X POST \
  http://127.0.0.1:18080/api/v1/nodes/<worker-node-id>/executions/<execution-id>/report \
  -H 'Content-Type: application/json' \
  -d '{
    "status": "failed",
    "reason": "readiness check never passed",
    "containerID": "container-v2-failed",
    "containerName": "mini-cloud-v2-failed",
    "hostPort": 10002
  }' | jq
```

这时再看 app：

- `status`
  - 会是：
    - `degraded`
- `currentReleaseID`
  - 仍然是旧的 `v1`

### 5. 重试当前失败 deployment

```bash
curl -fsS -X POST \
  http://127.0.0.1:18080/api/v1/apps/<app-id>/operations/retry | jq
```

### 6. candidate 成功后，再看 app

如果这次上报：

- `status = running`
- 并带上：
  - `supersededExecutionID`

那平台就会：

- 把旧 execution / deployment 标成：
  - `superseded`
- 把 `currentReleaseID`
  - 正式切到 `v2`

### 7. rollback 回历史 release

```bash
curl -fsS -X POST \
  http://127.0.0.1:18080/api/v1/apps/<app-id>/operations/rollback \
  -H 'Content-Type: application/json' \
  -d '{
    "releaseID": "<v1-release-id>"
  }' | jq
```

这会重新走一轮 candidate 发布，
只是目标变成：

- 历史上的 `v1`

## 这一章的现实限制

这一章虽然已经把“安全切流”补出来了，
但还要明确记住它的边界：

- 还没有跨 node 的 stop work queue
- 还没有多副本 rolling update
- 还没有批次、分批、金丝雀
- 还没有自动背景清理更多历史实例的完整机制

所以当前更准确的理解是：

- 发布主链已经从“直接切换”升级成了“先 candidate，再 promotion”
- 但它仍然是一个单实例、单 node 约束下的安全版本

## 这一章跑了哪些检查

当前已经跑过：

```bash
go test ./...
./scripts/test-integration.sh
./scripts/check.sh
```

其中新的集成测试重点覆盖了：

- `currentReleaseID`
  - 不会在 candidate 阶段提前切换
- candidate 失败后：
  - app 进入 `degraded`
  - 路由继续指向旧稳定版本
- candidate 成功后：
  - 旧 execution / deployment 进入 `superseded`
  - 路由切到新版本
- rollback
  - 会重新走一轮新的 candidate deployment

## 本章检查点

- 提交：
  - `6e7cbc81f22da36719ec9388137dc768a936b5e3`
- 状态：
  - `app.currentReleaseID`
    - 现在表达的是“当前稳定版本”
  - work item
    - 已经会携带 `supersededExecution`
  - agent
    - 已经会在 candidate 健康后停掉旧容器
  - execution / deployment
    - 已经新增 `superseded` 终态
  - rollback API
    - 已经能基于历史 release 再发起一轮安全切流
