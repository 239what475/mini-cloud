package api

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"

	"mini-cloud/internal/common/httpx"
	"mini-cloud/internal/controlplane/store"
)

const (
	authPrincipalKindAnonymous = "anonymous"
	authPrincipalKindAdmin     = "admin"
)

const (
	authPermissionControlRead    = "control.read"
	authPermissionControlWrite   = "control.write"
	authPermissionResourceRead   = "resource.read"
	authPermissionResourceWrite  = "resource.write"
	authPermissionServiceDeploy  = "service.deploy.write"
	authPermissionSelfRead       = "auth.self.read"
	authPermissionOperationsRead = "operations.read"
)

type authContextKey struct{}

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
	Kind string
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
	Kind string `json:"kind"`
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

	secret, err := httpx.ParseBearerSecret(authorization)
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
		return authState{Enabled: true, Principal: authPrincipal{Kind: authPrincipalKindAdmin}}
	}
	return authState{
		Enabled: true,
		Failure: &authFailure{
			StatusCode: http.StatusUnauthorized,
			Message:    "invalid bearer token",
		},
	}
}

func (a authController) platformAccessFunc(_ string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authRequest, ok := a.authorize(w, r)
		if !ok {
			return
		}
		next(w, authRequest)
	}
}

func (a authController) whoAmI(w http.ResponseWriter, r *http.Request) {
	state := authStateFromRequest(r)
	if !state.Enabled {
		writeJSON(w, http.StatusOK, authWhoAmIResponse{
			AuthenticationEnabled: false,
			Principal:             authPrincipalResponse{Kind: authPrincipalKindAnonymous},
			Permissions:           []string{},
		})
		return
	}

	principal, ok := a.requireAuthenticated(w, r)
	if !ok {
		return
	}

	writeJSON(w, http.StatusOK, authWhoAmIResponse{
		AuthenticationEnabled: true,
		Principal:             authPrincipalResponse{Kind: principal.Kind},
		Permissions:           permissionsForPrincipal(principal),
	})
}

func (a authController) authorize(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	state := authStateFromRequest(r)
	if !state.Enabled {
		return r, true
	}
	principal, ok := a.requireAuthenticated(w, r)
	if !ok {
		return nil, false
	}
	if principal.Kind != authPrincipalKindAdmin {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "admin token required"})
		return nil, false
	}
	return r, true
}

func (a authController) requireAuthenticated(w http.ResponseWriter, r *http.Request) (authPrincipal, bool) {
	state := authStateFromRequest(r)
	if !state.Enabled {
		return state.Principal, true
	}
	if state.Failure != nil {
		writeJSON(w, state.Failure.StatusCode, map[string]any{"error": state.Failure.Message})
		return authPrincipal{}, false
	}
	if state.Principal.Kind == authPrincipalKindAnonymous {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "bearer token required"})
		return authPrincipal{}, false
	}
	return state.Principal, true
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

func permissionsForPrincipal(principal authPrincipal) []string {
	if principal.Kind != authPrincipalKindAdmin {
		return []string{}
	}
	return []string{
		authPermissionSelfRead,
		authPermissionControlRead,
		authPermissionControlWrite,
		authPermissionResourceRead,
		authPermissionResourceWrite,
		authPermissionServiceDeploy,
		authPermissionOperationsRead,
	}
}
