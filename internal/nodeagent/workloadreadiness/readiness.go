package workloadreadiness

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	// defaultAttempts 是未显式配置尝试次数时的最小探测次数。
	defaultAttempts = 1
	// defaultInterval 是未显式配置两次探测间隔时使用的默认值。
	defaultInterval = time.Second
	// defaultTimeout 是未显式配置单次 HTTP 探测超时时使用的默认值。
	defaultTimeout = time.Second
)

// Config 描述等待工作负载 readiness 端点通过所需的探测参数。
type Config struct {
	// URL 是工作负载容器暴露给 node-agent 的 readiness HTTP 端点。
	URL string `json:"url"`
	// Attempts 是最多探测次数；小于等于 0 时按 1 次处理。
	Attempts int `json:"attempts"`
	// Interval 是两次探测之间的等待时间；小于等于 0 时使用默认值。
	Interval time.Duration `json:"interval"`
	// Timeout 是单次 HTTP 探测的超时时间；小于等于 0 时使用默认值。
	Timeout time.Duration `json:"timeout"`
}

// Observation 记录一次工作负载 readiness 探测尝试。
type Observation struct {
	// Attempt 是从 1 开始的尝试序号。
	Attempt int `json:"attempt"`
	// StartedAt 是本次尝试开始时的 UTC 时间。
	StartedAt time.Time `json:"startedAt"`
	// StatusCode 是收到响应时工作负载返回的 HTTP 状态码。
	StatusCode int `json:"statusCode,omitempty"`
	// Error 描述本次尝试中的请求、响应体读取、关闭、上下文取消或非成功状态码错误。
	Error string `json:"error,omitempty"`
}

// Result 汇总等待工作负载 readiness 期间采集到的所有探测结果。
type Result struct {
	// URL 是被探测的工作负载 readiness 端点。
	URL string `json:"url"`
	// Passed 表示是否有任一次尝试返回成功 HTTP 状态且响应处理无错误。
	Passed bool `json:"passed"`
	// PassedAt 是首次成功尝试开始时的观测时间。
	PassedAt *time.Time `json:"passedAt,omitempty"`
	// Observations 按顺序保存每一次探测结果。
	Observations []Observation `json:"observations"`
}

// httpDoer 是执行 HTTP 请求所需的最小能力，便于测试注入确定性实现。
type httpDoer interface {
	// Do 执行一次 HTTP 请求并返回响应。
	Do(*http.Request) (*http.Response, error)
}

// Checker 等待工作负载 readiness HTTP 端点通过。
type Checker struct {
	// client 执行单次 HTTP 探测；为空时使用 http.DefaultClient。
	client httpDoer
}

// NewChecker 创建工作负载 readiness 检查器。
func NewChecker(client httpDoer) Checker {
	if client == nil {
		client = http.DefaultClient
	}
	return Checker{client: client}
}

// Wait 按配置轮询工作负载 readiness URL，直到通过、耗尽尝试次数或上下文取消。
func (c Checker) Wait(ctx context.Context, cfg Config) Result {
	cfg = normalizeConfig(cfg)
	result := Result{
		URL:          cfg.URL,
		Observations: make([]Observation, 0, cfg.Attempts),
	}

	for attempt := 1; attempt <= cfg.Attempts; attempt++ {
		observation := c.probe(ctx, cfg, attempt)
		result.Observations = append(result.Observations, observation)
		if observation.Error == "" && observation.StatusCode >= 200 && observation.StatusCode < 400 {
			passedAt := observation.StartedAt
			result.Passed = true
			result.PassedAt = &passedAt
			return result
		}
		if ctx.Err() != nil || attempt == cfg.Attempts {
			return result
		}
		if !sleep(ctx, cfg.Interval) {
			return result
		}
	}

	return result
}

// probe 执行一次 readiness HTTP 探测并返回结构化观测结果。
func (c Checker) probe(ctx context.Context, cfg Config, attempt int) Observation {
	observation := Observation{
		Attempt:   attempt,
		StartedAt: time.Now().UTC(),
	}
	if err := ctx.Err(); err != nil {
		observation.Error = err.Error()
		return observation
	}

	probeCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, cfg.URL, nil)
	if err != nil {
		observation.Error = err.Error()
		return observation
	}

	client := c.client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		observation.Error = err.Error()
		return observation
	}
	observation.StatusCode = resp.StatusCode
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		observation.Error = fmt.Sprintf("drain response body: %v", err)
	}
	if closeErr := resp.Body.Close(); closeErr != nil && observation.Error == "" {
		observation.Error = fmt.Sprintf("close response body: %v", closeErr)
		return observation
	}
	if observation.Error != "" {
		return observation
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		observation.Error = fmt.Sprintf("unexpected status %d", resp.StatusCode)
	}
	return observation
}

// normalizeConfig 补齐 workload readiness 探测参数的安全默认值。
func normalizeConfig(cfg Config) Config {
	if cfg.Attempts <= 0 {
		cfg.Attempts = defaultAttempts
	}
	if cfg.Interval <= 0 {
		cfg.Interval = defaultInterval
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	return cfg
}

// sleep 等待下一次探测间隔；如果上下文先结束则返回 false。
func sleep(ctx context.Context, interval time.Duration) bool {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
