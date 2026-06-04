package api

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"mini-cloud/internal/common/httpx"
	planeclient "mini-cloud/internal/controlplane/planeclient"
)

func writeJSON(w http.ResponseWriter, statusCode int, value any) {
	httpx.WriteJSON(w, statusCode, value)
}

func writePlaneAPIError(w http.ResponseWriter, err error) bool {
	var rpcErr *planeclient.RPCError
	if !errors.As(err, &rpcErr) {
		return false
	}

	payload := map[string]any{
		"error": rpcErr.Message,
		"code":  rpcErr.Code,
	}
	if len(rpcErr.RejectReasons) > 0 {
		payload["rejectReasons"] = rpcErr.RejectReasons
	}
	writeJSON(w, http.StatusBadGateway, payload)
	return true
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func directoryExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return httpx.RequestLogger(logger, next)
}

func recoverPanics(logger *slog.Logger, next http.Handler) http.Handler {
	return httpx.RecoverPanics(logger, next)
}

func serveRootJSONOrIndex(logger *slog.Logger, uiDir string, mux *http.ServeMux) {
	if assetsPath := filepath.Join(uiDir, "assets"); directoryExists(assetsPath) {
		logger.Debug("serving built web assets", "path", assetsPath)
		mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir(assetsPath))))
	}

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}

		indexPath := filepath.Join(uiDir, "index.html")
		if _, err := os.Stat(indexPath); err == nil {
			logger.Debug("serving built web ui", "path", indexPath)
			http.ServeFile(w, r, indexPath)
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"service":        "mini-cloud-control-plane",
			"ui":             "not_built",
			"healthz":        "/api/healthz",
			"controlMetrics": "/metrics/control",
			"services":       "/api/v1/services",
			"configSets":     "/api/v1/config-sets",
			"controlPlanes":  "/api/v1/control/planes",
		})
	})
}
