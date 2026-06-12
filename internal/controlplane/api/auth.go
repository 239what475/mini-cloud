package api

import (
	"net/http"
	"strings"

	"mini-cloud/internal/bearer"

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

func (a bearerAuth) authenticate(c *gin.Context) (string, bool) {
	authorization := strings.TrimSpace(c.GetHeader("Authorization"))
	if authorization == "" {
		return "bearer token required", false
	}

	secret, ok := bearer.Parse(authorization)
	if !ok {
		return "invalid Authorization header; use Bearer <token>", false
	}
	if bearer.Matches(secret, a.token) {
		return "", true
	}
	return "invalid bearer token", false
}

func (a bearerAuth) requireToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		if message, ok := a.authenticate(c); !ok {
			c.JSON(http.StatusUnauthorized, map[string]any{"error": message})
			c.Abort()
			return
		}
		c.Next()
	}
}
