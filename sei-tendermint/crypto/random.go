package crypto

import (
	"crypto/rand"
	"encoding/hex"
	"io"
)

// This only uses the OS's randomness
func CRandBytes(numBytes int) []byte {
	b := make([]byte, numBytes)
	_, err := rand.Read(b)
	if err != nil {
		panic(err)
	}
	return b
}

func CRandHex(numDigits int) string {
	return hex.EncodeToString(CRandBytes(numDigits / 2))
}

// Returns a crand.Reader.
func CReader() io.Reader {
	return rand.Reader
}
