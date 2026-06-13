package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"mini-cloud/internal/controlplane/coordination"

	"github.com/gin-gonic/gin"
)

type Options struct {
	AdminToken        string
	SouthboundToken   string
	UIDir             string
	ServiceOperations *coordination.ServiceOperations
	PlaneSyncer       *coordination.PlaneSyncer
}

func NewMux(opts Options, logger *slog.Logger) (http.Handler, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if opts.ServiceOperations == nil {
		return nil, fmt.Errorf("service operations are required")
	}
	if opts.PlaneSyncer == nil {
		return nil, fmt.Errorf("plane syncer is required")
	}

	gin.SetMode(gin.ReleaseMode)
	gin.EnableJsonDecoderDisallowUnknownFields()
	router := gin.New()
	router.Use(ginRecoverPanics(logger), ginRequestLogger(logger))

	adminAuth := newBearerAuth(opts.AdminToken)

	serveRootJSONOrIndex(logger, opts.UIDir, router)

	router.GET("/api/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, map[string]any{
			"service": "mini-cloud-control-plane",
			"status":  "ok",
			"time":    time.Now().UTC().Format(time.RFC3339),
		})
	})

	admin := router.Group("/")
	admin.Use(adminAuth.requireToken())
	admin.GET("/metrics/control", metricsHandler(opts.PlaneSyncer))

	api := admin.Group("/api/v1")

	serviceHandler := newServiceHandler(logger, opts.ServiceOperations)

	services := api.Group("/services")
	services.GET("", serviceHandler.listServices)
	services.POST("", serviceHandler.createService)
	services.GET("/:serviceID", serviceHandler.getService)
	services.PUT("/:serviceID", serviceHandler.updateService)
	services.DELETE("/:serviceID", serviceHandler.deleteService)

	planeHandler := newPlaneHandler(logger, opts.PlaneSyncer)

	control := api.Group("/control")
	control.GET("/inventory", planeHandler.inventory)
	control.GET("/planes", planeHandler.listPlanes)
	control.GET("/planes/:planeID", planeHandler.getPlane)

	return router, nil
}
