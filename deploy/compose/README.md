# compose

`deploy/compose` 只保留本地开发和集成测试需要的基础依赖。

当前唯一服务是：

- `postgres`
  - 本地 store / API 集成测试数据库

启动：

```bash
docker compose -f deploy/compose/docker-compose.yml up -d postgres
```

停止并清理数据：

```bash
docker compose -f deploy/compose/docker-compose.yml down -v
```

可观测性相关的本地栈暂不放在 `deploy/` 下维护，后续会按新的 demo 需求重新设计。
