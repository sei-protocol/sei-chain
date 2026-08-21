package wal

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/wal"
)

func TestVerifyIntactAcceptsCompleteChangelog(t *testing.T) {
	dir := writeTestSegment(t, appendBinaryEntry(appendBinaryEntry(nil, []byte("one")), []byte("two")))
	before := snapshotDir(t, dir)

	require.NoError(t, VerifyIntact(dir))
	require.Equal(t, before, snapshotDir(t, dir))
}

func TestVerifyIntactRejectsTornTail(t *testing.T) {
	complete := appendBinaryEntry(nil, []byte("complete"))
	torn := appendBinaryEntry(complete, []byte("torn"))
	dir := writeTestSegment(t, torn[:len(torn)-1])
	before := snapshotDir(t, dir)

	err := VerifyIntact(dir)
	require.ErrorIs(t, err, ErrCorrupt)
	require.ErrorIs(t, err, wal.ErrCorrupt)
	require.Contains(t, err.Error(), "ends mid-record")
	require.Equal(t, before, snapshotDir(t, dir))
}

func TestVerifyIntactRejectsInterruptedTruncation(t *testing.T) {
	dir := writeTestSegment(t, appendBinaryEntry(nil, []byte("complete")))
	marker := filepath.Join(dir, "00000000000000000002.START")
	require.NoError(t, os.WriteFile(marker, appendBinaryEntry(nil, []byte("moved")), 0o600))
	before := snapshotDir(t, dir)

	err := VerifyIntact(dir)
	require.ErrorIs(t, err, ErrCorrupt)
	require.Contains(t, err.Error(), "truncation marker")
	require.Equal(t, before, snapshotDir(t, dir))
}

func TestVerifyIntactAcceptsMissingOrEmptyChangelog(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "changelog")
	require.NoError(t, VerifyIntact(missing))
	require.NoFileExists(t, missing)

	require.NoError(t, VerifyIntact(t.TempDir()))
}

func TestVerifyIntactAcceptsAChangelogTheWritableOpenerWrote(t *testing.T) {
	dir := t.TempDir()
	log, err := open(dir, nil)
	require.NoError(t, err)
	require.NoError(t, log.Write(1, []byte("entry")))
	require.NoError(t, log.Close())

	require.NoError(t, VerifyIntact(dir))
}

// TestVerifyIntactRejectsWhatTheWritableOpenerTruncates pins the reason the
// check exists: the same tail it rejects is one the writable opener repairs in
// place.
func TestVerifyIntactRejectsWhatTheWritableOpenerTruncates(t *testing.T) {
	dir := t.TempDir()
	log, err := open(dir, nil)
	require.NoError(t, err)
	require.NoError(t, log.Write(1, []byte("entry")))
	require.NoError(t, log.Close())

	segment := filepath.Join(dir, "00000000000000000001")
	data, err := os.ReadFile(filepath.Clean(segment))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(segment, append(data, 0x08), 0o600))
	before := snapshotDir(t, dir)

	require.ErrorIs(t, VerifyIntact(dir), ErrCorrupt)
	require.Equal(t, before, snapshotDir(t, dir))

	log, err = open(dir, nil)
	require.NoError(t, err)
	require.NoError(t, log.Close())
	require.NotEqual(t, before, snapshotDir(t, dir))
}

// writeTestSegment creates a changelog directory holding data as its only
// segment, and returns the directory.
func writeTestSegment(t *testing.T, data []byte) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "00000000000000000001"), data, 0o600))
	return dir
}

// snapshotDir returns the contents of every file in dir, keyed by name.
func snapshotDir(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	files := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, readErr := os.ReadFile(filepath.Clean(filepath.Join(dir, entry.Name())))
		require.NoError(t, readErr)
		files[entry.Name()] = data
	}
	return files
}

// appendBinaryEntry appends payload to data in the binary changelog framing of
// a size varint followed by the payload.
func appendBinaryEntry(data []byte, payload []byte) []byte {
	var size [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(size[:], uint64(len(payload)))
	data = append(data, size[:n]...)
	return append(data, payload...)
}
