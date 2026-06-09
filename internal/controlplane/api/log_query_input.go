package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"mini-cloud/internal/controlplane/logquery"
)

func parseLogQueryInput(r *http.Request) (logquery.QueryInput, error) {
	query := r.URL.Query()
	end, err := parseOptionalLogQueryTime(query.Get("end"))
	if err != nil {
		return logquery.QueryInput{}, err
	}
	if end.IsZero() {
		end = time.Now().UTC()
	}
	start, err := parseOptionalLogQueryTime(query.Get("start"))
	if err != nil {
		return logquery.QueryInput{}, err
	}
	if !start.IsZero() && start.After(end) {
		return logquery.QueryInput{}, errors.New("start must not be after end")
	}
	if start.IsZero() {
		since, err := parseLogQuerySince(query.Get("since"))
		if err != nil {
			return logquery.QueryInput{}, err
		}
		start = end.Add(-since)
	}

	return logquery.QueryInput{
		Start: start,
		End:   end,
		Filters: logquery.Filters{
			Component:    strings.TrimSpace(query.Get("component")),
			PlatformName: strings.TrimSpace(query.Get("platformName")),
			PlaneID:      strings.TrimSpace(query.Get("planeID")),
			ServiceID:    strings.TrimSpace(query.Get("serviceID")),
			NodeID:       strings.TrimSpace(query.Get("nodeID")),
			ExecutionID:  strings.TrimSpace(query.Get("executionID")),
			RequestID:    strings.TrimSpace(query.Get("requestID")),
			Level:        strings.TrimSpace(query.Get("level")),
			Contains:     strings.TrimSpace(query.Get("contains")),
		},
	}, nil
}

func parseOptionalLogQueryTime(value string) (time.Time, error) {
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

func parseLogQuerySince(value string) (time.Duration, error) {
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
