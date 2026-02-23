package wal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/wal"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/cosmos/iavl"
)

var (
	ChangeSets = []iavl.ChangeSet{
		{Pairs: MockKVPairs("hello", "world")},
		{Pairs: MockKVPairs("hello1", "world1", "hello2", "world2")},
		{Pairs: MockKVPairs("hello3", "world3")},
	}

	// marshal/unmarshal functions for testing
	marshalEntry   = func(e proto.ChangelogEntry) ([]byte, error) { return e.Marshal() }
	unmarshalEntry = func(data []byte) (proto.ChangelogEntry, error) {
		var e proto.ChangelogEntry
		err := e.Unmarshal(data)
		return e, err
	}
)

func TestOpenAndCorruptedTail(t *testing.T) {
	opts := &wal.Options{
		LogFormat: wal.JSON,
	}
	dir := t.TempDir()

	testCases := []struct {
		name      string
		logs      []byte
		lastIndex uint64
	}{
		{"failure-1", []byte("\n"), 0},
		{"failure-2", []byte(`{}` + "\n"), 0},
		{"failure-3", []byte(`{"index":"1"}` + "\n"), 0},
		{"failure-4", []byte(`{"index":"1","data":"?"}`), 0},
		{"failure-5", []byte(`{"index":1,"data":"?"}` + "\n" + `{"index":"1","data":"?"}`), 1},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := os.WriteFile(filepath.Join(dir, "00000000000000000001"), tc.logs, 0o600)
			require.NoError(t, err)

			_, err = wal.Open(dir, opts)
			require.Equal(t, wal.ErrCorrupt, err)

			log, err := open(dir, opts)
			require.NoError(t, err)

			lastIndex, err := log.LastIndex()
			require.NoError(t, err)
			require.Equal(t, tc.lastIndex, lastIndex)
		})
	}
}

func TestReplay(t *testing.T) {
	changelog := prepareTestData(t)
	var total = 0
	err := changelog.Replay(1, 2, func(index uint64, entry proto.ChangelogEntry) error {
		total++
		switch index {
		case 1:
			require.Equal(t, "test", entry.Changesets[0].Name)
			require.Equal(t, []byte("hello"), entry.Changesets[0].Changeset.Pairs[0].Key)
			require.Equal(t, []byte("world"), entry.Changesets[0].Changeset.Pairs[0].Value)
		case 2:
			require.Equal(t, []byte("hello1"), entry.Changesets[0].Changeset.Pairs[0].Key)
			require.Equal(t, []byte("world1"), entry.Changesets[0].Changeset.Pairs[0].Value)
			require.Equal(t, []byte("hello2"), entry.Changesets[0].Changeset.Pairs[1].Key)
			require.Equal(t, []byte("world2"), entry.Changesets[0].Changeset.Pairs[1].Value)
		default:
			require.Fail(t, fmt.Sprintf("unexpected index %d", index))
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 2, total)
	err = changelog.Close()
	require.NoError(t, err)
}

func TestRandomRead(t *testing.T) {
	changelog := prepareTestData(t)
	entry, err := changelog.ReadAt(2)
	require.NoError(t, err)
	require.Equal(t, []byte("hello1"), entry.Changesets[0].Changeset.Pairs[0].Key)
	require.Equal(t, []byte("world1"), entry.Changesets[0].Changeset.Pairs[0].Value)
	require.Equal(t, []byte("hello2"), entry.Changesets[0].Changeset.Pairs[1].Key)
	require.Equal(t, []byte("world2"), entry.Changesets[0].Changeset.Pairs[1].Value)
	entry, err = changelog.ReadAt(1)
	require.NoError(t, err)
	require.Equal(t, []byte("hello"), entry.Changesets[0].Changeset.Pairs[0].Key)
	require.Equal(t, []byte("world"), entry.Changesets[0].Changeset.Pairs[0].Value)
	entry, err = changelog.ReadAt(3)
	require.NoError(t, err)
	require.Equal(t, []byte("hello3"), entry.Changesets[0].Changeset.Pairs[0].Key)
	require.Equal(t, []byte("world3"), entry.Changesets[0].Changeset.Pairs[0].Value)
}

func prepareTestData(t *testing.T) *WAL[proto.ChangelogEntry] {
	dir := t.TempDir()
	changelog, err := NewWAL(t.Context(), marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir, Config{})
	require.NoError(t, err)
	writeTestData(t, changelog)
	return changelog
}

func writeTestData(t *testing.T, changelog *WAL[proto.ChangelogEntry]) {
	for _, changes := range ChangeSets {
		cs := []*proto.NamedChangeSet{
			{
				Name:      "test",
				Changeset: changes,
			},
		}
		entry := proto.ChangelogEntry{}
		entry.Changesets = cs
		require.NoError(t, changelog.Write(entry))
	}
}

func TestSynchronousWrite(t *testing.T) {
	changelog := prepareTestData(t)
	lastIndex, err := changelog.LastOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(3), lastIndex)
}

func TestAsyncWrite(t *testing.T) {
	dir := t.TempDir()
	changelog, err := NewWAL(t.Context(), marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir,
		Config{WriteBufferSize: 10})
	require.NoError(t, err)
	for _, changes := range ChangeSets {
		cs := []*proto.NamedChangeSet{
			{
				Name:      "test",
				Changeset: changes,
			},
		}
		entry := &proto.ChangelogEntry{}
		entry.Changesets = cs
		err := changelog.Write(*entry)
		require.NoError(t, err)
	}
	err = changelog.Close()
	require.NoError(t, err)
	changelog, err = NewWAL(t.Context(), marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir,
		Config{WriteBufferSize: 10})
	require.NoError(t, err)
	lastIndex, err := changelog.LastOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(3), lastIndex)
}

