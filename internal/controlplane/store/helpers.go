package store

import (
	"encoding/json"
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
