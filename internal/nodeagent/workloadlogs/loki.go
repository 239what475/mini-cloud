package workloadlogs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"mini-cloud/internal/nodeagent/runtime"
)

// Config 配置工作负载日志采集和 Loki 推送行为。
type Config struct {
	// LokiURL 是 Loki 的基础地址；为空表示禁用工作负载日志推送。
	LokiURL string
	// LokiTenantID 是可选的 Loki 租户头 X-Scope-OrgID。
	LokiTenantID string
	// PlatformName 是非空时写入工作负载日志内容的平台名称。
	PlatformName string

	// PushTimeout 控制单次 Loki HTTP push 请求超时时间，零值使用默认值。
	PushTimeout time.Duration
}

// StartRequest 描述一个执行的容器日志采集上下文。
type StartRequest struct {
	// ServiceID 是执行所属服务 ID。
	ServiceID string
	// ServiceName 是执行所属服务名称。
	ServiceName string
	// DeploymentID 是执行所属部署 ID。
	DeploymentID string
	// ExecutionID 是执行 ID，也是去重和关闭日志采集的 key。
	ExecutionID string
	// ReplicaIndex 是执行对应的副本序号。
	ReplicaIndex int
	// NodeID 是承载该执行的节点 ID。
	NodeID string
	// ContainerID 是要跟随日志的运行时容器 ID。
	ContainerID string
	// ContainerName 是要跟随日志的运行时容器名称。
	ContainerName string
}

const (
	// defaultPushTimeout 是默认单次 Loki push 超时时间。
	defaultPushTimeout = 5 * time.Second
)

// Manager 管理工作负载容器日志跟随和 Loki 推送。
type Manager struct {
	// logger 记录日志采集和推送失败。
	logger *slog.Logger
	// platform 是写入 logfmt 行的平台名称。
	platform string
	// loki 是 Loki HTTP push 客户端。
	loki *lokiClient
	// runtime 是底层工作负载运行时。
	runtime runtime.Runtime
	// pushTimeout 是单次 Loki push 请求超时时间。
	pushTimeout time.Duration
	// ctx 是 Manager 生命周期根上下文。
	ctx context.Context
	// cancel 取消 Manager 下所有日志采集任务。
	cancel context.CancelFunc
	// wg 等待所有跟随和推送 goroutine 退出。
	wg sync.WaitGroup
	// mu 保护 active。
	mu sync.Mutex
	// active 记录当前正在采集日志的执行，避免重复启动。
	active map[string]struct{}
}

// NewManager 创建工作负载日志 Manager；未配置 Loki 时返回 nil。
func NewManager(logger *slog.Logger, cfg Config, containerRuntime runtime.Runtime) (*Manager, error) {
	lokiURL := strings.TrimSpace(cfg.LokiURL)
	if lokiURL == "" {
		return nil, nil
	}
	if containerRuntime == nil {
		return nil, fmt.Errorf("workload log runtime follower is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.PushTimeout <= 0 {
		cfg.PushTimeout = defaultPushTimeout
	}

	rootCtx, cancel := context.WithCancel(context.Background())
	return &Manager{
		logger:   logger,
		platform: strings.TrimSpace(cfg.PlatformName),
		loki: newLokiClient(lokiURL, cfg.LokiTenantID, &http.Client{
			Timeout: cfg.PushTimeout,
		}),
		pushTimeout: cfg.PushTimeout,
		ctx:         rootCtx,
		cancel:      cancel,
		active:      map[string]struct{}{},
		runtime:     containerRuntime,
	}, nil
}

// Close 停止所有活动日志采集任务，并等待后台 goroutine 退出或关闭上下文取消。
func (m *Manager) Close(closeCtx context.Context) error {
	m.cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		m.wg.Wait()
	}()
	select {
	case <-done:
		return nil
	case <-closeCtx.Done():
		return closeCtx.Err()
	}
}

