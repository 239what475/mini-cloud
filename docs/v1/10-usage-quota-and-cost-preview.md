# 10 Usage Quota And Cost Preview

这一章给 `mini-cloud v1` 补上资源计划面。

前面的 `09`
已经让平台具备了：

- 节点维护
- 失联标记
- 失败重试

但如果平台开始真的给项目接应用，
马上还会遇到另一类问题：

- 一个项目到底已经占了多少资源
- 新 app 放进去之前，
  - 会不会把这个项目的容量和预算打爆

这一章就是先回答这两个问题。

## 这一章先记三句话

### 1. 这一章的 `usage` 不是实时 CPU 使用率

这一章里说的 `usage`，
不是：

- 某个容器此刻真的用了多少 CPU
- 某个进程此刻真的吃了多少内存

当前 `v1`
还没有做远程 cgroup 采样和长期 usage 上报，
所以这里的 `usage`
先统一定义成：

- 按 app 规格推导出来的“平台保留量”

也就是说，
这一章里用的是：

- `instanceClass`
- `replicas`

推导出来的：

- `requestedCPUMilli`
- `requestedMemoryMi`
- `estimatedMonthlyCostCents`

所以你可以先把它理解成：

- “如果平台答应接这个 app，
  - 那它大概要替你预留多少资源”

### 2. 这一章的 `quota` 是项目级护栏

这一章把 quota 放在：

- `Project`

上面。

当前一个项目会带一组最小配额：

- `maxApps`
- `cpuMilli`
- `memoryMi`
- `monthlyBudgetCents`

所以这章里，
`quota`
主要在回答：

- “这个项目最多还能再接多少 app / CPU / 内存 / 预算”

### 3. 这一章的 `cost preview` 是教学价卡，不是真账单

这个点非常重要。

现在平台里的 app 只选：

- `small`
- `medium`
- `large`

所以这一章故意做了一张教学价卡：

- `small`
  - `2500` cents / month
  - 也就是：
    - `CNY 25.00 / month`
- `medium`
  - `5000` cents / month
  - 也就是：
    - `CNY 50.00 / month`
- `large`
  - `9000` cents / month
  - 也就是：
    - `CNY 90.00 / month`

它的作用只是：

- 让你看到“规格变化会怎样传导到项目预算”

它不是：

- 阿里云真实价格
- 真实计费
- 真实账单系统

## 这一章实际新增了什么

这一章真正新增的是：

- `Project` 新增 quota 字段
  - `maxApps`
  - `cpuMilli`
  - `memoryMi`
  - `monthlyBudgetCents`
- `00008_add_project_quotas.sql`
  - 把这些 quota 落到数据库
- `internal/usage`
  - 统一做：
    - 资源保留量推导
    - 成本估算
    - quota 判断
- `GET /api/v1/platform/usage/projects`
  - 返回项目级用量总览
- `POST /api/v1/projects/{projectID}/usage/preview-app`
  - 先预估新 app 放进去后会不会超限
- `CreateApp`
  - 真正创建前会做 quota 校验
- 前端新增 `Usage` 页面
  - 展示项目当前用量、剩余额度和 app 明细
- `Apps` 页面新增：
  - `Preview usage and cost`

## 默认 quota 是多少

如果你创建项目时不显式传 quota，
当前默认值是：

- `maxApps`
  - `5`
- `cpuMilli`
  - `4000`
- `memoryMi`
  - `8192`
- `monthlyBudgetCents`
  - `20000`
  - 也就是：
    - `CNY 200.00`

也就是说，
前面章节里那些只传：

- `name`
- `displayName`

的项目创建命令，
现在仍然能继续用。

## 这一章的核心链路是什么

这一章的主链路可以按下面顺序理解：

1. 用户先创建一个项目
   - 项目带 quota
2. 平台根据项目里现有 app，
   - 计算当前 usage
3. 用户想再加一个 app 时，
   - 先做一次 preview
4. preview 会回答：
   - 这个 app 自己要多少资源
   - 放进去后的 projected usage 是多少
   - 有没有超过 quota
5. 如果 preview 仍然允许，
   - `CreateApp` 才真正落库
6. 如果 preview 已经超限，
   - `CreateApp` 也会被后端拒绝

所以这一章真正想讲的是：

- “创建前先做资源计划”

而不是：

- “先创建再说，超了再补救”

## 为什么 quota 校验放在 `CreateApp`

这是这一章里一个故意的取舍。

