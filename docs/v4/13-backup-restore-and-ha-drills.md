# v4/13 Backup Restore And HA Drills

这一章不打算把：

- backup
- restore
- standby
- HA

混成一句空话。

要先把它们拆开，
再对应到
`mini-cloud`
当前这套三层结构里。

否则很容易一上来就说：

- 做高可用

但实际上根本没说清：

- 哪一层坏了
- 哪一层的数据要保
- 恢复之后谁负责重新同步
- 哪些状态是主状态
- 哪些状态只是投影

## 这章先回答什么问题

做完 `v4/12` 以后，
我们已经有了：

- `control-plane`
  - fleet 资源模型
  - plane 注册与同步
  - project token / audit / incident / operation history
- `cloud-plane`
  - 单 plane 的 app / deployment / node / runtime worker / gateway / observability

这时候真正需要先解决的，
不是：

- 自动切主
- 多 writer
- 分布式共识

而是更基础也更务实的问题：

1. `control-plane`
   自己挂了以后，
   它的主状态能不能恢复回来。
2. `cloud-plane`
   自己挂了以后，
   它那份单 plane 主状态能不能恢复回来。
3. 恢复以后，
   `control-plane`
   能不能重新从活着的
   `cloud-plane`
   拉回最新快照，
   而不是要人工重新 register 一遍。
4. 我们说的
   `standby`
   到底是什么意思，
   它和真正自动化
   `HA`
   的边界在哪里。

所以这一章的定位是：

- 恢复手册
- 恢复脚本
- 恢复演练

不是：

- 真正的分布式高可用控制面

## 先把几个词分开

### 1. backup

`backup`
是把当前要保护的主状态打成一份可恢复的 bundle。

它关心的是：

- 需要收哪些数据
- 收完以后怎样校验完整性

它还不等于恢复。

### 2. restore

`restore`
是把 bundle 重新导回一套可运行环境。

它关心的是：

- 先校验 bundle 是不是完整的
- 再把数据库导回去
- 再把数据库外配置快照放回去

它还不等于自动切换。

### 3. standby

这一章里说的
`standby`
不是：

- 自动选主
- 双活
- 自动漂移流量

这里只把它理解成：

- 预先准备好的一套可接管落点

它在这一章里的作用先记成一句就够了：

- 为 restore 降低恢复时间

详细边界放到后面的：

- `standby 在这章里的定位`

### 4. HA

这一章标题里保留
`HA`，
但这里说的不是：

- 自动切主
- 双活控制面
- 多 writer

这一章里更准确的理解是：

- 通过 backup
- restore
- drill
- 可选 standby

把恢复时间压到一个可接受范围。

也就是说，
这里谈的是：

- 务实版可恢复性 / 可用性准备

不是：

- 正式分布式高可用控制面

### 5. drill

`drill`
就是演练。

不是看脚本文件存在就算完成，
而是要真的跑一遍：

- backup
- destroy
- restore
- verify

只有能重复演练，
恢复路径才算可信。

## 哪些状态到底归哪一层

这是这一章最重要的一步。

如果主状态归属没分清，
backup / restore
一定会写乱。

### `control-plane` 的主状态

当前
`control-plane`
数据库里保存的是全局控制面自己的主状态。

至少包括：

- fleet planes
- plane registration
- plane operation mode
- project
- project API token 元数据
- incidents
- operation events
- fleet 侧记录的 sync / health 元数据

这些东西不是
`cloud-plane`
帮它保存的，
而是
`control-plane`
自己的 authoritative state。

所以：

- `control-plane`
坏了，
要恢复的是这份数据库

而不是指望下面某个
`cloud-plane`
帮你“倒推出”这些对象。

### `cloud-plane` 的主状态

当前
`cloud-plane`
数据库里保存的是单 plane 自己的主状态。

至少包括：

- projects
- secret sets
- registry credentials
- apps
- revisions
- deployments
- nodes
- runtime workers
- 单 plane 的 operation history

这些东西是单 plane 自己的 authoritative state。

所以：

- `cloud-plane`
坏了，
要恢复的是它自己的数据库

### 哪些只是投影，不是主状态

比如：

- `control-plane`
里看到的某个 plane 的最新节点数
- `latestCapacitySnapshot`
  里的容量摘要
- 全局 inventory 里的部分聚合视图

它们虽然存在
`control-plane`
数据库里，
但本质上是：

- 从远端
  `cloud-plane`
  定期拉回来的投影

这也包括：

- `control-plane`
  里留存的 capacity snapshot 记录

它们对运维排查有价值，
但不是下层 plane 的权威来源。

这意味着：

1. 它们可以先跟着
   `control-plane`
   备份一起被恢复回来
2. 但恢复完以后，
   还应该允许后台 sync 再重新刷新

这就是这一章的关键区别：

- 有些状态要“原样恢复”
- 有些状态要“先恢复旧值，再重新同步成新值”

