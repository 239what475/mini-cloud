# v4/14 Real Dual Plane End To End

这一章不是再加一层新设计。

这一章要做的事情更直接：

- 把 `v4/01` 到 `v4/13`
  已经做出来的三层能力，
  放进一套真实双 `cloud-plane`
  环境里跑一遍。

但这里的“真实双 plane”
要先说清楚是什么意思。

## 这章的真实拓扑

这章跑出来的不是：

- 三套都在云上长期部署的正式平台

而是：

- 一套本地临时 `control-plane`
- 一套真实阿里云 `cloud-plane`
- 一套真实腾讯云 `cloud-plane`

也就是：

```text
本地电脑
  └─ local control-plane
       └─ local postgres
            ├─ register / sync -> aliyun cloud-plane
            └─ register / sync -> tencent cloud-plane
```

这里这样设计是刻意的。

因为这章真正要验的是：

- 一个 fleet `control-plane`
  能不能同时纳管两套真实远端 `cloud-plane`

而不是：

- `control-plane`
  自己怎么云上自举
- 多活 `control-plane`
  怎么部署

那些事情不属于这一章。

## 这章到底验什么

这章现在真实验掉的是下面这些链路：

1. 两套真实 `cloud-plane`
   能注册进同一个 `control-plane`
2. `control-plane`
   的全局 inventory
   能同时看到：
   - 阿里云 plane
   - 腾讯云 plane
3. `control-plane`
   的 `/metrics/fleet`
   能看到双 plane 的状态变化
4. 手工指定 plane 下发部署能走通
   - 明确下发到阿里云 plane
   - 明确下发到腾讯云 plane
5. 约束下的自动放置能走通
   - 当前仍然要求显式给 `provider`
   - 当前仍然要求显式给 `region`
6. `maintenance`
   会阻止该 plane 接收新部署
7. 单 plane 管理链路中断以后：
   - fleet 能把它看成 `offline`
   - 可以记录 incident
   - 恢复后可以重新 sync 回来
8. 两套真实 `cloud-plane`
   在结束时会先做显式 `teardown`
   再回收云资源

## 这章没有证明什么

这部分必须说清楚。

这章没有证明：

- 全局日志聚合已经在真实双 plane 里闭环
- Alertmanager 自动告警已经在真实双 plane 里闭环
- 用户流量已经可以跨云自动切换
- 阿里云 plane 挂了以后会自动切腾讯云 plane
- `control-plane`
  自己已经是高可用部署
- 这已经是“正式多云平台上线验证”

原因很简单：

- 这章的真实路径没有额外起共享 `Loki`
- 也没有额外起共享 `Alertmanager`
- 自动放置当前仍然是：
  - `provider + region`
    约束下的选址
- 故障演练当前打的是：
  - `control-plane -> cloud-plane`
    southbound 管理链路中断

所以这一章最准确的名字应该理解成：

- 真实双 `cloud-plane`
  管理面验收演练

不是：

- 完整多云控制平台能力证明

## 为什么这里不用“云厂商故障”来做演练

这章当前的故障注入方式是：

- 暂时把某个真实 plane
  的管理面放通
  从测试默认的 `0.0.0.0/0`
  收紧成一个故意不可达的坏 `CIDR`
- 再主动触发一次 fleet sync

这样模拟出来的是：

- fleet `control-plane`
  暂时无法访问这个 `cloud-plane`

也就是说，
这章演练的是：

- southbound 管理链路中断

不是：

- 机器真的坏了
- 业务流量真的断了
- 云厂商真的不可用

这样做的好处是：

- 注入边界清楚
- 恢复路径清楚
- 成本和时间可控

## 当前真实测试入口

这章对应的真实入口是：

- [main.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/tests/main.go)
- [run.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/tests/dual/run.go)

运行方式：

```bash
cd projects/mini-cloud/tests
go run . dual
```

它会依赖：

- [config.json.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/tests/aliyun/config.json.example)
- [config.json.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/tests/tencent/config.json.example)

所以在真正运行前，
你需要先准备好：

- `tests/aliyun/config.json`
- `tests/tencent/config.json`

以及本机对应的：

- 阿里云 CLI 凭证
- 腾讯云 CLI 凭证

这里再补一条这章现在的测试约定：

- 真实 E2E 测试阶段，
  `allowed_admin_cidrs`
  固定使用：
  - `0.0.0.0/0`
- 正式部署阶段，
  再换成云服务器稳定的静态公网 `IP/CIDR`

这样做不是为了“正式部署也全放开”，
而是因为本地电脑出口 `IP`
 经常变化，
这会把真实测试的不确定性
混进管理面验证里。

## 这条真实链路是怎么收口的

### 1. 先起本地临时 `control-plane`

对应代码：

- [runner.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/tests/internal/localcontrolplane/runner.go)

这里会在本地做两件事：

- 临时拉起一个本地 `Postgres`
- 临时启动一个本地 `control-plane`

这样做的目标不是长期部署，
而是为了让这一章把注意力放在：

