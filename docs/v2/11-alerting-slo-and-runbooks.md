# 11 Alerting, SLO, and Runbooks

到了 `v2/11`，
`mini-cloud`
终于不只是“能部署、能回滚、能审计”。

现在还要补上一层：

- 平台现在健不健康
- 哪些问题值得立刻告警
- 告警出来以后先看什么、先做什么

但这一章故意不直接做成一个很重的告警平台。

它先只补：

1. 一个管理员可读的可靠性快照接口
2. 三个最小但有代表性的 `SLO`
3. 三个直接对应的最小告警
4. 三份静态 `runbook`
5. 四项日常巡检项

## 先记一句话

这一章的核心不是：

- 把 `Prometheus Alertmanager`
- 通知路由
- 静默
- 升级策略

整套都做完。

而是先把：

- 我们到底在看什么
- 超过什么阈值算异常
- 异常以后先怎么排

正式落成一条最小闭环。

## 为什么只先选这三个 `SLO`

当前版本只盯三条线：

1. 发布线
2. worker 线
3. 入口流量线

原因很直接。

如果这三条线都健康，
那就意味着平台最关键的主链大体是通的：

- 能发版
- 有节点可跑
- 用户能进来

所以当前最小 `SLO` 是：

| SLO | 窗口 | 目标 | 代表什么 |
| --- | --- | --- | --- |
| 发布成功率 | `24h` | `>= 95%` | 最近一天的变更是不是大量失败 |
| worker 就绪率 | `current` | `>= 95%` | 当前节点池是不是足够可用 |
| 入口成功率 | `5m` | `>= 99.5%` | 最近入口流量是不是正在出问题 |

## 可靠性快照接口

当前新增了一个管理员接口：

```text
GET /api/v1/platform/reliability
```

它只允许：

- `admin`

访问。

因为这里返回的是平台级运维信息，
包括：

- 当前 `SLO`
- 当前告警状态
- runbook
- daily checks

这些都不适合直接暴露给普通项目 token。

返回结构大致是：

```json
{
  "generatedAt": "2026-04-11T10:00:00Z",
  "slos": [],
  "alerts": [],
  "runbooks": [],
  "dailyChecks": []
}
```

也就是说，
这一章先把“平台可靠性面板”做成了一个聚合快照，
而不是拆成很多个分散接口。

## 这三个 `SLO` 分别怎么看

### 1. 发布成功率

它看的是：

- 最近 `24h`
- 所有已经进入终态的 deployment
- 其中有多少最终状态是 `running`

也就是：

- 成功发布数 / 已结束发布总数

目标是：

- `95%`

为什么这样选：

- 发布是平台最直接的变更入口
- 如果最近一天失败很多
  - 平台即使“还能跑”
  - 也已经不稳定了

### 2. worker 就绪率

它看的是：

- 当前全部 `worker`
- 其中有多少处于 `ready`

也就是：

- ready worker / total worker

目标也是：

- `95%`

这条线代表的是：

- 平台的承载面是不是还够健康

如果这里掉下去，
即使入口暂时没报错，
平台也已经进入“容易继续恶化”的状态了。

### 3. 入口成功率

它看的是：

- 最近 `5m`
- gateway 请求里
  - `status < 500`
  的比例

目标是：

- `99.5%`

这里故意只把：

- `5xx`

看成平台侧严重异常，
因为：

- `4xx`
  更可能是用户请求问题
- `5xx`
  更能代表入口层或后端服务异常

## 现在有哪些最小告警

当前只做三条最小告警：

| Alert ID | 触发条件 | 严重级别 | runbook |
| --- | --- | --- | --- |
| `deployment-failures-active` | 当前存在 `failed deployment` | `warning` | `release-failed` |
| `worker-nodes-offline` | 当前存在 `offline worker` | `critical` | `worker-offline` |
| `gateway-server-errors-high` | 最近 `5m` 请求数 `>= 10` 且 `5xx` 比例 `>= 5%` | `critical` | `gateway-5xx-spike` |

这里要注意两件事。

### 第一，告警比 `SLO` 更即时

例如：

- 发布成功率是 `24h` 视角
- 但只要当前已经出现 `failed deployment`
  - 告警就应该先响

