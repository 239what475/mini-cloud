package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"mini-cloud/internal/common/httpx"
	"mini-cloud/internal/common/operationhistory"
	"mini-cloud/internal/controlplane/serviceaccount"
	"mini-cloud/internal/controlplane/store"
)

const (
	authPrincipalKindAnonymous      = "anonymous"
	authPrincipalKindBreakGlass     = "break_glass"
	authPrincipalKindServiceAccount = "service_account"
)

const (
	authPermissionControlRead       = "control.read"
	authPermissionControlWrite      = "control.write"
	authPermissionResourceRead      = "resource.read"
	authPermissionResourceWrite     = "resource.write"
	authPermissionServiceDeploy     = "service.deploy.write"
	authPermissionSelfRead          = "auth.self.read"
	authPermissionOperationsRead    = "operations.read"
	authScopeTypeControl            = "control"
	authDecisionAllowed             = "allowed"
	authDecisionDenied              = "denied"
	authDecisionFailed              = "failed"
	authMatchedRoleBreakGlassAdmin  = "break_glass_admin"
	authMatchedRolePlatformAdmin    = "platform_admin"
	authMatchedRolePlatformOwner    = "platform_owner"
	authMatchedRolePlatformOperator = "platform_operator"
	authMatchedRolePlatformAuditor  = "platform_auditor"
	authMatchedRoleAuthDisabled     = "auth_disabled"
)

type authContextKey struct{}
type authzContextKey struct{}

type authState struct {
	Enabled   bool
	Principal authPrincipal
	Failure   *authFailure
}

type authFailure struct {
	StatusCode int
	Message    string
}

type authPrincipal struct {
	Kind           string
	ServiceAccount *serviceaccount.Account
}

type authzDecision struct {
	RequiredPermission string
	ScopeType          string
	ScopeID            string
	Decision           string
	Reason             string
	MatchedRoles       []string
	HTTPStatus         int
}

type authController struct {
	adminToken string
	logger     *slog.Logger
	store      *store.Store
}

type authWhoAmIResponse struct {
	AuthenticationEnabled bool                  `json:"authenticationEnabled"`
	Principal             authPrincipalResponse `json:"principal"`
	Permissions           []string              `json:"permissions"`
}

type authPrincipalResponse struct {
	Kind           string                      `json:"kind"`
	ServiceAccount *authServiceAccountResponse `json:"serviceAccount,omitempty"`
}

type authServiceAccountResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Role        string  `json:"role"`
	TokenPrefix string  `json:"tokenPrefix"`
	LastUsedAt  *string `json:"lastUsedAt,omitempty"`
}

func newAuthController(adminToken string, logger *slog.Logger, stores *store.Store) authController {
	return authController{
		adminToken: strings.TrimSpace(adminToken),
		logger:     logger,
		store:      stores,
	}
}

func (a authController) enabled() bool {
	return a.adminToken != ""
}

