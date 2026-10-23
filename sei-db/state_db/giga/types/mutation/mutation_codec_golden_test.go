package mutation

import (
	"encoding/hex"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// mutationGoldenRecord regenerates the committed golden file instead of checking against it. Off by default, and
// refused outright on CI: see recordMutationGolden().
var mutationGoldenRecord = flag.Bool("mutation-golden-record", false,
	"rewrite the committed mutation golden file from this build instead of verifying against it")

// mutationGoldenFileV0 holds the version 0 encoding of goldenMutations(), hex encoded.
const mutationGoldenFileV0 = "testdata/mutations_v0.hex"

// goldenMutations returns the mutations pinned by the golden file. Used only by the golden test, so that editing
// another test's fixtures cannot move the recorded bytes.
func goldenMutations() []Mutation {
	return []Mutation{
		{Key: []byte("insert"), NewValue: []byte("new")},
		{Key: []byte("update"), PreviousValue: []byte("old"), NewValue: []byte("new")},
		{Key: []byte("delete"), PreviousValue: []byte("old")},
		{Key: []byte("delete-absent")},
		{Key: []byte{}, PreviousValue: []byte{}, NewValue: []byte{}},
		{Key: []byte{0x00, 0xff, 0x80}, PreviousValue: []byte{0xde, 0xad}, NewValue: []byte{0xbe, 0xef, 0x00}},
	}
}

// TestMutationsGoldenFile_V0 requires this build to produce the committed version 0 bytes, and to decode them back
// into the mutations they were recorded from.
func TestMutationsGoldenFile_V0(t *testing.T) {
	serialized := SerializeMutations(goldenMutations())

	if *mutationGoldenRecord {
		recordMutationGolden(t, serialized)
		return
	}

	data, err := os.ReadFile(mutationGoldenFileV0)
	require.NoError(t, err, "read %s; record it with -mutation-golden-record", mutationGoldenFileV0)
	want, err := hex.DecodeString(strings.TrimSpace(string(data)))
	require.NoError(t, err)

	require.Equal(t, want, serialized, "serialization differs from %s; if the change is intended, "+
		"re-record with -mutation-golden-record and review the diff", mutationGoldenFileV0)

	decoded, err := DeserializeMutations(want)
	require.NoError(t, err)
	require.Equal(t, goldenMutations(), decoded)
}

// recordMutationGolden replaces the committed golden file with serialized.
//
// It refuses to run on CI. The recorded bytes are the only statement of what the format is expected to be, so a
// build that regenerated them as part of an ordinary test run would pass by agreeing with itself.
func recordMutationGolden(t *testing.T, serialized []byte) {
	t.Helper()
	if os.Getenv("CI") != "" {
		t.Fatal("refusing to re-record the mutation golden file on CI: " +
			"the recorded bytes are the expected values, so a build that rewrites them verifies nothing")
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(mutationGoldenFileV0), 0o750))
	require.NoError(t, os.WriteFile(mutationGoldenFileV0, []byte(hex.EncodeToString(serialized)+"\n"), 0o600))
	t.Logf("recorded %d mutations into %s — review the diff before committing",
		len(goldenMutations()), mutationGoldenFileV0)
}
