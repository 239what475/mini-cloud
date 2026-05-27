package store

import (
	"database/sql"
	"log/slog"
)

// closeRows 关闭数据库 rows，并忽略关闭时的二次错误。
// 参数说明：rows 是需要关闭的数据库结果集。
func closeRows(rows *sql.Rows) {
	// nil rows 没有可关闭资源。
	if rows == nil {
		return
	}
	// rows.Close 失败只记录 warning；主查询错误由调用方在 rows.Err 中处理。
	if err := rows.Close(); err != nil {
		slog.Default().Warn("close database rows failed", "error", err)
	}
}
