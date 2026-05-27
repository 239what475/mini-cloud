# Mini Cloud v7

`v7`
是源码结构重构阶段。

这一版不先写完整
`ROADMAP.md`，
因为重构会按实际源码边界一步一步推进。

这里先只记录：

- 阶段定位
- 重构原则
- 每一步完成后的检查点

## 定位

`v7`
只做源码结构和工程边界重构。

这一版不做：

- 新用户侧产品功能
- 新 provider
- 新 workload 类型
- 为旧 API 保留兼容层
- 新旧两套实现并存

这一版要解决的是：

- 源码目录是否和当前架构一致
- `control-plane`
  / `cloud-plane`
  / `node agent`
  的职责是否清楚
- `proto`
  / `contract`
  / `client`
  的边界是否干净
- provider、测试和部署资产是否还有历史演进残留

## 重构原则

- 每一步只重构一个清晰边界
- 每一步都要能编译或有明确验证方式
- 不保留为了兼容旧设计而存在的过渡层
- 如果发现旧命名已经不符合当前架构，直接改成当前语义
- 不为了移动文件而移动文件
- 每一步完成后记录检查点和提交

## 当前关注点

后续每一步重构都围绕这些方向展开：

- `internal/`
  下的包边界
- `cmd/`
  里的进程入口
- `proto/`
  和生成代码
- `internal/contract/`
  与跨进程接口
- provider 抽象和阿里云 / 腾讯云实现
- `tests/`
  的本地集成、真实单云、真实双云测试组织
- 历史命名和死代码清理

## 重构日志

后续每完成一步，
在这里追加：

```text
N. <重构主题>
   - 范围：
   - 验证：
   - commit:
```

1. 工程入口与关闭错误处理重构
   - 范围：新增项目级 `Makefile` 作为工程动作入口，保留 `check`、`test`、`vet`、`staticcheck`、`lint`、`tests`、`build`、`build-release`、`proto` 和 `clean` 等目标。
   - 范围：明确 `Makefile` 和 `scripts/` 的边界：`Makefile` 只放格式检查、静态检查、测试、构建和生成代码；会启动 `Postgres`、服务进程或 runtime 容器的流程放回 `scripts/`。
   - 范围：删除历史上分散且不再维护的 `scripts/` 脚本，只保留 `scripts/test-integration.sh` 和 `scripts/smoke.sh` 两个环境编排测试入口；`make integration` 和 `make smoke` 不再保留兼容 target。
   - 范围：简化 `make check`，直接对目录执行 `gofmt -l`，用 `find ./deploy ./scripts -type f -name '*.sh' -exec bash -n {} +` 检查 shell 语法，并移除手写 import boundary 检查和自定义 Go cache。
   - 范围：修复 `scripts/test-integration.sh` 的集成测试包路径，不再引用已经不存在的 `internal/cloudplane/api/worker`。
   - 范围：按现有 `slog` / `logctx` 方向清理非生成代码里的 `Close` 错误吞掉问题：生产代码记录 `Warn` 或返回 `errors.Join`，测试代码用 `t.Logf` 记录，store 查询统一通过 `closeRows` 记录 `rows.Close` 失败。
   - 范围：为当前保留的 `Makefile`、`scripts/test-integration.sh` 和 `scripts/smoke.sh` 补充面向初学者的说明注释。
   - 验证：`make -n help`、`make -n check`、`bash -n scripts/test-integration.sh`、`bash -n scripts/smoke.sh`、项目根 Go module 的 `go test ./...`、`tests` 子 module 的 `go test ./...`、`git diff --check`。
   - 备注：`scripts/` 不是恢复旧脚本集合，只保留需要单独编排环境的测试脚本；旧的兼容 wrapper 不再保留。
   - 备注：`internal/gen/proto/**` 是生成代码，本次不手工修改其中的 `Close` 处理。
   - commit: `cloud/mini-cloud: refactor engineering entrypoints`

