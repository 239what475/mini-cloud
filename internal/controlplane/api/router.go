package api

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"mini-cloud/internal/common/logquery"
	"mini-cloud/internal/common/util"
	"mini-cloud/internal/controlplane/deploy"
	plane "mini-cloud/internal/controlplane/plane"
	"mini-cloud/internal/controlplane/planeselector"
	"mini-cloud/internal/controlplane/planesync"
	servicecontroller "mini-cloud/internal/controlplane/servicecontroller"
	"mini-cloud/internal/controlplane/store"
)

type Options struct {
	AdminToken        string
	UIDir             string
	LogQueryService   logquery.Backend
	PlaneSyncService  *planesync.Service
	DeployService     *deploy.Service
	PlaneSelector     *planeselector.Service
	ServiceController *servicecontroller.Controller
}

func NewMux(opts Options, logger *slog.Logger, stores *store.Store) http.Handler {
	mux := http.NewServeMux()
	authz := newAuthController(opts.AdminToken, logger, stores)

	if opts.DeployService == nil {
		opts.DeployService = deploy.NewService(logger, stores)
	}
	if opts.PlaneSelector == nil {
		opts.PlaneSelector = planeselector.NewService(logger, stores)
	}
	if opts.ServiceController == nil {
		opts.ServiceController = servicecontroller.New(logger, stores, opts.PlaneSelector, opts.DeployService)
	}

	serveRootJSONOrIndex(logger, opts.UIDir, mux)

	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"service": "mini-cloud-control-plane",
			"status":  "ok",
			"time":    time.Now().UTC().Format(time.RFC3339),
		})
	})
	mux.HandleFunc("GET /api/v1/auth/whoami", authz.whoAmI)
	mux.HandleFunc("GET /metrics/control", authz.platformAccessFunc(authPermissionControlRead, func(w http.ResponseWriter, r *http.Request) {
		items, err := stores.ListPlanes(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		util.Fprint(w, renderControlMetrics(items, time.Now().UTC()))
	}))

	resourceHandler := newResourceHandler(logger, stores)
	serviceHandler := newServiceHandler(logger, stores, opts.ServiceController)
	operationHistoryHandler := newOperationHistoryHandler(logger, stores)
	mux.HandleFunc("GET /api/v1/config-sets", authz.platformAccessFunc(authPermissionResourceRead, resourceHandler.listConfigSets))
	mux.HandleFunc("POST /api/v1/config-sets", authz.platformAccessFunc(authPermissionResourceWrite, resourceHandler.createConfigSet))
	mux.HandleFunc("GET /api/v1/secret-sets", authz.platformAccessFunc(authPermissionResourceRead, resourceHandler.listSecretSets))
	mux.HandleFunc("POST /api/v1/secret-sets", authz.platformAccessFunc(authPermissionResourceWrite, resourceHandler.createSecretSet))
	mux.HandleFunc("GET /api/v1/registry-credentials", authz.platformAccessFunc(authPermissionResourceRead, resourceHandler.listRegistryCredentials))
	mux.HandleFunc("POST /api/v1/registry-credentials", authz.platformAccessFunc(authPermissionResourceWrite, resourceHandler.createRegistryCredential))
	mux.HandleFunc("GET /api/v1/services", authz.platformAccessFunc(authPermissionResourceRead, serviceHandler.listServices))
	mux.HandleFunc("POST /api/v1/services", authz.platformAccessFunc(authPermissionServiceDeploy, serviceHandler.createService))
	mux.HandleFunc("GET /api/v1/services/{serviceID}", authz.platformAccessFunc(authPermissionResourceRead, serviceHandler.getService))
	mux.HandleFunc("PUT /api/v1/services/{serviceID}", authz.platformAccessFunc(authPermissionServiceDeploy, serviceHandler.updateService))
	mux.HandleFunc("DELETE /api/v1/services/{serviceID}", authz.platformAccessFunc(authPermissionServiceDeploy, serviceHandler.deleteService))

	logQueryHandler := newLogQueryHandler(logger, opts.LogQueryService)
	mux.HandleFunc("GET /api/v1/control/logs", authz.platformAccessFunc(authPermissionControlRead, logQueryHandler.queryControlLogs))

	controlHandler := newControlHandler(logger, stores, opts.PlaneSyncService)
	controlDeployHandler := newControlDeployHandler(logger, stores, opts.DeployService)
	controlPlaneSelectionHandler := newControlPlaneSelectionHandler(logger, stores, opts.PlaneSelector)
	mux.HandleFunc("GET /api/v1/control/inventory", authz.platformAccessFunc(authPermissionControlRead, controlHandler.inventory))
	mux.HandleFunc("GET /api/v1/control/planes", authz.platformAccessFunc(authPermissionControlRead, controlHandler.listPlanes))
	mux.HandleFunc("POST /api/v1/control/planes", authz.platformAccessFunc(authPermissionControlWrite, controlHandler.createPlane))
	mux.HandleFunc("GET /api/v1/control/planes/{planeID}", authz.platformAccessFunc(authPermissionControlRead, controlHandler.getPlane))
	mux.HandleFunc("DELETE /api/v1/control/planes/{planeID}", authz.platformAccessFunc(authPermissionControlWrite, controlHandler.deletePlane))
	mux.HandleFunc("POST /api/v1/control/planes/{planeID}/actions/register", authz.platformAccessFunc(authPermissionControlWrite, controlHandler.registerPlane))
	mux.HandleFunc("POST /api/v1/control/planes/{planeID}/actions/sync", authz.platformAccessFunc(authPermissionControlWrite, controlHandler.syncPlane))
	mux.HandleFunc("PUT /api/v1/control/planes/{planeID}/operation", authz.platformAccessFunc(authPermissionControlWrite, controlHandler.updatePlaneOperation))
	mux.HandleFunc("POST /api/v1/control/planes/{planeID}/actions/apply-service", authz.platformAccessFunc(authPermissionServiceDeploy, controlDeployHandler.applyService))
	mux.HandleFunc("POST /api/v1/control/plane-selection/preview-service", authz.platformAccessFunc(authPermissionControlRead, controlPlaneSelectionHandler.previewSelection))
	mux.HandleFunc("GET /api/v1/control/operations", authz.platformAccessFunc(authPermissionOperationsRead, operationHistoryHandler.listControlOperations))

	return requestLogger(logger, recoverPanics(logger, authz.wrap(mux)))
}

func renderControlMetrics(controlPlanes []plane.Detail, now time.Time) string {
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
