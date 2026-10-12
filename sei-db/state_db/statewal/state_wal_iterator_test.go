package statewal

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
	"github.com/stretchr/testify/require"
)

// TestIteratorEmptyWALErrors verifies that an empty WAL has no latest block, so any requested end block is
// beyond it: iterator creation fails with ErrIteratorRange rather than bricking the WAL.
func TestIteratorEmptyWALErrors(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()

	_, err := w.Iterator(0, 0)
	require.ErrorIs(t, err, seiwal.ErrIteratorRange)

	// The rejection must not brick the WAL: it still accepts a block and iterates.
	writeBlock(t, w, 1)
	require.NoError(t, w.Flush())
	require.Equal(t, []uint64{1}, collectBlocks(t, w, 1, 1))
}

// TestIteratorRangeErrorDoesNotBrick verifies that an out-of-range request on a non-empty WAL is reported as
// ErrIteratorRange and leaves the WAL usable, rather than bricking the wrapper.
func TestIteratorRangeErrorDoesNotBrick(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()
	for block := uint64(1); block <= 3; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())

	_, err := w.Iterator(1, 4) // end block beyond the latest stored block
	require.ErrorIs(t, err, seiwal.ErrIteratorRange)

	require.Equal(t, []uint64{1, 2, 3}, collectBlocks(t, w, 1, 3), "the WAL remains usable after a rejected range")
}

func TestIteratorFromMiddle(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()
	for block := uint64(1); block <= 5; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())

	require.Equal(t, []uint64{3, 4, 5}, collectBlocks(t, w, 3, 5))
}

func TestIteratorYieldsChangesetContents(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()

	cs := []*proto.NamedChangeSet{makeChangeSet("evm", []byte("key"), []byte("value"))}
	require.NoError(t, w.Write(1, cs))
	require.NoError(t, w.Flush())

	it, err := w.Iterator(1, 1)
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

// TestIteratorDoesNotSeePostConstructionBlocks confirms the snapshot contract at the wrapper level: an
// iterator yields only blocks that were complete when it was created.
func TestIteratorDoesNotSeePostConstructionBlocks(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()

	for block := uint64(1); block <= 3; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())

	it, err := w.Iterator(1, 3)
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
