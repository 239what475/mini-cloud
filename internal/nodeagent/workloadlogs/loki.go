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

	// QueueSize 控制每个执行的内存日志队列大小，零值使用默认值。
	QueueSize int
	// BatchSize 控制一次 Loki push 最多发送的日志行数，零值使用默认值。
	BatchSize int
	// BatchWait 控制未满批次等待推送的最长时间，零值使用默认值。
	BatchWait time.Duration
	// PushTimeout 控制单次 Loki HTTP push 请求超时时间，零值使用默认值。
	PushTimeout time.Duration
}

// StartRequest 描述一个执行的容器日志采集上下文。
type StartRequest struct {
	// ProjectID 是执行所属项目 ID。
	ProjectID string
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
	// defaultQueueSize 是每个执行日志队列的默认长度。
	defaultQueueSize = 1024
	// defaultBatchSize 是默认单次 Loki push 日志条目数。
	defaultBatchSize = 100
	// defaultBatchWait 是默认批次最大等待时间。
	defaultBatchWait = time.Second
	// defaultPushTimeout 是默认单次 Loki push 超时时间。
	defaultPushTimeout = 5 * time.Second
)

// Manager 管理工作负载容器日志跟随、批处理和 Loki 推送。
type Manager struct {
	// logger 记录日志采集和推送失败。
	logger *slog.Logger
	// platform 是写入 logfmt 行的平台名称。
	platform string
	// loki 是 Loki HTTP push 客户端。
	loki *lokiClient
	// logs 是底层工作负载运行时。
	logs runtime.Runtime
	// queueSize 是每个执行的内存日志队列大小。
	queueSize int
	// batchSize 是单批次最多推送的日志行数。
	batchSize int
	// batchWait 是未满批次等待推送的最长时间。
	batchWait time.Duration
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
	// active 保存当前正在采集日志的执行句柄，key 为 executionID。
	active map[string]*Handle
}

// Handle 表示一次工作负载日志采集任务。
type Handle struct {
	// cancel 停止该执行的日志跟随上下文。
	cancel context.CancelFunc
	// done 在该执行的推送 goroutine 退出时关闭。
	done chan struct{}
	// once 保证 Close 只取消一次。
	once sync.Once
}

// queuedLog 是进入批处理队列的一条容器日志。
type queuedLog struct {
	// stream 是日志流名称，例如 stdout 或 stderr。
	stream string
	// timestamp 是日志记录时间。
	timestamp time.Time
	// line 是日志正文。
	line string
}

// NewManager 创建工作负载日志 Manager；未配置 Loki 时返回禁用但可安全调用的 Manager。
func NewManager(logger *slog.Logger, cfg Config, logs runtime.Runtime) (*Manager, error) {
	rootCtx, cancel := context.WithCancel(context.Background())
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = defaultQueueSize
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = defaultBatchSize
	}
	if cfg.BatchWait <= 0 {
		cfg.BatchWait = defaultBatchWait
	}
	if cfg.PushTimeout <= 0 {
		cfg.PushTimeout = defaultPushTimeout
	}
	loki := newLokiClient(cfg.LokiURL, cfg.LokiTenantID, &http.Client{
		Timeout: cfg.PushTimeout,
	})
	manager := &Manager{
		logger:      logger,
		platform:    strings.TrimSpace(cfg.PlatformName),
		loki:        loki,
		queueSize:   cfg.QueueSize,
		batchSize:   cfg.BatchSize,
		batchWait:   cfg.BatchWait,
		pushTimeout: cfg.PushTimeout,
		ctx:         rootCtx,
		cancel:      cancel,
		active:      map[string]*Handle{},
		logs:        logs,
	}
	if !manager.Configured() {
		return manager, nil
	}
	if manager.logs == nil {
		cancel()
		return nil, fmt.Errorf("workload log runtime follower is required")
	}
	return manager, nil
}

// Configured 返回工作负载日志推送是否已配置并启用。
func (m *Manager) Configured() bool {
	return m != nil && m.loki != nil && m.loki.Configured()
}