所以：

- `SLO`
  更像健康目标
- `Alert`
  更像立刻要不要处理

### 第二，现在还没有完整告警引擎

这一章还没有去实现：

- 告警规则持久化
- 抑制
- 分组
- 通知发送
- 值班升级

它现在只是把：

- 当前告警状态

作为可靠性快照的一部分暴露出来，
同时在 `/metrics`
里导出状态指标。

## runbook 在这里是什么意思

这一章里的 `runbook`
可以直接理解成：

- “某类问题出现后，先检查什么，再做什么”

当前 runbook 不是数据库里的动态配置，
而是：

- control-plane 代码里内嵌的静态内容

这样做是故意的。

因为我们现在先要的是：

- 有一份稳定、可读、能跟着代码演进的最小排障手册

而不是先做一套很重的文档管理系统。

当前三份 runbook 是：

| Runbook ID | 对应问题 |
| --- | --- |
| `release-failed` | 发布失败 |
| `worker-offline` | worker 离线 |
| `gateway-5xx-spike` | 入口 5xx 激增 |

## 告警和 runbook 是怎么连起来的

关系非常直接：

| 告警 | 先看哪里 | 常见动作 |
| --- | --- | --- |
| `deployment-failures-active` | `/api/v1/platform/deployments`、项目操作历史 | `probe`、`retry`、`rollback` |
| `worker-nodes-offline` | `/api/v1/platform/nodes`、最近心跳与操作历史 | 恢复心跳、`activate`、替换节点 |
| `gateway-server-errors-high` | `/api/v1/platform/access-logs`、app 当前 execution / release | 查 backend、查 gateway、必要时回滚 |

也就是说，
现在系统里已经明确表达了：

- 这个告警应该看哪份 runbook

而不是只丢给你一句：

- “出错了”

## daily checks 是什么

这里的：

- `daily checks`

可以先理解成：

- 每天值班或日常巡检时，
  最值得先看的几项简短检查

它不是新的存储对象，
也不是人工维护的一张表。

当前实现里，
它是由可靠性快照即时派生出来的：

1. 当前是否存在 `firing` 告警
2. worker 是否存在 `offline/not_ready`
3. 最近 `24h` 发布成功率是否异常
4. 最近 `24h` 是否发生过新的控制面变更

这里第四项和上一章的：

- `operation history`

直接连起来了。

也就是说，
这章不是凭空又造了一套“巡检数据”，
而是把已有的：

- 告警状态
- 节点状态
- 发布状态
- 变更历史

重新组织成一份更适合日常值班阅读的摘要。

## `/metrics` 里新增了什么

除了原来的平台总览指标，
这一章还新增了几组与可靠性相关的指标：

```text
minicloud_slo_ratio{id,window}
minicloud_slo_target_ratio{id,window}
minicloud_alert_state{id,severity}
minicloud_alerts_firing_total
```

所以现在：

- `/api/v1/platform/reliability`
  更适合人直接看
- `/metrics`
  更适合后面继续接 `Prometheus`

## 最小检查方法

### 1. 直接看可靠性快照

```bash
curl -s \
  -H "Authorization: Bearer $MINICLOUD_ADMIN_TOKEN" \
  http://127.0.0.1:8080/api/v1/platform/reliability
```

### 2. 看指标里有没有对应 `SLO` / 告警状态

```bash
curl -s http://127.0.0.1:8080/metrics | rg 'minicloud_(slo|alert)'
```

## 这一章做完后，平台多了什么

到这里，
`mini-cloud v2`
就不只是：

- 能发版
- 能回滚
- 能看操作历史

还多了三层很关键的运维视角：

1. 当前可靠性目标是否达标
2. 当前有哪些问题正在触发告警
3. 告警后应该按哪条最小 runbook 去排

虽然它离完整生产级值班系统还差很远，
但对当前这条教学主线来说，
已经把：

- 指标
- 阈值
- 告警
- 排障动作

真正串成了第一条闭环。

## 本章检查点

- 提交：
  - `5f4d0d4e6d54ca6f3073f7a2f811ce7ff997a3b7`
- 状态：
  - `SLO`、告警视图和最小 runbook 已经正式进入平台运维入口
