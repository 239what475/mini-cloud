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
		opts.PlaneSelector = planeselector.NewService(logger, stores, opts.DeployService)
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

	projectHandler := newProjectHandler(logger, stores)
	projectResourceHandler := newProjectResourceHandler(logger, stores)
	serviceHandler := newServiceHandler(logger, stores, opts.ServiceController)
	projectTokenHandler := newProjectTokenHandler(logger, stores)
	platformServiceAccountHandler := newPlatformServiceAccountHandler(logger, stores)
	operationHistoryHandler := newOperationHistoryHandler(logger, stores)
	mux.HandleFunc("GET /api/v1/projects", authz.platformAccessFunc(authPermissionProjectRead, projectHandler.listProjects))
	mux.HandleFunc("POST /api/v1/projects", authz.platformAccessFunc(authPermissionProjectWrite, projectHandler.createProject))
	mux.HandleFunc("GET /api/v1/projects/{projectID}", authz.projectAccessByPathFunc(authPermissionProjectRead, "projectID", projectHandler.getProject))
	mux.HandleFunc("PUT /api/v1/projects/{projectID}", authz.platformAccessFunc(authPermissionProjectWrite, projectHandler.updateProject))
	mux.HandleFunc("POST /api/v1/projects/{projectID}/actions/transfer-ownership", authz.platformAccessFunc(authPermissionProjectOwnership, projectHandler.transferProjectOwnership))
	mux.HandleFunc("DELETE /api/v1/projects/{projectID}", authz.platformAccessFunc(authPermissionProjectWrite, projectHandler.deleteProject))
	mux.HandleFunc("GET /api/v1/projects/{projectID}/config-sets", authz.projectAccessByPathFunc(authPermissionProjectRead, "projectID", projectResourceHandler.listConfigSets))
	mux.HandleFunc("POST /api/v1/projects/{projectID}/config-sets", authz.projectAccessByPathFunc(authPermissionProjectDeploy, "projectID", projectResourceHandler.createConfigSet))
	mux.HandleFunc("GET /api/v1/projects/{projectID}/secret-sets", authz.projectAccessByPathFunc(authPermissionProjectRead, "projectID", projectResourceHandler.listSecretSets))
	mux.HandleFunc("POST /api/v1/projects/{projectID}/secret-sets", authz.projectAccessByPathFunc(authPermissionProjectDeploy, "projectID", projectResourceHandler.createSecretSet))
	mux.HandleFunc("GET /api/v1/projects/{projectID}/registry-credentials", authz.projectAccessByPathFunc(authPermissionProjectRead, "projectID", projectResourceHandler.listRegistryCredentials))
	mux.HandleFunc("POST /api/v1/projects/{projectID}/registry-credentials", authz.projectAccessByPathFunc(authPermissionProjectDeploy, "projectID", projectResourceHandler.createRegistryCredential))
	mux.HandleFunc("GET /api/v1/projects/{projectID}/services", authz.projectAccessByPathFunc(authPermissionProjectRead, "projectID", serviceHandler.listProjectServices))
	mux.HandleFunc("POST /api/v1/projects/{projectID}/services", authz.projectAccessByPathFunc(authPermissionProjectDeploy, "projectID", serviceHandler.createProjectService))
	mux.HandleFunc("GET /api/v1/projects/{projectID}/services/{serviceID}", authz.projectAccessByPathFunc(authPermissionProjectRead, "projectID", serviceHandler.getProjectService))
	mux.HandleFunc("PUT /api/v1/projects/{projectID}/services/{serviceID}", authz.projectAccessByPathFunc(authPermissionProjectDeploy, "projectID", serviceHandler.updateProjectService))
	mux.HandleFunc("DELETE /api/v1/projects/{projectID}/services/{serviceID}", authz.projectAccessByPathFunc(authPermissionProjectDeploy, "projectID", serviceHandler.deleteProjectService))
	mux.HandleFunc("GET /api/v1/projects/{projectID}/api-tokens", authz.platformAccessFunc(authPermissionProjectAPIToken, projectTokenHandler.listProjectAPITokens))
	mux.HandleFunc("POST /api/v1/projects/{projectID}/api-tokens", authz.platformAccessFunc(authPermissionProjectAPIToken, projectTokenHandler.issueProjectAPIToken))
	mux.HandleFunc("DELETE /api/v1/projects/{projectID}/api-tokens/{tokenID}", authz.platformAccessFunc(authPermissionProjectAPIToken, projectTokenHandler.deleteProjectAPIToken))
	mux.HandleFunc("GET /api/v1/projects/{projectID}/operations", authz.projectAccessByPathFunc(authPermissionOperationsRead, "projectID", operationHistoryHandler.listProjectOperations))
	mux.HandleFunc("GET /api/v1/control/service-accounts", authz.breakGlassOnlyFunc(authPermissionControlRead, platformServiceAccountHandler.listPlatformServiceAccounts))
	mux.HandleFunc("POST /api/v1/control/service-accounts", authz.breakGlassOnlyFunc(authPermissionControlWrite, platformServiceAccountHandler.issuePlatformServiceAccount))
	mux.HandleFunc("DELETE /api/v1/control/service-accounts/{accountID}", authz.breakGlassOnlyFunc(authPermissionControlWrite, platformServiceAccountHandler.deletePlatformServiceAccount))

	logQueryHandler := newLogQueryHandler(logger, opts.LogQueryService)
	mux.HandleFunc("GET /api/v1/control/logs", authz.platformAccessFunc(authPermissionControlRead, logQueryHandler.queryControlLogs))

	controlHandler := newControlHandler(logger, stores, opts.PlaneSyncService)
	controlRuntimePoolHandler := newControlRuntimePoolHandler(logger, stores)
	controlDeployHandler := newControlDeployHandler(logger, stores, opts.DeployService)
	controlPlaneSelectionHandler := newControlPlaneSelectionHandler(logger, stores, opts.PlaneSelector)
	mux.HandleFunc("GET /api/v1/control/inventory", authz.platformAccessFunc(authPermissionControlRead, controlHandler.inventory))
	mux.HandleFunc("GET /api/v1/control/runtime-node-pools", authz.platformAccessFunc(authPermissionControlRead, controlRuntimePoolHandler.listRuntimeNodePools))
	mux.HandleFunc("GET /api/v1/control/planes", authz.platformAccessFunc(authPermissionControlRead, controlHandler.listPlanes))
	mux.HandleFunc("POST /api/v1/control/planes", authz.platformAccessFunc(authPermissionControlWrite, controlHandler.createPlane))
	mux.HandleFunc("GET /api/v1/control/planes/{planeID}", authz.platformAccessFunc(authPermissionControlRead, controlHandler.getPlane))
	mux.HandleFunc("DELETE /api/v1/control/planes/{planeID}", authz.platformAccessFunc(authPermissionControlWrite, controlHandler.deletePlane))
	mux.HandleFunc("GET /api/v1/control/planes/{planeID}/runtime-node-pool", authz.platformAccessFunc(authPermissionControlRead, controlRuntimePoolHandler.getRuntimeNodePool))
	mux.HandleFunc("PUT /api/v1/control/planes/{planeID}/runtime-node-pool", authz.platformAccessFunc(authPermissionControlWrite, controlRuntimePoolHandler.upsertRuntimeNodePool))
	mux.HandleFunc("DELETE /api/v1/control/planes/{planeID}/runtime-node-pool", authz.platformAccessFunc(authPermissionControlWrite, controlRuntimePoolHandler.deleteRuntimeNodePool))
	mux.HandleFunc("POST /api/v1/control/planes/{planeID}/actions/register", authz.breakGlassOnlyFunc(authPermissionControlWrite, controlHandler.registerPlane))
	mux.HandleFunc("POST /api/v1/control/planes/{planeID}/actions/sync", authz.platformAccessFunc(authPermissionControlWrite, controlHandler.syncPlane))
	mux.HandleFunc("PUT /api/v1/control/planes/{planeID}/operation", authz.platformAccessFunc(authPermissionControlWrite, controlHandler.updatePlaneOperation))
	mux.HandleFunc("GET /api/v1/control/planes/{planeID}/capacity-snapshots", authz.platformAccessFunc(authPermissionControlRead, controlHandler.listPlaneCapacitySnapshots))
	mux.HandleFunc("POST /api/v1/control/planes/{planeID}/capacity-snapshots", authz.platformAccessFunc(authPermissionControlWrite, controlHandler.recordPlaneCapacitySnapshot))
	mux.HandleFunc("POST /api/v1/control/planes/{planeID}/projects/{projectID}/actions/apply-service", authz.platformAccessFunc(authPermissionProjectDeploy, controlDeployHandler.applyService))
	mux.HandleFunc("POST /api/v1/control/projects/{projectID}/plane-selection/preview-service", authz.platformAccessFunc(authPermissionControlRead, controlPlaneSelectionHandler.previewProjectSelection))
	mux.HandleFunc("POST /api/v1/control/projects/{projectID}/actions/apply-service", authz.projectAccessByPathFunc(authPermissionProjectDeploy, "projectID", controlPlaneSelectionHandler.applyService))
	mux.HandleFunc("GET /api/v1/control/incidents", authz.platformAccessFunc(authPermissionControlRead, controlHandler.listIncidents))
	mux.HandleFunc("POST /api/v1/control/incidents", authz.platformAccessFunc(authPermissionControlWrite, controlHandler.createIncident))
	mux.HandleFunc("PUT /api/v1/control/incidents/{incidentID}", authz.platformAccessFunc(authPermissionControlWrite, controlHandler.updateIncident))
	mux.HandleFunc("POST /api/v1/control/incidents/{incidentID}/resolve", authz.platformAccessFunc(authPermissionControlWrite, controlHandler.resolveIncident))
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
	out.WriteString("# HELP minicloud_plane_accepting_new_deployments Whether the plane is currently accepting new deployments.\n")
	out.WriteString("# TYPE minicloud_plane_accepting_new_deployments gauge\n")
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
		if item.Operation.AcceptingNewDeployments() {
			acceptingValue = 1
		}
		util.Fprintf(&out, "minicloud_plane_accepting_new_deployments{plane_id=%q} %d\n", item.ID, acceptingValue)
		if item.Status.LastSyncAt != nil {
			util.Fprintf(&out, "minicloud_plane_last_sync_age_seconds{plane_id=%q} %.0f\n", item.ID, now.Sub(item.Status.LastSyncAt.UTC()).Seconds())
		}
	}
	return out.String()
}