func TestOpenWithNilOptions(t *testing.T) {
	dir := t.TempDir()

	// Test that open function handles nil options correctly
	log, err := open(dir, nil)
	require.NoError(t, err)
	require.NotNil(t, log)

	// Verify the log is functional by checking first and last index
	firstIndex, err := log.FirstIndex()
	require.NoError(t, err)
	require.Equal(t, uint64(0), firstIndex)

	lastIndex, err := log.LastIndex()
	require.NoError(t, err)
	require.Equal(t, uint64(0), lastIndex)

	// Clean up
	err = log.Close()
	require.NoError(t, err)
}

func TestTruncateAfter(t *testing.T) {
	changelog := prepareTestData(t)
	t.Cleanup(func() { require.NoError(t, changelog.Close()) })

	// Verify we have 3 entries
	lastIndex, err := changelog.LastOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(3), lastIndex)

	// Truncate after index 2 (removes entry 3)
	err = changelog.TruncateAfter(2)
	require.NoError(t, err)

	// Verify last index is now 2
	lastIndex, err = changelog.LastOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(2), lastIndex)

	// Verify nextOffset was updated - write a new entry and check its index
	entry := &proto.ChangelogEntry{}
	entry.Changesets = []*proto.NamedChangeSet{{Name: "new", Changeset: iavl.ChangeSet{Pairs: MockKVPairs("new", "entry")}}}
	err = changelog.Write(*entry)
	require.NoError(t, err)

	lastIndex, err = changelog.LastOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(3), lastIndex)
}

func TestTruncateBefore(t *testing.T) {
	changelog := prepareTestData(t)
	t.Cleanup(func() { require.NoError(t, changelog.Close()) })

	// Verify we have 3 entries starting at 1
	firstIndex, err := changelog.FirstOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(1), firstIndex)

	lastIndex, err := changelog.LastOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(3), lastIndex)

	// Truncate before index 2 (removes entry 1)
	err = changelog.TruncateBefore(2)
	require.NoError(t, err)

	// Verify first index is now 2
	firstIndex, err = changelog.FirstOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(2), firstIndex)

	// Last index should still be 3
	lastIndex, err = changelog.LastOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(3), lastIndex)

	// Verify entry 2 is still readable
	entry, err := changelog.ReadAt(2)
	require.NoError(t, err)
	require.Equal(t, []byte("hello1"), entry.Changesets[0].Changeset.Pairs[0].Key)
}

func TestCloseSyncMode(t *testing.T) {
	dir := t.TempDir()
	changelog, err := NewWAL(t.Context(), marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir, Config{})
	require.NoError(t, err)

	// Write some data in sync mode
	writeTestData(t, changelog)

	// Close the changelog
	err = changelog.Close()
	require.NoError(t, err)

	// Reopen and verify data persisted
	changelog2, err := NewWAL(t.Context(), marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir, Config{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, changelog2.Close()) })

	lastIndex, err := changelog2.LastOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(3), lastIndex)
}

