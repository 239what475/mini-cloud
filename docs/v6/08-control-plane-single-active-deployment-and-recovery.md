# 08 Control Plane Single Active Deployment And Recovery

这一章不做多活 `control-plane`。

这一章只做一件更务实的事：

- 把 `control-plane`
  收成一个正式可部署、
  可备份、
  可恢复、
  可替换的单活控制面

## 为什么这一章不再叫 fleet

到 `06`
为止，
当前平台已经明确收成：

- `control-plane`
  - 唯一控制决策面
- `cloud-plane`
  - 单云执行面
- `agent`
  - 节点侧执行进程

所以这一章如果还继续写：

- `fleet control-plane`

只会把已经收好的三层边界重新搅乱。

这章之后，
我们直接用：

- `control-plane`

这个正式名字。

## 这一章要解决什么问题

到 `07`
结束时，
平台已经能跑很多事情了，
但 `control-plane`
本身还更像：

- 本地开发入口
- 本地实验进程
- 可以演练恢复，
  但没有正式部署布局

这会带来一个很现实的问题：

- 如果 `control-plane`
  所在主机坏了，
  平台有没有一条明确、
  可重复、
  可验证的恢复路径

这一章就是要把这条路径收出来。

## 这一章明确收什么能力

这一章结束后，
`control-plane`
要具备下面这些正式能力：

1. 单活部署布局
   - 二进制放在哪里
   - 环境文件放在哪里
   - `systemd`
     单元怎么写
2. 单活数据库依赖布局
   - `control-plane`
     依赖一个明确的
     `Postgres`
   - 当前这章先按：
     - 单机
     - 单实例
     - 本地容器化 `Postgres`
     来收
3. 备份 bundle
   - 数据库逻辑备份
   - 进程环境文件快照
4. 恢复路径
   - 先恢复数据库
   - 再恢复配置快照
   - 再重启 `control-plane`
5. replace-in-place /
   cold-standby
   恢复
   - 主机坏了以后，
     可以在替代主机上
     重新拉起同一份
     `control-plane`
   - 不要求自动切主
6. restore 后自动重新同步
   - 不重新手工 register
     `cloud-plane`
   - 只依赖已有 registration
     和后台 sync
     把运行快照重新拉回来

## 这一章明确不做什么

这一章明确不做：

- 多活 `control-plane`
- leader election
- 共识复制
- 自动切主
- `cloud-plane / node / service`
  的 `day-2`
  运维
- `09`
  才会收的：
  - token 轮转
  - 证书轮转
  - 更完整的控制链安全
- `11`
  才会收的：
  - operator
    可观测性产品化

也就是说，
这章的目标不是：

- “`control-plane`
   永远不会挂”

而是：

- “`control-plane`
   挂了以后，
   有正式恢复路径”

## 这一章采用的正式部署布局

这一章新增一套正式部署资产：

- [deploy/control-plane/README.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/control-plane/README.md)
- [control-plane.env.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/control-plane/control-plane.env.example)
- [docker-compose.postgres.yml.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/control-plane/docker-compose.postgres.yml.example)
- [mini-cloud-control-plane.service.example](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/control-plane/systemd/mini-cloud-control-plane.service.example)

这套布局背后的决定是：

- `control-plane`
  本体用：
  - Go 二进制
  - `systemd`
    管理
- `Postgres`
  先按：
  - 单机
  - 单实例
  - Docker 容器
    依赖
  来收

这样做的原因是：

- 不把 `control-plane`
  再包成另一层复杂运行模型
- 也不在这一章引入托管数据库、
  外部编排器、
  多活数据库这些新变量

## 这一章把恢复链路怎么收成正式能力

仓库里原来已经有三条脚本：

- [backup-control-plane.sh](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/scripts/backup-control-plane.sh)
- [restore-control-plane.sh](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/scripts/restore-control-plane.sh)
- [control-plane-disaster-drill.sh](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/scripts/control-plane-disaster-drill.sh)

但它们之前更像：

- 本地实验脚本

这一章把它们正式收成：

- 单活 `control-plane`
  的最小恢复主线

也就是说，
现在要把下面这条动作链当成正式验收目标：

1. `backup`
   - 打出数据库 dump
   - 打出环境文件快照
2. `destroy`
   - 模拟 `control-plane`
     主状态丢失
3. `restore`
   - 恢复数据库
   - 恢复环境文件
4. `restart`
   - 重新拉起
     `control-plane`
5. `resync`
   - 后台同步把
     `cloud-plane`
     当前快照重新投影回来

## 这一章的 northbound / operator 语义

做到这里以后，
operator
应该这样理解
`control-plane`
的可用性：

- 它是单活的
- 它不是自动切主的
- 但它有：
  - 标准部署布局
  - 标准备份 bundle
  - 标准恢复入口
  - 标准灾难演练入口

这比“本地进程挂了再手工想办法修”
要前进很多。

## 这一章影响的代码与资产

- [config.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane/processconfig/config.go)
- [backup-control-plane.sh](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/scripts/backup-control-plane.sh)
- [restore-control-plane.sh](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/scripts/restore-control-plane.sh)
- [control-plane-disaster-drill.sh](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/scripts/control-plane-disaster-drill.sh)
- [README.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/README.md)
- [deploy/control-plane/README.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/control-plane/README.md)

## 这一章的测试重点

这一章重点验证：

1. `control-plane`
   有正式部署布局和示例资产
2. `backup-control-plane.sh`
   可以对正式布局里的环境文件和数据库做 bundle
3. `restore-control-plane.sh`
   可以从 bundle 恢复数据库和环境文件
4. `control-plane-disaster-drill.sh`
   能验证：
   - restore 后 registration 仍在
   - 后台 sync
     会把 `cloud-plane`
     快照重新拉回来

## 这一章结束后的平台理解方式

做到这里以后，
`control-plane`
不能再被理解成：

- 只适合本地跑的实验入口

而要开始被理解成：

- 一个单活、
  正式部署、
  有恢复路径的控制面

## 检查点

- 待本章提交时回填 commit hash。
