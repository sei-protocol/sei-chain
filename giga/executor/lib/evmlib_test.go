package lib

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLibSHA256MatchesCheckedInLibrary(t *testing.T) {
	_, srcFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "failed to determine source path")

	libPath := filepath.Join(filepath.Dir(srcFile), libName)
	f, err := os.Open(libPath) //nolint:gosec // test reads a fixed, in-tree path
	require.NoError(t, err, "open %q: %v", libPath, err)
	defer func() { require.NoError(t, f.Close()) }()

	h := sha256.New()
	_, err = io.Copy(h, f)
	require.NoError(t, err, "hash %q: %v", libPath, err)

	got := hex.EncodeToString(h.Sum(nil))
	require.Equal(t, libSHA256, got, "checked-in %s digest = %s, want %s", libName, got, libSHA256)
}
