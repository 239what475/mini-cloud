package store

import "maps"

// mergeStringMaps 合并多个 string map，后者覆盖前者。
// 参数说明：items 是当前处理的对象集合。
func mergeStringMaps(items ...map[string]string) map[string]string {
	// 始终返回新 map，避免调用方修改原始输入。
	out := make(map[string]string)
	for _, item := range items {
		// 后面的 map 会覆盖前面 map 中相同 key 的值。
		maps.Copy(out, item)
	}
	return out
}
