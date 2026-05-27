package httpx

import (
	"errors"
	"strings"
)

func ParseBearerSecret(value string) (string, error) {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) != 2 {
		return "", errors.New("invalid authorization header")
	}
	if !strings.EqualFold(fields[0], "Bearer") {
		return "", errors.New("invalid authorization header")
	}
	if strings.TrimSpace(fields[1]) == "" {
		return "", errors.New("invalid authorization header")
	}
	return fields[1], nil
}
