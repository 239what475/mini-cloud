package logquery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

var ErrNotConfigured = errors.New("log query backend is not configured")

type InputError struct {
	Message string
}

func (e *InputError) Error() string {
	return e.Message
}

type BackendError struct {
	Query      string
	StatusCode int
	Message    string
}

func (e *BackendError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type Backend interface {
	Configured() bool
	QueryRange(context.Context, QueryInput) (Result, error)
}

type Service struct {
	baseURL    string
	tenantID   string
	httpClient *http.Client
}

type QueryInput struct {
	Start     time.Time
	End       time.Time
	Limit     int
	Direction string
	Filters   Filters
}

type Filters struct {
	Component     string
	PlatformName  string
	PlaneID       string
	ServiceID     string
	DeploymentID  string
	NodeID        string
	RuntimeNodeID string
	ExecutionID   string
	RequestID     string
	Level         string
	Contains      string
}

type Result struct {
	Query     string      `json:"query"`
	Start     time.Time   `json:"start"`
	End       time.Time   `json:"end"`
	Limit     int         `json:"limit"`
	Direction string      `json:"direction"`
	Items     []LogRecord `json:"items"`
}

type LogRecord struct {
	Timestamp time.Time         `json:"timestamp"`
	Labels    map[string]string `json:"labels"`
	Line      string            `json:"line"`
}

func NewService(baseURL string, tenantID string, timeout time.Duration) *Service {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Service{
		baseURL:  strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		tenantID: strings.TrimSpace(tenantID),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (s *Service) Configured() bool {
	return s != nil && s.baseURL != ""
}

func (s *Service) QueryRange(ctx context.Context, input QueryInput) (result Result, err error) {
	if !s.Configured() {
		return Result{}, ErrNotConfigured
	}

	normalized, err := normalizeInput(input)
	if err != nil {
		return Result{}, err
	}

	logQL := buildLogQL(normalized.Filters)
	result = Result{
		Query:     logQL,
		Start:     normalized.Start,
		End:       normalized.End,
		Limit:     normalized.Limit,
		Direction: normalized.Direction,
	}
	values := url.Values{}
	values.Set("query", logQL)
	values.Set("limit", strconv.Itoa(normalized.Limit))
	values.Set("direction", normalized.Direction)
	values.Set("start", strconv.FormatInt(normalized.Start.UTC().UnixNano(), 10))
	values.Set("end", strconv.FormatInt(normalized.End.UTC().UnixNano(), 10))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/loki/api/v1/query_range?"+values.Encode(), nil)
	if err != nil {
		return result, fmt.Errorf("create Loki request: %w", err)
	}
	if s.tenantID != "" {
		req.Header.Set("X-Scope-OrgID", s.tenantID)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return result, fmt.Errorf("query Loki: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			closeErr = fmt.Errorf("close Loki response body: %w", closeErr)
			if err != nil {
				err = errors.Join(err, closeErr)
				return
			}
			err = closeErr
		}
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := fmt.Sprintf("query Loki failed with status %d", resp.StatusCode)
		if body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096)); readErr == nil {
			if trimmed := strings.TrimSpace(string(body)); trimmed != "" {
				message = fmt.Sprintf("%s: %s", message, trimmed)
			}
		}
		return result, &BackendError{
			Query:      logQL,
			StatusCode: resp.StatusCode,
			Message:    message,
		}
	}

	var payload struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Stream map[string]string `json:"stream"`
				Values [][2]string       `json:"values"`
			} `json:"result"`
		} `json:"data"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return result, fmt.Errorf("decode Loki response: %w", err)
	}
	if payload.Status != "success" {
		if payload.Error != "" {
			return result, &BackendError{
				Query:   logQL,
				Message: fmt.Sprintf("Loki query failed: %s", payload.Error),
			}
		}
		return result, &BackendError{
			Query:   logQL,
			Message: fmt.Sprintf("Loki query returned status %q", payload.Status),
		}
	}
	if payload.Data.ResultType != "streams" {
		return result, fmt.Errorf("unsupported Loki result type %q", payload.Data.ResultType)
	}

	items := make([]LogRecord, 0, normalized.Limit)
	for _, stream := range payload.Data.Result {
		for _, pair := range stream.Values {
			timestamp, err := parseLokiTimestamp(pair[0])
			if err != nil {
				return result, fmt.Errorf("parse Loki timestamp %q: %w", pair[0], err)
			}
			items = append(items, LogRecord{
				Timestamp: timestamp,
				Labels:    cloneLabels(stream.Stream),
				Line:      pair[1],
			})
		}
	}

	sort.Slice(items, func(i, j int) bool {
		if normalized.Direction == "forward" {
			return items[i].Timestamp.Before(items[j].Timestamp)
		}
		return items[i].Timestamp.After(items[j].Timestamp)
	})
	if len(items) > normalized.Limit {
		items = items[:normalized.Limit]
	}

	result.Items = items
	return result, nil
}

func normalizeInput(input QueryInput) (QueryInput, error) {
	out := input
	if out.Limit <= 0 {
		out.Limit = 200
	}
	if out.Limit > 2000 {
		out.Limit = 2000
	}
	if out.Direction == "" {
		out.Direction = "backward"
	}
	if out.Direction != "backward" && out.Direction != "forward" {
		return QueryInput{}, &InputError{Message: "direction must be backward or forward"}
	}

	end := out.End.UTC()
	if end.IsZero() {
		end = time.Now().UTC()
	}
	start := out.Start.UTC()
	if start.IsZero() {
		start = end.Add(-15 * time.Minute)
	}
	if start.After(end) {
		return QueryInput{}, &InputError{Message: "start must not be after end"}
	}
	out.Start = start
	out.End = end
	return out, nil
}

func buildLogQL(filters Filters) string {
	selectorLabels := map[string]string{
		"job": "mini-cloud",
	}
	if component := strings.TrimSpace(filters.Component); component != "" {
		selectorLabels["component"] = component
	}

	var builder strings.Builder
	builder.WriteString("{")
	keys := make([]string, 0, len(selectorLabels))
	for key := range selectorLabels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for i, key := range keys {
		if i > 0 {
			builder.WriteString(",")
		}
		builder.WriteString(key)
		builder.WriteString("=")
		builder.WriteString(strconv.Quote(selectorLabels[key]))
	}
	builder.WriteString("}")

	if contains := strings.TrimSpace(filters.Contains); contains != "" {
		builder.WriteString(" |= ")
		builder.WriteString(strconv.Quote(contains))
	}

	builder.WriteString(" | logfmt")
	appendParsedFilter(&builder, "platform_name", filters.PlatformName)
	appendParsedFilter(&builder, "plane_id", filters.PlaneID)
	appendParsedFilter(&builder, "service_id", filters.ServiceID)
	appendParsedFilter(&builder, "deployment_id", filters.DeploymentID)
	appendParsedFilter(&builder, "node_id", filters.NodeID)
	appendParsedFilter(&builder, "runtime_node_id", filters.RuntimeNodeID)
	appendParsedFilter(&builder, "execution_id", filters.ExecutionID)
	appendParsedFilter(&builder, "request_id", filters.RequestID)
	appendParsedFilter(&builder, "level", strings.ToUpper(strings.TrimSpace(filters.Level)))
	return builder.String()
}

func appendParsedFilter(builder *strings.Builder, key string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	builder.WriteString(" | ")
	builder.WriteString(key)
	builder.WriteString("=")
	builder.WriteString(strconv.Quote(value))
}

func parseLokiTimestamp(value string) (time.Time, error) {
	nanos, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, nanos).UTC(), nil
}

func cloneLabels(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
