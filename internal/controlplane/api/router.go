package api

import (
	"log/slog"
	"net/http"
	"time"

	"mini-cloud/internal/controlplane/coordination"
	"mini-cloud/internal/controlplane/logquery"
	"mini-cloud/internal/controlplane/store"

	"github.com/gin-gonic/gin"
)

type Options struct {
	AdminToken        string
	SouthboundToken   string
	UIDir             string
	LogQueryService   *logquery.Service
	ServiceOperations *coordination.ServiceOperations
	PlaneSyncer       *coordination.PlaneSyncer
}

func NewMux(opts Options, logger *slog.Logger, stores *store.Store) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	gin.SetMode(gin.ReleaseMode)
	gin.EnableJsonDecoderDisallowUnknownFields()
	router := gin.New()
	router.Use(ginRecoverPanics(logger), ginRequestLogger(logger))

	adminAuth := newBearerAuth(opts.AdminToken)
	southboundAuth := newBearerAuth(opts.SouthboundToken)

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
	admin.GET("/metrics/control", metricsHandler(stores))

	api := admin.Group("/api/v1")

	serviceHandler := newServiceHandler(logger, stores, opts.ServiceOperations)

	services := api.Group("/services")
	services.GET("", serviceHandler.listServices)
	services.POST("", serviceHandler.createService)
	services.GET("/:serviceID", serviceHandler.getService)
	services.PUT("/:serviceID", serviceHandler.updateService)
	services.DELETE("/:serviceID", serviceHandler.deleteService)

	logQueryHandler := newLogQueryHandler(logger, opts.LogQueryService)
	planeHandler := newPlaneHandler(logger, stores, opts.PlaneSyncer)
	eventHandler := newEventHandler(logger, stores)

	control := api.Group("/control")
	control.GET("/logs", logQueryHandler.queryControlLogs)
	control.GET("/inventory", planeHandler.inventory)
	control.GET("/planes", planeHandler.listPlanes)
	control.GET("/planes/:planeID", planeHandler.getPlane)
	control.DELETE("/planes/:planeID", planeHandler.deletePlane)
	control.GET("/events", eventHandler.listControlEvents)

	internal := router.Group("/api/v1/internal")
	internal.Use(southboundAuth.requireToken())
	internal.POST("/planes/register", planeHandler.registerPlane)

	return router
}