2. Node Agent 命名与配置入口重构
   - 范围：将用户侧入口从 `agent` 改为 `node-agent`，包括 `cmd/node-agent`、构建产物、release artifact、cloud-plane 静态 artifact 路由、systemd unit 和本地/真实云测试构建入口；旧 `cmd/agent`、旧 `agent` 二进制和旧 artifact 名不保留兼容。
   - 范围：将内部实现包从 `internal/agent` 改为 `internal/nodeagent`，协议层只保留 `nodeagentapi` gRPC 语义，不再保留 `/api/node-agent/v1` HTTP 路径。
   - 范围：将常驻进程入口收敛为 `node-agent --config <node-agent.yaml>`，节点身份、bootstrap token、容量、轮询间隔、状态文件和工作负载日志 / 遥测配置都进入 YAML；删除 `run`、`register`、`heartbeat`、`work` 子命令，不保留兼容入口。
   - 范围：`scripts/smoke.sh` 不再手动调用单步调试命令，而是启动真实 `node-agent` daemon，让它自行完成注册、心跳、work 轮询和 runtime 容器启动。
   - 范围：部署示例从 `agent.env` 改为 `node-agent.yaml`，`start-agent.sh` 只作为薄启动脚本；Terraform cloud-init 和 provider runtime node bootstrap 都改为写入 `node-agent.yaml` 后启动 `node-agent --config`。
   - TODO：清理历史 proto RPC 命名，给复用 `Empty`、`Project`、`ServiceEnvelope` 等消息的 RPC 补齐专用 Request / Response，再移除 `buf.yaml` 里的 `RPC_REQUEST_RESPONSE_UNIQUE`、`RPC_REQUEST_STANDARD_NAME` 和 `RPC_RESPONSE_STANDARD_NAME` 例外。
   - 验证：`go test ./...`、`tests` 子 module 的 `go test ./...`、`bash -n scripts/smoke.sh`、`bash -n scripts/test-integration.sh`、`bash -n deploy/cloud-plane/start-agent.sh.example`、`make -n build`。
   - commit: 待提交后回填

3. Node Agent 包边界重构
   - 范围：将 `cmd/node-agent` 收缩为真正的进程入口，只负责初始化 logger、调用 `internal/nodeagent.RunCLI` 和处理退出码。
   - 范围：把配置加载、容量探测、state 文件读写、HTTP readiness 等待、工作负载 OTEL env 注入、单个 work 执行和 daemon 生命周期编排迁入 `internal/nodeagent/{config,capacity,state,workloadreadiness,workloadtelemetry,work,daemon}`。
   - 范围：保留 `internal/nodeagent/client`、`runtime` 和 `workloadlogs` 作为底层适配包，由 `daemon` / `work` 组合使用，避免子包反向依赖 `cmd` 或根入口包。
   - 验证：`go test ./cmd/node-agent ./internal/nodeagent/...`。
   - commit: 待提交后回填

4. Node Agent 第二阶段内部清理
   - 范围：继续拆分 `work.ExecuteNext`，将 poll、run、readiness、superseded cleanup、running / failed report 拆到 `Executor` 的小函数，并把 client、readiness waiter、工作负载日志采集都改为窄接口注入。
   - 范围：补齐 work 核心分支测试，覆盖无 work、runtime start 失败、readiness 成功、readiness 失败、superseded stop 失败和 report 失败。
   - 范围：为 `daemon` 引入 `Runner`，`Run` 只负责组装真实控制面客户端、运行时、工作负载日志管理器和状态存储；`Runner` 直接展开持有这些组件，不再使用依赖聚合结构，测试可以直接注入 fake 组件。
   - 范围：将 OS signal context 移到 `cmd/node-agent`，`internal/nodeagent.RunCLI` 改为接收外部 `context.Context`；同步清理 `managed` / `worker` 历史命名。
   - 范围：Docker detect host port 失败后的 stop 清理改用短 timeout context；工作负载 OTEL env 排序改用标准库 `sort.Strings`。
   - 范围：修复真实云 cloud-init 等待 runtime node ready 时仍读取旧 `workerNodesReady` 字段的问题；修复 `scripts/smoke.sh` 平台配置仍写旧 `workerDefaults` 字段的问题。
   - 验证：`go test ./cmd/node-agent ./internal/nodeagent/...`。
   - commit: 待提交后回填

5. Node Runtime Provider 抽象重构
   - 范围：为 `internal/nodeagent/runtime` 增加轻量 runtime provider factory，`daemon` 不再直接调用 Docker 构造函数，而是按 `runtime.type` 选择运行时；当前只实现 `docker`。
   - 范围：将 node-agent 配置增加 `runtime.type: docker`，并把 work 配置中的 `dockerTimeout` 改为更通用的 `runtimeTimeout`。
   - 范围：将工作负载日志采集从 Docker client 中解耦，`workloadlogs.Manager` 改为依赖 runtime 暴露的 `LogFollower`，为后续 containerd 日志流实现保留边界。
   - 范围：同步更新 smoke、本地集成 runner、Terraform cloud-init、provider bootstrap 脚本和部署示例里的 node-agent YAML。
   - 验证：`go test ./cmd/node-agent ./internal/nodeagent/...`。
   - commit: 待提交后回填

