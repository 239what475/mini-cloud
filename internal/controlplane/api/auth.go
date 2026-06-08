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
	authPrincipalKindAdmin = "admin"
)

type authContextKey struct{}

type authState struct {
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
	Principal authPrincipalResponse `json:"principal"`
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

func (a authController) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := a.resolveRequest(r)
		ctx := context.WithValue(r.Context(), authContextKey{}, state)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a authController) resolveRequest(r *http.Request) authState {
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if authorization == "" {
		return authState{
			Failure: &authFailure{
				StatusCode: http.StatusUnauthorized,
				Message:    "bearer token required",
			},
		}
	}

	secret, err := httpx.ParseBearerSecret(authorization)
	if err != nil {
		return authState{
			Failure: &authFailure{
				StatusCode: http.StatusUnauthorized,
				Message:    "invalid Authorization header; use Bearer <token>",
			},
		}
	}
	if subtle.ConstantTimeCompare([]byte(secret), []byte(a.adminToken)) == 1 {
		return authState{Principal: authPrincipal{Kind: authPrincipalKindAdmin}}
	}
	return authState{
		Failure: &authFailure{
			StatusCode: http.StatusUnauthorized,
			Message:    "invalid bearer token",
		},
	}
}

func (a authController) adminOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authRequest, ok := a.authorize(w, r)
		if !ok {
			return
		}
		next(w, authRequest)
	}
}

func (a authController) whoAmI(w http.ResponseWriter, r *http.Request) {
	principal, ok := a.requireAuthenticated(w, r)
	if !ok {
		return
	}

	writeJSON(w, http.StatusOK, authWhoAmIResponse{
		Principal: authPrincipalResponse{Kind: principal.Kind},
	})
}

func (a authController) authorize(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
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
	if state.Failure != nil {
		writeJSON(w, state.Failure.StatusCode, map[string]any{"error": state.Failure.Message})
		return authPrincipal{}, false
	}
	if state.Principal.Kind == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "bearer token required"})
		return authPrincipal{}, false
	}
	return state.Principal, true
}

func authStateFromRequest(r *http.Request) authState {
	if r == nil {
		return authState{}
	}
	state, ok := r.Context().Value(authContextKey{}).(authState)
	if !ok {
		return authState{}
	}
	return state
}
