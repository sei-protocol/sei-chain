//go:build linux || darwin

package memiavl

import (
	"bytes"
	"fmt"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// residentPages returns how many of the pages holding bytes [from, to) of the mapped file data are in
// the page cache, and how many pages hold them.
func residentPages(t *testing.T, data []byte, from, to int) (resident, pages int) {
	t.Helper()
	if from >= to {
		return 0, 0
	}
	pageSize := unix.Getpagesize()
	from -= from % pageSize
	vec := make([]byte, (to-from+pageSize-1)/pageSize)
	_, _, errno := unix.Syscall(unix.SYS_MINCORE, uintptr(unsafe.Pointer(&data[from])), uintptr(to-from), uintptr(unsafe.Pointer(&vec[0])))
	require.Zero(t, errno, "mincore: %v", errno)
	for _, v := range vec {
		if v&1 == 1 {
			resident++
		}
	}
	return resident, len(vec)
}

// residentFraction returns the fraction of the pages of the snapshot's files that are in the page cache.
func residentFraction(t *testing.T, snapshot *Snapshot) float64 {
	t.Helper()
	var resident, pages int
	for _, data := range [][]byte{snapshot.leaves, snapshot.nodes, snapshot.kvs} {
		r, p := residentPages(t, data, 0, len(data))
		resident, pages = resident+r, pages+p
	}
	return float64(resident) / float64(pages)
}

// evictSnapshot drops the snapshot's files from the page cache, skipping the test when the filesystem
// keeps them resident.
func evictSnapshot(t *testing.T, snapshot *Snapshot) {
	t.Helper()
	for _, file := range []*MmapFile{snapshot.leavesMap, snapshot.nodesMap, snapshot.kvsMap} {
		evictFromPageCache(t, file)
	}
	if residentFraction(t, snapshot) > 0.5 {
		t.Skip("the filesystem under TMPDIR keeps file pages resident, so their eviction cannot be observed")
	}
}

// walkedFile is one of a snapshot's mapped files with the offset where the part an ascending walk over
// a run of leaves from the first reads ends.
type walkedFile struct {
	name string
	data []byte
	end  int
}

// walkedFiles returns the snapshot's files, each with the offset where the part an ascending walk over
// the leaves below leaf reads ends.
func walkedFiles(snapshot *Snapshot, leaf uint64) []walkedFile {
	return []walkedFile{
		{"leaves", snapshot.leaves, int(leaf) * SizeLeaf},
		{"nodes", snapshot.nodes, min(int(leaf)*SizeNode, len(snapshot.nodes))},
		{"kvs", snapshot.kvs, int(snapshot.kvsOffset(leaf))},
	}
}

func TestReadAheadStoreReadsTheNamedTreeWhenAnIteratorReachesIt(t *testing.T) {
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
	acc, bank := cs.db.TreeByName("acc").snapshot, cs.db.TreeByName("bank").snapshot
	evictSnapshot(t, acc)
	evictSnapshot(t, bank)

	stop := cs.ReadAheadStore("acc")
	defer stop()
	cs.ReadAheadStore("not-a-tree")()
	require.Less(t, residentFraction(t, acc), 0.5, "arming the read-ahead read the acc snapshot")

	iter := cs.GetChildStoreByName("acc").Iterator(nil, nil, true)
	defer func() { require.NoError(t, iter.Close()) }()
	require.Equal(t, 1.0, residentFraction(t, acc), "a page of the acc snapshot is not in the page cache")
	require.Less(t, residentFraction(t, bank), 0.5, "reading acc ahead read the bank snapshot")
}

func TestReadAheadKeepsTheFilesPastAnIteratorsWindowOutOfThePageCache(t *testing.T) {
	tree := newSnapshotTree(t, 100_000, 200)
	snapshot := tree.snapshot
	evictSnapshot(t, snapshot)

	windowSize := snapshotSize(snapshot) / 8
	stop := tree.readAhead("test", windowSize)
	defer stop()
	windows := snapshot.readAhead.Load()
	require.NotNil(t, windows)

	pageSize := unix.Getpagesize()
	iter := tree.Iterator(nil, nil, true)
	defer func() { require.NoError(t, iter.Close()) }()
	for leaf := uint64(0); iter.Valid(); leaf++ {
		if leaf%windows.windowLeaves == 0 {
			windowEnd := min(leaf+windows.windowLeaves, windows.leaves)
			var through, past, pastResident int
			for _, file := range walkedFiles(snapshot, windowEnd) {
				resident, pages := residentPages(t, file.data, 0, file.end)
				require.Equal(t, pages, resident,
					"window %d: a page of the %s file below the end of the window is not in the page cache", leaf/windows.windowLeaves+1, file.name)
				pastStart := (file.end + pageSize - 1) / pageSize * pageSize
				resident, _ = residentPages(t, file.data, pastStart, len(file.data))
				through, past, pastResident = through+file.end, past+len(file.data)-file.end, pastResident+resident*pageSize
			}
			t.Logf("window %d of %d: iterator at leaf %d, page cache holds the %d KiB through this window and %d KiB of the %d KiB past it",
				leaf/windows.windowLeaves+1, len(windows.read), leaf, through>>10, pastResident>>10, past>>10)
			require.Less(t, uint64(pastResident), windowSize/4,
				"window %d: more than a quarter of a window past the iterator is in the page cache", leaf/windows.windowLeaves+1)
		}
		iter.Next()
	}
	require.Equal(t, 1.0, residentFraction(t, snapshot))
}
