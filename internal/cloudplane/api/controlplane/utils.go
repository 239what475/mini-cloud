package controlplane

import (
	"maps"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// ProtoTimestamp 将 Go time 转为 protobuf Timestamp。
// 参数说明：value 是领域或 contract 视图中的时间值。
func ProtoTimestamp(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value.UTC())
}

// CopyStringMap 复制 string map，避免共享可变引用。
// 参数说明：input 是待复制的字符串键值映射。
func CopyStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	maps.Copy(out, input)
	return out
}
