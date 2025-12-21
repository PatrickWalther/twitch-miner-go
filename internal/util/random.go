package util

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// RandomHex generates a random hex string of the specified byte length.
// The returned string will be 2*bytes characters long.
func RandomHex(bytes int) string {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return strings.Repeat("0", bytes*2)
	}
	return hex.EncodeToString(b)
}

// DeviceID generates a random 32-character device ID (16 bytes as hex)
func DeviceID() string {
	return RandomHex(16)
}
