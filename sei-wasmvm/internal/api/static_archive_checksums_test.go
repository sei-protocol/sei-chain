package api

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestStaticArchiveChecksums asserts that the static libwasmvm archives checked into
// this package match their expected SHA256 sums.
//
// These are the archives the cgo linker resolves the link_*.go directives against.
// Pinning the hashes rejects a replacement archive built for the wrong architecture,
// which otherwise surfaces only as a link failure on whichever platform stops
// matching. Update the values here after a deliberate rebuild, once the provenance
// of the new archives is confirmed.
func TestStaticArchiveChecksums(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
	}{
		{
			name: "libwasmvm_muslc.a",
			want: "2c2d90423e5bebba911b0be7963b09062adb555ecd6149f7cc759a494bb2eda6",
		},
		{
			name: "libwasmvm_muslc.aarch64.a",
			want: "409cd9da695d4f1989271230094563c38aa2d867b26908b2ca2082a0d71e3b8e",
		},
		{
			name: "libwasmvmstatic_darwin.a",
			want: "713d221d712bee043794fc600e093b62e9e878b312f044f548a8852b670451c8",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, archiveSHA256(t, tc.name))
		})
	}
}

// archiveSHA256 returns the hex-encoded SHA256 of the file at path.
func archiveSHA256(t *testing.T, path string) string {
	t.Helper()

	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	h := sha256.New()
	_, err = io.Copy(h, f)
	require.NoError(t, err)
	return hex.EncodeToString(h.Sum(nil))
}