6. Node Agent 容量模型重构
   - 范围：参考 Kubernetes Node Allocatable 思路，将 node-agent YAML 中旧的 `cpuMilliFree` / `memoryMiFree` / `cpuMilliReserve` / `memoryMiReserve` 删除，不保留兼容。
   - 范围：容量配置拆成 `total`、`systemReserved`、`agentReserved`、`evictionReserved`，删除旧顶层 total/free/reserve 字段和显式 allocatable 配置入口。
   - 范围：`total` 表示 node-agent 纳入 mini-cloud 资源账本的总资源池，不强制等同于宿主机物理总容量；字段为 0 时从本机探测。
   - 范围：节点注册上报资源账本总量 `total`；心跳上报静态可调度预算 `cpuMilliAllocatable` / `memoryMiAllocatable`。
   - 范围：可调度容量只能由 `allocatable = total - systemReserved - agentReserved - evictionReserved` 计算，不再允许显式 allocatable 覆盖。
   - 范围：node-agent gRPC / contract 字段同步改名，不再用 `cpuMilliFree` / `memoryMiFree` 承载 allocatable 语义。
   - 范围：cloud-plane 节点存储拆分 `total`、`allocatable`、`allocated`；心跳只刷新 allocatable，不再用 `total - allocatable` 反推 allocated。
   - 范围：`systemReserved` 用于 OS、ssh、journald、用户进程等非 mini-cloud / 非 node-agent 负载；`agentReserved` 用于 node-agent、Docker/containerd、日志采集等运行时管理开销；`evictionReserved` 当前只用于内存压力保护预算。
   - 范围：代码注释必须明确：这里计算的是 mini-cloud 允许调度的静态 allocatable 预算，不是宿主机实时 free，也不包含实时 CPU idle / MemAvailable 观测。
   - 验证：`go test ./internal/nodeagent/...`、`go test ./...`。
   - commit: 待提交后回填

### cloud-plane 内部入口

- cloud-plane 现在只暴露内部 gRPC 入口，由 `cloud-plane --config <path>` 指定的 YAML 文件中的 `server.listenGRPCAddr` 配置监听地址。
- 只注册 `ControlPlaneSnapshotService、ControlPlaneProjectService、ControlPlaneWorkloadService` 和 `NodeAgentService`：`control-plane -> cloud-plane` 使用 `ControlPlaneSnapshotService、ControlPlaneProjectService、ControlPlaneWorkloadService`，`node-agent -> cloud-plane` 使用 `NodeAgentService`。
- 已移除 cloud-plane 自身的 northbound HTTP API、grpc-gateway、静态 UI、`/api/healthz`、`/metrics` 和 `/internal/artifacts/node-agent-linux-amd64`。
- node-agent 二进制分发必须使用 `nodeAgent.artifact.binaryUrl` 指向外部 artifact 地址，不再由 cloud-plane 进程托管。
- cloud-plane 不再读取 `MINICLOUD_*` 环境变量；命令行只允许用 `--config` 指定配置文件路径，不承载业务配置项；`cloud-plane.env` 和 `platform-config.json` 双入口已删除。
- cloud-plane 数据库迁移已压缩为 `00001_init_schema.sql` 单一开发期 baseline。v7 之前的本地 cloud-plane 数据库不再支持原地升级，继续使用前必须重建数据库或重置 lab。

### cloud-plane 入口和旧 gateway/domain 模型清理

- cloud-plane 不再保留 gateway request event、gateway counter、gateway SLO/alert/runbook 以及对应表结构。
- cloud-plane 不再负责 northbound HTTP gateway 或旧 domain binding API，删除 `service_domains`、domain binding RPC、`PlaneSnapshot.ingress` 和相关 control-plane/operator/TUI 展示入口。
- public service 的真实 ingress 数据面改为外置 Caddy，由 cloud-plane 后台 reconciler 生成 Caddyfile 并执行 reload；cloud-plane 进程本身不内嵌 Caddy，也不承载业务 HTTP 流量。
- runtime node egress 数据面改为外置 Tinyproxy，runtime node 默认无公网 IP，bootstrap、Docker daemon 和 workload HTTP(S) 出站统一走 platform host egress proxy。
- control-plane 注册 plane 时使用 `grpcEndpoint`，不再使用 `apiBaseURL`；值是 gRPC target，例如 `127.0.0.1:18081` 或 `grpcs://plane.example.com:443`，HTTP URL 不再作为兼容输入。
- cloud-plane 进程配置只保留运行时真正使用和下发给 node-agent 的字段；配置结构拆为 `plane.identity`、`controlPlane.auth`、`nodeAgent`、`infrastructure`、`runtimeProvisioning` 和 `observability`，删除旧 `platform` / `provider` / `cloudPlane` / `artifacts` / `providerRuntimeSpec` 聚合模型。

