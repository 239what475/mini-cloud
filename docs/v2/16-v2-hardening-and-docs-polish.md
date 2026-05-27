# 16 v2 Hardening and Docs Polish

到 `15`
为止，
`mini-cloud v2`
已经不缺“能力点”了。

它已经有：

- 本地快速检查
- 真实 `Postgres` 集成测试
- 最小主链 smoke
- 完整模拟环境
- 备份 / 恢复 / 灾难演练
- 真实阿里云端到端实验
- 真实阿里云里的升级 / 重装 / 销毁

这时最容易出现的新问题反而是：

- 入口太多
- 不知道什么时候该跑哪一条
- 本地脚本和真实云脚本的边界开始变模糊

所以这一章不再新增平台能力，
而是做三件事：

1. 把 `v2` 现在已有的脚本入口统一整理
2. 把真实环境里暴露出来的小毛刺重新明确下来
3. 给 `v2` 收出一套更稳定的操作顺序

## 先把 v2 的入口重新分层

现在可以把 `v2`
的入口理解成四层。

### 第一层：本地快速检查

这层只关心：

- 改动有没有立刻把项目弄坏

入口是：

- `./scripts/check.sh`

它负责：

- `go test ./...`
- `go vet ./...`
- `staticcheck ./...`

这层的特点是：

- 最快
- 成本最低
- 适合频繁跑

但它不验证：

- 真实数据库
- 真实容器发布
- 真实备份恢复
- 真实云资源

## 第二层：本地关键链验证

这层开始验证“平台是不是还能真的跑起来”。

它主要有两条：

- `./scripts/test-integration.sh`
- `./scripts/smoke.sh`

两者的边界可以这么记：

| 入口 | 重点 |
| --- | --- |
| `test-integration.sh` | 真实 `Postgres` 集成 |
| `smoke.sh` | 从节点注册到 app 运行的最小主链 |

如果你改的是：

- store
- migration
- API 持久化

优先跑：

- `test-integration.sh`

如果你改的是：

- scheduler
- deployment
- execution
- runtime
- release 主链

优先跑：

- `smoke.sh`

这两条都还是：

- 本地
- 低成本
- 自动清理现场

所以它们适合当成：

- “比 `check.sh` 更重一点，但仍然应该经常跑”的中间层

## 第三层：本地完整组合场景

到了这层，
重点已经不再是单个子系统，
而是：

- 多个能力放在一起还能不能成立

这层对应两条脚本：

- `./scripts/simulated-env.sh`
- `./scripts/disaster-drill.sh`

### simulated-env.sh 在回答什么

它回答的是：

- 如果把 `v2`
  当前最关键的组合能力串起来，
  本地能不能得到一套可重复的完整教学环境

它适合验证：

- 发布
- 回滚
- 节点掉线
- 备份恢复

而且它更适合：

- 教学
- 展示
- 回归一整套 example

### disaster-drill.sh 在回答什么

它回答的是：

- 当前平台状态真的备份得出来吗
- 备份出来以后真的恢复得回来吗

所以：

- `simulated-env.sh`
  更像“完整 example”
- `disaster-drill.sh`
  更像“聚焦灾难恢复”

## 第四层：真实阿里云验收

这层对应：

- `./scripts/real-aliyun-lab.sh`

它不是为了取代前面三层，
而是为了验证前面三层都无法完全覆盖的东西：

- `bootstrap`
- 阿里云资源生命周期
- 真实公网访问
- 真实 SSH
- 真实远端安装
- 真实 `systemd`
- 真实镜像拉取速度

现在这条真实链路已经不是只有：

- `install`
- `status`
- `demo`
- `cleanup`

而是扩展成了：

- `install`
- `status`
- `demo`
- `upgrade`
- `reinstall`
- `cleanup`
- `destroy`

这里要特别注意：

- `destroy`
  只是 `cleanup` 的别名

## 现在推荐的验证顺序

如果你在做 `v2`
的新改动，
现在比较推荐按下面顺序加深。

### 1. 先跑最轻的

```bash
./scripts/check.sh
```

这一步不过，
通常没必要立刻去跑更重的脚本。

### 2. 再看你改动碰到哪一层

如果主要碰到：

- store
- migration
- API

补：

```bash
./scripts/test-integration.sh
```

如果主要碰到：

- 调度
- 发布
- runtime
- deployment 状态机

补：

```bash
./scripts/smoke.sh
```

### 3. 如果这次改动是 v2 级别组合能力

补：