当前 `v1`
里：

- app 一旦创建
  - 就已经带着：
    - `instanceClass`
    - `replicas`

这些资源规划信息了。

所以这一章里，
最自然的 quota 校验位置就是：

- `CreateApp`

而不是：

- 提交 release 之后
- runtime 真跑起来之后

因为这章的目标是：

- 在“规划阶段”就把超限挡住

## 这一章故意没有做什么

这些东西，
这一章故意没有做：

- 实时 CPU / 内存采样
- 历史 usage 曲线
- 真正的账单周期
- 按流量 / 存储 / 出网单独计费
- 多租户账单系统
- 充值、扣费、发票

所以这章更准确地说，
是在做：

- `usage / quota / cost preview`

而不是：

- 完整 billing

## 当前 API 会返回什么

### 1. 项目用量总览

`GET /api/v1/platform/usage/projects`
返回的每个项目项里，
最重要的是：

- `project.quota`
- `usage`
- `remaining`
- `apps`

也就是说，
你现在至少能直接看到：

- 这个项目已经用了几个 app
- 这几个 app 一共占了多少保留 CPU / 内存
- 预估月成本是多少
- 还剩多少 quota

### 2. 新 app 的预估结果

`POST /api/v1/projects/{projectID}/usage/preview-app`
返回的重点是：

- `candidate`
  - 新 app 自己要多少资源和预算
- `currentUsage`
  - 当前项目已经用了多少
- `projectedUsage`
  - 如果真的创建，它会变成多少
- `allowed`
  - 允不允许
- `reasons`
  - 为什么不允许

这正好对应了真实平台里很常见的一步：

- admission 前先做资源计划检查

## 本章实验

这一章不需要节点和 runtime。

因为它主要讲的是：

- 资源规划
- quota 校验

所以实验只需要：

- Postgres
- control-plane

### 1. 启动数据库和 control-plane

```bash
cd projects/mini-cloud

docker compose -f deploy/compose/docker-compose.yml up -d
go run ./cmd/control-plane
```

另开一个终端继续下面命令。

### 2. 创建一个小 quota 项目

这一节故意把 quota 设小，
这样后面更容易看到超限效果。

```bash
PROJECT_ID="$(
  curl -s http://127.0.0.1:8080/api/v1/projects \
    -H 'Content-Type: application/json' \
    -d '{
      "name":"quota-demo",
      "displayName":"Quota Demo",
      "quota":{
        "maxApps":2,
        "cpuMilli":1200,
        "memoryMi":1500,
        "monthlyBudgetCents":6000
      }
    }' \
  | jq -r '.id'
)"

printf 'PROJECT_ID=%s\n' "$PROJECT_ID"
```

### 3. 先看一次空项目的 usage

```bash
curl -s http://127.0.0.1:8080/api/v1/platform/usage/projects \
  | jq '.items[0]'
```

你应该能看到类似：

```json
{
  "project": {
    "name": "quota-demo",
    "quota": {
      "maxApps": 2,
      "cpuMilli": 1200,
      "memoryMi": 1500,
      "monthlyBudgetCents": 6000
    }
  },
  "usage": {
    "appsUsed": 0,
    "requestedCPUMilli": 0,
    "requestedMemoryMi": 0,
    "estimatedMonthlyCostCents": 0
  },
  "remaining": {
    "apps": 2,
    "cpuMilli": 1200,
    "memoryMi": 1500,
    "monthlyBudgetCents": 6000
  }
}
```

### 4. preview 一个 `small` app

```bash
curl -s \
  http://127.0.0.1:8080/api/v1/projects/"$PROJECT_ID"/usage/preview-app \
  -H 'Content-Type: application/json' \
  -d '{
    "instanceClass":"small",
    "replicas":1
  }' \
  | jq
```

当前真实实验里，
返回类似：

```json
{
  "candidate": {
    "instanceClass": "small",
    "replicas": 1,
    "requestedCPUMilli": 500,
    "requestedMemoryMi": 512,
    "estimatedMonthlyCostCents": 2500
  },
  "projectedUsage": {
    "appsUsed": 1,
    "requestedCPUMilli": 500,
    "requestedMemoryMi": 512,
    "estimatedMonthlyCostCents": 2500
  },
  "remaining": {
    "apps": 1,
    "cpuMilli": 700,
    "memoryMi": 988,
    "monthlyBudgetCents": 3500
  },
  "allowed": true,
  "reasons": []
}
```

