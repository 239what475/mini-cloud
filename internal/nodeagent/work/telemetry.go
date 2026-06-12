package work

import (
	"sort"
	"strings"

	nodeagentv1 "mini-cloud/internal/gen/proto/minicloud/nodeagent/v1"
)

type telemetryEnvOptions struct {
	PlatformName         string
	WorkloadOTLPEndpoint string
}

func injectTelemetryEnv(base map[string]string, item *nodeagentv1.WorkItem, opts telemetryEnvOptions) map[string]string {
	env := make(map[string]string, len(base)+4)
	for key, value := range base {
		env[key] = value
	}
	if item == nil {
		return env
	}

	endpoint := strings.TrimSpace(opts.WorkloadOTLPEndpoint)
	if endpoint != "" {
		env["OTEL_EXPORTER_OTLP_ENDPOINT"] = endpoint
		env["OTEL_EXPORTER_OTLP_PROTOCOL"] = "http/protobuf"
	}
	if strings.TrimSpace(env["OTEL_SERVICE_NAME"]) == "" && strings.TrimSpace(item.GetServiceName()) != "" {
		env["OTEL_SERVICE_NAME"] = item.GetServiceName()
	}
	reserved := map[string]string{
		"mini_cloud.service_id":   sanitizeOTelResourceValue(item.GetServiceId()),
		"mini_cloud.execution_id": sanitizeOTelResourceValue(item.GetExecutionId()),
	}
	if strings.TrimSpace(opts.PlatformName) != "" {
		reserved["mini_cloud.platform_name"] = sanitizeOTelResourceValue(opts.PlatformName)
	}
	env["OTEL_RESOURCE_ATTRIBUTES"] = mergeOTelResourceAttributes(env["OTEL_RESOURCE_ATTRIBUTES"], reserved)
	return env
}

func sanitizeOTelResourceValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	replacer := strings.NewReplacer(",", "_", "=", "_")
	return replacer.Replace(value)
}

func mergeOTelResourceAttributes(existing string, reserved map[string]string) string {
	values := parseOTelResourceAttributes(existing)
	for key, value := range reserved {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		values[key] = value
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, escapeOTelResourceAttribute(key)+"="+escapeOTelResourceAttribute(values[key]))
	}
	return strings.Join(parts, ",")
}

func parseOTelResourceAttributes(raw string) map[string]string {
	values := map[string]string{}
	for _, part := range splitOTelResourceAttributes(raw) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := cutUnescapedOTelAttribute(part)
		key = strings.TrimSpace(unescapeOTelResourceAttribute(key))
		value = strings.TrimSpace(unescapeOTelResourceAttribute(value))
		if !ok || key == "" || value == "" {
			continue
		}
		values[key] = value
	}
	return values
}

// splitOTelResourceAttributes splits on unescaped commas.
func splitOTelResourceAttributes(raw string) []string {
	parts := make([]string, 0, 4)
	var current strings.Builder
	escaped := false
	for _, r := range raw {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			current.WriteRune(r)
			continue
		}
		if r == ',' {
			parts = append(parts, current.String())
			current.Reset()
			continue
		}
		current.WriteRune(r)
	}
	if current.Len() > 0 || raw != "" {
		parts = append(parts, current.String())
	}
	return parts
}

// cutUnescapedOTelAttribute splits on the first unescaped equals sign.
func cutUnescapedOTelAttribute(part string) (string, string, bool) {
	escaped := false
	for i, r := range part {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '=' {
			return part[:i], part[i+1:], true
		}
	}
	return "", "", false
}

func unescapeOTelResourceAttribute(value string) string {
	var builder strings.Builder
	escaped := false
	for _, r := range value {
		if escaped {
			builder.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		builder.WriteRune(r)
	}
	if escaped {
		builder.WriteRune('\\')
	}
	return builder.String()
}

func escapeOTelResourceAttribute(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, ",", `\,`, "=", `\=`)
	return replacer.Replace(value)
}