```bash
./scripts/simulated-env.sh start
./scripts/simulated-env.sh scenario
./scripts/simulated-env.sh reset
```

如果重点就是备份恢复，
则优先：

```bash
./scripts/disaster-drill.sh
```

### 4. 只有碰到真实云能力时，再上阿里云

例如你改的是：

- `bootstrap`
- 远端安装
- 真实平台生命周期
- 升级 / 重装 / 销毁

这时再跑：

```bash
./scripts/real-aliyun-lab.sh install --ssh-key ~/.ssh/<your-private-key.pem>
./scripts/real-aliyun-lab.sh status
./scripts/real-aliyun-lab.sh demo
./scripts/real-aliyun-lab.sh upgrade
./scripts/real-aliyun-lab.sh status
./scripts/real-aliyun-lab.sh reinstall
./scripts/real-aliyun-lab.sh status
./scripts/real-aliyun-lab.sh demo
./scripts/real-aliyun-lab.sh cleanup
```

这样做的意义是：

- 把高成本验证留到最后
- 先用本地脚本拦住大部分低级问题

## 真实环境里这次确认下来的几个细节

这一章虽然主要是整理，
但不是纯文档整理。

因为在上一章真实验收里，
我们确实看到了一些“只有真实环境才会暴露出来”的细节。

### 1. 冷拉业务镜像时，Docker 超时不能太短

第一次在真实阿里云里发布：

- `nginx:1.27-alpine`

时，
原来的 agent 默认：

- `docker-timeout=30s`

不够长。

结果表现成：

- agent 先报：
  - `docker run failed: context deadline exceeded`
- 但 Docker daemon 后面其实还把镜像拉下来了
- 容器甚至已经在宿主机上启动了

所以后面已经把默认值提高到：

- `2m`

这个改动不是为了“把错误藏起来”，
而是因为真实云环境里的：

- 首次冷拉镜像

本来就比本地慢很多。

### 2. reinstall 不只是删平台数据，也要删旧 workload 容器

如果只删：

- control-plane
- agent state
- `Postgres`

却不删旧的：

- `mini-cloud-dep_*`

业务容器，
那就会出现一种很怪的状态：

- 平台数据库已经是空白
- 但宿主机上还有旧业务容器在跑

这不符合：

- “保留云资源，但把平台重装干净”

的目标。

所以现在 `reinstall`
会一起清理：

- `mini-cloud-dep_*`

这一点很重要，
因为它说明：

- “平台状态”
  不只在数据库里
- 还包括节点上的运行时残留

### 3. cleanup 会删云资源，但不会删 key pair

当前这套真实实验里：

- `KeyPairName`

依然被当成外部输入，
不是平台受管资源。

所以 `cleanup`
会删除：

- 平台 `ECS`
- `EIP`
- `security group`
- `vSwitch`
- `VPC`
- RAM role

但不会删除：

- 你本来就在阿里云里准备好的 key pair

这不是遗漏，
而是当前设计有意保留的边界。

## 现在应该怎么理解 v2 的“完成度”

到了这里，
`v2`
其实已经有两条比较完整的闭环：

1. 本地闭环
   - `check`
   - `test-integration`
   - `smoke`
   - `simulated-env`
   - `disaster-drill`
2. 真实阿里云闭环
   - `bootstrap`
   - `install`
   - `deploy`
   - `upgrade`
   - `reinstall`
   - `cleanup`

这意味着：

- `v2`
  已经不再只是“做了一些零散功能”
- 而是真的有了：
  - 本地教学入口
  - 本地回归入口
  - 真实云验收入口

这就是这一章所谓的：

- `hardening`
- `docs polish`

它不是做一个花哨的新功能，
而是把前面已经做成的东西，
整理成一套更清楚、更可维护、更适合继续往 `17` 收束的结构。

## 这章之后还剩什么

到了 `16`
之后，
`v2`
剩下的工作已经很集中：

- 回看这版到底做成了什么
- 明确哪些边界还留给 `v3`
- 把这一版的得失总结出来

也就是说，
下一章：

- `17-v2-final-review`

更像是对整条 `v2`
主线做一次最终回看，
而不再是继续铺新能力。

## 本章检查点

- 提交：
  - `8254c0bf6531d32c7630d40427658dda3a7434be`
- 状态：
  - `README.md` 已补“按场景选择入口”
  - `docs/v2/README.md` 已同步到 `15/16`
  - `v2` 当前脚本分层、推荐验证顺序和真实环境注意点已经重新整理
