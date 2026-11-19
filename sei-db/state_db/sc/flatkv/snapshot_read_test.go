package flatkv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

func TestReadSnapshotMiscValue(t *testing.T) {
	s := setupSnapshottingStore(t, 10)
	defer func() { require.NoError(t, s.Close()) }()
	commitToEveryDB(t, s, 1)
	commitToEveryDB(t, s, 2)
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte{'b', 1}, Delete: true},
			{Key: []byte{'e'}, Value: []byte{}},
		}}},
	}))
	h := commitAndCheck(t, s)
	dir := filepath.Join(t.TempDir(), "linked")
	require.NoError(t, HardLinkSnapshot(s.flatkvDir(), h, dir))

	got, found, err := ReadSnapshotMiscValue(dir, "bank", []byte{'b', 2})
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte{2}, got)

	got, found, err = ReadSnapshotMiscValue(dir, "bank", []byte{'e'})
	require.NoError(t, err)
	require.True(t, found, "an empty value is a row")
	require.Empty(t, got)

	for name, key := range map[string][]byte{"deleted": {'b', 1}, "never written": {'b', 9}} {
		_, found, err := ReadSnapshotMiscValue(dir, "bank", key)
		require.NoError(t, err, name)
		require.False(t, found, name)
	}

	_, _, err = ReadSnapshotMiscValue(dir, keys.EVMStoreKey, []byte{1})
	require.Error(t, err)

	require.NoError(t, os.RemoveAll(filepath.Join(dir, miscDBDir)))
	_, _, err = ReadSnapshotMiscValue(dir, "bank", []byte{'b', 2})
	require.ErrorContains(t, err, "read-only")
}
