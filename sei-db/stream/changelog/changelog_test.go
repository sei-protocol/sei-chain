package changelog

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cosmos/iavl"
	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/proto"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/wal"
)

var (
	ChangeSets = []iavl.ChangeSet{
		{Pairs: MockKVPairs("hello", "world")},
		{Pairs: MockKVPairs("hello1", "world1", "hello2", "world2")},
		{Pairs: MockKVPairs("hello3", "world3")},
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

func prepareTestData(t *testing.T) *Stream {
	dir := t.TempDir()
	changelog, err := NewStream(logger.NewNopLogger(), dir, Config{})
	require.NoError(t, err)
	writeTestData(changelog)
	return changelog
}

func writeTestData(changelog *Stream) {
	for i, changes := range ChangeSets {
		cs := []*proto.NamedChangeSet{
			{
				Name:      "test",
				Changeset: changes,
			},
		}
		entry := &proto.ChangelogEntry{}
		entry.Changesets = cs
		_ = changelog.Write(uint64(i+1), *entry)
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
	changelog, err := NewStream(logger.NewNopLogger(), dir, Config{WriteBufferSize: 10})
	require.NoError(t, err)
	for i, changes := range ChangeSets {
		cs := []*proto.NamedChangeSet{
			{
				Name:      "test",
				Changeset: changes,
			},
		}
		entry := &proto.ChangelogEntry{}
		entry.Changesets = cs
		err := changelog.Write(uint64(i+1), *entry)
		require.NoError(t, err)
		lastIndex, err := changelog.LastOffset()
		require.NoError(t, err)
		// Writes happen async, so lastIndex should not move yet
		require.Greater(t, uint64(3), lastIndex)
	}
	err = changelog.Close()
	require.NoError(t, err)
	changelog, err = NewStream(logger.NewNopLogger(), dir, Config{WriteBufferSize: 10})
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
