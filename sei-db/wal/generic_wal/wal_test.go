package generic_wal

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/wal"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
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
			os.WriteFile(filepath.Join(dir, "00000000000000000001"), tc.logs, 0o600)

			_, err := wal.Open(dir, opts)
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
	changelog, err := NewWAL(marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir, Config{})
	require.NoError(t, err)
	writeTestData(changelog)
	return changelog
}

func writeTestData(changelog *WAL[proto.ChangelogEntry]) {
	for _, changes := range ChangeSets {
		cs := []*proto.NamedChangeSet{
			{
				Name:      "test",
				Changeset: changes,
			},
		}
		entry := proto.ChangelogEntry{}
		entry.Changesets = cs
		_ = changelog.Write(entry)
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
	changelog, err := NewWAL(marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir, Config{WriteBufferSize: 10})
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
	changelog, err = NewWAL(marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir, Config{WriteBufferSize: 10})
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
	defer changelog.Close()

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
	defer changelog.Close()

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
	changelog, err := NewWAL(marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir, Config{})
	require.NoError(t, err)

	// Write some data in sync mode
	writeTestData(changelog)

	// Close the changelog
	err = changelog.Close()
	require.NoError(t, err)

	// Verify isClosed is set
	require.True(t, changelog.isClosed)

	// Reopen and verify data persisted
	changelog2, err := NewWAL(marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir, Config{})
	require.NoError(t, err)
	defer changelog2.Close()

	lastIndex, err := changelog2.LastOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(3), lastIndex)
}

func TestReadAtNonExistent(t *testing.T) {
	changelog := prepareTestData(t)
	defer changelog.Close()

	// Try to read an entry that doesn't exist
	_, err := changelog.ReadAt(100)
	require.Error(t, err)
}

func TestReplayWithError(t *testing.T) {
	changelog := prepareTestData(t)
	defer changelog.Close()

	// Replay with a function that returns an error
	expectedErr := fmt.Errorf("test error")
	err := changelog.Replay(1, 3, func(index uint64, entry proto.ChangelogEntry) error {
		if index == 2 {
			return expectedErr
		}
		return nil
	})
	require.Error(t, err)
	require.Equal(t, expectedErr, err)
}

func TestReopenAndContinueWrite(t *testing.T) {
	dir := t.TempDir()

	// Create and write initial data
	changelog, err := NewWAL(marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir, Config{})
	require.NoError(t, err)
	writeTestData(changelog)
	err = changelog.Close()
	require.NoError(t, err)

	// Reopen and continue writing
	changelog2, err := NewWAL(marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir, Config{})
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
	changelog, err := NewWAL(marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir, Config{})
	require.NoError(t, err)
	defer changelog.Close()

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
	changelog, err := NewWAL(marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir, Config{WriteBufferSize: 10})
	require.NoError(t, err)

	// Write some data to initialize async mode
	entry := &proto.ChangelogEntry{}
	entry.Changesets = []*proto.NamedChangeSet{{Name: "test", Changeset: iavl.ChangeSet{Pairs: MockKVPairs("k", "v")}}}
	err = changelog.Write(*entry)
	require.NoError(t, err)

	// CheckError should return nil when no errors
	err = changelog.CheckError()
	require.NoError(t, err)

	err = changelog.Close()
	require.NoError(t, err)
}

func TestFirstAndLastOffset(t *testing.T) {
	changelog := prepareTestData(t)
	defer changelog.Close()

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
	changelog, err := NewWAL(marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir, Config{WriteBufferSize: 10})
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
	changelog2, err := NewWAL(marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir, Config{WriteBufferSize: 10})
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
	changelog3, err := NewWAL(marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir, Config{})
	require.NoError(t, err)
	defer changelog3.Close()

	lastIndex, err := changelog3.LastOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(6), lastIndex)
}

func TestReplaySingleEntry(t *testing.T) {
	changelog := prepareTestData(t)
	defer changelog.Close()

	var count int
	err := changelog.Replay(2, 2, func(index uint64, entry proto.ChangelogEntry) error {
		count++
		require.Equal(t, uint64(2), index)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestWriteMultipleChangesets(t *testing.T) {
	dir := t.TempDir()
	changelog, err := NewWAL(marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir, Config{})
	require.NoError(t, err)
	defer changelog.Close()

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

func TestGetLastIndex(t *testing.T) {
	dir := t.TempDir()
	changelog, err := NewWAL(marshalEntry, unmarshalEntry, logger.NewNopLogger(), dir, Config{})
	require.NoError(t, err)
	writeTestData(changelog)
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
