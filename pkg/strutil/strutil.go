package strutil

import (
	"crypto/rand"
	"encoding/hex"
)

// GenerateRandomKey output string of length 32 + prefix
func GenerateRandomKey(prefix string) string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(bytes)
}
