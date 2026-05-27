package httpx

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func ParseBoundedPositiveIntQuery(r *http.Request, key string, fallback int, max int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	if max > 0 && value > max {
		return 0, fmt.Errorf("%s must not exceed %d", key, max)
	}
	return value, nil
}
