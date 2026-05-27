package httpx

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"mini-cloud/internal/common/logquery"
)

func ParseLogQueryInput(r *http.Request, forced logquery.Filters) (logquery.QueryInput, error) {
	query := r.URL.Query()
	end, err := parseOptionalTime(query.Get("end"))
	if err != nil {
		return logquery.QueryInput{}, err
	}
	if end.IsZero() {
		end = time.Now().UTC()
	}
	start, err := parseOptionalTime(query.Get("start"))
	if err != nil {
		return logquery.QueryInput{}, err
	}
	if !start.IsZero() && !end.IsZero() && start.After(end) {
		return logquery.QueryInput{}, errors.New("start must not be after end")
	}
	if start.IsZero() {
		since, err := parseSinceDuration(query.Get("since"))
		if err != nil {
			return logquery.QueryInput{}, err
		}
		start = end.Add(-since)
	}

	limit := 200
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return logquery.QueryInput{}, errors.New("limit must be a positive integer")
		}
		if parsed > 2000 {
			return logquery.QueryInput{}, errors.New("limit must not exceed 2000")
		}
		limit = parsed
	}

	direction := strings.TrimSpace(query.Get("direction"))
	if direction == "" {
		direction = "backward"
	}
	if direction != "backward" && direction != "forward" {
		return logquery.QueryInput{}, errors.New("direction must be backward or forward")
	}

	filters := logquery.Filters{
		Component:     firstNonEmpty(forced.Component, query.Get("component")),
		PlatformName:  firstNonEmpty(forced.PlatformName, query.Get("platformName")),
		PlaneID:       firstNonEmpty(forced.PlaneID, query.Get("planeID")),
		ProjectID:     firstNonEmpty(forced.ProjectID, query.Get("projectID")),
		ServiceID:     firstNonEmpty(forced.ServiceID, query.Get("serviceID")),
		DeploymentID:  firstNonEmpty(forced.DeploymentID, query.Get("deploymentID")),
		NodeID:        firstNonEmpty(forced.NodeID, query.Get("nodeID")),
		RuntimeNodeID: firstNonEmpty(forced.RuntimeNodeID, query.Get("runtimeNodeID")),
		ExecutionID:   firstNonEmpty(forced.ExecutionID, query.Get("executionID")),
		RequestID:     firstNonEmpty(forced.RequestID, query.Get("requestID")),
		Level:         firstNonEmpty(forced.Level, query.Get("level")),
		Contains:      firstNonEmpty(forced.Contains, query.Get("contains")),
	}

	return logquery.QueryInput{
		Start:     start,
		End:       end,
		Limit:     limit,
		Direction: direction,
		Filters:   filters,
	}, nil
}

func parseOptionalTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, errors.New("time parameters must use RFC3339")
	}
	return parsed.UTC(), nil
}

func parseSinceDuration(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 15 * time.Minute, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, errors.New("since must be a positive duration such as 15m or 1h")
	}
	return duration, nil
}

func firstNonEmpty(primary string, fallback string) string {
	if value := strings.TrimSpace(primary); value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}
