package wal

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

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
	require.ErrorIs(t, err, ErrCorrupt)
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
	for _, suffix := range []string{".START", ".END"} {
		t.Run(suffix, func(t *testing.T) {
			dir := t.TempDir()
			marker := filepath.Join(dir, "00000000000000000001"+suffix)
			require.NoError(t, os.WriteFile(marker, nil, 0o600))
			before := snapshotWALFiles(t, dir)

			_, err := OpenReadOnlyChangelogWAL(dir)
			require.ErrorIs(t, err, ErrCorrupt)
			require.Equal(t, before, snapshotWALFiles(t, dir),
				"read-only open must not complete writable WAL recovery")
		})
	}
}

func TestOpenReadOnlyChangelogWALEmptyDirectory(t *testing.T) {
	dir := t.TempDir()

	readOnly, err := OpenReadOnlyChangelogWAL(dir)
	require.NoError(t, err)
	first, err := readOnly.FirstOffset()
	require.NoError(t, err)
	require.Zero(t, first)
	last, err := readOnly.LastOffset()
	require.NoError(t, err)
	require.Zero(t, last)
	require.NoError(t, readOnly.Close())

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries, "read-only open must not create an initial segment")
}

func TestOpenReadOnlyChangelogWALDoesNotCreateMissingDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")

	_, err := OpenReadOnlyChangelogWAL(dir)
	require.Error(t, err)
	require.NoDirExists(t, dir)
}

func TestOpenReadOnlyChangelogWALConcurrentWriter(t *testing.T) {
	dir := t.TempDir()
	writable, err := NewChangelogWAL(dir, Config{})
	require.NoError(t, err)

	const versions = 500
	done := make(chan struct{})
	writerErr := make(chan error, 1)
	go func() {
		var runErr error
		for version := 1; version <= versions && runErr == nil; version++ {
			runErr = writable.Write(proto.ChangelogEntry{Version: int64(version)})
			if runErr != nil || version <= 20 || version%10 != 0 {
				continue
			}
			first, err := writable.FirstOffset()
			if err != nil {
				runErr = err
				break
			}
			keepFrom := uint64(version - 20)
			if keepFrom > first {
				runErr = writable.TruncateBefore(keepFrom)
			}
		}
		if closeErr := writable.Close(); runErr == nil {
			runErr = closeErr
		}
		writerErr <- runErr
		close(done)
	}()

reading:
	for {
		select {
		case <-done:
			break reading
		default:
		}

		readOnly, err := OpenReadOnlyChangelogWAL(dir)
		if err != nil {
			continue // source changed while opening; fail-closed and retry is valid
		}
		first, err := readOnly.FirstOffset()
		require.NoError(t, err)
		last, err := readOnly.LastOffset()
		require.NoError(t, err)
		if first > 0 {
			require.GreaterOrEqual(t, last, first)
			entry, err := readOnly.ReadAt(last)
			require.NoError(t, err)
			require.Equal(t, int64(last), entry.Version)
		}
		require.NoError(t, readOnly.Close())
	}
	require.NoError(t, <-writerErr)

	readOnly, err := OpenReadOnlyChangelogWAL(dir)
	require.NoError(t, err)
	defer func() { require.NoError(t, readOnly.Close()) }()
	last, err := readOnly.LastOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(versions), last)
	entry, err := readOnly.ReadAt(last)
	require.NoError(t, err)
	require.Equal(t, int64(versions), entry.Version)
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