func TestReadAtNonExistent(t *testing.T) {
	changelog := prepareTestData(t)
	t.Cleanup(func() { require.NoError(t, changelog.Close()) })

	// Try to read an entry that doesn't exist
	_, err := changelog.ReadAt(100)
	require.Error(t, err)
}

func TestReplayWithError(t *testing.T) {
	changelog := prepareTestData(t)
	t.Cleanup(func() { require.NoError(t, changelog.Close()) })

	// Replay with a function that returns an error
	expectedErr := fmt.Errorf("test error")
	err := changelog.Replay(1, 3, func(index uint64, entry proto.ChangelogEntry) error {
		if index == 2 {
			return expectedErr
		}
		return nil
	})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), expectedErr.Error()))
}

func TestReopenAndContinueWrite(t *testing.T) {
	dir := t.TempDir()

	// Create and write initial data
	changelog, err := NewWAL(t.Context(), marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir, Config{})
	require.NoError(t, err)
	writeTestData(t, changelog)
	err = changelog.Close()
	require.NoError(t, err)

	// Reopen and continue writing
	changelog2, err := NewWAL(t.Context(), marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir, Config{})
	require.NoError(t, err)

	// Verify nextOffset is correctly set after reopen
	lastIndex, err := changelog2.LastOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(3), lastIndex)

	// Write more data
	entry := &proto.ChangelogEntry{}
	entry.Changesets = []*proto.NamedChangeSet{{Name: "continued", Changeset: iavl.ChangeSet{Pairs: MockKVPairs("key4", "value4")}}}
	err = changelog2.Write(*entry)
	require.NoError(t, err)

	// Verify new entry is at index 4
	lastIndex, err = changelog2.LastOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(4), lastIndex)

	// Verify data integrity
	readEntry, err := changelog2.ReadAt(4)
	require.NoError(t, err)
	require.Equal(t, "continued", readEntry.Changesets[0].Name)
	require.Equal(t, []byte("key4"), readEntry.Changesets[0].Changeset.Pairs[0].Key)

	err = changelog2.Close()
	require.NoError(t, err)
}

func TestEmptyLog(t *testing.T) {
	dir := t.TempDir()
	changelog, err := NewWAL(t.Context(), marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir, Config{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, changelog.Close()) })

	// Empty log should have 0 for both first and last index
	firstIndex, err := changelog.FirstOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(0), firstIndex)

	lastIndex, err := changelog.LastOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(0), lastIndex)
}

func TestCheckErrorNoError(t *testing.T) {
	dir := t.TempDir()
	changelog, err := NewWAL(t.Context(), marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir,
		Config{WriteBufferSize: 10})
	require.NoError(t, err)

	// Write some data to initialize async mode
	entry := &proto.ChangelogEntry{}
	entry.Changesets = []*proto.NamedChangeSet{{Name: "test", Changeset: iavl.ChangeSet{Pairs: MockKVPairs("k", "v")}}}
	err = changelog.Write(*entry)
	require.NoError(t, err)

	err = changelog.Close()
	require.NoError(t, err)
}

func TestFirstAndLastOffset(t *testing.T) {
	changelog := prepareTestData(t)
	t.Cleanup(func() { require.NoError(t, changelog.Close()) })

	firstIndex, err := changelog.FirstOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(1), firstIndex)

	lastIndex, err := changelog.LastOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(3), lastIndex)
}

func TestAsyncWriteReopenAndContinue(t *testing.T) {
	dir := t.TempDir()

	// Create with async write and write data
	changelog, err := NewWAL(t.Context(), marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir,
		Config{WriteBufferSize: 10})
	require.NoError(t, err)

	for _, changes := range ChangeSets {
		cs := []*proto.NamedChangeSet{{Name: "test", Changeset: changes}}
		entry := &proto.ChangelogEntry{Changesets: cs}
		err := changelog.Write(*entry)
		require.NoError(t, err)
	}

	err = changelog.Close()
	require.NoError(t, err)

	// Reopen with async write and continue
	changelog2, err := NewWAL(t.Context(), marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir,
		Config{WriteBufferSize: 10})
	require.NoError(t, err)

	// Write more entries
	for i := 0; i < 3; i++ {
		entry := &proto.ChangelogEntry{}
		entry.Changesets = []*proto.NamedChangeSet{{Name: fmt.Sprintf("batch2-%d", i), Changeset: iavl.ChangeSet{Pairs: MockKVPairs("k", "v")}}}
		err := changelog2.Write(*entry)
		require.NoError(t, err)
	}

	err = changelog2.Close()
	require.NoError(t, err)

	// Reopen and verify all 6 entries
	changelog3, err := NewWAL(t.Context(), marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir, Config{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, changelog3.Close()) })

	lastIndex, err := changelog3.LastOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(6), lastIndex)
}