也就是说，
在真正创建之前，
平台已经先算出来：

- 这个 `small` app 会占：
  - `500` mCPU
  - `512` Mi
  - `CNY 25.00 / month`

### 5. 真正创建这个 `small` app

```bash
curl -s http://127.0.0.1:8080/api/v1/apps \
  -H 'Content-Type: application/json' \
  -d "{
    \"projectID\":\"$PROJECT_ID\",
    \"name\":\"app-small\",
    \"displayName\":\"App Small\",
    \"region\":\"cn-beijing\",
    \"replicas\":1,
    \"instanceClass\":\"small\",
    \"defaultPort\":8080,
    \"readinessPath\":\"/healthz\",
    \"env\":{}
  }" \
  | jq
```

这次会成功。

### 6. 再看一次项目 usage

```bash
curl -s http://127.0.0.1:8080/api/v1/platform/usage/projects \
  | jq '.items[0]'
```

现在你会看到：

- `appsUsed`
  - 变成 `1`
- `requestedCPUMilli`
  - 变成 `500`
- `requestedMemoryMi`
  - 变成 `512`
- `estimatedMonthlyCostCents`
  - 变成 `2500`

### 7. 再 preview 一个 `medium` app

```bash
curl -s \
  http://127.0.0.1:8080/api/v1/projects/"$PROJECT_ID"/usage/preview-app \
  -H 'Content-Type: application/json' \
  -d '{
    "instanceClass":"medium",
    "replicas":1
  }' \
  | jq
```

当前真实实验里，
返回类似：

```json
{
  "candidate": {
    "instanceClass": "medium",
    "replicas": 1,
    "requestedCPUMilli": 1000,
    "requestedMemoryMi": 1024,
    "estimatedMonthlyCostCents": 5000
  },
  "projectedUsage": {
    "appsUsed": 2,
    "requestedCPUMilli": 1500,
    "requestedMemoryMi": 1536,
    "estimatedMonthlyCostCents": 7500
  },
  "remaining": {
    "apps": 0,
    "cpuMilli": -300,
    "memoryMi": -36,
    "monthlyBudgetCents": -1500
  },
  "allowed": false,
  "reasons": [
    "cpu quota exceeded: 1500 > 1200 mCPU",
    "memory quota exceeded: 1536 > 1500 Mi",
    "monthly budget exceeded: 7500 > 6000 cents"
  ]
}
```

这里很关键：

- app 数量本身没超
  - `2 <= 2`
- 但：
  - CPU 超了
  - 内存超了
  - 月预算也超了

所以 preview 已经告诉你：

- 这次不能建

### 8. 真正尝试创建这个 `medium` app

```bash
curl -i http://127.0.0.1:8080/api/v1/apps \
  -H 'Content-Type: application/json' \
  -d "{
    \"projectID\":\"$PROJECT_ID\",
    \"name\":\"app-medium\",
    \"displayName\":\"App Medium\",
    \"region\":\"cn-beijing\",
    \"replicas\":1,
    \"instanceClass\":\"medium\",
    \"defaultPort\":8080,
    \"readinessPath\":\"/healthz\",
    \"env\":{}
  }"
```

当前真实实验里，
状态码是：

- `409`

返回体类似：

```json
{
  "error": "project quota exceeded: cpu quota exceeded: 1500 > 1200 mCPU; memory quota exceeded: 1536 > 1500 Mi; monthly budget exceeded: 7500 > 6000 cents",
  "reasons": [
    "cpu quota exceeded: 1500 > 1200 mCPU",
    "memory quota exceeded: 1536 > 1500 Mi",
    "monthly budget exceeded: 7500 > 6000 cents"
  ]
}
```

这说明：

- preview 不是“装饰性信息”
- 后端真的会在 `CreateApp` 时执行同样的 quota 校验

## 这一章的结论

做到这里，
`mini-cloud v1`
第一次有了一个最小但真实的资源计划面：

- 项目知道自己有多少 quota
- 平台知道项目已经保留了多少资源
- 新 app 创建前可以先 preview
- 真正创建时也会被后端硬性校验

这一步很重要，
因为后面的：

- 成本看板
- 更多产品规格
- 多项目多租户
- 更正式的 admission / policy

都会以这层能力为基础继续长出来。

## 本章检查点

- commit:
  - `c8d68077ed62b272f8daee864ad36114c0a2f5cc`
- 状态：
  - 项目 quota、usage 总览、创建前 preview 和创建时配额校验已经落地
