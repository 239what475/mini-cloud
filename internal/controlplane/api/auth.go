package api

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type adminAuth struct {
	adminToken string
}

func bearerSecret(value string) (string, bool) {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) != 2 {
		return "", false
	}
	if !strings.EqualFold(fields[0], "Bearer") {
		return "", false
	}
	if strings.TrimSpace(fields[1]) == "" {
		return "", false
	}
	return fields[1], true
}

func newAdminAuth(adminToken string) adminAuth {
	return adminAuth{
		adminToken: strings.TrimSpace(adminToken),
	}
}

func (a adminAuth) authenticate(c *gin.Context) (string, bool) {
	authorization := strings.TrimSpace(c.GetHeader("Authorization"))
	if authorization == "" {
		return "bearer token required", false
	}

	secret, ok := bearerSecret(authorization)
	if !ok {
		return "invalid Authorization header; use Bearer <token>", false
	}
	if subtle.ConstantTimeCompare([]byte(secret), []byte(a.adminToken)) == 1 {
		return "", true
	}
	return "invalid bearer token", false
}

func (a adminAuth) requireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if message, ok := a.authenticate(c); !ok {
			c.JSON(http.StatusUnauthorized, map[string]any{"error": message})
			c.Abort()
			return
		}
		c.Next()
	}
}
