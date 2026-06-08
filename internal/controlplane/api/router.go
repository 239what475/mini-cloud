package api

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"mini-cloud/internal/common/logquery"
	"mini-cloud/internal/common/util"
	domain "mini-cloud/internal/controlplane/domain"
	"mini-cloud/internal/controlplane/planesync"
	"mini-cloud/internal/controlplane/serviceops"
	"mini-cloud/internal/controlplane/store"

	"github.com/gin-gonic/gin"
)

type Options struct {
	AdminToken        string
	UIDir             string
	LogQueryService   logquery.Backend
	PlaneSyncer       *planesync.Syncer
	Dispatcher        *serviceops.Dispatcher
	ServiceController *serviceops.Controller
}

func NewMux(opts Options, logger *slog.Logger, stores *store.Store) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(ginRecoverPanics(logger), ginRequestLogger(logger))

	authz := newAuthController(opts.AdminToken, logger, stores)

	if opts.Dispatcher == nil {
		opts.Dispatcher = serviceops.NewDispatcher(logger, stores)
	}
	if opts.ServiceController == nil {
		opts.ServiceController = serviceops.New(logger, stores, opts.Dispatcher)
	}

	serveRootJSONOrIndex(logger, opts.UIDir, router)

	router.GET("/api/healthz", func(c *gin.Context) {
		writeJSON(c, http.StatusOK, map[string]any{
			"service": "mini-cloud-control-plane",
			"status":  "ok",
			"time":    time.Now().UTC().Format(time.RFC3339),
		})
	})

	admin := router.Group("/")
	admin.Use(authz.adminOnly())
	admin.GET("/metrics/control", func(c *gin.Context) {
		items, err := stores.ListPlanes(c.Request.Context())
		if err != nil {
			writeJSON(c, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
		c.Header("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		util.Fprint(c.Writer, renderControlMetrics(items, time.Now().UTC()))
	})

	resourceHandler := newResourceHandler(logger, stores)
	serviceHandler := newServiceHandler(logger, stores, opts.ServiceController)
	operationHistoryHandler := newOperationHistoryHandler(logger, stores)
	api := admin.Group("/api/v1")
	api.GET("/auth/whoami", authz.whoAmI)

	resources := api.Group("/registry-credentials")
	resources.GET("", resourceHandler.listRegistryCredentials)
	resources.POST("", resourceHandler.createRegistryCredential)

	services := api.Group("/services")
	services.GET("", serviceHandler.listServices)
	services.POST("", serviceHandler.createService)
	services.GET("/:serviceID", serviceHandler.getService)
	services.PUT("/:serviceID", serviceHandler.updateService)
	services.DELETE("/:serviceID", serviceHandler.deleteService)

	logQueryHandler := newLogQueryHandler(logger, opts.LogQueryService)

	controlHandler := newControlHandler(logger, stores, opts.PlaneSyncer)
	control := api.Group("/control")
	control.GET("/logs", logQueryHandler.queryControlLogs)
	control.GET("/inventory", controlHandler.inventory)
	control.GET("/planes", controlHandler.listPlanes)
	control.POST("/planes", controlHandler.createPlane)
	control.GET("/planes/:planeID", controlHandler.getPlane)
	control.DELETE("/planes/:planeID", controlHandler.deletePlane)
	control.POST("/planes/:planeID/actions/sync", controlHandler.syncPlane)
	control.PUT("/planes/:planeID/operation", controlHandler.updatePlaneOperation)
	control.GET("/operations", operationHistoryHandler.listControlOperations)

	return router
}

func renderControlMetrics(controlPlanes []domain.Detail, now time.Time) string {
	var out strings.Builder
	out.WriteString("# HELP minicloud_plane_count Current number of planes registered in the control-plane store.\n")
	out.WriteString("# TYPE minicloud_plane_count gauge\n")
	util.Fprintf(&out, "minicloud_plane_count %d\n", len(controlPlanes))
	out.WriteString("# HELP minicloud_plane_status Current plane status, emitted as one-hot samples per plane and status.\n")
	out.WriteString("# TYPE minicloud_plane_status gauge\n")
	out.WriteString("# HELP minicloud_plane_operation_state Current plane operation mode, emitted as one-hot samples per plane and state.\n")
	out.WriteString("# TYPE minicloud_plane_operation_state gauge\n")
	out.WriteString("# HELP minicloud_plane_accepting_new_runs Whether the plane is currently accepting new runs.\n")
	out.WriteString("# TYPE minicloud_plane_accepting_new_runs gauge\n")
	for _, item := range controlPlanes {
		for _, status := range []string{"registering", "ready", "degraded", "offline"} {
			value := 0
			if string(item.Status.Status) == status {
				value = 1
			}
			util.Fprintf(&out, "minicloud_plane_status{name=%q,plane_id=%q,status=%q} %d\n", item.Name, item.ID, status, value)
		}
		operationState := string(item.Operation.ResolvedState())
		for _, state := range []string{"active", "maintenance", "draining"} {
			value := 0
			if operationState == state {
				value = 1
			}
			util.Fprintf(&out, "minicloud_plane_operation_state{name=%q,plane_id=%q,state=%q} %d\n", item.Name, item.ID, state, value)
		}
		acceptingValue := 0
		if item.Operation.AcceptingNewRuns() {
			acceptingValue = 1
		}
		util.Fprintf(&out, "minicloud_plane_accepting_new_runs{plane_id=%q} %d\n", item.ID, acceptingValue)
		if item.Status.LastSyncAt != nil {
			util.Fprintf(&out, "minicloud_plane_last_sync_age_seconds{plane_id=%q} %.0f\n", item.ID, now.Sub(item.Status.LastSyncAt.UTC()).Seconds())
		}
	}
	return out.String()
}