7. Cloud Plane 本地 Reconciler 重构
   - 范围：将 cloud-plane 从请求同步执行模型改为 plane-local accepted desired state + 后台 reconciler 模型。
   - 范围：`ApplyService` 只接受并持久化 service desired state，不再携带 control-plane 计算的 runtime node 放置计划；runtime node 调度由 cloud-plane 本地 reconciler 完成。
   - 范围：新增 `service_desired` 作为权威 desired state，`services` 只保留 identity、observed summary 和最新 accepted cache。
   - 范围：引入 cloud-plane reconciler manager，后台推进 service desired、placement、runtime node、deployment status、rollout 和 node health。
   - 范围：清理 SQL raw string 中历史 `// provider` 残留，避免 PostgreSQL 运行时语法错误。
   - 设计文档：`07-cloud-plane-local-reconciler-refactor.md`。
   - 验证：`GOCACHE=/tmp/go-cache go test ./internal/cloudplane/...`、`GOCACHE=/tmp/go-cache go vet ./...`。
   - 备注：`GOCACHE=/tmp/go-cache go test ./...` 在当前沙箱因 `httptest` 监听本地端口被拒绝失败，失败包与本次 cloud-plane 改动无关。
   - commit: 待提交后回填

8. 外置 Ingress/Egress 数据面重构
   - 范围：恢复 public service 的入口数据面，但不恢复 embedded Caddy；Caddy 作为 platform host 外置进程，cloud-plane 只负责生成 Caddyfile 和 reload。
   - 范围：新增 runtimeProvisioning.imagePull / runtimeProvisioning.egress.proxy 通用配置，删除 providerSpec 中的 `dockerRegistryMirror` 和 runtime node 公网字段。
   - 范围：runtime node 默认无公网 IP；Aliyun 不申请公网出带宽，Tencent 固定 `PublicIpAssigned=false`。
   - 范围：node-agent 通过 `runtime.hostPortRange` 显式分配 workload hostPort，Terraform lab 使用同一端口范围配置安全组。
   - 范围：Terraform lab 拆分 platform/runtime security group，runtime node 使用独立 runtime security group，不再复用 platform security group。
   - 范围：Terraform lab 安装 Caddy 和 Tinyproxy，并把 Caddy/Tinyproxy 端口纳入安全组。
   - 设计文档：`08-external-dataplane-ingress-egress-refactor.md`。
   - commit: 待提交后回填

9. Runtime Node 自动缩容重构
   - 范围：为 runtime node 生命周期补齐 `draining`、`deleting`、`deleted` 状态，cloud-plane 后台 reconciler 自动回收没有 active execution 的 runtime node。
   - 范围：`RuntimeDriver` 增加 `Delete`，阿里云 ECS 和腾讯云 CVM driver 直接按云实例 ID 删除 runtime node；provider not found 按幂等成功处理。
   - 范围：runtime node 允许缩到 0 台，不引入 autoscaling/scaleIn 开关、最小保留节点数、冷却时间、空闲时间或每轮删除上限。
   - 范围：删除前先把 node 标记为 draining 并关闭 schedulable，node-agent 领取 work 时再次检查 node 仍为 ready 且可调度。
   - 设计文档：`09-runtime-node-scale-in-refactor.md`。
   - commit: 待提交后回填

10. Cloud Plane Store 边界重构
   - 范围：将 `infra/store` 从 repository + usecase + reconciler 混合体收敛为 repository 和必要事务 primitive。
   - 范围：control-plane owner 映射、token 签发、desired accept、node-agent work claim/report、node health reconcile 和 runtime node scale-in 策略迁出 store。
   - 范围：删除旧 usecase 型 store API，不保留兼容 wrapper。
   - 设计文档：`10-cloud-plane-store-boundary-refactor.md`。
   - 验证：`go test ./internal/cloudplane/...`、`go vet ./internal/cloudplane/...`、`staticcheck ./internal/cloudplane/...`。
   - commit: 待提交后回填
