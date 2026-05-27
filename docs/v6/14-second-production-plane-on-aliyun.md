# 14 Second Production Plane On Aliyun

这一章把
`v6/11`
里已经收好的：

- 长期运行 `control-plane`
- 第一个长期运行 Tencent `cloud-plane`

再往前推一步，
正式接入：

- 第二个长期运行的 Aliyun `cloud-plane`

这里的目标不是继续发散平台模型，
而是把“多运行面”这件事本身先收干净。

## 这章只做什么

这章只做三件事：

1. 把阿里云入口机收成第二个长期运行 plane
2. 让 `control-plane` 能稳定同时纳管两个 plane
3. 让服务可以显式指定运行到某个 plane

## 这章明确不做什么

这章刻意不做：

- 跨 plane 服务切换
- 双活 / 主备流量调度
- 全局 destroy 编排
- CDN / 域名 / TLS
- 观测栈落地

也就是说，
这里先只把：

- 第二个长期运行面
- 双 plane 纳管
- 显式 placement

收成正式能力。

## 为什么这章现在必须做

到 `13`
结束时，
单个 `cloud-plane`
的：

- `teardown`
- `destroy-readiness`

已经收成了正式闭环。

但平台仍然只有一个长期运行的生产 plane。

这会导致后面的：

- 观测分层
- 前门入口
- 真实应用验证

仍然只能在“单 plane”前提下成立。

所以这章要先把生产拓扑扩成：

- Tencent 长期运行 plane
- Aliyun 长期运行 plane

并确认 `control-plane`
不会因为其中一个 plane 的状态变化，
污染另一个 plane 的视图和调度。

## 这一章补的正式资产

### Aliyun 平台配置模板

新增：

- [platform-config.aliyun.json.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/cloud-plane/platform-config.aliyun.json.example)

这让 `deploy/cloud-plane/`
不再是 Tencent-first 的单一模板目录，
而是正式变成：

- 同一套长期运行部署资产
- 按 provider 选择平台配置模板

### Project 绑定脚本

新增：

- [bind-project-plane.sh](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/scripts/bind-project-plane.sh)

这一步很重要，
因为服务能运行到某个 plane，
前提是 project
已经被正式绑定到这个 plane。

之前仓库里虽然已经有 binding API，
但 operator 还需要手工拼请求。

这章把它收成：

- 幂等脚本
- 标准输入参数
- 标准输出摘要

这样接第二个长期运行 plane
时，
operator 的最小链路就完整了：

1. 部署 `cloud-plane`
2. 注册 plane
3. 绑定 project
4. 显式指定服务运行面

## 这一章修掉的行为缺口

这章顺手修了一个真实实现问题：

- `control-plane`
  的 auto-apply
  输入虽然已经有
  `pinnedPlaneID`
  语义
- 但在进入 placement
  决策时，
  这个字段之前没有继续往下传

结果就是：

- 模型上看起来支持“显式指定 plane”
- 但 `control-plane`
  的 auto-apply
  路径实际上可能忽略这个要求

现在这条链已经收通：

- 请求里带 `pinnedPlaneID`
- placement 只在这个 plane 上做决策
- auto-apply
  结果也必须落到这个 plane

## 双 plane 稳定纳管现在怎么验证

这章新增了两组验证。

### 1. plane sync 独立性

`planesync`
现在补了测试，
明确验证：

- 两个 plane 都已注册
- 其中一个同步成功
- 另一个同步失败

结果应该是：

- 成功的 plane
  正常写入 runtime inventory /
  capacity snapshot /
  ready 状态
- 失败的 plane
  只更新自己的失败状态
- 一个 plane 的失败
  不会阻断另一个 plane 的成功收敛

这就是“稳定纳管”的最小含义。

### 2. 显式 pinned plane

placement 和 control API
现在都补了验证：

- 同 provider / 同 region
  下存在两个 ready plane
- 默认 preview / auto-apply
  会按容量和策略选更合适的 plane
- 如果显式给出
  `pinnedPlaneID`
  就必须落到指定 plane

这保证了“多 plane”
不会立刻演变成“只能自动调度，不能人工指定”。

## operator 现在的最小操作链

下面这条链就是这章完成后的正式用法。

### 1. 在 Aliyun 主机准备 cloud-plane 配置

从：

- [platform-config.aliyun.json.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/cloud-plane/platform-config.aliyun.json.example)

复制出本机实际使用的：

- `deploy/cloud-plane/platform-config.json`

然后继续沿用
[deploy/cloud-plane/README.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/cloud-plane/README.md)
里的长期运行安装步骤：

- 安装二进制
- 安装 env
- 安装 `systemd`
- 启动 `cloud-plane`
- 启动本机 `agent`

### 2. 注册第二个生产 plane

继续用：

- [register-production-plane.sh](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/scripts/register-production-plane.sh)

把这台 Aliyun 主机注册到：

- 已长期运行的 Tencent `control-plane`

### 3. 绑定 project 到这个 plane

用：

- [bind-project-plane.sh](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/scripts/bind-project-plane.sh)

把需要部署的 project
绑定到这个 Aliyun plane。

这里不再把这一步写成
break-glass / admin
日常流程，
而是明确要求：

- 使用具备
  `project.deploy.write`
  的平台 token

### 4. 显式把服务 pin 到这个 plane

后续服务创建或 control auto-apply
都可以显式带上：

```json
{
  "pinnedPlaneID": "pln_xxx"
}
```

这样即使另一个 plane
容量更高，
调度也不会偏离 operator
显式指定的运行面。

## 这章结束后的平台形态

到这里，
平台的正式形态变成：

- Tencent `control-plane`
- Tencent 第一个长期运行 plane
- Aliyun 第二个长期运行 plane

并且已经具备：

- 双 plane 独立纳管
- project 到 plane 的正式绑定
- 服务显式指定 plane

后面做观测栈时，
就可以在这个真实双 plane
拓扑上继续往下收：

- 每个 plane 的本地采集
- 中心层聚合
- operator 级观测入口

## 检查点

- 待本章提交时回填 commit hash。
