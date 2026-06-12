package api

import (
	"net/http"
	"strings"

	"mini-cloud/internal/transport"

	"github.com/gin-gonic/gin"
)

type bearerAuth struct {
	token string
}

func newBearerAuth(token string) bearerAuth {
	return bearerAuth{
		token: strings.TrimSpace(token),
	}
}

func (a bearerAuth) requireToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		authorization := strings.TrimSpace(c.GetHeader("Authorization"))
		if authorization == "" {
			c.JSON(http.StatusUnauthorized, map[string]any{"error": "bearer token required"})
			c.Abort()
			return
		}

		secret, ok := transport.ParseBearer(authorization)
		if !ok {
			c.JSON(http.StatusUnauthorized, map[string]any{"error": "invalid Authorization header; use Bearer <token>"})
			c.Abort()
			return
		}
		if !transport.BearerMatches(secret, a.token) {
			c.JSON(http.StatusUnauthorized, map[string]any{"error": "invalid bearer token"})
			c.Abort()
			return
		}
		c.Next()
	}
}
