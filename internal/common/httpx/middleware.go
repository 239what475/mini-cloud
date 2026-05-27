package httpx

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"mini-cloud/internal/common/logctx"
)

func RequestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := logctx.EnsureRequestID(r.Header.Get(logctx.HeaderRequestID))
		ctx := logctx.WithFields(r.Context(), logctx.Fields{RequestID: requestID})
		r = r.WithContext(ctx)
		w.Header().Set(logctx.HeaderRequestID, requestID)

		start := time.Now()
		capture := &StatusCapturingResponseWriter{ResponseWriter: w}
		next.ServeHTTP(capture, r)
		logctx.Logger(ctx, logger).Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"host", r.Host,
			"remote_addr", r.RemoteAddr,
			"status_code", capture.StatusCode(),
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

func RecoverPanics(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logctx.Logger(r.Context(), logger).Error("panic while handling request",
					"panic", recovered,
					"stack", string(debug.Stack()),
					"method", r.Method,
					"path", r.URL.Path,
				)
				WriteJSON(w, http.StatusInternalServerError, map[string]any{
					"error": "internal server error",
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}
