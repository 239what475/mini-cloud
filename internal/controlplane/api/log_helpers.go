package api

import (
	"log/slog"
	"net/http"

	"mini-cloud/internal/common/httpx"
	"mini-cloud/internal/common/logctx"
)

func withRequestLogFields(r *http.Request, fields logctx.Fields) *http.Request {
	return httpx.WithRequestLogFields(r, fields)
}

func requestScopedLogger(r *http.Request, logger *slog.Logger) *slog.Logger {
	return httpx.RequestScopedLogger(r, logger)
}
