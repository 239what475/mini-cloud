package processconfig

import (
	"os"
	"strconv"
)

type Config struct {
	HTTPAddr                       string
	UIDir                          string
	DatabaseURL                    string
	AdminToken                     string
	PlaneSyncIntervalSeconds       int
	ServiceReconcileTimeoutSeconds int
	LokiURL                        string
	LokiTenantID                   string
	LokiQueryTimeoutSeconds        int
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:                       getenv("MINICLOUD_HTTP_ADDR", ":8080"),
		UIDir:                          getenv("MINICLOUD_UI_DIR", "web/dist"),
		DatabaseURL:                    getenv("MINICLOUD_CONTROL_PLANE_DATABASE_URL", "postgres://mini_cloud:mini_cloud@127.0.0.1:5432/mini_cloud_control_plane?sslmode=disable"),
		AdminToken:                     getenv("MINICLOUD_ADMIN_TOKEN", ""),
		PlaneSyncIntervalSeconds:       getenvInt("MINICLOUD_PLANE_SYNC_INTERVAL_SECONDS", 30),
		ServiceReconcileTimeoutSeconds: getenvInt("MINICLOUD_SERVICE_RECONCILE_TIMEOUT_SECONDS", 1200),
		LokiURL:                        getenv("MINICLOUD_LOKI_URL", ""),
		LokiTenantID:                   getenv("MINICLOUD_LOKI_TENANT_ID", ""),
		LokiQueryTimeoutSeconds:        getenvInt("MINICLOUD_LOKI_QUERY_TIMEOUT_SECONDS", 5),
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	if value := os.Getenv(key); value != "" {
		parsed, err := strconv.Atoi(value)
		if err == nil {
			return parsed
		}
	}
	return fallback
}
