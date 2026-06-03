# Mini Cloud v8

`v8` 的目标是把操作入口从两套 UI 收敛成一套 CLI。

这一版先不做 MCP、skill 或 agent 专用协议。CLI 必须先成为稳定、可审计、可脚本化的操作面；后续 agent 只站在 CLI 或同一层客户端之上做解释和编排。

## Todo

- [x] 盘点 Web 和 TUI 当前覆盖的操作。
- [x] 定义 `minicloud` CLI 的资源模型和命令树。
- [ ] 实现只读命令：`status`、`list`、`get`、`logs`。
- [ ] 实现写命令：`create`、`apply`、`delete`、`retry`、`rollback`、`drain`。
- [ ] CLI 覆盖现有 UI 后删除 `web/` 和 Go TUI。

## 文档

- [01-ui-inventory-and-cli-model.md](./01-ui-inventory-and-cli-model.md)
  - 现有 Web / TUI 覆盖面盘点。
  - `minicloud` CLI 的资源模型、命令树和迁移边界。

## 原则

- `cmd/control-plane`、`cmd/cloud-plane`、`cmd/node-agent` 保持 daemon 入口。
- 新增 `cmd/minicloud` 作为唯一 operator-facing CLI。
- CLI 不直接操作数据库，只走 control-plane 暴露的正式 API。
- 默认输出给人读；`-o json` 输出给脚本和后续 agent 读。
- 资源创建和变更统一使用 `apply -f <file.yaml>`，不把资源 spec 拆成大量命令行参数。
- 写操作必须有清晰的确认、幂等语义和错误返回。
