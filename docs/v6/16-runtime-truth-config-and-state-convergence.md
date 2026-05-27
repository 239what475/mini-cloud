# 16 运行时真相、配置边界与状态收敛

回到本版路线图：

- [ROADMAP.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/docs/v6/ROADMAP.md)

## 这一章要解决什么

到
`v6/15`
之后，
平台已经有了真实观测栈，
但还有一个更基础的问题没有收干净：

- 文件到底是 bootstrap
  材料
- 还是运行时真相
- 还是组件之间的配置分发边界

如果这三件事混在一起，
后面再接：

- 域名 /
  TLS /
  CDN
- 真实应用验收
- 长期运行排障

就会越来越乱。

所以这一章先做一次明确 break change：

- `platform-config.json`
  只保留 bootstrap /
  静态平台配置
- 运行时敏感配置和出口配置
  明确转到进程环境
- northbound
  不再回显原始 bootstrap
  文件，
  而是回显“当前进程已加载的运行时摘要”

## 这一章的设计结论

### 1. `platform-config.json` 只负责静态平台身份

这一章之后，
`platform-config.json`
只继续承载：

- `platform`
- `provider`
- `cloudPlane.internalBaseURL`
- `network`
- `artifacts`
- `runtimeNodeDefaults`
- `providerRuntimeSpec`

它不再承载：

- `nodeAgentBootstrapToken`
- `workloadLogPushUrl`
- `lokiQueryUrl`
- `lokiTenantId`
- `workloadOtlpEndpoint`
- `grafanaBaseUrl`

这些值即使还残留在旧文件里，
当前实现也不再把它们当有效输入。

### 2. 运行时敏感配置全部走进程环境

`cloud-plane`
运行时真正会生效的这组配置，
现在统一从 env
读取：

- `MINICLOUD_NODE_AGENT_BOOTSTRAP_TOKEN`
- `MINICLOUD_WORKLOAD_LOG_PUSH_URL`
- `MINICLOUD_LOKI_URL`
- `MINICLOUD_LOKI_TENANT_ID`
- `MINICLOUD_WORKLOAD_OTLP_ENDPOINT`
- `MINICLOUD_GRAFANA_BASE_URL`

这组值的角色非常明确：

- 它们是运行态输入
- 不是平台 bootstrap
  身份的一部分
- 也不应该被固定在仓库模板 json
  里

### 3. 本机固定 agent 不再读取 `platform-config.json`

以前最大的混乱点之一是：

- `start-agent.sh`
  会去解析
  `platform-config.json`
  里的 token
  和观测配置

这会让同一份文件同时变成：

- `cloud-plane`
  的 bootstrap
  输入
- 本机 agent
  的 secret
  输入
- 观测出口配置源

现在这条链被切断了：

- `start-agent.sh`
  只读 `agent.env`
- token
  和workload 日志 /
  trace
  出口都从 env
  取

### 4. `/api/v1/platform/config` 现在返回运行时摘要

这一章不再让 northbound
把原始 `platform-config`
直接回显出来。

现在返回的是：

- 当前进程已加载的运行时摘要
- 每一组值来自：
  - `bootstrap_file`
  - `process_env`
  - `unset`
- 一份 fingerprint

这样 operator
在排障时看到的是：

- 现在真正生效了什么
- 它来自哪里

而不是：

- 某份模板文件长什么样

### 5. `control-plane` 现在持久化 plane 的运行时配置真相

这一章继续往前收了一步：

- `cloud-plane`
  的 southbound snapshot
  现在会带上 runtime config
  摘要
- `control-plane`
  在 plane sync
  时会把它一起落库
- `plane detail`
  现在可以直接看到最近一次同步到的：
  - `observedAt`
  - `fingerprint`
  - `summary`

这里没有把摘要再拆成一堆独立列，
而是保留成一份 observed JSON
快照。

因为它的职责是：

- 排障真相
- 配置来源可见性
- 远端状态收敛

而不是新的调度主状态。

## 这次 break change 实际改了什么

这一轮已经落下的行为变化是：

1. `cloud-plane`
   启动现在要求：
   `MINICLOUD_NODE_AGENT_BOOTSTRAP_TOKEN`
2. `platform-config.json`
   不再要求
   `cloudPlane.nodeAgentBootstrapToken`
3. 手工部署路径里的
   `start-agent.sh`
   不再解析
   `platform-config.json`
4. 手工部署示例里的观测配置
   从 json
   挪到 env
5. `/api/v1/platform/config`
   不再暴露原始平台配置，
   而是暴露运行时摘要
6. `plane snapshot`
   现在会附带 runtime config
   摘要
7. `control-plane`
   现在会持久化每个 plane
   最近一次同步到的 runtime config
   真相

## 这一章还没有做什么

这一章做完之后，
仍然没有继续做：

- 进程热更新 /
  reload
  语义
- 配置变更后的后台 reconcile

也就是说，
这一章已经解决的是：

- authority
  边界
- 输入边界
- 排障可见性
- 远端 plane
  的 runtime config
  observed state 收敛

但还没有继续做“配置漂移后的主动治理”。

## 这一轮完成后的判断标准

如果这一章第一轮做对了，
现在应该能比较明确地回答：

1. 哪些值是 bootstrap
   文件负责的
2. 哪些值是运行时 env
   负责的
3. 固定 agent
   还会不会偷偷读
   `platform-config.json`
4. `/api/v1/platform/config`
   返回的是原始模板，
   还是当前真实已加载配置
5. `control-plane`
   能不能看到远端 plane
   最近一次真实生效的 runtime config

## 检查点

- 本章代码已经把：
  - node bootstrap token
  - 平台观测出口
  从
  `platform-config.json`
  移到了进程环境
- 本章代码已经把：
  `/api/v1/platform/config`
  改成运行时摘要视图
- 本章代码已经把：
  runtime config
  摘要接入了 plane southbound snapshot
- 本章代码已经把：
  `control-plane`
  的 plane sync /
  store /
  detail
  接上 runtime config
  observed state