func TestReplaySingleEntry(t *testing.T) {
	changelog := prepareTestData(t)
	t.Cleanup(func() { require.NoError(t, changelog.Close()) })

	var count int
	err := changelog.Replay(2, 2, func(index uint64, entry proto.ChangelogEntry) error {
		count++
		require.Equal(t, uint64(2), index)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

// TestBatchWrite exercises the batch write path by writing many entries quickly so they
// are processed in batches, then verifies all entries were written correctly.
func TestBatchWrite(t *testing.T) {
	const (
		batchSize = 8
		numWrites = 32
	)
	dir := t.TempDir()
	changelog, err := NewWAL(t.Context(), marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir,
		Config{
			WriteBatchSize:  batchSize,
			WriteBufferSize: 64,
		})
	require.NoError(t, err)

	// Pump writes quickly so the main loop batches them (handleBatchedWrite drains up to batchSize).
	for i := 0; i < numWrites; i++ {
		entry := &proto.ChangelogEntry{}
		entry.Changesets = []*proto.NamedChangeSet{{
			Name:      fmt.Sprintf("batch-%d", i),
			Changeset: iavl.ChangeSet{Pairs: MockKVPairs(fmt.Sprintf("key-%d", i), fmt.Sprintf("val-%d", i))},
		}}
		require.NoError(t, changelog.Write(*entry))
	}

	require.NoError(t, changelog.Close())

	// Reopen and verify all entries
	changelog2, err := NewWAL(t.Context(), marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir, Config{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, changelog2.Close()) })

	first, err := changelog2.FirstOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(1), first)
	last, err := changelog2.LastOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(numWrites), last)

	var replayed int
	err = changelog2.Replay(1, uint64(numWrites), func(index uint64, entry proto.ChangelogEntry) error {
		replayed++
		require.Len(t, entry.Changesets, 1)
		require.Equal(t, fmt.Sprintf("batch-%d", index-1), entry.Changesets[0].Name)
		require.Len(t, entry.Changesets[0].Changeset.Pairs, 1)
		require.Equal(t, []byte(fmt.Sprintf("key-%d", index-1)), entry.Changesets[0].Changeset.Pairs[0].Key)
		require.Equal(t, []byte(fmt.Sprintf("val-%d", index-1)), entry.Changesets[0].Changeset.Pairs[0].Value)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, numWrites, replayed)
}

func TestWriteMultipleChangesets(t *testing.T) {
	dir := t.TempDir()
	changelog, err := NewWAL(t.Context(), marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir, Config{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, changelog.Close()) })

	// Write entry with multiple changesets
	entry := &proto.ChangelogEntry{
		Changesets: []*proto.NamedChangeSet{
			{Name: "store1", Changeset: iavl.ChangeSet{Pairs: MockKVPairs("a", "1")}},
			{Name: "store2", Changeset: iavl.ChangeSet{Pairs: MockKVPairs("b", "2")}},
			{Name: "store3", Changeset: iavl.ChangeSet{Pairs: MockKVPairs("c", "3")}},
		},
	}
	err = changelog.Write(*entry)
	require.NoError(t, err)

	// Read and verify
	readEntry, err := changelog.ReadAt(1)
	require.NoError(t, err)
	require.Len(t, readEntry.Changesets, 3)
	require.Equal(t, "store1", readEntry.Changesets[0].Name)
	require.Equal(t, "store2", readEntry.Changesets[1].Name)
	require.Equal(t, "store3", readEntry.Changesets[2].Name)
}

