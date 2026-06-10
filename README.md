# Mini Cloud

`mini-cloud` 是一个围绕真实项目持续演进的学习主线。

它的目标不是重造一整套云厂商，
而是先做一个：

- 多地域
- 面向容器应用托管

的最小平台控制面。

## 当前版本线

`mini-cloud`
现在已经演进到：

- `v1`
  到 `v7`

其中：

- `v6`
  已经完成生产化硬化主线
- `v7`
  是当前源码结构重构阶段

## v1 目标

`v1` 先把范围收敛到：

- 单 provider：
  - 阿里云
- 单类底层资源：
  - `ECS`
- 可以多地域
- 但暂时不做真正的多 provider 抽象

在这个范围里，
`v1` 再只做这些能力：

- 注册节点
- 给节点打标签
  - `provider`
  - `region`
  - `capacity`
- 创建项目
- 创建应用
- 提交镜像发布
- 选择地域和副本数
- 调度到节点
- 在节点上运行容器
- 查看部署状态
- 基础健康检查与自动重试
- 基础域名接入
- 基础日志、指标、用量查看
- 节点维护模式

## v1 非目标

`v1` 明确不做：

- 多 provider 后端
- 卖虚拟机
- 自己造完整编排器
- 自己造完整 IAM
- 支付、充值、发票
- 托管数据库产品
- 对象存储产品

## 文档结构

- `docs/ROADMAP.md`
  - 总路线图
- `docs/v1/`
  - `v1`
    实现主线
- `docs/v2/`
  - `v2`
    实现主线
- `docs/v3/`
  - `v3`
    实现主线
- `docs/v4/`
  - `v4`
    实现主线
- `docs/v5/`
  - `v5`
    当前最新已完成主线
- `docs/v6/`
  - `v6`
    生产化硬化主线
- `docs/v7/`
  - `v7`
    源码结构重构记录
- 每一章对应一个明确的代码检查点
- 检查点直接使用普通 `git commit`
  - 不额外引入 `tag`

## 术语与目录约定

这里有两层概念要分开看：

- 仓库层：
  - [projects/mini-cloud](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud)
    是仓库里的一个真实项目目录
  - 顶层 [agent](/home/what/myproject/swe-tools-learn-etcd/agent)
    是另一条独立学习主线
- 平台层：
  - `project`
    指平台里的租户 `project`
  - `service`
    指部署在平台里的长期服务
  - `node agent`
    指跑在节点上的执行进程

也就是说：

- 顶层 `projects/`
  只是仓库里的项目集合目录
- `mini-cloud`
  里的 `project`
  不是仓库目录，
  而是平台内部资源

## 代码结构

- `cmd/`
  - 程序入口
- `internal/`
  - 核心实现
  - [internal/controlplane](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/controlplane)
    - 全局管理面
  - [internal/cloudplane](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/cloudplane)
    - 单云执行面
  - [internal/nodeagent](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/nodeagent)
    - 节点侧执行逻辑
- `web/`
  - `React` 前端控制台
- `proto/`
  - `gRPC`
    协议与公共接口定义
- `internal/contract/`
  - 进程间契约和内部接口适配
- `deploy/`
  - 当前可维护的部署与实验环境入口
  - [deploy/compose](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/compose)
    - 本地开发和集成测试依赖
  - [deploy/terraform](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/deploy/terraform)
    - 真实云基础设施底座
  - `deploy/lab`
    - 真实云 lab 的 `labctl` 配置和说明
- [Makefile](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/Makefile)
  - 项目级检查、二进制构建和 proto 生成入口
- `scripts/`
  - 需要单独编排环境的测试脚本，包括 integration 和 smoke

## 建议入口

1. `projects/mini-cloud/README.md`
2. `projects/mini-cloud/docs/ROADMAP.md`
3. `projects/mini-cloud/docs/v5/README.md`
4. `projects/mini-cloud/docs/v5/ROADMAP.md`
5. `projects/mini-cloud/docs/v6/README.md`
6. `projects/mini-cloud/docs/v6/ROADMAP.md`
7. `projects/mini-cloud/docs/v7/README.md`
8. `projects/mini-cloud/docs/v4/ROADMAP.md`
9. `projects/mini-cloud/docs/v3/ROADMAP.md`
10. `projects/mini-cloud/docs/v2/ROADMAP.md`

## 开发入口

当前项目入口分成两类：

- `Makefile`
  负责本地工程动作
- `scripts/`
  只保留需要单独编排环境的测试脚本

