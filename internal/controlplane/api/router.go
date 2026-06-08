package api

import (
	"log/slog"
	"net/http"
	"time"

	"mini-cloud/internal/common/logquery"
	"mini-cloud/internal/controlplane/serviceops"
	"mini-cloud/internal/controlplane/store"

	"github.com/gin-gonic/gin"
)

type Options struct {
	AdminToken        string
	UIDir             string
	LogQueryService   logquery.Backend
	ServiceController *serviceops.Controller
}

func NewMux(opts Options, logger *slog.Logger, stores *store.Store) http.Handler {
	router := gin.New()
	router.Use(ginRecoverPanics(logger), ginRequestLogger(logger))

	auth := newAdminAuth(opts.AdminToken)

	serveRootJSONOrIndex(logger, opts.UIDir, router)

	router.GET("/api/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, map[string]any{
			"service": "mini-cloud-control-plane",
			"status":  "ok",
			"time":    time.Now().UTC().Format(time.RFC3339),
		})
	})

	admin := router.Group("/")
	admin.Use(auth.requireAdmin())
	admin.GET("/metrics/control", metricsHandler(stores))

	api := admin.Group("/api/v1")

	registryHandler := newRegistryHandler(logger, stores)
	resources := api.Group("/registry-credentials")
	resources.GET("", registryHandler.listRegistryCredentials)
	resources.POST("", registryHandler.createRegistryCredential)

	serviceHandler := newServiceHandler(logger, stores, opts.ServiceController)

	services := api.Group("/services")
	services.GET("", serviceHandler.listServices)
	services.POST("", serviceHandler.createService)
	services.GET("/:serviceID", serviceHandler.getService)
	services.PUT("/:serviceID", serviceHandler.updateService)
	services.DELETE("/:serviceID", serviceHandler.deleteService)

	logQueryHandler := newLogQueryHandler(logger, opts.LogQueryService)
	planeHandler := newPlaneHandler(logger, stores)
	eventHandler := newEventHandler(logger, stores)

	control := api.Group("/control")
	control.GET("/logs", logQueryHandler.queryControlLogs)
	control.GET("/inventory", planeHandler.inventory)
	control.GET("/planes", planeHandler.listPlanes)
	control.POST("/planes", planeHandler.createPlane)
	control.GET("/planes/:planeID", planeHandler.getPlane)
	control.DELETE("/planes/:planeID", planeHandler.deletePlane)
	control.GET("/events", eventHandler.listControlEvents)

	return router
}
