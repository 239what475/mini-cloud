package store

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"time"
)

func nullableTime(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return value.UTC()
}

func marshalJSON(value any, fallback any) ([]byte, error) {
	if value == nil {
		return json.Marshal(fallback)
	}
	return json.Marshal(value)
}

func unmarshalJSON[T any](raw []byte, target *T, fallback T) error {
	if len(raw) == 0 {
		*target = fallback
		return nil
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return err
	}
	return nil
}

func closeRows(rows *sql.Rows) {
	if rows == nil {
		return
	}
	if err := rows.Close(); err != nil {
		slog.Default().Warn("close database rows failed", "error", err)
	}
}
