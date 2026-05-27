package lifecycle

import (
	"maps"
	"strings"
)

// copyStringMap 复制输入数据，避免调用方和内部状态共享可变引用。
// 参数说明：values 表示值集合。
func copyStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(values))
	maps.Copy(out, values)
	return out
}

// fallbackString 在首选值为空时返回备用值。
// 参数说明：preferred 是优先使用的字符串；fallback 是 preferred 为空时的备用值。
func fallbackString(preferred string, fallback string) string {
	if strings.TrimSpace(preferred) != "" {
		return preferred
	}
	return fallback
}