- fleet 能不能纳管两套真实远端 plane

### 2. 再分别起两套真实 `cloud-plane`

对应代码：

- [environment.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/tests/aliyun/environment.go)
- [environment.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/tests/tencent/environment.go)

这里不会共用同一份 Terraform 状态。

现在每套真实环境都会先把：

- `deploy/terraform`

复制到自己的临时目录里，
再分别做：

- `terraform init`
- `terraform apply`
- `terraform destroy`

对应代码：

- [workdir.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/tests/internal/terraformlab/workdir.go)

这一点很重要，
否则双 plane 测试会共用同一份本地 state，
第二套会直接覆盖第一套。

### 3. 注册、同步、全局 inventory

两套真实 plane 起好以后，
`control-plane`
会做：

- `create plane`
- `register`
- `sync`

然后检查：

- `/api/v1/fleet/inventory`
- `/metrics/fleet`

这一步真正证明的是：

- 全局控制面已经能同时看见两套 provider 不同的 plane

但这里只是：

- inventory
- fleet metrics

不是：

- 全量日志联邦
- 全量告警联邦

### 4. 手工指定 plane 下发

这章会明确做两次：

- 把一个 app 手工下发到阿里云 plane
- 把另一个 app 手工下发到腾讯云 plane

然后分别回读对应 plane 上的：

- app 状态
- runtime worker 状态

所以这一步证明的是：

- `control-plane`
  对指定 plane 的 southbound 下发闭环是通的

### 5. 自动放置

这里要特别注意边界。

当前自动放置并不是：

- 在阿里云和腾讯云之间自由比较后选最优

当前仍然是：

- 先指定 `provider`
- 再指定 `region`
- 然后在这个约束下选可落点

所以这一章里“自动放置走通”的准确含义是：

- 约束下的 placement preview / auto apply 是通的

不是：

- 跨云自动接管已经成立

### 6. maintenance 拒绝新部署

这章会把其中一套 plane
切到：

- `maintenance`

然后再做一次 placement preview。

你会看到：

- 不是“自动换云”
- 而是“当前约束下已经没有可接单的 plane”

这一步是在真实双 plane 环境里，
把 `v4/11`
的 maintenance 语义真正跑一遍。

这里现在不只检查：

- preview 没有给出最终决策

还会继续检查：

- 目标 candidate
  仍然出现在返回里
- `operationState=maintenance`
- `acceptingNewDeployments=false`
- `filteredCounts.operation >= 1`
- 没有 candidate
  被错误地标成 `selected`

### 7. 单 plane 管理链路中断与恢复

这章最后会做一次最小故障演练：

- 暂时把阿里云 plane
  的管理面放通
  从 `0.0.0.0/0`
  改成一个故意不可达的坏 `CIDR`
- 让 fleet sync 最终观察到：
  - 该 plane 进入 `offline`
- 手工记录一条 incident
- 再把管理面放通
  恢复回：
  - `0.0.0.0/0`
- 再手工 `sync`
  让 plane 回到 `ready`
- 再 resolve 这条 incident

离线这一步现在也不只检查：

- plane 名义上变成了 `offline`

还会继续检查：

- offline preview
  的 `decision=null`
- 目标 candidate
  仍然在返回里
- `status=offline`
- `filteredCounts.status >= 1`
- 没有 candidate
  被错误地标成 `selected`
- 顶层 `failureReason`
  不是空

这一步验证的是：

- 可观测
- 可记录
- 可恢复

而且现在的“恢复”
也不只是：

- 手工 sync 回 `ready`

恢复放通以后，
这章还会对同一个阿里云 app
再发一次带
`RECOVERY_MARKER`
的新更新，
并等待一个新的 revision
真正被 promoted 到 `running`。

也就是说，
这里验证到的是：

- southbound 恢复后
  还能继续完成一次真实 rollout

但它仍然只是：

- southbound 管理链路恢复

不是：

- 业务自动迁移

## 资源清理为什么现在更稳

这章对应的真实清理路径做了两件事情：

1. 每套真实 plane
   用自己的临时 Terraform 工作目录
2. 在真正 `destroy`
   之前，
   先显式调用 plane 自己的：
   - `/api/v1/platform/teardown`

另外，
当前环境封装还会尽量处理这些失败路径：

- `PrepareEnvironment`
  失败时先尝试自动 destroy
- destroy 失败时不立即删掉本地 Terraform 工作目录
- 临时收紧管理面放通后如果测试中途失败，
  也会尽量先恢复到：
  - `0.0.0.0/0`

这一轮又补了三个真正影响真实回收稳定性的细节：

- 如果当前环境连
  `cloud-plane`
  的 `baseURL`
  都还没有建立起来，
  destroy 前就不会再为了恢复测试放通或切换
  `runtime_cleanup_on_destroy`
  而额外做一次 pre-destroy `terraform apply`
- 上传到 `OSS/COS`
  的二进制清理，
  现在使用独立的 cleanup 超时上下文，
  不再复用前面可能已经超时的 destroy 上下文
