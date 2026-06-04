# Mini Cloud v8

`v8` 的目标是保留 Web，移除不再维护的 Go TUI，并为后续 CLI 预留一致的资源模型。

这一版先不做 MCP、skill 或 agent 专用协议。CLI 必须先成为稳定、可审计、可脚本化的操作面；后续 agent 只站在 CLI 或同一层客户端之上做解释和编排。

同时，v8 要收敛 control-plane / cloud-plane 的职责边界：保留多个 cloud-plane，但避免 control-plane 和 cloud-plane 各自维护一套 service lifecycle。

## Todo

- [x] 盘点 Web 当前覆盖的操作。
- [x] 定义 `minicloud` CLI 的资源模型和命令树。
- [x] 删除 Go TUI。
- [x] 记录 control-plane / cloud-plane 精简设计。
- [ ] 实现只读命令：`status`、`list`、`get`、`logs`。
- [ ] 实现写命令：`create`、`apply`、`delete`、`retry`、`drain`。

## 文档

- [01-ui-inventory-and-cli-model.md](./01-ui-inventory-and-cli-model.md)
  - 现有 Web 覆盖面盘点。
  - `minicloud` CLI 的资源模型、命令树和迁移边界。
- [02-control-plane-cloud-plane-simplification.md](./02-control-plane-cloud-plane-simplification.md)
  - 多 cloud-plane 保留前提下的职责边界精简。
  - 废弃双 service 状态机和平台级 rollback 的迁移计划。

## 原则

- `cmd/control-plane`、`cmd/cloud-plane`、`cmd/node-agent` 保持 daemon 入口。
- Web 继续作为 operator-facing UI。
- 后续可新增 `cmd/minicloud` 作为脚本化 operator-facing CLI。
- CLI 不直接操作数据库，只走 control-plane 暴露的正式 API。
- 默认输出给人读；`-o json` 输出给脚本和后续 agent 读。
- 资源创建和变更统一使用 `apply -f <file.yaml>`，不把资源 spec 拆成大量命令行参数。
- 写操作必须有清晰的确认、幂等语义和错误返回。
- 平台级 rollback 暂不作为核心能力；需要恢复旧版本时，通过重新 apply 旧 spec 表达。
- control-plane / cloud-plane 精简按完整业务链路纵切推进，每次修改必须包含状态写入、plane 交互、执行结果回流和可验证读模型。
