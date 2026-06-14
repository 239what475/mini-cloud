package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"mini-cloud/internal/transport"

	"github.com/gin-gonic/gin"
)

const (
	sessionCookieName = "mini_cloud_session"
	sessionTTL        = 24 * time.Hour
)

type adminAuth struct {
	token string
}

type loginRequest struct {
	Token string `json:"token"`
}

func newAdminAuth(token string) adminAuth {
	return adminAuth{
		token: strings.TrimSpace(token),
	}
}

func (a adminAuth) requireToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		if a.validBearer(c) || a.validSession(c) {
			c.Next()
			return
		}
		c.JSON(http.StatusUnauthorized, map[string]any{"error": "login required"})
		c.Abort()
	}
}

func (a adminAuth) login(c *gin.Context) {
	var request loginRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	if !transport.BearerMatches(request.Token, a.token) {
		c.JSON(http.StatusUnauthorized, map[string]any{"error": "invalid admin token"})
		return
	}
	expiresAt := time.Now().UTC().Add(sessionTTL)
	value, err := a.sessionCookieValue(expiresAt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(c),
	})
	c.JSON(http.StatusOK, map[string]any{"status": "ok"})
}

func (a adminAuth) logout(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(c),
	})
	c.JSON(http.StatusOK, map[string]any{"status": "ok"})
}

func (a adminAuth) validBearer(c *gin.Context) bool {
	authorization := strings.TrimSpace(c.GetHeader("Authorization"))
	if authorization == "" {
		return false
	}
	secret, ok := transport.ParseBearer(authorization)
	return ok && transport.BearerMatches(secret, a.token)
}

func (a adminAuth) validSession(c *gin.Context) bool {
	cookie, err := c.Cookie(sessionCookieName)
	if err != nil {
		return false
	}
	expiresAt, signature, ok := strings.Cut(cookie, ".")
	if !ok {
		return false
	}
	expiresUnix, err := strconv.ParseInt(expiresAt, 10, 64)
	if err != nil {
		return false
	}
	if time.Now().UTC().After(time.Unix(expiresUnix, 0).UTC()) {
		return false
	}
	expected := a.sessionSignature(expiresAt)
	return hmac.Equal([]byte(signature), []byte(expected))
}

func (a adminAuth) sessionCookieValue(expiresAt time.Time) (string, error) {
	expires := strconv.FormatInt(expiresAt.Unix(), 10)
	return fmt.Sprintf("%s.%s", expires, a.sessionSignature(expires)), nil
}

func (a adminAuth) sessionSignature(expiresAt string) string {
	mac := hmac.New(sha256.New, []byte(a.token))
	mac.Write([]byte("mini-cloud-control-plane-session:"))
	mac.Write([]byte(expiresAt))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func requestIsHTTPS(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	return strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") ||
		strings.EqualFold(c.GetHeader("X-Forwarded-Protocol"), "https")
}
