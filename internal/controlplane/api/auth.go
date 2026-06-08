package api

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"

	"mini-cloud/internal/common/httpx"
	"mini-cloud/internal/controlplane/store"

	"github.com/gin-gonic/gin"
)

const (
	authPrincipalKindAdmin = "admin"
)

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

func (a authController) resolveRequest(c *gin.Context) (authPrincipal, *authFailure) {
	authorization := strings.TrimSpace(c.GetHeader("Authorization"))
	if authorization == "" {
		return authPrincipal{}, &authFailure{
			StatusCode: http.StatusUnauthorized,
			Message:    "bearer token required",
		}
	}

	secret, err := httpx.ParseBearerSecret(authorization)
	if err != nil {
		return authPrincipal{}, &authFailure{
			StatusCode: http.StatusUnauthorized,
			Message:    "invalid Authorization header; use Bearer <token>",
		}
	}
	if subtle.ConstantTimeCompare([]byte(secret), []byte(a.adminToken)) == 1 {
		return authPrincipal{Kind: authPrincipalKindAdmin}, nil
	}
	return authPrincipal{}, &authFailure{
		StatusCode: http.StatusUnauthorized,
		Message:    "invalid bearer token",
	}
}

func (a authController) adminOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		principal, failure := a.resolveRequest(c)
		if failure != nil {
			writeJSON(c, failure.StatusCode, map[string]any{"error": failure.Message})
			c.Abort()
			return
		}
		if principal.Kind != authPrincipalKindAdmin {
			writeJSON(c, http.StatusForbidden, map[string]any{"error": "admin token required"})
			c.Abort()
			return
		}
		c.Set("principal", principal)
		c.Next()
	}
}

func (a authController) whoAmI(c *gin.Context) {
	principal, ok := c.Get("principal")
	if !ok {
		writeJSON(c, http.StatusUnauthorized, map[string]any{"error": "bearer token required"})
		return
	}
	authPrincipal, ok := principal.(authPrincipal)
	if !ok || authPrincipal.Kind == "" {
		writeJSON(c, http.StatusUnauthorized, map[string]any{"error": "bearer token required"})
		return
	}

	writeJSON(c, http.StatusOK, authWhoAmIResponse{
		Principal: authPrincipalResponse{Kind: authPrincipal.Kind},
	})
}
