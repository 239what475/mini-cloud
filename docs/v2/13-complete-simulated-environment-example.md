# 13 Complete Simulated Environment Example

到了这一章，
`mini-cloud v2`
终于把前面分散的能力收成了一套真正可重复的完整例子。

这一章不是再补一个单点功能，
而是把这些已经做过的东西真正串起来：

- 本地 `Postgres`
- control-plane
- 内置 gateway
- 两个 managed worker
- demo project / app / release
- 发布、回滚、节点离线告警
- 备份、恢复、灾难演练

也就是说，
从这一章开始，
`v2`
已经有了一套：

- 成本低
- 可重复
- 适合教学
- 也适合回归验证

的完整模拟环境。

## 为什么这一章很重要

前面的每一章都只证明了：

- 某个能力已经存在

但真正做项目时，
你更需要一套东西来回答：

- 如果我要从零把整个平台起起来，
  它能不能自己跑成一条完整主链
- 如果我要给别人演示，
  有没有一条不用上真实阿里云也能稳定复现的路径
- 如果后面继续改代码，
  有没有一套“功能不是孤零零的”综合例子可以反复回归

这一章做的，
就是这套综合例子。

## 这一章新增了什么

当前新增了一个统一脚本：

| 文件 | 作用 |
| --- | --- |
| `./scripts/simulated-env.sh` | 提供 `start / status / scenario / reset` 四个子命令 |

它不是把前面脚本替掉，
而是把前面这些能力组织成一个更完整的入口：

- `./scripts/backup-platform.sh`
- `./scripts/restore-platform.sh`
- `./scripts/disaster-drill.sh`

其中：

- `backup/restore/disaster-drill`
  - 更偏单个运维主题
- `simulated-env.sh`
  - 更偏完整环境 example

## 这套模拟环境长什么样

当前完整拓扑可以先这样理解：

```text
curl / 浏览器
    |
    | Host: echo.sim-demo.apps.sim.example.test
    v
control-plane + gateway
127.0.0.1:18080
    |
    +--> Postgres
    |
    +--> sim-worker-a (managed agent)
    |       |
    |       +--> demo app container
    |
    +--> sim-worker-b (managed agent)
            |
            +--> 作为 standby worker，专门用来演示 node offline 告警
```

这里有两个很关键的设计点。

### 1. gateway 直接复用 control-plane 进程

当前项目里，
gateway 不是单独一个进程。

它就是 control-plane HTTP 进程里的反向代理能力。

所以你访问这套模拟环境时，
入口统一都是：

- `http://127.0.0.1:18080`

### 2. 两个 worker 都在本机，但仍然按“两个节点”来模拟

当前两个 managed worker 都运行在同一台本机上，
并且都把节点私网地址注册成：

- `127.0.0.1`

这样做不是偷懒，
而是为了让本地 gateway 真能反代到 Docker 暴露出来的宿主机端口。

也就是说：

- 平台看到的是两个独立 worker
- 但底层网络仍然保持本地可跑

这让教学环境既简单，
又不至于把“多 worker 拓扑”完全抹掉。

## 四个子命令分别做什么

### `start`

```bash
./scripts/simulated-env.sh start
```

它会做一套全新的基线环境：

1. 清理旧的模拟环境状态
2. 起本地 compose `Postgres`
3. 启动 control-plane
4. 启动两个 managed worker
5. 等两个 worker 都进入 `ready`
6. 创建 demo project
7. 创建 demo app
8. 提交 `v1` release
9. 等 gateway 真正返回：
   - `hello-v1`

也就是说，
`start`
不是只起“空平台”，
而是直接把：

- 一套可用平台
- 一条已经跑起来的 demo app

一起准备好。

### `status`

```bash
./scripts/simulated-env.sh status
```

它会输出：

- control-plane / agent 进程状态
- `healthz`
- 当前 node 列表
- demo app 当前 release / deployment / execution 状态
- 可直接复制的 `curl` 示例

所以它的目标不是做漂亮 UI，
而是做一个足够直接的：

- 教学状态页
- 本地排障入口

### `scenario`

```bash
./scripts/simulated-env.sh scenario
```

这是这一章最重要的命令。

它会从 fresh `start` 开始，
完整跑一轮真正的综合场景：

1. 先起一套全新 baseline
2. 提交 `v2` release
3. 等 gateway 返回：
   - `hello-v2`
4. 触发 rollback 到 `v1`
5. 等 gateway 回到：
   - `hello-v1`
