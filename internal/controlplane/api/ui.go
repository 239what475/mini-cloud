package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

func serveRootJSONOrIndex(logger *slog.Logger, uiDir string, router *gin.Engine) {
	assetsPath := filepath.Join(uiDir, "assets")
	if info, err := os.Stat(assetsPath); err == nil && info.IsDir() {
		logger.Debug("serving built web assets", "path", assetsPath)
		router.GET("/assets/*filepath", func(c *gin.Context) {
			assetPath := filepath.Clean(strings.TrimPrefix(c.Param("filepath"), "/"))
			if assetPath == "." || strings.HasPrefix(assetPath, "..") {
				c.Status(http.StatusNotFound)
				return
			}
			c.Header("Content-Disposition", "inline")
			c.File(filepath.Join(assetsPath, assetPath))
		})
	}

	router.GET("/", func(c *gin.Context) {
		indexPath := filepath.Join(uiDir, "index.html")
		if _, err := os.Stat(indexPath); err == nil {
			logger.Debug("serving built web ui", "path", indexPath)
			data, err := os.ReadFile(indexPath)
			if err != nil {
				c.String(http.StatusInternalServerError, fmt.Sprintf("read ui index: %v", err))
				return
			}
			c.Header("Content-Disposition", "inline")
			c.Data(http.StatusOK, "text/html; charset=utf-8", data)
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
