package wal

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/wal"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// writeSegmentedLog writes changelog entries 1..n to a log in dir, one segment per entry.
func writeSegmentedLog(t *testing.T, dir string, n int) {
	t.Helper()
	log, err := wal.Open(dir, &wal.Options{SegmentSize: 1, NoSync: true})
	require.NoError(t, err)
	for i := 1; i <= n; i++ {
		data, err := (&proto.ChangelogEntry{Version: int64(i)}).Marshal()
		require.NoError(t, err)
		require.NoError(t, log.Write(uint64(i), data))
	}
	require.NoError(t, log.Close())
}

// dirState returns the name and contents of every file in dir.
func dirState(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	state := make(map[string]string, len(entries))
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		require.NoError(t, err)
		state[entry.Name()] = string(data)
	}
	return state
}

// tailSegment returns the path of the segment a log in dir appends to.
func tailSegment(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	return filepath.Join(dir, entries[len(entries)-1].Name())
}

func requireOnlyEntry(t *testing.T, root, name string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, name, entries[0].Name())
}

func TestReadOnlyOpenLeavesInterruptedTruncateFront(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "changelog")
	writeSegmentedLog(t, dir, 5)
	// A writer's TruncateFront(3) stopped after writing its START marker,
	// before removing the segments it replaces.
	seg3, err := os.ReadFile(filepath.Join(dir, "00000000000000000003"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "00000000000000000003.START"), seg3, 0o600))
	before := dirState(t, dir)

	changelog, err := NewChangelogWAL(dir, Config{ReadOnly: true})
	require.NoError(t, err)
	first, err := changelog.FirstOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(3), first)
	var versions []int64
	require.NoError(t, changelog.Replay(3, 5, func(_ uint64, entry proto.ChangelogEntry) error {
		versions = append(versions, entry.Version)
		return nil
	}))
	require.Equal(t, []int64{3, 4, 5}, versions)
	require.Equal(t, before, dirState(t, dir))

	require.NoError(t, changelog.Close())
	require.Equal(t, before, dirState(t, dir))
	requireOnlyEntry(t, root, "changelog")
}

func TestReadOnlyOpenLeavesCorruptedTail(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "changelog")
	writeSegmentedLog(t, dir, 3)
	tail, err := os.OpenFile(tailSegment(t, dir), os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	// A length prefix of 16 followed by one byte: a record cut off mid-write.
	_, err = tail.Write([]byte{0x10, 0x01})
	require.NoError(t, err)
	require.NoError(t, tail.Close())
	before := dirState(t, dir)

	changelog, err := NewChangelogWAL(dir, Config{ReadOnly: true})
	require.NoError(t, err)
	last, err := changelog.LastOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(3), last)
	require.NoError(t, changelog.Close())
	require.Equal(t, before, dirState(t, dir))
	requireOnlyEntry(t, root, "changelog")
}

func TestReadOnlyOpenLeavesCorruptedTailBeforeStrayFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "changelog")
	writeSegmentedLog(t, dir, 3)
	tail, err := os.OpenFile(tailSegment(t, dir), os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = tail.Write([]byte{0x10, 0x01})
	require.NoError(t, err)
	require.NoError(t, tail.Close())
	// A file that is not a segment and sorts after the tail.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stray"), []byte("x"), 0o600))
	before := dirState(t, dir)

	changelog, err := NewChangelogWAL(dir, Config{ReadOnly: true})
	require.NoError(t, err)
	require.NoError(t, changelog.Close())
	require.Equal(t, before, dirState(t, dir))
	requireOnlyEntry(t, root, "changelog")
}

func TestReadOnlyRefusesWritesAndTruncations(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "changelog")
	writeSegmentedLog(t, dir, 3)
	before := dirState(t, dir)

	changelog, err := NewChangelogWAL(dir, Config{ReadOnly: true})
	require.NoError(t, err)
	require.ErrorIs(t, changelog.Write(proto.ChangelogEntry{Version: 4}), errReadOnly)
	require.ErrorIs(t, changelog.TruncateBefore(2), errReadOnly)
	require.ErrorIs(t, changelog.TruncateAfter(2), errReadOnly)
	require.NoError(t, changelog.Close())
	require.Equal(t, before, dirState(t, dir))
}

func TestReadOnlyOpenOfMissingDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "changelog")

	changelog, err := NewChangelogWAL(dir, Config{ReadOnly: true})
	require.NoError(t, err)
	last, err := changelog.LastOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(0), last)
	require.NoError(t, changelog.Close())
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestReadOnlyOpenDuringWriterTruncation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "changelog")
	writer, err := NewChangelogWAL(dir, Config{})
	require.NoError(t, err)
	require.NoError(t, writer.Write(proto.ChangelogEntry{Version: 1}))

	var stop atomic.Bool
	var opens, failures atomic.Int64
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				reader, err := NewChangelogWAL(dir, Config{ReadOnly: true, NoRepairOnOpen: true})
				if err != nil {
					// A tail copied mid-append fails the open with wal.ErrCorrupt.
					failures.Add(1)
					continue
				}
				opens.Add(1)
				_ = reader.Close()
			}
		}()
	}

	var writeErr error
	for version := int64(2); version <= 2000 && writeErr == nil; version++ {
		writeErr = writer.Write(proto.ChangelogEntry{Version: version})
		if writeErr == nil && version%4 == 0 {
			writeErr = writer.TruncateBefore(uint64(version - 2))
		}
	}
	stop.Store(true)
	wg.Wait()
	require.NoError(t, writeErr)
	require.NoError(t, writer.Close())
	t.Logf("read-only opens: %d succeeded, %d failed", opens.Load(), failures.Load())
	require.Positive(t, opens.Load())
	require.Less(t, failures.Load(), opens.Load())
}
