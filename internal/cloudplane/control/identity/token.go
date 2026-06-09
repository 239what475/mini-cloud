package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

type secretToken struct {
	Value  string
	Prefix string
	Hash   string
}

func newSecretToken(prefix string, randomBytes int) (secretToken, error) {
	if randomBytes <= 0 {
		return secretToken{}, fmt.Errorf("randomBytes must be greater than 0")
	}
	raw := make([]byte, randomBytes)
	if _, err := rand.Read(raw); err != nil {
		return secretToken{}, fmt.Errorf("read random bytes for token: %w", err)
	}
	value := prefix + hex.EncodeToString(raw)
	return secretToken{
		Value:  value,
		Prefix: tokenPrefix(value),
		Hash:   tokenHash(value),
	}, nil
}

func tokenHash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func tokenPrefix(secret string) string {
	if len(secret) <= 12 {
		return secret
	}
	return secret[:12]
}
