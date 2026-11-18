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
			want: "fd62da57153d3200140fe07d3c493db06f6c83bf878ef70537a96a8facb753f9",
		},
		{
			name: "libwasmvm_muslc.aarch64.a",
			want: "3079f06a24769c5060dcee3d11d03aedc613fd426a440cde422f83e4c8f17fc5",
		},
		{
			name: "libwasmvmstatic_darwin.a",
			want: "00f891847079544c4e5019da8f60045110ae281ec67bba76a0e50f53229e941b",
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
