package crypto

import (
	"crypto/rand"
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

// Returns a crand.Reader.
func CReader() io.Reader {
	return rand.Reader
}