func TestConcurrentCloseWithInFlightAsyncWrites(t *testing.T) {
	dir := t.TempDir()
	changelog, err := NewWAL(t.Context(), marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir,
		Config{WriteBufferSize: 8})
	require.NoError(t, err)

	// Intentionally avoid t.Cleanup here: we want Close() to race with in-flight async writes.

	// Writers: keep calling Write() until it returns an error (which should happen once Close() starts).
	// If Write() or Close() deadlocks, the test will time out waiting for the goroutines to exit.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				entry := proto.ChangelogEntry{
					Changesets: []*proto.NamedChangeSet{{
						Name:      "test",
						Changeset: iavl.ChangeSet{Pairs: MockKVPairs("k", "v")},
					}},
				}
				if err := changelog.Write(entry); err != nil {
					return
				}
			}
		}()
	}

	// Ensure we actually have in-flight async activity before closing.
	require.Eventually(t, func() bool {
		last, err := changelog.LastOffset()
		return err == nil && last > 0
	}, 1*time.Second, 10*time.Millisecond, "expected some writes before Close()")

	closeDone := make(chan struct{})
	closeErr := make(chan error, 1)
	go func() {
		closeErr <- changelog.Close()
		close(closeDone)
	}()

	// Wait for writers to observe Close() and exit.
	writersDone := make(chan struct{})
	go func() { wg.Wait(); close(writersDone) }()
	require.Eventually(t, func() bool {
		select {
		case <-writersDone:
			return true
		default:
			return false
		}
	}, 3*time.Second, 10*time.Millisecond, "writers did not exit (possible deadlock)")

	// Ensure Close() returns too.
	require.Eventually(t, func() bool {
		select {
		case <-closeDone:
			return true
		default:
			return false
		}
	}, 3*time.Second, 10*time.Millisecond, "Close() did not return (possible deadlock)")

	require.NoError(t, <-closeErr)
}

func TestConcurrentTruncateBeforeWithAsyncWrites(t *testing.T) {
	dir := t.TempDir()
	changelog, err := NewWAL(t.Context(), marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir, Config{
		WriteBufferSize: 10,
		KeepRecent:      10,
		PruneInterval:   1 * time.Millisecond,
	})
	require.NoError(t, err)

	const (
		totalWrites = 50
	)

	// Write a bunch of entries (async writes). We'll wait until they're all persisted.
	for i := 1; i <= totalWrites; i++ {
		entry := proto.ChangelogEntry{
			Changesets: []*proto.NamedChangeSet{{
				Name:      "test",
				Changeset: iavl.ChangeSet{Pairs: MockKVPairs(fmt.Sprintf("k-%d", i), "v")},
			}},
		}
		require.NoError(t, changelog.Write(entry))
	}

	// Ensure async writer has flushed to disk.
	require.Eventually(t, func() bool {
		last, err := changelog.LastOffset()
		return err == nil && last == uint64(totalWrites)
	}, 3*time.Second, 10*time.Millisecond, "async writes did not flush")

	// Let the background pruning goroutine run and advance FirstOffset.
	require.Eventually(t, func() bool {
		first, err := changelog.FirstOffset()
		return err == nil && first > 1
	}, 3*time.Second, 10*time.Millisecond, "background pruning did not advance FirstOffset")

	// Manual front truncation while pruning is enabled.
	firstBefore, err := changelog.FirstOffset()
	require.NoError(t, err)
	last, err := changelog.LastOffset()
	require.NoError(t, err)
	require.True(t, firstBefore < last, "expected a non-empty range after writes")

	require.NoError(t, changelog.TruncateBefore(firstBefore+1))
	require.Eventually(t, func() bool {
		first, err := changelog.FirstOffset()
		return err == nil && first >= firstBefore+1
	}, 3*time.Second, 10*time.Millisecond, "manual truncation did not take effect")

	// Read first + last entries to ensure no corruption (decode succeeds; expected structure).
	first, err := changelog.FirstOffset()
	require.NoError(t, err)
	last, err = changelog.LastOffset()
	require.NoError(t, err)
	require.True(t, first <= last, "invalid WAL range after pruning/truncation")

	firstEntry, err := changelog.ReadAt(first)
	require.NoError(t, err)
	require.NotEmpty(t, firstEntry.Changesets)
	require.Equal(t, "test", firstEntry.Changesets[0].Name)
	require.NotEmpty(t, firstEntry.Changesets[0].Changeset.Pairs)

	lastEntry, err := changelog.ReadAt(last)
	require.NoError(t, err)
	require.NotEmpty(t, lastEntry.Changesets)
	require.Equal(t, "test", lastEntry.Changesets[0].Name)
	require.NotEmpty(t, lastEntry.Changesets[0].Changeset.Pairs)

	require.NoError(t, changelog.Close())
}