func (a authController) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := a.resolveRequest(r)
		ctx := context.WithValue(r.Context(), authContextKey{}, state)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a authController) resolveRequest(r *http.Request) authState {
	if !a.enabled() {
		return authState{Enabled: false, Principal: authPrincipal{Kind: authPrincipalKindAnonymous}}
	}

	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if authorization == "" {
		return authState{Enabled: true, Principal: authPrincipal{Kind: authPrincipalKindAnonymous}}
	}

	secret, err := parseBearerSecret(authorization)
	if err != nil {
		return authState{
			Enabled: true,
			Failure: &authFailure{
				StatusCode: http.StatusUnauthorized,
				Message:    "invalid Authorization header; use Bearer <token>",
			},
		}
	}
	if subtle.ConstantTimeCompare([]byte(secret), []byte(a.adminToken)) == 1 {
		return authState{Enabled: true, Principal: authPrincipal{Kind: authPrincipalKindBreakGlass}}
	}
	if a.store == nil {
		return authState{
			Enabled: true,
			Failure: &authFailure{
				StatusCode: http.StatusInternalServerError,
				Message:    "internal server error",
			},
		}
	}

	platformServiceAccount, err := a.store.ResolvePlatformServiceAccountBySecret(r.Context(), secret)
	if err == nil {
		return authState{
			Enabled: true,
			Principal: authPrincipal{
				Kind:           authPrincipalKindServiceAccount,
				ServiceAccount: &platformServiceAccount,
			},
		}
	}
	if err != nil && !errors.Is(err, store.ErrPlatformServiceAccountNotFound) {
		requestScopedLogger(r, a.logger).Error("resolve control-plane platform service account failed", "error", err)
		return authState{
			Enabled: true,
			Failure: &authFailure{
				StatusCode: http.StatusInternalServerError,
				Message:    "internal server error",
			},
		}
	}

	return authState{
		Enabled: true,
		Failure: &authFailure{
			StatusCode: http.StatusUnauthorized,
			Message:    "invalid bearer token",
		},
	}
}

func (a authController) platformAccessFunc(requiredPermission string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authzRequest, _, ok := a.authorizePlatform(w, r, requiredPermission)
		if !ok {
			return
		}
		next(w, authzRequest)
	}
}

func (a authController) breakGlassOnlyFunc(requiredPermission string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authzRequest, principal, ok := a.authorizePlatform(w, r, requiredPermission)
		if !ok {
			return
		}
		if principal.Kind != authPrincipalKindBreakGlass {
			a.deny(w, r, requiredPermission, authScopeTypeControl, "", http.StatusForbidden, "break-glass admin token required", principal)
			return
		}
		next(w, authzRequest)
	}
}

func (a authController) whoAmI(w http.ResponseWriter, r *http.Request) {
	state := authStateFromRequest(r)
	if !state.Enabled {
		writeJSON(w, http.StatusOK, authWhoAmIResponse{
			AuthenticationEnabled: false,
			Principal:             authPrincipalResponse{Kind: string(authPrincipalKindAnonymous)},
			Permissions:           []string{},
		})
		return
	}

	principal, ok := a.requireAuthenticated(w, r, authPermissionSelfRead, authScopeTypeControl, "")
	if !ok {
		return
	}

	response := authWhoAmIResponse{
		AuthenticationEnabled: true,
		Principal:             authPrincipalResponse{Kind: string(principal.Kind)},
		Permissions:           permissionsForPrincipal(principal),
	}

	if principal.ServiceAccount != nil {
		var lastUsedAt *string
		if principal.ServiceAccount.LastUsedAt != nil {
			formatted := principal.ServiceAccount.LastUsedAt.UTC().Format(http.TimeFormat)
			lastUsedAt = &formatted
		}
		response.Principal.ServiceAccount = &authServiceAccountResponse{
			ID:          principal.ServiceAccount.ID,
			Name:        principal.ServiceAccount.Name,
			Role:        string(principal.ServiceAccount.Role),
			TokenPrefix: principal.ServiceAccount.TokenPrefix,
			LastUsedAt:  lastUsedAt,
		}
	}

	writeJSON(w, http.StatusOK, response)
}

