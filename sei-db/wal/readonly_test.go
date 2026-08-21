package wal

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	tidwallwal "github.com/tidwall/wal"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

func TestOpenReadOnlyChangelogWALReplaysWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	writable, err := NewChangelogWAL(dir, Config{})
	require.NoError(t, err)
	writeReadOnlyTestData(t, writable)
	require.NoError(t, writable.Close())

	before := snapshotWALFiles(t, dir)
	readOnly, err := OpenReadOnlyChangelogWAL(dir)
	require.NoError(t, err)

	first, err := readOnly.FirstOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(1), first)
	last, err := readOnly.LastOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(3), last)

	var names []string
	require.NoError(t, readOnly.Replay(first, last, func(_ uint64, entry proto.ChangelogEntry) error {
		names = append(names, entry.Changesets[0].Name)
		return nil
	}))
	require.Equal(t, []string{"test", "test", "test"}, names)
	require.Equal(t, before, snapshotWALFiles(t, dir))

	require.ErrorIs(t, readOnly.Write(proto.ChangelogEntry{}), ErrReadOnly)
	require.ErrorIs(t, readOnly.TruncateBefore(2), ErrReadOnly)
	require.ErrorIs(t, readOnly.TruncateAfter(2), ErrReadOnly)
	require.NoError(t, readOnly.Close())
	require.NoError(t, readOnly.Close())
}

func TestOpenReadOnlyChangelogWALRejectsTornTailWithoutRepair(t *testing.T) {
	dir := t.TempDir()
	writable, err := NewChangelogWAL(dir, Config{})
	require.NoError(t, err)
	writeReadOnlyTestData(t, writable)
	require.NoError(t, writable.Close())

	segment := lastPlainWALSegment(t, dir)
	file, err := os.OpenFile(filepath.Clean(segment), os.O_WRONLY|os.O_APPEND, 0)
	require.NoError(t, err)
	_, err = file.Write([]byte{0x10}) // declares a 16-byte record whose payload has not arrived
	require.NoError(t, err)
	require.NoError(t, file.Close())
	before := snapshotWALFiles(t, dir)

	_, err = OpenReadOnlyChangelogWAL(dir)
	require.ErrorIs(t, err, tidwallwal.ErrCorrupt)
	require.Equal(t, before, snapshotWALFiles(t, dir), "read-only open must not repair the source tail")
}

func TestOpenReadOnlyChangelogWALKeepsPointInTimeView(t *testing.T) {
	dir := t.TempDir()
	writable, err := NewChangelogWAL(dir, Config{})
	require.NoError(t, err)
	require.NoError(t, writable.Write(proto.ChangelogEntry{Version: 1}))

	readOnly, err := OpenReadOnlyChangelogWAL(dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, readOnly.Close()) })

	require.NoError(t, writable.Write(proto.ChangelogEntry{Version: 2}))
	require.NoError(t, writable.Close())

	last, err := readOnly.LastOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(1), last)
	_, err = readOnly.ReadAt(2)
	require.Error(t, err)
}

func TestOpenReadOnlyChangelogWALRejectsRecoveryMarkers(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "00000000000000000001.START")
	require.NoError(t, os.WriteFile(marker, nil, 0o600))

	_, err := OpenReadOnlyChangelogWAL(dir)
	require.ErrorIs(t, err, tidwallwal.ErrCorrupt)
	require.FileExists(t, marker, "read-only open must not complete writable WAL recovery")
}

func TestOpenReadOnlyChangelogWALDoesNotCreateMissingDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")

	_, err := OpenReadOnlyChangelogWAL(dir)
	require.Error(t, err)
	require.NoDirExists(t, dir)
}

func lastPlainWALSegment(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && len(entry.Name()) == 20 {
			names = append(names, entry.Name())
		}
	}
	require.NotEmpty(t, names)
	sort.Strings(names)
	return filepath.Join(dir, names[len(names)-1])
}

func snapshotWALFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	files := make(map[string][]byte)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Clean(filepath.Join(dir, entry.Name())))
		require.NoError(t, err)
		files[entry.Name()] = data
	}
	return files
}

func writeReadOnlyTestData(t *testing.T, changelog ChangelogWAL) {
	t.Helper()
	for i, changeset := range ChangeSets {
		require.NoError(t, changelog.Write(proto.ChangelogEntry{
			Version: int64(i + 1),
			Changesets: []*proto.NamedChangeSet{{
				Name:      "test",
				Changeset: changeset,
			}},
		}))
	}
}
