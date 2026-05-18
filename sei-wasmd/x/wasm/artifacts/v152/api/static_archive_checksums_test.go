package api

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestStaticArchiveChecksums asserts that the libwasmvm152 musl static archives
// checked into this package match the SHA256 sums published with the sei-wasmd
// v0.3.6 release:
//
//	https://github.com/sei-protocol/sei-wasmd/releases/tag/v0.3.6
//
// These are the archives the cgo linker resolves `-lwasmvm152_muslc` against
// when building seid with the `muslc` build tag (see link_muslc.go). Locking
// the hashes here prevents accidental replacement with a wrong-arch artefact
// (the previous failure mode: a Mach-O macOS archive was checked in under a
// generic name, causing static linux builds to fail with
// `cannot find -lwasmvm152_muslc`).
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
			name: "libwasmvm152_muslc.a",
			want: "ef197c577318dfe3a148f602fc663e1a168a9d86eb56cba73d06de1717d69c05",
		},
		{
			name: "libwasmvm152_muslc.aarch64.a",
			want: "100053516d512d40c2f9c3a392693efd28c2c4f57a78c6c18aff67613bda94b9",
		},
		{
			name: "libwasmvm152static_darwin.a",
			want: "06e7b039d47bb103b0363aa2032f1e6f566a6c92dd59c8866f470999bd32e595",
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
