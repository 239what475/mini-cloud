// Package token 提供敏感令牌的生成、展示前缀和不可逆哈希能力。
package token

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Secret 表示一次生成出的明文令牌及其可持久化派生值。
type Secret struct {
	// Value 是只返回给调用方一次的明文令牌。
	Value string
	// Prefix 是可安全展示和审计的令牌前缀。
	Prefix string
	// Hash 是写入数据库的不可逆 SHA-256 十六进制摘要。
	Hash string
}

// NewSecret 生成带类型前缀的随机令牌，并返回明文、展示前缀和哈希。
// 参数说明：prefix 是令牌类型前缀；randomBytes 是随机字节数。
func NewSecret(prefix string, randomBytes int) (Secret, error) {
	if randomBytes <= 0 {
		return Secret{}, fmt.Errorf("randomBytes must be greater than 0")
	}
	raw := make([]byte, randomBytes)
	if _, err := rand.Read(raw); err != nil {
		return Secret{}, fmt.Errorf("read random bytes for token: %w", err)
	}
	value := prefix + hex.EncodeToString(raw)
	return Secret{
		Value:  value,
		Prefix: Prefix(value),
		Hash:   Hash(value),
	}, nil
}

// Hash 对明文令牌做不可逆 SHA-256 哈希。
// 参数说明：secret 是调用方提交或刚生成的明文令牌。
func Hash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// Prefix 返回可展示的令牌前缀。
// 参数说明：secret 是明文令牌。
func Prefix(secret string) string {
	if len(secret) <= 12 {
		return secret
	}
	return secret[:12]
}
