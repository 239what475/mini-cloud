// Package id 提供 mini-cloud 内部资源 ID 生成能力。
package id

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// New 生成带业务前缀的短随机 ID。
// 参数说明：prefix 是 ID 的可读前缀。
func New(prefix string) (string, error) {
	// 6 字节随机数编码为 12 个十六进制字符，降低同前缀 ID 冲突概率。
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	// 前缀和随机后缀用下划线分隔，方便人工排查数据。
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(raw)), nil
}
