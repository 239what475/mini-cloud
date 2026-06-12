package workloadlogs

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"mini-cloud/internal/nodeagent/runtime"
)

type Config struct {
	LokiURL      string
	LokiTenantID string
	PlatformName string
}

type StartRequest struct {
	ServiceID     string
	ServiceName   string
	PlanID        string
	ExecutionID   string
	NodeID        string
	ContainerID   string
	ContainerName string
}

const pushTimeout = 5 * time.Second

type logRuntime interface {
	StreamLogs(context.Context, string, runtime.LogEmitter) error
}

type Collector struct {
	logger   *slog.Logger
	platform string
	loki     *lokiClient
	runtime  logRuntime
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	mu       sync.Mutex
	active   map[string]struct{}
}

func NewCollector(logger *slog.Logger, cfg Config, containerRuntime logRuntime) (*Collector, error) {
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

	rootCtx, cancel := context.WithCancel(context.Background())
	return &Collector{
		logger:   logger,
		platform: strings.TrimSpace(cfg.PlatformName),
		loki: newLokiClient(lokiURL, cfg.LokiTenantID, &http.Client{
			Timeout: pushTimeout,
		}),
		ctx:     rootCtx,
		cancel:  cancel,
		active:  map[string]struct{}{},
		runtime: containerRuntime,
	}, nil
}

func (c *Collector) Close(closeCtx context.Context) error {
	c.cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		c.wg.Wait()
	}()
	select {
	case <-done:
		return nil
	case <-closeCtx.Done():
		return closeCtx.Err()
	}
}

func (c *Collector) Start(req StartRequest) {
	if strings.TrimSpace(req.ExecutionID) == "" || strings.TrimSpace(req.ContainerID) == "" {
		return
	}

	c.mu.Lock()
	if _, exists := c.active[req.ExecutionID]; exists {
		c.mu.Unlock()
		return
	}
	c.active[req.ExecutionID] = struct{}{}
	c.wg.Add(1)
	c.mu.Unlock()

	go func() {
		defer c.wg.Done()
		defer func() {
			c.mu.Lock()
			delete(c.active, req.ExecutionID)
			c.mu.Unlock()
		}()
		err := c.runtime.StreamLogs(c.ctx, req.ContainerID, func(record runtime.LogRecord) {
			if c.ctx.Err() != nil {
				return
			}
			c.pushLine(c.ctx, req, record.Stream, record.Timestamp, record.Line)
		})
		if err != nil && c.ctx.Err() == nil {
			c.logger.Warn("open workload container log stream failed",
				"execution_id", req.ExecutionID,
				"container_id", req.ContainerID,
				"error", err,
			)
		}
	}()
}

func (c *Collector) pushLine(ctx context.Context, req StartRequest, stream string, timestamp time.Time, rawLine string) {
	rawLine = strings.TrimSpace(rawLine)
	if rawLine == "" {
		return
	}
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}
	timestamp = timestamp.UTC()
	line := formatLogfmtLine(c.platform, req, stream, timestamp, rawLine)
	reqCtx, cancel := context.WithTimeout(ctx, pushTimeout)
	defer cancel()
	err := c.loki.push(
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
		c.logger.Warn("push workload container log to Loki failed", "execution_id", req.ExecutionID, "error", err)
	}
}
