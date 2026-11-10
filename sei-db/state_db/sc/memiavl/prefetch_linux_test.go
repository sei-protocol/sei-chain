package memiavl

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// treeFiles returns the mapped nodes, leaves and kvs files of the snapshot backing the tree named name.
func treeFiles(t *testing.T, cs *CommitStore, name string) []*MmapFile {
	t.Helper()
	tree := cs.db.TreeByName(name)
	require.NotNil(t, tree, "no tree named %s", name)
	return []*MmapFile{tree.snapshot.nodesMap, tree.snapshot.leavesMap, tree.snapshot.kvsMap}
}

// residentRatios returns the page cache residency of each of the tree's snapshot files.
func residentRatios(t *testing.T, cs *CommitStore, name string) []float64 {
	t.Helper()
	var ratios []float64
	for _, file := range treeFiles(t, cs, name) {
		ratio, err := residentRatio(file.data)
		require.NoError(t, err)
		ratios = append(ratios, ratio)
	}
	return ratios
}

func TestPrefetchStoreReadsTheNamedTreeIntoThePageCache(t *testing.T) {
	cs := NewCommitStore(t.TempDir(), DefaultConfig())
	require.NoError(t, cs.Initialize([]string{"acc", "bank"}))
	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cs.Close()) })

	pairs := make([]*proto.KVPair, 0, 20_000)
	for i := range 20_000 {
		pairs = append(pairs, &proto.KVPair{Key: fmt.Appendf(nil, "key-%08d", i), Value: bytes.Repeat([]byte{byte(i)}, 100)})
	}
	require.NoError(t, cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: "acc", Changeset: proto.ChangeSet{Pairs: pairs}},
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: pairs}},
	}))
	_, err = cs.Commit(1)
	require.NoError(t, err)
	require.NoError(t, cs.GetDB().RewriteSnapshot(t.Context()))
	require.NoError(t, cs.GetDB().Reload())

	for _, name := range []string{"acc", "bank"} {
		for _, file := range treeFiles(t, cs, name) {
			require.NoError(t, unix.Fadvise(int(file.file.Fd()), 0, 0, unix.FADV_DONTNEED))
		}
	}
	for _, ratio := range residentRatios(t, cs, "acc") {
		if ratio > 0.5 {
			t.Skip("the filesystem under TMPDIR keeps file pages resident, so their eviction cannot be observed")
		}
	}

	cs.PrefetchStore("acc")
	cs.PrefetchStore("not-a-tree")

	for _, ratio := range residentRatios(t, cs, "acc") {
		require.Equal(t, 1.0, ratio, "a page of the acc snapshot is not in the page cache")
	}
	for _, ratio := range residentRatios(t, cs, "bank") {
		require.Less(t, ratio, 0.5, "prefetching acc read the bank snapshot")
	}
}
