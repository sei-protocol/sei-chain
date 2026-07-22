package flatkv

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

// TestDeleteImportedMiscKey pins the post-import delete path for non-EVM
// (misc-lane) module keys: a key that entered the store through the snapshot
// importer must be deletable by a later ApplyChangeSets+Commit, exactly like
// a live-written key. Regression test for the forked-chain wedge where
// staking's unbonding-queue entry survived its EndBlock deletion after a
// full import-flatkv-from-memiavl conversion.
func TestDeleteImportedMiscKey(t *testing.T) {
	moduleKey := []byte{0x43, 0x01, 0x02, 0x03}
	moduleVal := []byte("queued-validators")

	// --- Source store: live-write a staking key and export it ---
	src := setupTestStore(t)
	defer src.Close()
	require.NoError(t, src.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "staking", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: moduleKey, Value: moduleVal},
		}}},
	}))
	commitAndCheck(t, src)

	exp, err := src.Exporter(1)
	require.NoError(t, err)
	nodes := drainExporter(t, exp)
	require.NoError(t, exp.Close())
	require.Greater(t, len(nodes), 0)

	// --- Import into a fresh store ---
	dst := setupTestStore(t)
	defer dst.Close()
	imp, err := dst.Importer(1)
	require.NoError(t, err)
	require.NoError(t, imp.AddModule("flatkv"))
	for _, n := range nodes {
		imp.AddNode(n)
	}
	require.NoError(t, imp.Close())

	got, found := dst.Get("staking", moduleKey)
	require.True(t, found, "imported staking key must be readable")
	require.Equal(t, moduleVal, got)

	// --- Delete the imported key in a normal block commit ---
	require.NoError(t, dst.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "staking", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: moduleKey, Delete: true},
		}}},
	}))
	commitAndCheck(t, dst)

	_, found = dst.Get("staking", moduleKey)
	require.False(t, found, "deleted imported key must not be readable")

	iter, err := dst.Iterator("staking", []byte{0x43}, []byte{0x44}, true)
	require.NoError(t, err)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		require.NotEqual(t, moduleKey, iter.Key(),
			"deleted imported key must not appear in iteration")
	}
	require.NoError(t, iter.Error())

	// --- Control: the same flow on the live-written source store ---
	require.NoError(t, src.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "staking", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: moduleKey, Delete: true},
		}}},
	}))
	commitAndCheck(t, src)
	_, found = src.Get("staking", moduleKey)
	require.False(t, found, "deleted live-written key must not be readable")
}
