package utils

import (
	crand "crypto/rand"
	"crypto/sha256"
	"io"
)

func Sha256(bytes []byte) []byte {
	hasher := sha256.New()
	hasher.Write(bytes)
	return hasher.Sum(nil)
}

func CReader() io.Reader {
	return crand.Reader
}