历史上的通用脚本不再保留兼容 wrapper。

先查看可用目标：

```bash
make help
```

日常检查：

```bash
make check
```

`make check`
会覆盖：

- `gofmt`
- `go test ./...`
- `go vet ./...`
- `staticcheck ./...`
- `golangci-lint run ./...`
- `tests`
  子模块
  `go test ./...`
- 可选
  `terraform fmt`、
  `buf lint`
  和
  `shellcheck`
- Web
  `npm run check`
  和
  `npm run build`

如果本机还没装
`staticcheck`，
先执行：

```bash
go install honnef.co/go/tools/cmd/staticcheck@latest
```

更完整的验证入口：

- `./scripts/test-integration.sh`
  - 起本地 `Postgres`
  - 跑 store / API 集成测试
  - 自动清理测试数据库环境
- `make build-release`
  - 构建
    `control-plane`、
    `cloud-plane`
    和
    `node-agent`
    的 release 二进制
  - 默认输出到
    `dist/release/linux-amd64/`
- `make proto`
  - 通过
    `buf generate`
    生成 proto 代码
- `cd projects/mini-cloud/tests && go run . local-identity`
  - `v5/08`
    的唯一一条本地身份与授权集成场景
  - 真实 `Authelia -> auth bridge -> cloud-plane`
    认证链

## 按场景选择入口

可以先按这个表记：

| 场景 | 入口 | 成本 | 主要验证什么 |
| --- | --- | --- | --- |
| 日常开发快速回归 | `make check` | 最低 | 编译、单测、`vet`、`staticcheck`、lint、web build |
| 改数据库 / store / API | `./scripts/test-integration.sh` | 低 | 真实 `Postgres` 集成 |
| 构建长期运行二进制 | `make build-release` | 低 | `control-plane`、`cloud-plane`、`node-agent` release 输出 |
| 生成 proto 代码 | `make proto` | 低 | `buf generate` |
| 想验证 v5/08 的身份授权主链 | `cd projects/mini-cloud/tests && go run . local-identity` | 中 | 唯一身份场景；真实 `Authelia -> auth bridge -> cloud-plane`；角色绑定、项目成员、platform/project service account、本地服务创建 |
| 想拉起真实云 lab | `go run ./cmd/labctl bootstrap` + `go run ./cmd/labctl install` | 最高 | 创建云底座，通过 SSH 安装 `control-plane` 和 `cloud-plane` |

真实云环境回收现在明确是：

- `go run ./cmd/labctl destroy --config deploy/lab/lab.yaml`
  先通过云厂商 CLI 清理当前平台名下的 runtime 节点
- 再执行
  `terraform destroy`
  删除 Terraform 管理的平台机、网络、安全组、密钥和角色

也就是说，
`terraform destroy`
不再内嵌平台安装、teardown 或 runtime cleanup hook；
真正的 destroy
入口现在是：

- `go run ./cmd/labctl destroy --config deploy/lab/lab.yaml`

Terraform 只管理真实云底座。
平台进程安装、更新和销毁前 runtime 清理由
`labctl`
负责。

如果只是：

- 改了一点控制面代码
- 想先确认没编译问题

先跑：

- `make check`

如果改动已经影响到：

- store
- migration
- API 持久化

再补：

- `./scripts/test-integration.sh`

只有当改动真的碰到：

- Terraform 平台底座
- lab 安装编排
- 真实阿里云资源生命周期

才需要跑：

- `terraform -chdir=deploy/terraform/lab fmt -check`
- `go test ./cmd/labctl ./internal/lab`

要注意：

- 当前环境通常不能直接跑真实云 E2E。
- 真实 lab 的完整顺序是：
  - `cp deploy/lab/lab.yaml.example deploy/lab/lab.yaml`
  - `go run ./cmd/labctl bootstrap --config deploy/lab/lab.yaml`
  - `make build-release`
  - `go run ./cmd/labctl install --config deploy/lab/lab.yaml`
  - `go run ./cmd/labctl destroy --config deploy/lab/lab.yaml`

相关背景和章节说明再回头看：

- `docs/v3/01`
- `docs/v3/02`
- `docs/v3/03`
- `docs/v3/06`

这样理解以后，
当前入口不再是很多分散脚本，
而是：

- `Makefile`
  负责可持续维护的工程动作
- `scripts/test-integration.sh`
  负责需要本地数据库编排的集成测试
- `tests`
  子模块负责真实集成场景
- Terraform
  只负责真实云底座
- `deploy/lab`
  负责编排 lab 安装和销毁
