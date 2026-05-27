package runtime

import "sort"

// sortedEnvKeys 返回环境变量 map 的稳定排序 key 列表。
func sortedEnvKeys(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