- 腾讯云平台主机绑定 `EIP`
  后，
  Terraform 不再尝试回写
  `internet_max_bandwidth_out`
  这个会漂移的字段，
  避免 destroy 之前先被一次无效更新卡住

对应代码：

- [main.tf](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/terraform/providers/tencent/modules/platform-host/main.tf)

这并不代表：

- 真实云测试已经完全不会残留资源

但至少现在这条链路已经不是：

- 只要中途失败就直接把状态删掉

## 这次真实双 plane 重跑里确认的四件事

### 1. 腾讯云 bootstrap 的镜像拉取会偶发失败，必须在模板层重试

这次真实双 plane 调试里，
腾讯云最早暴露出来的问题不是：

- Terraform 没建出机器

而是：

- `cloud-init`
  里的 `docker pull postgres:17`
  会偶发失败
- 失败时会让
  `scripts-user`
  提前退出，
  于是 `cloud-plane`
  服务根本还没创建好

从本地看，
你只会看到：

- `:8080 connect: connection refused`

但真正的根因在节点里。

这次最终的收敛方式是：

- 在阿里云 / 腾讯云的 bootstrap 模板里，
  都加上：
  - 带单次超时的 `docker_pull_with_retry`
- 让镜像站偶发 `500`
  不再直接把整条首启链路打死

对应代码：

- [user-data.sh.tftpl](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/terraform/providers/aliyun/templates/user-data.sh.tftpl)
- [user-data.sh.tftpl](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/terraform/providers/tencent/templates/user-data.sh.tftpl)

### 2. `ImagePull` 返回了，不代表镜像已经真的可用

这次真实双 plane 里，
腾讯云 app 部署阶段还暴露了另一个更隐蔽的问题：

- worker 侧已经调用了 `ImagePull`
- 但后续 `docker run`
  仍然报：
  - `No such image`

这说明旧实现里存在一个错误假设：

- pull 流返回结束
  就等于镜像已经落盘可用了

真实运行证明这个假设不稳。

这次最终的修复方式是：

- `ensureImageAvailable()`
  在 pull 后重新 `inspect`
  镜像
- 如果镜像还不存在，
  就继续重试
- 每次 pull
  都带单次超时，
  不让一次卡死拖垮整个部署

对应代码：

- [docker_engine.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudworker/runtime/docker_engine.go)

### 3. 腾讯云 destroy 阶段会遇到绑定 `EIP` 后的带宽漂移

这次真实失败 run 的清理阶段，
还暴露了一个纯销毁路径的问题：

- 平台主机绑定 `EIP`
  以后，
  腾讯云侧回读出来的带宽字段
  和 Terraform 状态不再稳定一致
- destroy 前，
  Terraform 会先试图“修正”这个字段
- 但这个修正请求本身就是无效的，
  结果反而把 destroy 卡住

所以这里真正要做的不是：

- 再写一层额外补丁逻辑

而是：

- 明确告诉 Terraform
  这个字段在当前资源组合下不值得做一致性回写

也就是前面提到的：

- `ignore_changes = [internet_max_bandwidth_out]`

### 4. `2026-04-15` 这轮真实双 plane 已经全自动通过

这一轮最终通过的不是：

- 单边阿里云
- 单边腾讯云

而是完整的双 plane 主链：

- 本地临时 `control-plane`
  + 本地临时 `Postgres`
- 一套真实阿里云 `cloud-plane`
- 一套真实腾讯云 `cloud-plane`
- 两套 plane
  自动注册进同一个 fleet
- 手工指定阿里云 plane
  的下发成功
- 手工指定腾讯云 plane
  的下发成功
- 受 `provider + region`
  约束的自动放置成功
- `maintenance`
  场景下的 preview
  正确返回：
  - 没有最终决策
  - 没有候选项被选中
- 暂时切断阿里云 southbound 管理链路后，
  plane 进入 `offline`
- 恢复放通后，
  plane 回到 `ready`
- 恢复后对同一个阿里云 app
  的新 apply
  成功触发了新的 revision promotion
- 显式 `teardown`
  成功回收两侧 runtime
- 阿里云 / 腾讯云两边的
  `terraform destroy`
  都成功
- 临时上传到 `OSS/COS`
  的二进制对象也都删除成功

这次最终的测试进程以：

- `dual e2e passed`

结束。

实际执行命令就是：

```bash
cd projects/mini-cloud/tests
go run . dual
```

## 本章建议怎么理解

如果你只记一句话，
我建议记这个：

- `v4/14`
  证明的是：
  - 一个本地 fleet `control-plane`
    已经能把两套真实 provider 不同的 `cloud-plane`
    纳管起来，
    并完成最核心的管理面验收闭环

不要把它理解成：

- `mini-cloud`
  已经完成完整多云平台上线验证

## 本章检查点

- 章节检查点：
  - `468e01e88981e3ccf178db76043fb6c269c183c6`
