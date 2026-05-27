# 13 Reliable Teardown And Destroy Readiness

这一章不去碰第二个生产
`plane`，
也不去提前做多
`plane`
退役编排。

这一章只把当前这条已经存在的单
`cloud-plane`
退役链，
正式收成：

- 显式
  `teardown`
- 只读
  `destroy-readiness`
- provider-authoritative
  校验
- fail-closed
  的 destroy
  前置条件

## 这一章要解决什么问题

到 `12`
结束时，
平台其实已经能做真实云回收，
但 authority
还偏测试 harness：

- 真实云测试会：
  - 先调
    `POST /api/v1/platform/teardown`
  - 再等待 runtime
    节点回收
  - 再直接用测试侧 provider SDK
    去看 inventory
  - 最后才
    `terraform destroy`

这条链能用，
但它还有两个问题：

1. destroy
   前置条件不是正式平台能力
   - 平台自己还不能回答：
     - 现在能不能安全 destroy
     - 为什么不能
2. orphan runtime
   只能在 teardown
   的副作用路径里被发现
   - 还没有只读解释入口

所以这一章不是再重写一次 provider reclaim，
而是把已经存在的 teardown 能力，
收成正式的 destroy-ready
判定链。

## 这一章的正式边界

这一章明确只做单
`cloud-plane`
边界内的事：

- `teardown`
  继续是唯一会改 provider
  状态的动作
- `destroy-readiness`
  只读，
  不做自动清理
- destroy
  只能在
  `destroy-readiness.ready=true`
  时继续
- provider inventory
  不可读时一律 fail-closed

这一章明确不做：

- `control-plane`
  侧的多
  `plane`
  destroy orchestration
- 跨
  `plane`
  退役顺序编排
- 第二个生产
  `plane`
  接入
- destroy
  dashboard /
  告警 /
  趋势图

这些能力留给后续章节。

## 这一章新增的正式能力

### 1. `GET /api/v1/platform/destroy-readiness`

这章新增了正式只读入口：

- `GET /api/v1/platform/destroy-readiness`

它的职责很单纯：

- 读平台 store
  里的 runtime node
  生命周期记录
- 读 provider-authoritative
  runtime inventory
- 返回：
  - `ready`
  - `summary`
  - `issues`

也就是说，
destroy
前置检查终于不再藏在测试脚本里，
而是变成了平台 API
自己的正式能力。

### 2. orphan runtime
正式可解释

destroy-readiness
会显式区分下面这些 blocker：

- `active_runtime_node`
  - store
    里还有活跃 runtime，
    provider inventory
    里也还在
- `runtime_reclaim_failed`
  - 之前回收失败，
    必须先处理
- `runtime_record_missing_provider`
  - store
    里还有活跃 runtime
    记录，
    但 provider inventory
    已经找不到
- `reclaimed_runtime_still_owned`
  - store
    里标记成
    `reclaimed`，
    但 provider
    还报告资源存在
- `provider_orphan_runtime`
  - provider inventory
    里还有受管 runtime，
    但 store
    里找不到匹配记录

这样：

- destroy
  为什么被拒绝
- 具体卡在哪类残留
- 是否存在 orphan runtime

都能直接解释出来。

### 3. fail-closed
destroy 前置检查

destroy-readiness
现在明确是 fail-closed：

- provider inventory
  不可读：
  - 直接返回错误
- 任何 blocker
  存在：
  - `ready=false`
- 只有 blocker
  为零，
  destroy
  才允许继续

这也意味着：

- 测试侧不再自己拼
  provider inventory
  检查逻辑
- destroy
  authority
  回到了平台自身

## 为什么这章不把 `teardownActive` 当 destroy 前提

这章刻意没有把：

- `platformteardown.Controller`
  里的进程内
  `active`
  布尔值

当成 destroy-ready
的正式前提。

原因很简单：

- 它是进程内状态
- 重启后就丢
- 不能作为长期环境里的 durable
  退役判据

所以这一章的 destroy-ready
只看：

- 持久化的 runtime
  生命周期记录
- 实时 provider inventory

这让：

- 即使
  `cloud-plane`
  进程重启过
- destroy-ready
  也仍然能正确回答：
  - 现在是否安全
  - 为什么不安全

## 这章对真实云 destroy 流程的改变

这章之前，
阿里云 / 腾讯云真实测试在 destroy
前会：

- 显式 teardown
- 等 runtime
  节点回收
- 直接用测试侧 provider SDK
  验证 inventory
  归零

这章之后，
真实云 destroy
改成：

1. 显式
   `teardown`
2. 等 runtime
   节点在平台视图里回收完成
3. 调
   `GET /api/v1/platform/destroy-readiness`
4. 只有
   `ready=true`
   才继续
   `terraform destroy`

也就是说，
测试 harness
不再自己持有“什么叫可 destroy”
的规则，
它只消费平台 API
给出的正式判定。

## 这一章补的最小审计面

这章还补了两条平台级审计事件：

- `platform.teardown`
- `platform.destroy_readiness.check`

它们会记录：

- 谁触发
- 什么时候触发
- summary
  统计
- destroy-ready
  的 blocker code

这让：

- 为什么这次 destroy
  能继续
- 为什么这次 destroy
  被挡住

不再只是终端输出，
而是能回到平台操作历史里查。

## 这一章的代码落点

关键实现落在：

- [destroy_readiness.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/runtimenodeservice/destroy_readiness.go)
  - destroy-ready
    判定逻辑
- [platform.proto](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/proto/minicloud/cloudplane/v1/platform.proto)
  - 新增：
    - `OwnedRuntimeNode`
    - `DestroyReadinessIssue`
    - `DestroyReadinessSummary`
    - `DestroyReadinessResponse`
    - `GetDestroyReadiness`
- [grpc_platform_service.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/northbound/grpc_platform_service.go)
  - 新增 destroy-ready
    northbound
    接口
  - 为 teardown /
    destroy-ready
    补平台级审计
- [grpc_platform_convert.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/northbound/grpc_platform_convert.go)
  - destroy-ready
    响应转换
- [integration_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane/api/integration_test.go)
  - destroy-ready
    集成测试
- [aliyun/prepare.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/tests/aliyun/prepare.go)
- [tencent/prepare.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/tests/tencent/prepare.go)
  - 真实云 destroy
    前改成调用正式
    destroy-ready
    API

另外，
测试侧之前那组直接查询 provider inventory
的辅助文件已经删除，
避免 authority
继续散在测试代码里。

## 本地验证覆盖了什么

这章新增和修正后的本地验证，
重点覆盖了三件事：

1. destroy-ready
   能在 teardown
   前发现 blocker
   - 活跃 runtime
   - provider orphan
2. teardown
   完成后，
   destroy-ready
   能变成
   `ready=true`
3. provider inventory
   不可读时，
   destroy-ready
   会直接 fail-closed

也就是说，
这章已经把：

- 发现残留
- 解释残留
- 拒绝 destroy
- 在清理完成后放行 destroy

这条最小闭环正式收起来了。

## 这一章和下一章的关系

这章做完以后，
平台就能先回答：

- 当前这套单
  `cloud-plane`
  还能不能安全退役

只有这一点成立，
`14`
去接第二个生产
`plane`
才不会把残留资源问题直接翻倍。

所以顺序上必须先有：

- 可靠 teardown
- 正式 destroy-ready

再去做：

- 第二个长期运行
  `plane`

## 检查点

- 待本章提交时回填 commit hash。
