package httpx

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"mini-cloud/internal/common/logquery"
)

func ParseLogQueryInput(r *http.Request) (logquery.QueryInput, error) {
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
		Component:    strings.TrimSpace(query.Get("component")),
		PlatformName: strings.TrimSpace(query.Get("platformName")),
		PlaneID:      strings.TrimSpace(query.Get("planeID")),
		ServiceID:    strings.TrimSpace(query.Get("serviceID")),
		DeploymentID: strings.TrimSpace(query.Get("deploymentID")),
		NodeID:       strings.TrimSpace(query.Get("nodeID")),
		ExecutionID:  strings.TrimSpace(query.Get("executionID")),
		RequestID:    strings.TrimSpace(query.Get("requestID")),
		Level:        strings.TrimSpace(query.Get("level")),
		Contains:     strings.TrimSpace(query.Get("contains")),
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