## 还有一类东西：数据库外配置

除了数据库，
当前还有一类状态同样必须保护：

- 进程环境文件
- platform config

例如：

- `control-plane`
  的：
  - admin token
  - database url
  - fleet sync interval
- `cloud-plane`
  的：
  - admin token
  - fleet bootstrap token
  - worker bootstrap token
  - platform config path
- `cloud-plane`
  的 `platform-config.json`

这些不在数据库里。

所以这一章的脚本都不是只收
`pg_dump`，
而是：

- 数据库 dump
- 配置快照
- manifest

打成一个 bundle。

同时，
恢复前也不再只看文件在不在，
而是会校验：

- `manifest.json`
  里记录的
  `sha256`
- `sizeBytes`
- `component`
- 恢复目标的：
  - postgres container
  - database name
  - database user

确认 bundle 没被改坏，
也没有指向错误目标，
再开始恢复。

## 主路径：先把 `control-plane` 的恢复讲清楚

这一章的主路径是：

- `control-plane`
  恢复

因为它最能体现三层关系：

- 上层坏了
- 下层的
  `cloud-plane`
  还活着
- 恢复后，
  上层应该重新把下层状态拉回来

### 新增了三条脚本

1. `scripts/backup-control-plane.sh`
   - 备份
     `mini_cloud_control_plane`
   - 可选带上
     `control-plane.env`
2. `scripts/restore-control-plane.sh`
   - 先校验 bundle
   - 再恢复
     `mini_cloud_control_plane`
   - 可选把配置快照拷回目录
3. `scripts/control-plane-disaster-drill.sh`
   - 真正跑一轮：
     - 起本地
       `cloud-plane`
     - 起本地
       `control-plane`
     - backup
     - destroy
     - restore
     - 等待 auto-resync

### 这条演练链路验证什么

`scripts/control-plane-disaster-drill.sh`
故意把流程设计成下面这样：

1. 先起一个本地
   `cloud-plane`
   并注册
   `worker-a`
2. 再起
   `control-plane`
   并把这个 plane register 进去
3. 在
   `control-plane`
   里再额外造一些真正属于它自己的状态：
   - project
   - project token
   - incident
   - fleet operation events
4. 对
   `control-plane`
   做 backup
5. 停掉
   `control-plane`
6. 在 backup 之后，
   只改下面的
   `cloud-plane`
   状态：
   - 再注册一个
     `worker-b`
7. 模拟
   `control-plane`
   数据库丢失
8. restore
   `control-plane`
9. 刚恢复起来时，
   先看到的仍然是 backup 时的旧快照
   - `nodesTotal = 1`
10. 不手工执行 register / sync
11. 只等后台
    `fleet sync loop`
    自己跑
12. 最终看到快照自动变成：
    - `nodesTotal = 2`

这里最关键的不是最后那个数字本身，
而是它表达的含义：

- `control-plane`
  自己的主状态
  通过 restore 回来了
- 来自
  `cloud-plane`
  的投影视图
  通过 auto-resync 刷新成了 backup 之后的新状态

也就是说，
恢复之后不需要重新手工注册 plane。

这里还要专门补一句：

- `worker-a`
  和
  `worker-b`
  在这条脚本里只是用来制造
  `cloud-plane snapshot`
  的可观察变化

脚本并没有单独覆盖：

- 独立 `cloud-worker` 进程自身的恢复

所以这条主路径真正验证的是：

- `control-plane restore`
- `cloud-plane 继续存活`
- `fleet auto-resync`

不是：

- 第三层 worker 自身也完成了一套独立恢复演练

## 次路径：`cloud-plane` 的冷恢复继续保留

这一章没有把原来的：

- `backup-platform.sh`
- `restore-platform.sh`
- `disaster-drill.sh`

删掉。

但现在要给它们一个更准确的定位。

### 先说一个名字上的历史包袱

`backup-platform.sh`
和
`restore-platform.sh`
这个名字来自更早期的单平面阶段。

现在放到三层模型里看，
它们真正备份和恢复的是：

- `cloud-plane`

不是整个
`mini-cloud`
全平台。

所以这一章里要这样理解它们：

- 这是
  `cloud-plane`
  的局部冷恢复脚本

### 这条路径现在负责什么

如果坏掉的是：

- 某一个
  `cloud-plane`

那么这条恢复链路负责的是：

1. 恢复它自己的数据库
2. 恢复它自己的配置快照
3. 让这个 plane 再次对外提供：
   - southbound snapshot
   - project / app / node / deployment
     这套单 plane 能力

对应脚本仍然是：

- `bash ./scripts/disaster-drill.sh`

这一轮 drill 验证的是：

- 单 plane 自己的主状态能不能回来

而不是：

- 全局 control-plane 能不能重建 fleet 视图

这里还要补一句“重新接入”到底是什么意思：