func (a authController) authorizePlatform(w http.ResponseWriter, r *http.Request, requiredPermission string) (*http.Request, authPrincipal, bool) {
	state := authStateFromRequest(r)
	if !state.Enabled {
		return withAuthzDecision(r, authzDecision{
			RequiredPermission: requiredPermission,
			ScopeType:          authScopeTypeControl,
			Decision:           authDecisionAllowed,
			Reason:             "authentication is disabled",
			MatchedRoles:       []string{authMatchedRoleAuthDisabled},
		}), state.Principal, true
	}

	principal, ok := a.requireAuthenticated(w, r, requiredPermission, authScopeTypeControl, "")
	if !ok {
		return nil, authPrincipal{}, false
	}
	if principal.Kind != authPrincipalKindBreakGlass && principal.Kind != authPrincipalKindServiceAccount {
		a.deny(w, r, requiredPermission, authScopeTypeControl, "", http.StatusForbidden, "platform credential required", principal)
		return nil, authPrincipal{}, false
	}
	if !principalAllowsPermission(principal, requiredPermission) {
		a.deny(w, r, requiredPermission, authScopeTypeControl, "", http.StatusForbidden, "token lacks required permission", principal)
		return nil, authPrincipal{}, false
	}

	return withAuthzDecision(r, authzDecision{
		RequiredPermission: requiredPermission,
		ScopeType:          authScopeTypeControl,
		Decision:           authDecisionAllowed,
		MatchedRoles:       matchedRolesForPrincipal(principal),
	}), principal, true
}

func (a authController) requireAuthenticated(w http.ResponseWriter, r *http.Request, requiredPermission string, scopeType string, scopeID string) (authPrincipal, bool) {
	state := authStateFromRequest(r)
	if !state.Enabled {
		return state.Principal, true
	}
	if state.Failure != nil {
		if state.Failure.StatusCode >= http.StatusInternalServerError {
			a.fail(w, r, requiredPermission, scopeType, scopeID, state.Failure.StatusCode, state.Failure.Message, state.Principal)
			return authPrincipal{}, false
		}
		a.deny(w, r, requiredPermission, scopeType, scopeID, state.Failure.StatusCode, state.Failure.Message, state.Principal)
		return authPrincipal{}, false
	}
	if state.Principal.Kind == authPrincipalKindAnonymous {
		a.deny(w, r, requiredPermission, scopeType, scopeID, http.StatusUnauthorized, "bearer token required", state.Principal)
		return authPrincipal{}, false
	}
	return state.Principal, true
}

func (a authController) deny(w http.ResponseWriter, r *http.Request, requiredPermission string, scopeType string, scopeID string, statusCode int, message string, principal authPrincipal) {
	a.reject(w, r, requiredPermission, scopeType, scopeID, statusCode, message, principal, operationhistory.ResultDenied, authDecisionDenied)
}

func (a authController) fail(w http.ResponseWriter, r *http.Request, requiredPermission string, scopeType string, scopeID string, statusCode int, message string, principal authPrincipal) {
	a.reject(w, r, requiredPermission, scopeType, scopeID, statusCode, message, principal, operationhistory.ResultFailed, authDecisionFailed)
}

func (a authController) reject(w http.ResponseWriter, r *http.Request, requiredPermission string, scopeType string, scopeID string, statusCode int, message string, principal authPrincipal, result string, decisionLabel string) {
	decision := authzDecision{
		RequiredPermission: requiredPermission,
		ScopeType:          scopeType,
		ScopeID:            scopeID,
		Decision:           decisionLabel,
		Reason:             message,
		MatchedRoles:       matchedRolesForPrincipal(principal),
		HTTPStatus:         statusCode,
	}
	if a.store != nil {
			auditRequest := withAuthzDecision(r, decision)
			recordOperationEvent(a.logger, a.store, auditRequest, operationhistory.CreateInput{
				Action:     deniedAction(scopeType),
				TargetType: deniedTargetType(scopeType),
			TargetID:   deniedTargetID(requiredPermission, scopeID),
			TargetName: deniedTargetName(scopeID),
			Result:     result,
		})
	}
	writeJSON(w, statusCode, map[string]any{"error": message})
}

func authStateFromRequest(r *http.Request) authState {
	if r == nil {
		return authState{Enabled: false, Principal: authPrincipal{Kind: authPrincipalKindAnonymous}}
	}
	state, ok := r.Context().Value(authContextKey{}).(authState)
	if !ok {
		return authState{Enabled: false, Principal: authPrincipal{Kind: authPrincipalKindAnonymous}}
	}
	return state
}

