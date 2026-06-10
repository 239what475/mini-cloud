#!/usr/bin/env bash

# 这个脚本负责跑 mini-cloud 的真实 Postgres 集成测试。
#
# 它和 Makefile 的边界：
# - Makefile 负责格式、静态检查、单元测试、构建等工程动作。
# - 这个脚本负责会启动本地依赖、改动本地 Docker compose 状态的测试。
#
# 执行过程：
# 1. 切到 projects/mini-cloud 项目根目录。
# 2. 清理旧的本地 compose 环境。
# 3. 启动 compose 里的 Postgres。
# 4. 等待 Postgres 在容器内真正可用。
# 5. 给 Go 测试注入 MINICLOUD_TEST_DATABASE_URL。
# 6. 跑依赖真实数据库的 store / API 测试包。
# 7. 脚本退出时自动 down -v，清理本地测试数据库环境。
#
# 注意：
# - 它会接管 deploy/compose/docker-compose.yml 里的本地 Postgres。
set -euo pipefail

# 项目根目录。用脚本自身位置推导，保证从任意目录执行都能回到 mini-cloud 根目录。
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# 本地依赖栈，只在这里启动 postgres，不启动整套观测组件。
COMPOSE_FILE="$ROOT_DIR/deploy/compose/docker-compose.yml"

# 无论测试成功还是失败，都尽量清理 compose 环境。
# 这里允许 cleanup 失败，因为真正的测试失败原因应该保留在前面的输出里。
cleanup() {
  docker compose -f "$COMPOSE_FILE" down -v >/dev/null 2>&1 || true
}

# 等待 Postgres 进入可连接状态。
# 使用 docker exec + pg_isready 检查容器内服务，而不是只看容器是否 started。
wait_for_postgres() {
  for _ in $(seq 1 60); do
    if docker exec mini-cloud-postgres pg_isready -h 127.0.0.1 -p 5432 -U mini_cloud -d mini_cloud >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  return 1
}

# 后面的 go test 使用相对包路径，所以先切回项目根目录。
cd "$ROOT_DIR"

echo "[integration] reset local postgres"

# 从这里开始注册退出清理。
# 如果启动 Postgres 或 Go 测试中途失败，也会自动清掉 compose volume。
trap cleanup EXIT
cleanup

echo "[integration] start postgres"
docker compose -f "$COMPOSE_FILE" up -d postgres >/dev/null

echo "[integration] wait for postgres"
if ! wait_for_postgres; then
  echo "[integration] postgres did not become ready" >&2
  docker logs mini-cloud-postgres >&2 || true
  exit 1
fi

echo "[integration] run Go integration tests"

# 测试连接 postgres 数据库。
# 具体测试包会按自己的 migration / store 逻辑创建或使用测试所需 schema。
MINICLOUD_TEST_DATABASE_URL="postgres://mini_cloud:mini_cloud@127.0.0.1:5432/postgres?sslmode=disable" \
  go test -count=1 \
    ./internal/controlplane/store \
    ./internal/cloudplane/infra/store \
    ./internal/cloudplane/api/... \
    ./internal/controlplane/api
