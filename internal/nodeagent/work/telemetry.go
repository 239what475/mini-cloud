package work

import (
	"sort"
	"strings"

	"mini-cloud/internal/contract/nodeagentapi"
)

// telemetryEnvOptions 配置注入到工作负载环境变量中的遥测元数据。
type telemetryEnvOptions struct {
	// PlatformName 是写入 OTEL_RESOURCE_ATTRIBUTES 的平台名称。
	PlatformName string
	// WorkloadOTLPEndpoint 是写入 OTEL_EXPORTER_OTLP_ENDPOINT 的 OTLP HTTP endpoint。
	WorkloadOTLPEndpoint string
}

// injectTelemetryEnv 按需注入 OpenTelemetry exporter 默认配置，并写入 mini-cloud 资源属性。
func injectTelemetryEnv(base map[string]string, item *nodeagentapi.WorkItem, opts telemetryEnvOptions) map[string]string {
	env := cloneStringMap(base)
	if env == nil {
		env = map[string]string{}
	}
	if item == nil {
		return env
	}

	endpoint := strings.TrimSpace(opts.WorkloadOTLPEndpoint)
	if endpoint != "" {
		env["OTEL_EXPORTER_OTLP_ENDPOINT"] = endpoint
		env["OTEL_EXPORTER_OTLP_PROTOCOL"] = "http/protobuf"
	}
	if strings.TrimSpace(env["OTEL_SERVICE_NAME"]) == "" && strings.TrimSpace(item.ServiceName) != "" {
		env["OTEL_SERVICE_NAME"] = item.ServiceName
	}
	reserved := map[string]string{
		"mini_cloud.service_id":   sanitizeOTelResourceValue(item.ServiceID),
		"mini_cloud.plan_id":      sanitizeOTelResourceValue(item.PlanID),
		"mini_cloud.execution_id": sanitizeOTelResourceValue(item.ExecutionID),
	}
	if strings.TrimSpace(opts.PlatformName) != "" {
		reserved["mini_cloud.platform_name"] = sanitizeOTelResourceValue(opts.PlatformName)
	}
	env["OTEL_RESOURCE_ATTRIBUTES"] = mergeOTelResourceAttributes(env["OTEL_RESOURCE_ATTRIBUTES"], reserved)
	return env
}

// sanitizeOTelResourceValue 清理 OTEL resource attribute 值，空值替换为 unknown。
func sanitizeOTelResourceValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	replacer := strings.NewReplacer(",", "_", "=", "_")
	return replacer.Replace(value)
}

// mergeOTelResourceAttributes 合并已有资源属性和平台保留资源属性，保留属性覆盖同名已有值。
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

// parseOTelResourceAttributes 解析 OTEL_RESOURCE_ATTRIBUTES 字符串为键值映射。
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

// splitOTelResourceAttributes 按未转义逗号拆分 OTEL resource attribute 字符串。
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

// cutUnescapedOTelAttribute 按第一个未转义等号拆分单个 resource attribute。
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

// unescapeOTelResourceAttribute 还原 resource attribute 中的反斜杠转义。
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

// escapeOTelResourceAttribute 转义 resource attribute 中的反斜杠、逗号和等号。
func escapeOTelResourceAttribute(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, ",", `\,`, "=", `\=`)
	return replacer.Replace(value)
}

// cloneStringMap 复制字符串 map，空输入返回空 map。
func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
