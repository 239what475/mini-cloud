package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"mini-cloud/internal/common/operationhistory"
	"mini-cloud/internal/controlplane/store"
)

func recordOperationEvent(logger *slog.Logger, stores *store.Store, r *http.Request, input operationhistory.CreateInput) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Second)
	defer cancel()

	actorKind, actorID, actorLabel, actorProjectID := operationActorFromRequest(r)
	input.Details = mergeAuthorizationDetails(input.Details, r)
	input.ActorKind = actorKind
	input.ActorID = actorID
	input.ActorLabel = actorLabel
	input.ActorProjectID = actorProjectID
	input.RequestMethod = r.Method
	input.RequestPath = r.URL.Path
	if input.Result == "" {
		input.Result = operationhistory.ResultSucceeded
	}

	if _, err := stores.CreateOperationEvent(ctx, input); err != nil {
		logger.Error("record operation event failed", "error", err, "action", input.Action, "target_type", input.TargetType, "target_id", input.TargetID)
	}
}

func operationActorFromRequest(r *http.Request) (kind string, id string, label string, projectID string) {
	state := authStateFromRequest(r)
	principal := state.Principal

	switch principal.Kind {
	case authPrincipalKindBreakGlass:
		return "break_glass", "break-glass-admin", "break-glass-admin", ""
	case authPrincipalKindServiceAccount:
		if principal.ServiceAccount != nil {
			return "service_account", principal.ServiceAccount.ID, principal.ServiceAccount.Name, ""
		}
		return "service_account", "", "service-account", ""
	case authPrincipalKindProjectToken:
		if principal.ProjectToken != nil {
			return "project_token", principal.ProjectToken.ID, principal.ProjectToken.Name, principal.ProjectToken.ProjectID
		}
		return "project_token", "", "project-token", ""
	default:
		return "anonymous", "", "anonymous", ""
	}
}

func mergeAuthorizationDetails(details map[string]any, r *http.Request) map[string]any {
	decision, ok := authzDecisionFromRequest(r)
	if !ok {
		return details
	}

	out := cloneMap(details)
	out["authorization"] = map[string]any{
		"requiredPermission": decision.RequiredPermission,
		"scopeType":          decision.ScopeType,
		"scopeID":            decision.ScopeID,
		"decision":           decision.Decision,
		"reason":             decision.Reason,
		"matchedRoles":       decision.MatchedRoles,
		"httpStatus":         decision.HTTPStatus,
	}
	return out
}

func cloneMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
