package api

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestStaticArchiveChecksums asserts that the libwasmvm155 musl static archives
// checked into this package match the SHA256 sums published with the sei-wasmd
// v0.3.6 release:
//
//	https://github.com/sei-protocol/sei-wasmd/releases/tag/v0.3.6
//
// These are the archives the cgo linker resolves `-lwasmvm155_muslc` against
// when building seid with the `muslc` build tag (see link_muslc.go). Locking
// the hashes here prevents accidental replacement with a wrong-arch artefact
// (the previous failure mode: a Mach-O macOS archive was checked in under a
// generic name, causing static linux builds to fail with
// `cannot find -lwasmvm155_muslc`).
//
// If this test fails after a deliberate rebuild or version bump, verify the
// provenance of the new archives (signed release tag, audit log) and then
// update the expected hashes below.
func TestStaticArchiveChecksums(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
	}{
		{
			name: "libwasmvm155_muslc.a",
			want: "ef3fe84ba47f63384beb48c929531d3f4550fce3aea0ac676cbac915a51b202f",
		},
		{
			name: "libwasmvm155_muslc.aarch64.a",
			want: "7fd5118234735e5fef148c860979fd7668c7d000177cc0144f6cb07e397ec387",
		},
		{
			name: "libwasmvm155static_darwin.a",
			want: "afcb224c05cbfbf7c2de5d7cc03ab47d20c4282f0008f64ef6b8698faf5ee16f",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := sha256Sum(t, tc.name)
			require.Equal(t, tc.want, got)
		})
	}
}

func sha256Sum(t *testing.T, path string) string {
	t.Helper()

	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	h := sha256.New()
	_, err = io.Copy(h, f)
	require.NoError(t, err)
	return hex.EncodeToString(h.Sum(nil))
}
