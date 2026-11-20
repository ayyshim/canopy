package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// NewToken returns a random 32-byte URL-safe token and the SHA-256 hash to store.
func NewToken() (raw string, hash []byte) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, HashToken(raw)
}

func HashToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}
