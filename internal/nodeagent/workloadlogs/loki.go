package workloadlogs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type lokiClient struct {
	pushURL    string
	tenantID   string
	httpClient *http.Client
}

func newLokiClient(rawURL string, tenantID string, httpClient *http.Client) *lokiClient {
	return &lokiClient{
		pushURL:    strings.TrimRight(strings.TrimSpace(rawURL), "/"),
		tenantID:   strings.TrimSpace(tenantID),
		httpClient: httpClient,
	}
}

func (c *lokiClient) push(ctx context.Context, labels map[string]string, timestamp time.Time, line string) error {
	payload := map[string]any{
		"streams": []map[string]any{{
			"stream": labels,
			"values": [][]string{{
				strconv.FormatInt(timestamp.UTC().UnixNano(), 10),
				line,
			}},
		}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal Loki push payload: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.pushURL+"/loki/api/v1/push", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create Loki push request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.tenantID != "" {
		httpReq.Header.Set("X-Scope-OrgID", c.tenantID)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send Loki push request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := fmt.Sprintf("status=%d", resp.StatusCode)
		if data, readErr := io.ReadAll(io.LimitReader(resp.Body, 2048)); readErr == nil {
			if trimmed := strings.TrimSpace(string(data)); trimmed != "" {
				message = fmt.Sprintf("%s body=%s", message, trimmed)
			}
		}
		return fmt.Errorf("loki rejected push: %s", message)
	}
	return nil
}

func formatLogfmtLine(platformName string, req StartRequest, stream string, timestamp time.Time, message string) string {
	var builder strings.Builder
	writeLogfmtKV(&builder, "time", timestamp.Format(time.RFC3339Nano))
	writeLogfmtKV(&builder, "platform_name", platformName)
	writeLogfmtKV(&builder, "service_id", req.ServiceID)
	writeLogfmtKV(&builder, "service_name", req.ServiceName)
	writeLogfmtKV(&builder, "plan_id", req.PlanID)
	writeLogfmtKV(&builder, "execution_id", req.ExecutionID)
	writeLogfmtKV(&builder, "node_id", req.NodeID)
	writeLogfmtKV(&builder, "container_name", req.ContainerName)
	writeLogfmtKV(&builder, "stream", stream)
	writeLogfmtKV(&builder, "msg", message)
	return strings.TrimSpace(builder.String())
}

func writeLogfmtKV(builder *strings.Builder, key string, value string) {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" || value == "" {
		return
	}
	if builder.Len() > 0 {
		builder.WriteByte(' ')
	}
	builder.WriteString(key)
	builder.WriteByte('=')
	builder.WriteString(strconv.Quote(value))
}
