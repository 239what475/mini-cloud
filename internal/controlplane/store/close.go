package store

import (
	"database/sql"
	"log/slog"
)

func closeRows(rows *sql.Rows) {
	if rows == nil {
		return
	}
	if err := rows.Close(); err != nil {
		slog.Default().Warn("close database rows failed", "error", err)
	}
}
