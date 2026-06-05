package store

import (
	"encoding/json"
	"fmt"
)

// marshalJSON 把结构化字段编码为 JSON；nil 输入使用 fallback。
func marshalJSON(value any, fallback any) ([]byte, error) {
	if value == nil {
		value = fallback
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// unmarshalJSON 把 JSON 字段解码到 target；空字段使用 fallback。
func unmarshalJSON[T any](raw []byte, target *T, fallback T) error {
	if target == nil {
		return fmt.Errorf("target must not be nil")
	}
	if len(raw) == 0 {
		*target = fallback
		return nil
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode json: %w", err)
	}
	return nil
}
