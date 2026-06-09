package utils

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ShellQuote 把普通字符串转换成 shell 双引号字面量，返回值包含首尾双引号。
//
// 使用场景只限 node bootstrap 模板里的两类位置：
//  1. shell 变量赋值，例如 AGENT_BINARY_URL={{.AgentBinaryURL}}；
//  2. 未引用 heredoc 中的 YAML 标量，例如 url: {{.ConnectEndpoint}}。
//
// shell 在双引号和未引用 heredoc 中仍会解释反斜杠、双引号、$ 和反引号；
// 因此这里必须给这些字符补反斜杠，避免配置值里的 "$TOKEN"、"$(cmd)" 或反引号被二次展开。
// 换行、回车和 tab 被写成 \n、\r、\t，避免把单个配置值拆成多行脚本。
//
// 注意：这个函数不是通用 shell 命令转义器，只能用于写入“单个字符串值”；
// 不要用它拼接 shell 命令片段、参数列表或已经包含 shell 语法的内容。
// 参数说明：value 是待写入 shell 脚本模板的原始字符串值。
func ShellQuote(value string) string {
	// 返回值必须自带外围双引号，这样模板调用方可以直接写 VAR={{.Value}}。
	var b strings.Builder
	b.WriteByte('"')
	for _, ch := range value {
		switch ch {
		case '\\', '"', '$', '`':
			// 这些字符在 shell 双引号或未引用 heredoc 中有特殊含义，必须转义成普通字符。
			b.WriteByte('\\')
			b.WriteRune(ch)
		case '\n':
			// 控制字符写成反斜杠序列，避免破坏生成脚本的行结构。
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			// 其它字符按原样写入双引号字面量。
			b.WriteRune(ch)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// ShellQuoteItems 批量转换字符串列表，供 bootstrap 模板直接写入 shell/YAML 字符串值。
// 参数说明：items 是待转换的字符串列表。
func ShellQuoteItems(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, ShellQuote(item))
	}
	return out
}

// BuildDockerDaemonJSON 生成 node Docker daemon 配置。
// 参数说明：mirrors 是要写入 registry-mirrors 的镜像源列表。
func BuildDockerDaemonJSON(mirrors []string) (string, error) {
	type dockerDaemonConfig struct {
		RegistryMirrors []string `json:"registry-mirrors,omitempty"`
	}
	cleaned := make([]string, 0, len(mirrors))
	for _, mirror := range mirrors {
		trimmed := strings.TrimSpace(mirror)
		if trimmed == "" {
			continue
		}
		cleaned = append(cleaned, trimmed)
	}
	if len(cleaned) == 0 {
		return "", nil
	}
	data, err := json.MarshalIndent(dockerDaemonConfig{RegistryMirrors: cleaned}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("render docker daemon config: %w", err)
	}
	return string(data) + "\n", nil
}

const (
	TagKeyManagedBy   = "managed-by"
	TagKeyPlatform    = "mini-cloud/platform"
	TagValueManagedBy = "mini-cloud"
)

// BuildOwnershipTags 构建 node 云资源的标准 ownership 标签。
// 参数说明：platformName 是平台名称。
func BuildOwnershipTags(platformName string) map[string]string {
	return map[string]string{
		TagKeyManagedBy: TagValueManagedBy,
		TagKeyPlatform:  strings.TrimSpace(platformName),
	}
}