1. 如果你是原地恢复：
   - 同一份
     `apiBaseURL`
   - 同一个
     `fleet bootstrap token`
   - 同一个 plane 身份
   那么
   `control-plane`
   后台 sync 可以继续直接拉取，
   不需要重新 register。
2. 如果你是换主机重装，
   甚至连
   `apiBaseURL`
   都变了，
   那就不再是这一章当前脚本覆盖的“原地冷恢复”。
   这时应该把它视为：
   - 一个新的接入动作
   - 需要重新建立 fleet 纳管关系

也就是说，
这一章里说的
`cloud-plane`
重新接入，
必须先分清：

- 是原地恢复继续接着跑
- 还是异地 / 异机重建后重新纳管

## standby 在这章里的定位

这章可以谈
`standby`，
但只能按当前实现来谈。

### 现在能说的

如果你愿意把下面这些东西提前准备好：

- 另一台机器
- 同版本二进制
- 一份可用的环境变量模板
- 同样的
  `Postgres`
  恢复入口
- 而且恢复目标形态要和 bundle manifest 里记录的：
  - postgres container
  - database name
  - database user
  保持一致

那么这台机器就可以被你视为：

- `standby`

更准确一点说，
它更像是：

- 同形态的冷备恢复落点

出事以后，
你做的是：

1. 把 bundle 拿过去
2. restore 数据库
3. 放回配置快照
4. 启动进程

### 现在不能说的

这还不是：

- 自动 leader election
- 自动流量漂移
- active-active
- 多 writer
- 自动故障切换

所以如果这里说
`HA`，
一定要把语气收住：

- 当前做的是可恢复性
- 加上一点 standby 准备

不是正式分布式高可用控制面。

## 这章建议怎么自己跑

这两条 drill 现在都会自己生成一份临时 compose project，
并使用专门的
`Postgres`
容器名。

所以它们不会直接复用你当前已经跑着的那套本地 compose 现场。

### 练 `control-plane` 恢复

直接跑：

```bash
bash ./scripts/control-plane-disaster-drill.sh
```

你应该看到的关键现象是：

1. 恢复后，
   project token 仍然可用
2. open incident 还在
3. 刚启动时看到的是 backup 时的旧快照
4. 稍等一会儿，
   后台 sync 会把快照自动刷新成 backup 之后的最新值

### 练 `cloud-plane` 冷恢复

直接跑：

```bash
bash ./scripts/disaster-drill.sh
```

它验证的是：

- `cloud-plane`
  自己的数据库和配置快照能不能独立恢复

### 只想单独打包或恢复

也可以分别跑：

```bash
./scripts/backup-control-plane.sh --output-dir /tmp/control-plane-bundle --env-file /path/to/control-plane.env
./scripts/restore-control-plane.sh --backup-dir /tmp/control-plane-bundle --restore-config-dir /tmp/control-plane-restore
```

以及：

```bash
./scripts/backup-platform.sh --output-dir /tmp/cloud-plane-bundle --env-file /path/to/cloud-plane.env --platform-config /path/to/platform-config.json
./scripts/restore-platform.sh --backup-dir /tmp/cloud-plane-bundle --restore-config-dir /tmp/cloud-plane-restore
```

## 这章刻意不做什么

这一章刻意先不做：

- 真正多副本
  `control-plane`
- 自动主备切换
- 分布式数据库共识
- 跨 plane 自动流量接管
- 一个 plane 坏掉以后，
  workload 自动迁移到别的 plane
- 远程对象存储备份编排
- 定时备份调度器

原因很直接：

如果现在连：

- 主状态归属
- backup bundle 内容
- restore 顺序
- restore 之后谁负责重新 sync

都还没讲清，
就去谈真正
`HA`，
只会继续把系统说乱。

## 这章结束后，应该记住什么

1. `backup`
   不是
   `restore`，
   `restore`
   也不是
   `HA`。
2. `control-plane`
   和
   `cloud-plane`
   各自有自己的 authoritative state，
   不能混着备份。
3. `control-plane`
   里的一部分 fleet 视图只是投影，
   restore 后应该允许后台 sync 再刷新。
4. 当前说的
   `standby`
   只是预先准备好的恢复落点，
   不是自动切主系统。
5. 恢复路径必须靠 drill 反复验证，
   不能只靠“脚本写出来了”。

## 本章检查点

- 新增了：
  - `backup-control-plane.sh`
  - `restore-control-plane.sh`
  - `control-plane-disaster-drill.sh`
- `control-plane`
  恢复路径已经能验证：
  - 数据库主状态恢复
  - 配置快照恢复
  - restore 后自动重新 sync `cloud-plane`
- 原有
  `cloud-plane`
  冷恢复脚本已明确收敛成局部恢复路径
- 恢复脚本在导入前会校验：
  - `component`
  - `sha256`
  - `sizeBytes`