6. 停掉 standby worker
7. 触发 stale heartbeat reconcile
8. 验证：
   - `worker-nodes-offline`
   - 进入 `firing`
9. 执行平台备份
10. 模拟 control-plane / worker / Postgres 丢失
11. 执行恢复
12. 重启 control-plane 和两个 worker
13. 验证：
    - 两个 worker 都回到 `ready`
    - gateway 仍然返回 `hello-v1`
    - `worker-nodes-offline`
      已经清除

这一条链路很重要，
因为它把前面章节里这些看起来分散的主题，
第一次真正放到同一个环境里一起验证了：

- 安全发布
- 回滚
- worker 离线处理
- 告警
- 备份
- 恢复

### `reset`

```bash
./scripts/simulated-env.sh reset
```

它会做“一键清理”：

- 停 control-plane
- 停两个 agent
- 删 demo runtime container
- `docker compose down -v`
- 删除这套模拟环境的状态目录

所以如果你想重新从干净环境开始，
只需要：

```bash
./scripts/simulated-env.sh reset
./scripts/simulated-env.sh start
```

## 为什么访问 demo app 要带 `Host`

当前 demo app 的受管域名是：

- `echo.sim-demo.apps.sim.example.test`

但这不是一条真实 DNS 记录。

这套模拟环境里，
我们故意不去要求你真的配本地域名解析，
而是直接通过 `Host` 头让 gateway 走到正确路由。

所以最常用的访问方式是：

```bash
curl -H "Host: echo.sim-demo.apps.sim.example.test" \
  http://127.0.0.1:18080/
```

这里真正发生的事情是：

1. 请求先到 control-plane 的 HTTP 入口
2. gateway 读取：
   - `Host`
3. 用这个 host 去查 domain binding
4. 再反代到当前 promoted execution 对应的 node + hostPort

所以这里要记住一句话：

- `127.0.0.1:18080`
  - 是入口地址
- `Host`
  - 才决定你访问的是哪一个 app

## 这套 example 为什么适合作为后续回归入口

这套脚本现在已经覆盖了下面几类问题：

| 类别 | 通过什么动作体现 |
| --- | --- |
| 平台启动 | `start` |
| worker 注册与心跳 | `start` / `status` |
| demo app 发布 | `start` / `scenario` |
| 安全切流与回滚 | `scenario` |
| 节点离线告警 | `scenario` |
| 备份恢复 | `scenario` |
| 清理重来 | `reset` |

也就是说，
以后如果 `v2`
继续增加能力，
最值得优先保住的本地综合回归路径，
就是这一套。

因为它已经不是单个 API 是否还在，
而是：

- 一条完整平台主链是不是还通

## 这一章怎么跑

最常用的顺序就是：

```bash
./scripts/simulated-env.sh start
./scripts/simulated-env.sh status
./scripts/simulated-env.sh scenario
./scripts/simulated-env.sh reset
```

如果你只是想先把环境起起来再手工看，
通常只需要前两条。

如果你想做一次完整回归，
直接跑：

```bash
./scripts/simulated-env.sh scenario
```

就够了。

## 这一章跑了哪些检查

这一章完成后，
至少应该跑下面这些检查：

```bash
go test ./...
./scripts/check.sh
./scripts/test-integration.sh
./scripts/simulated-env.sh start
./scripts/simulated-env.sh status
./scripts/simulated-env.sh scenario
./scripts/simulated-env.sh reset
```

这里要注意：

- `check`
  - 负责轻量静态与单元检查
- `test-integration`
  - 负责真实 `Postgres` 集成测试
- `simulated-env.sh`
  - 负责 `v2` 这一套完整综合例子

三者不是互相替代，
而是三层不同深度的验证。

## 这一章的结论

从这一章开始，
`mini-cloud v2`
不再只是“有很多单独功能”。

它已经有了一套真正适合教学和回归的完整模拟环境：

- 起得来
- 看得清
- 能发版
- 能回滚
- 能制造故障
- 能恢复
- 也能一键清理

这意味着后面去做：

- 真实阿里云端到端实验
- `bootstrap` 重装、升级、销毁

时，
本地已经有了一套足够稳定的完整参照物。

## 本章检查点

- 提交：
  - `38308f729339f39a47ea09b45510a442c2121b6c`
- 状态：
  - `simulated-env.sh` 已提供完整模拟环境的统一入口，支持 `start / status / scenario / reset`
