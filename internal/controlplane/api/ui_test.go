package api

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestServeRootIndexAsHTML(t *testing.T) {
	t.Parallel()

	uiDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(uiDir, "index.html"), []byte("<!doctype html><html></html>"), 0600); err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	serveRootJSONOrIndex(slog.Default(), uiDir, router)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", contentType)
	}
	if disposition := recorder.Header().Get("Content-Disposition"); disposition != "inline" {
		t.Fatalf("Content-Disposition = %q, want inline", disposition)
	}
}
