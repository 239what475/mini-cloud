# 10 Day2 Maintenance Upgrade And Decommission

这一章先不把
`day-2`
所有能力一次做完。

这一章先收最危险的一条链：

- runtime
  节点创建
- runtime
  节点回收
- platform teardown
- `terraform destroy`

因为这条链如果语义不对，
平台就会反复留下：

- 云上还在跑、
  但 Terraform
  和数据库都以为已经结束的资源

这不是：

- 重试不够久

而是：

- authority
  本身放错了地方

## 这一章要解决什么问题

到 `09`
结束时，
真实测试里已经反复出现同一类问题：

1. runtime
   节点不是 Terraform
   state
   里的资源
2. `platform teardown`
   又主要按数据库里的
   `runtime_nodes`
   去删
3. 测试销毁链还是：
   - 先 best-effort
     调一次
     `teardown`
   - 失败也继续
     `terraform destroy`

这会直接造成一个结构性问题：

- 只要数据库没记住、
  或者 `teardown`
  还没真正把 provider
  侧 runtime
  清空，
  后面的网络底座就会先被删

然后就会留下：

- 还挂在安全组 /
  子网里的真实实例

## 这一章采用的新正式语义

这一章把 runtime
生命周期收成下面这条链：

1. 先写
   runtime node
   `intent`
   到数据库
2. 再调用 provider
   创建实例
3. provider
   创建出来的实例必须带统一 tag：
   - `managed-by=mini-cloud`
   - `mini-cloud/platform=<platform>`
   - `mini-cloud/role=runtime`
   - `mini-cloud/runtime-node-id=<runtimeNodeID>`
4. provider
   返回后，
   再把真实
   `instance_id`
   绑定回这条
   `intent`
5. 如果 provider
   调用报错，
   `intent`
   不允许直接删掉
   - 因为这时候最危险的恰好就是：
     - 调用失败，
       但云上其实已经半成功

也就是说，
现在 runtime
资源的 authority
不再是：

- “数据库里有没有这条最终记录”

而是：

- 数据库里先有一条
  `intent`
- provider
  侧再用统一 tag
  成为真实存在性的权威面

## 这一章把 teardown 怎么改对

`platform teardown`
现在不再只是：

- 列出数据库里的
  `runtime_nodes`
  然后逐条删

它现在要走下面这条链：

1. 先读数据库里的
   `runtime_nodes`
2. 再直接读 provider
   authoritative
   runtime inventory
3. 把两边做一次 reconcile
   - store
     里有、
     provider
     也有：
     正常回收
   - store
     里有、
     provider
     没有：
     把数据库状态收成
     `reclaimed`
   - provider
     里有、
     store
     没有：
     当成 orphan
     直接回收
4. 回收完成后，
   再次回读 provider
   inventory
5. 只有 provider
   侧确认为 `0`，
   `teardown`
   才算成功

这就是这一章最重要的变化：

- `teardown`
  必须对 provider
  侧为零负责

而不是：

- 只对数据库视角里的“看起来删完了”负责

## 这一章把 destroy 改成 fail-closed

真实云测试里的销毁链，
现在明确改成：

1. 先恢复管理员访问 CIDR
2. 再跑
   `POST /api/v1/platform/teardown`
3. 再等待
   `cloud-plane`
   自己的 runtime
   inventory
   收敛到空
4. 再直接用 provider
   API
   回读：
   - 这个平台名下的 runtime
     是否真的已经为 `0`
5. 只有前面都成功，
   才允许继续
   `terraform destroy`

也就是说，
现在不会再接受这种语义：

- `teardown`
  失败了，
  但先把网络和主机删掉再说

如果 provider
回读还看到 runtime
资源，
测试会直接中止。

## 代码上这一章收了哪些点

runtime lifecycle
这边的关键变更：

- [service.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/deployservice/service.go)
  - 先创建
    runtime node
    `intent`
  - provider
    报错时不再删掉
    `intent`
- [runtime_node_store.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/store/runtime_node_store.go)
  - 允许
    `instance_id`
    先为空
  - 增加
    bind /
    intent name
    update /
    provisioning pending
    这些持久化动作
  - 把 bind
    改成
    compare-and-set
    语义
- [00031_runtime_nodes_allow_unbound_instance_id.sql](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/store/migrations/00031_runtime_nodes_allow_unbound_instance_id.sql)
  - 允许
    `runtime_nodes.instance_id`
    在
    `intent`
    阶段为空

provider authoritative inventory
这边的关键变更：

- [helpers.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/provider/common/helpers.go)
  - 统一 runtime
    tag
    合同
- [provisioner.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/provider/aliyun/provisioner.go)
  - 阿里云创建实例时补齐统一 tag
  - 增加
    `ListOwnedRuntimeNodes`
- [provisioner.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/provider/tencent/provisioner.go)
  - 腾讯云创建实例时补齐统一 tag
  - 增加
    `ListOwnedRuntimeNodes`
- [provisioner.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/provider/local/provisioner.go)
  - 明确
    `local`
    provider
    不支持 provider-authoritative
    runtime inventory

teardown
这边的关键变更：

- [service.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/runtimenodeservice/service.go)
  - `platform teardown`
    现在合并：
    - store inventory
    - provider inventory
  - 能直接回收 provider
    orphan runtime
  - 最后会再做一轮 provider
    verify

真实测试
这边的关键变更：

- [prepare.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/tests/aliyun/prepare.go)
- [prepare.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/tests/tencent/prepare.go)
  - destroy
    前不再是 best-effort
  - 现在是
    fail-closed
- [provider_inventory.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/tests/aliyun/provider_inventory.go)
- [provider_inventory.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/tests/tencent/provider_inventory.go)
  - 真实测试直接用 provider
    API
    回读平台名下的 runtime
    inventory

## 这一章刻意没做什么

这一章没有把所有
`day-2`
能力一次做完。

还没展开的内容包括：

- `cloud-plane`
  升级编排
- `agent`
  版本漂移治理
- 节点替换
- pool shrink
- 服务迁移

原因很简单：

在这些能力之前，
必须先保证：

- 资源退役
- 平台销毁
- 失败时停止继续删底座

这三件事是可靠的。

## 这一章完成后应该怎么理解平台

做到这里以后，
平台的退役链应该这样理解：

- Terraform
  只负责底座
- runtime
  实例的存在性真相，
  由 provider
  inventory
  决定
- `cloud-plane`
  负责把：
  - store
    视图
  - provider
    视图
  收敛到一致
- destroy
  默认必须
  fail-closed

这比：

- “加更多轮询和重试”

重要得多，
因为它修的是：

- 资源生命周期 authority

而不是：

- 单次 API
  调用的偶发抖动
