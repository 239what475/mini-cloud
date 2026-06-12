package api

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

func serveRootJSONOrIndex(logger *slog.Logger, uiDir string, router *gin.Engine) {
	assetsPath := filepath.Join(uiDir, "assets")
	if info, err := os.Stat(assetsPath); err == nil && info.IsDir() {
		logger.Debug("serving built web assets", "path", assetsPath)
		router.StaticFS("/assets", http.Dir(assetsPath))
	}

	router.GET("/", func(c *gin.Context) {
		indexPath := filepath.Join(uiDir, "index.html")
		if _, err := os.Stat(indexPath); err == nil {
			logger.Debug("serving built web ui", "path", indexPath)
			c.File(indexPath)
			return
		}

		c.JSON(http.StatusOK, map[string]any{
			"service":        "mini-cloud-control-plane",
			"ui":             "not_built",
			"healthz":        "/api/healthz",
			"controlMetrics": "/metrics/control",
			"services":       "/api/v1/services",
			"controlPlanes":  "/api/v1/control/planes",
		})
	})
}