func TestGetLastIndex(t *testing.T) {
	dir := t.TempDir()
	changelog, err := NewWAL(t.Context(), marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir, Config{})
	require.NoError(t, err)
	writeTestData(t, changelog)
	err = changelog.Close()
	require.NoError(t, err)

	// Use utility function to get last index without opening stream
	lastIndex, err := GetLastIndex(dir)
	require.NoError(t, err)
	require.Equal(t, uint64(3), lastIndex)
}

func TestLogPath(t *testing.T) {
	path := LogPath("/some/dir")
	require.Equal(t, "/some/dir/changelog", path)
}

// batchTestEntry is a simple type for testing batch marshal failures.
type batchTestEntry struct {
	value string
}

func TestBatchWriteWithMarshalFailure(t *testing.T) {
	dir := t.TempDir()

	// Marshal fails for entries with value "fail"
	marshalBatchTest := func(e batchTestEntry) ([]byte, error) {
		if e.value == "fail" {
			return nil, fmt.Errorf("mock marshal failure")
		}
		return []byte(e.value), nil
	}
	unmarshalBatchTest := func(b []byte) (batchTestEntry, error) {
		return batchTestEntry{value: string(b)}, nil
	}

	// Use sync writes (WriteBufferSize 0) and batching (WriteBatchSize 4)
	// so we can observe per-write errors. The channel buffer allows multiple
	// goroutines to push before the handler runs, forming a batch.
	config := Config{
		WriteBufferSize: 0, // sync writes
		WriteBatchSize:  4, // batch up to 4
	}

	w, err := NewWAL(t.Context(), marshalBatchTest, unmarshalBatchTest, logger.NewNopLogger(), dir, config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, w.Close()) })

	// Write 4 entries concurrently so they get batched. The second one will fail to marshal.
	entries := []batchTestEntry{
		{value: "ok1"},
		{value: "fail"},
		{value: "ok2"},
		{value: "ok3"},
	}

	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range entries {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = w.Write(entries[idx])
		}(i)
	}
	wg.Wait()

	// The "fail" entry should have errored
	require.Error(t, errs[1])
	require.Contains(t, errs[1].Error(), "mock marshal failure")

	// The successful entries should have no error
	require.NoError(t, errs[0])
	require.NoError(t, errs[2])
	require.NoError(t, errs[3])

	// The WAL should contain exactly 3 entries (the successfully marshalled ones; "fail" is skipped)
	lastOffset, err := w.LastOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(3), lastOffset)

	// Goroutines may push in any order, so we collect the written values and verify we have ok1, ok2, ok3
	written := make(map[string]bool)
	for i := uint64(1); i <= 3; i++ {
		e, err := w.ReadAt(i)
		require.NoError(t, err)
		written[e.value] = true
	}
	require.True(t, written["ok1"], "expected ok1 in WAL")
	require.True(t, written["ok2"], "expected ok2 in WAL")
	require.True(t, written["ok3"], "expected ok3 in WAL")
	require.False(t, written["fail"], "fail should not be in WAL")
}

func TestMultipleCloseCalls(t *testing.T) {
	changelog := prepareTestData(t)
	entry, err := changelog.ReadAt(2)
	require.NoError(t, err)
	require.Equal(t, []byte("hello1"), entry.Changesets[0].Changeset.Pairs[0].Key)
	require.Equal(t, []byte("world1"), entry.Changesets[0].Changeset.Pairs[0].Value)
	require.Equal(t, []byte("hello2"), entry.Changesets[0].Changeset.Pairs[1].Key)
	require.Equal(t, []byte("world2"), entry.Changesets[0].Changeset.Pairs[1].Value)
	entry, err = changelog.ReadAt(1)
	require.NoError(t, err)
	require.Equal(t, []byte("hello"), entry.Changesets[0].Changeset.Pairs[0].Key)
	require.Equal(t, []byte("world"), entry.Changesets[0].Changeset.Pairs[0].Value)
	entry, err = changelog.ReadAt(3)
	require.NoError(t, err)
	require.Equal(t, []byte("hello3"), entry.Changesets[0].Changeset.Pairs[0].Key)
	require.Equal(t, []byte("world3"), entry.Changesets[0].Changeset.Pairs[0].Value)

	// Calling close lots of times shouldn't cause any problems.
	for i := 0; i < 10; i++ {
		require.NoError(t, changelog.Close())
	}
}
