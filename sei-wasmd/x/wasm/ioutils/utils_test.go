package ioutils

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func GetTestData() ([]byte, []byte, []byte, error) {
	wasmCode, err := os.ReadFile("../keeper/testdata/hackatom.wasm")
	if err != nil {
		return nil, nil, nil, err
	}

	gzipData, err := GzipIt(wasmCode)
	if err != nil {
		return nil, nil, nil, err
	}

	someRandomStr := []byte("hello world")

	return wasmCode, someRandomStr, gzipData, nil
}

func TestIsWasm(t *testing.T) {
	wasmCode, someRandomStr, gzipData, err := GetTestData()
	require.NoError(t, err)

	t.Log("should return false for some random string data")
	require.False(t, IsWasm(someRandomStr))
	t.Log("should return false for gzip data")
	require.False(t, IsWasm(gzipData))
	t.Log("should return true for exact wasm")
	require.True(t, IsWasm(wasmCode))
}

func TestIsGzip(t *testing.T) {
	wasmCode, someRandomStr, gzipData, err := GetTestData()
	require.NoError(t, err)

	require.False(t, IsGzip(wasmCode))
	require.False(t, IsGzip(someRandomStr))
	require.True(t, IsGzip(gzipData))
}

func TestGzipIt(t *testing.T) {
	wasmCode, someRandomStr, _, err := GetTestData()
	require.NoError(t, err)

	t.Log("gzip wasm with no error")
	_, err = GzipIt(wasmCode)
	require.NoError(t, err)

	t.Log("gzip of a string should round-trip")
	strToGzip, err := GzipIt(someRandomStr)
	require.NoError(t, err)
	require.True(t, IsGzip(strToGzip))

	uncompressed, err := Uncompress(strToGzip, uint64(len(strToGzip)*2))
	require.NoError(t, err)
	require.Equal(t, someRandomStr, uncompressed)
}