func principalFromRequest(r *http.Request) authPrincipal {
	return authStateFromRequest(r).Principal
}

func withAuthzDecision(r *http.Request, decision authzDecision) *http.Request {
	ctx := context.WithValue(r.Context(), authzContextKey{}, decision)
	return r.WithContext(ctx)
}

func authzDecisionFromRequest(r *http.Request) (authzDecision, bool) {
	if r == nil {
		return authzDecision{}, false
	}
	decision, ok := r.Context().Value(authzContextKey{}).(authzDecision)
	return decision, ok
}

func parseBearerSecret(value string) (string, error) {
	return httpx.ParseBearerSecret(value)
}

func permissionsForPrincipal(principal authPrincipal) []string {
	switch principal.Kind {
	case authPrincipalKindBreakGlass:
		return []string{
			authPermissionSelfRead,
			authPermissionControlRead,
			authPermissionControlWrite,
			authPermissionResourceRead,
			authPermissionResourceWrite,
			authPermissionServiceDeploy,
			authPermissionOperationsRead,
		}
	case authPrincipalKindServiceAccount:
		if principal.ServiceAccount == nil {
			return []string{}
		}
		switch principal.ServiceAccount.Role {
		case serviceaccount.PlatformRoleOwner:
			return []string{
				authPermissionSelfRead,
				authPermissionControlRead,
				authPermissionControlWrite,
				authPermissionResourceRead,
				authPermissionResourceWrite,
				authPermissionServiceDeploy,
				authPermissionOperationsRead,
			}
		case serviceaccount.PlatformRoleAdmin:
			return []string{
				authPermissionSelfRead,
				authPermissionControlRead,
				authPermissionControlWrite,
				authPermissionResourceRead,
				authPermissionResourceWrite,
				authPermissionServiceDeploy,
				authPermissionOperationsRead,
			}
		case serviceaccount.PlatformRoleOperator:
			return []string{
				authPermissionSelfRead,
				authPermissionControlRead,
				authPermissionResourceRead,
				authPermissionServiceDeploy,
				authPermissionOperationsRead,
			}
		case serviceaccount.PlatformRoleAuditor:
			return []string{
				authPermissionSelfRead,
				authPermissionControlRead,
				authPermissionResourceRead,
				authPermissionOperationsRead,
			}
		default:
			return []string{}
		}
	default:
		return []string{}
	}
}

func principalAllowsPermission(principal authPrincipal, requiredPermission string) bool {
	if requiredPermission == "" {
		return true
	}
	for _, permission := range permissionsForPrincipal(principal) {
		if permission == requiredPermission {
			return true
		}
	}
	return false
}

func matchedRolesForPrincipal(principal authPrincipal) []string {
	switch principal.Kind {
	case authPrincipalKindBreakGlass:
		return []string{authMatchedRoleBreakGlassAdmin}
	case authPrincipalKindServiceAccount:
		if principal.ServiceAccount == nil {
			return nil
		}
		switch principal.ServiceAccount.Role {
		case serviceaccount.PlatformRoleOwner:
			return []string{authMatchedRolePlatformOwner}
		case serviceaccount.PlatformRoleAdmin:
			return []string{authMatchedRolePlatformAdmin}
		case serviceaccount.PlatformRoleOperator:
			return []string{authMatchedRolePlatformOperator}
		case serviceaccount.PlatformRoleAuditor:
			return []string{authMatchedRolePlatformAuditor}
		default:
			return nil
		}
	default:
		return nil
	}
}

func deniedAction(scopeType string) string {
	return "control.auth.denied"
}

func deniedTargetType(scopeType string) string {
	return "authorization"
}

func deniedTargetID(requiredPermission string, scopeID string) string {
	if strings.TrimSpace(scopeID) != "" {
		return scopeID
	}
	return requiredPermission
}

func deniedTargetName(scopeID string) string {
	return strings.TrimSpace(scopeID)
}
