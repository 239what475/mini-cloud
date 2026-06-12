package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"mini-cloud/internal/controlplane/model"
	"mini-cloud/internal/controlplane/store"

	"github.com/gin-gonic/gin"
)

func metricsHandler(stores *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := stores.ListPlanes(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
		c.Header("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		fmt.Fprint(c.Writer, renderControlMetrics(items, time.Now().UTC()))
	}
}

func renderControlMetrics(controlPlanes []model.PlaneDetail, now time.Time) string {
	var out strings.Builder
	out.WriteString("# HELP minicloud_plane_count Current number of planes in the control-plane store.\n")
	out.WriteString("# TYPE minicloud_plane_count gauge\n")
	fmt.Fprintf(&out, "minicloud_plane_count %d\n", len(controlPlanes))
	out.WriteString("# HELP minicloud_plane_status Current plane status, emitted as one-hot samples per plane and status.\n")
	out.WriteString("# TYPE minicloud_plane_status gauge\n")
	for _, item := range controlPlanes {
		for _, status := range []string{model.StatusSyncing, model.StatusReady, model.StatusDegraded, model.StatusOffline} {
			value := 0
			if item.Status.Status == status {
				value = 1
			}
			fmt.Fprintf(&out, "minicloud_plane_status{name=%q,plane_id=%q,status=%q} %d\n", item.Name, item.ID, status, value)
		}
		if item.Status.LastSyncAt != nil {
			fmt.Fprintf(&out, "minicloud_plane_last_sync_age_seconds{plane_id=%q} %.0f\n", item.ID, now.Sub(item.Status.LastSyncAt.UTC()).Seconds())
		}
	}
	return out.String()
}
