package statewal

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

func TestIteratorEmpty(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()

	it, err := w.Iterator(0)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	ok, err := it.Next()
	require.NoError(t, err)
	require.False(t, ok)
}

func TestIteratorFromMiddle(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()
	for block := uint64(1); block <= 5; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())

	require.Equal(t, []uint64{3, 4, 5}, collectBlocks(t, w, 3))
}

func TestIteratorYieldsChangesetContents(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()

	cs := []*proto.NamedChangeSet{makeChangeSet("evm", []byte("key"), []byte("value"))}
	require.NoError(t, w.Write(1, cs))
	require.NoError(t, w.SignalEndOfBlock())
	require.NoError(t, w.Flush())

	it, err := w.Iterator(1)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	ok, err := it.Next()
	require.NoError(t, err)
	require.True(t, ok)
	blockNumber, changeset := it.Entry()
	require.Equal(t, uint64(1), blockNumber)
	require.Len(t, changeset, 1)
	require.Equal(t, "evm", changeset[0].Name)
	require.Equal(t, []byte("key"), changeset[0].Changeset.Pairs[0].Key)
	require.Equal(t, []byte("value"), changeset[0].Changeset.Pairs[0].Value)

	ok, err = it.Next()
	require.NoError(t, err)
	require.False(t, ok)
}

// TestIteratorCombinesMultipleWritesInOrder verifies that all changesets written for one block across several
// Write calls appear, in write order, in that block's single entry.
func TestIteratorCombinesMultipleWritesInOrder(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()

	require.NoError(t, w.Write(1, []*proto.NamedChangeSet{makeChangeSet("a", []byte("k1"), []byte("v1"))}))
	require.NoError(t, w.Write(1, []*proto.NamedChangeSet{
		makeChangeSet("b", []byte("k2"), []byte("v2")),
		makeChangeSet("c", []byte("k3"), []byte("v3")),
	}))
	require.NoError(t, w.SignalEndOfBlock())
	require.NoError(t, w.Flush())

	it, err := w.Iterator(1)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	ok, err := it.Next()
	require.NoError(t, err)
	require.True(t, ok)

	blockNumber, changeset := it.Entry()
	require.Equal(t, uint64(1), blockNumber)
	// Three changesets total (1 from the first Write, 2 from the second), in write order.
	require.Len(t, changeset, 3)
	require.Equal(t, "a", changeset[0].Name)
	require.Equal(t, "b", changeset[1].Name)
	require.Equal(t, "c", changeset[2].Name)

	ok, err = it.Next()
	require.NoError(t, err)
	require.False(t, ok)
}

func TestIteratorStopsBeforeIncompleteBlock(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()
	for block := uint64(1); block <= 3; block++ {
		writeBlock(t, w, block)
	}
	// Block 4 written but not ended: it was never appended, so it must not be yielded.
	require.NoError(t, w.Write(4, []*proto.NamedChangeSet{makeChangeSet("evm", []byte{4}, []byte{4})}))
	require.NoError(t, w.Flush())

	require.Equal(t, []uint64{1, 2, 3}, collectBlocks(t, w, 1))
}

// TestIteratorDoesNotSeePostConstructionBlocks confirms the snapshot contract at the wrapper level: an
// iterator yields only blocks that were complete when it was created.
func TestIteratorDoesNotSeePostConstructionBlocks(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()

	for block := uint64(1); block <= 3; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())

	it, err := w.Iterator(1)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	// Written after the iterator exists, before draining: must not be observed.
	writeBlock(t, w, 4)
	require.NoError(t, w.Flush())

	var got []uint64
	for {
		ok, err := it.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
		blockNumber, _ := it.Entry()
		got = append(got, blockNumber)
	}
	require.Equal(t, []uint64{1, 2, 3}, got, "post-construction block 4 must not be iterated")
}