// Start 启动指定执行的容器日志采集；未配置、参数不足或已启动时直接返回。
func (m *Manager) Start(req StartRequest) {
	if strings.TrimSpace(req.ExecutionID) == "" || strings.TrimSpace(req.ContainerID) == "" {
		return
	}

	m.mu.Lock()
	if _, exists := m.active[req.ExecutionID]; exists {
		m.mu.Unlock()
		return
	}
	m.active[req.ExecutionID] = struct{}{}
	m.wg.Add(1)
	m.mu.Unlock()

	go func() {
		defer m.wg.Done()
		defer func() {
			m.mu.Lock()
			delete(m.active, req.ExecutionID)
			m.mu.Unlock()
		}()
		err := m.runtime.StreamLogs(m.ctx, req.ContainerID, func(record runtime.LogRecord) {
			if m.ctx.Err() != nil {
				return
			}
			m.pushLine(m.ctx, req, record.Stream, record.Timestamp, record.Line)
		})
		if err != nil && m.ctx.Err() == nil {
			m.logger.Warn("open workload container log stream failed",
				"execution_id", req.ExecutionID,
				"container_id", req.ContainerID,
				"error", err,
			)
		}
	}()
}

// pushLine 规范化单行日志并推送到 Loki。
func (m *Manager) pushLine(ctx context.Context, req StartRequest, stream string, timestamp time.Time, rawLine string) {
	rawLine = strings.TrimSpace(rawLine)
	if rawLine == "" {
		return
	}
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}
	timestamp = timestamp.UTC()
	line := formatLogfmtLine(m.platform, req, stream, timestamp, rawLine)
	reqCtx, cancel := context.WithTimeout(ctx, m.pushTimeout)
	defer cancel()
	err := m.loki.push(
		reqCtx,
		map[string]string{
			"job":        "mini-cloud",
			"component":  "workload",
			"log_source": "container",
			"stream":     stream,
		},
		timestamp,
		line,
	)
	if err != nil && reqCtx.Err() == nil {
		m.logger.Warn("push workload container log to Loki failed", "execution_id", req.ExecutionID, "error", err)
	}
}

// lokiClient 是最小 Loki push API HTTP 客户端。
type lokiClient struct {
	// pushURL 是配置约定的 Loki 基础地址，不应包含 /loki/api/v1/push。
	pushURL string
	// tenantID 是可选的 Loki 租户 ID。
	tenantID string
	// httpClient 执行 Loki push HTTP 请求。
	httpClient *http.Client
}

// newLokiClient 创建 Loki HTTP push 客户端。
func newLokiClient(rawURL string, tenantID string, httpClient *http.Client) *lokiClient {
	return &lokiClient{
		pushURL:    strings.TrimRight(strings.TrimSpace(rawURL), "/"),
		tenantID:   strings.TrimSpace(tenantID),
		httpClient: httpClient,
	}
}

// push 将单条日志编码为 Loki push payload 并发送。
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

// formatLogfmtLine 将工作负载日志和执行上下文格式化为 logfmt 文本。
func formatLogfmtLine(platformName string, req StartRequest, stream string, timestamp time.Time, message string) string {
	var builder strings.Builder
	writeLogfmtKV(&builder, "time", timestamp.Format(time.RFC3339Nano))
	writeLogfmtKV(&builder, "platform_name", platformName)
	writeLogfmtKV(&builder, "service_id", req.ServiceID)
	writeLogfmtKV(&builder, "service_name", req.ServiceName)
	writeLogfmtKV(&builder, "deployment_id", req.DeploymentID)
	writeLogfmtKV(&builder, "execution_id", req.ExecutionID)
	writeLogfmtKV(&builder, "replica_index", strconv.Itoa(req.ReplicaIndex))
	writeLogfmtKV(&builder, "node_id", req.NodeID)
	writeLogfmtKV(&builder, "container_name", req.ContainerName)
	writeLogfmtKV(&builder, "stream", stream)
	writeLogfmtKV(&builder, "msg", message)
	return strings.TrimSpace(builder.String())
}

// writeLogfmtKV 在 key 和 value 非空时向 builder 追加一个 logfmt 键值对。
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