// Close 停止所有活动日志采集任务，并等待后台 goroutine 退出或关闭上下文取消。
func (m *Manager) Close(closeCtx context.Context) error {
	if m == nil {
		return nil
	}
	if closeCtx == nil {
		closeCtx = context.Background()
	}
	if m.cancel != nil {
		m.cancel()
	}

	m.mu.Lock()
	handles := make([]*Handle, 0, len(m.active))
	for _, handle := range m.active {
		handles = append(handles, handle)
	}
	m.mu.Unlock()

	for _, handle := range handles {
		if err := handle.Close(closeCtx); err != nil {
			return err
		}
	}

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

// Start 启动指定执行的容器日志采集；未配置或参数不足时返回已关闭句柄。
func (m *Manager) Start(req StartRequest) *Handle {
	if !m.Configured() || strings.TrimSpace(req.ExecutionID) == "" || strings.TrimSpace(req.ContainerID) == "" {
		return closedHandle()
	}

	m.mu.Lock()
	if handle, exists := m.active[req.ExecutionID]; exists {
		m.mu.Unlock()
		return handle
	}
	ctx, cancel := context.WithCancel(m.ctx)
	handle := &Handle{cancel: cancel, done: make(chan struct{})}
	m.active[req.ExecutionID] = handle
	queue := make(chan queuedLog, m.queueSize)
	m.wg.Add(2)
	m.mu.Unlock()

	go func() {
		defer m.wg.Done()
		defer close(queue)
		m.followExecution(ctx, req, queue)
	}()

	go func() {
		defer m.wg.Done()
		defer close(handle.done)
		defer func() {
			m.mu.Lock()
			delete(m.active, req.ExecutionID)
			m.mu.Unlock()
		}()
		m.pushQueued(req, queue)
	}()
	return handle
}

// closedHandle 返回一个已经完成的空日志采集句柄。
func closedHandle() *Handle {
	done := make(chan struct{})
	close(done)
	return &Handle{done: done}
}

// Close 停止该执行的日志采集，并等待队列中日志完成推送或上下文取消。
func (h *Handle) Close(ctx context.Context) error {
	if h == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	h.once.Do(func() {
		if h.cancel != nil {
			h.cancel()
		}
	})
	select {
	case <-h.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// followExecution 从运行时持续读取容器日志，并写入批处理队列。
func (m *Manager) followExecution(ctx context.Context, req StartRequest, queue chan<- queuedLog) {
	err := m.logs.FollowLogs(ctx, req.ContainerID, func(record runtime.LogRecord) {
		line := strings.TrimSpace(record.Line)
		if line == "" {
			return
		}
		if record.Timestamp.IsZero() {
			record.Timestamp = time.Now().UTC()
		}
		item := queuedLog{stream: record.Stream, timestamp: record.Timestamp.UTC(), line: line}
		select {
		case queue <- item:
		case <-ctx.Done():
		default:
			m.logger.Warn("drop workload container log because queue is full", "execution_id", req.ExecutionID)
		}
	})
	if err != nil && ctx.Err() == nil {
		m.logger.Warn("open workload container log stream failed",
			"execution_id", req.ExecutionID,
			"container_id", req.ContainerID,
			"error", err,
		)
	}
}

// pushQueued 从队列读取日志，按大小或时间窗口聚合后推送，直到队列关闭并完成最后一次刷新。
func (m *Manager) pushQueued(req StartRequest, queue <-chan queuedLog) {
	batchSize := m.batchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}
	batchWait := m.batchWait
	if batchWait <= 0 {
		batchWait = defaultBatchWait
	}
	ticker := time.NewTicker(batchWait)
	defer ticker.Stop()

	batch := make([]queuedLog, 0, batchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		m.pushBatch(req, batch)
		batch = batch[:0]
	}

	for {
		select {
		case item, ok := <-queue:
			if !ok {
				flush()
				return
			}
			batch = append(batch, item)
			if len(batch) >= batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// pushBatch 将一批日志按 stream 分组，并使用内部 pushTimeout 上下文推送到 Loki。
func (m *Manager) pushBatch(req StartRequest, items []queuedLog) {
	streams := map[string][]lokiEntry{}
	for _, item := range items {
		line := formatLogfmtLine(m.platform, req, item.stream, item.timestamp.UTC(), item.line)
		if line == "" {
			continue
		}
		streams[item.stream] = append(streams[item.stream], lokiEntry{Timestamp: item.timestamp.UTC(), Line: line})
	}
	if len(streams) == 0 {
		return
	}
	payloadStreams := make([]lokiStream, 0, len(streams))
	for stream, entries := range streams {
		payloadStreams = append(payloadStreams, lokiStream{
			Labels: map[string]string{
				"job":        "mini-cloud",
				"component":  "workload",
				"log_source": "container",
				"stream":     stream,
			},
			Entries: entries,
		})
	}
	pushTimeout := m.pushTimeout
	if pushTimeout <= 0 {
		pushTimeout = defaultPushTimeout
	}
	reqCtx, cancel := context.WithTimeout(context.Background(), pushTimeout)
	defer cancel()
	if err := m.loki.PushStreams(reqCtx, payloadStreams); err != nil && reqCtx.Err() == nil {
		m.logger.Warn("push workload container log batch to Loki failed", "execution_id", req.ExecutionID, "error", err)
	}
}

// pushLine 规范化单行日志并通过批量路径推送，主要用于测试和小批量场景。
func (m *Manager) pushLine(req StartRequest, stream string, timestamp time.Time, rawLine string) {
	rawLine = strings.TrimSpace(rawLine)
	if rawLine == "" {
		return
	}
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}
	m.pushBatch(req, []queuedLog{{stream: stream, timestamp: timestamp.UTC(), line: rawLine}})
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

// lokiStream 表示一次 Loki push payload 中的一个 stream。
type lokiStream struct {
	// Labels 是 Loki stream labels。
	Labels map[string]string
	// Entries 是该 stream 下的日志条目。
	Entries []lokiEntry
}

// lokiEntry 表示一条准备发送给 Loki 的日志。
type lokiEntry struct {
	// Timestamp 是 Loki value 使用的日志时间。
	Timestamp time.Time
	// Line 是 Loki value 使用的日志内容。
	Line string
}

// newLokiClient 创建 Loki HTTP push 客户端。
func newLokiClient(rawURL string, tenantID string, httpClient *http.Client) *lokiClient {
	return &lokiClient{
		pushURL:    strings.TrimRight(strings.TrimSpace(rawURL), "/"),
		tenantID:   strings.TrimSpace(tenantID),
		httpClient: httpClient,
	}
}

// Configured 返回 Loki push 地址是否已配置。
func (c *lokiClient) Configured() bool {
	return c != nil && c.pushURL != ""
}

// Push 推送单个 Loki stream。
func (c *lokiClient) Push(ctx context.Context, stream lokiStream) error {
	return c.PushStreams(ctx, []lokiStream{stream})
}

// PushStreams 将多个 Loki stream 编码为 push payload 并发送。
func (c *lokiClient) PushStreams(ctx context.Context, streams []lokiStream) error {
	if !c.Configured() {
		return nil
	}
	if c.httpClient == nil {
		c.httpClient = http.DefaultClient
	}

	payloadStreams := make([]map[string]any, 0, len(streams))
	for _, stream := range streams {
		values := lokiValues(stream.Entries)
		if len(values) == 0 {
			continue
		}
		payloadStreams = append(payloadStreams, map[string]any{
			"stream": stream.Labels,
			"values": values,
		})
	}
	if len(payloadStreams) == 0 {
		return nil
	}
	payload := map[string]any{"streams": payloadStreams}
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

// lokiValues 将日志条目转换为 Loki values 数组。
func lokiValues(entries []lokiEntry) [][]string {
	values := make([][]string, 0, len(entries))
	for _, entry := range entries {
		timestamp := entry.Timestamp
		if timestamp.IsZero() {
			timestamp = time.Now().UTC()
		}
		line := strings.TrimSpace(entry.Line)
		if line == "" {
			continue
		}
		values = append(values, []string{
			strconv.FormatInt(timestamp.UTC().UnixNano(), 10),
			line,
		})
	}
	return values
}

// formatLogfmtLine 将工作负载日志和执行上下文格式化为 logfmt 文本。
func formatLogfmtLine(platformName string, req StartRequest, stream string, timestamp time.Time, message string) string {
	var builder strings.Builder
	writeLogfmtKV(&builder, "time", timestamp.Format(time.RFC3339Nano))
	writeLogfmtKV(&builder, "platform_name", platformName)
	writeLogfmtKV(&builder, "project_id", req.ProjectID)
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
