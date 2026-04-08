package persist

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func makeGlobalBlocks(rng utils.Rng, n int) []*types.Block {
	blocks := make([]*types.Block, n)
	for i := range blocks {
		blocks[i] = types.GenBlock(rng)
	}
	return blocks
}

func TestNewGlobalBlockPersisterEmptyDir(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	committee, _ := types.GenCommittee(rng, 3)
	fb := committee.FirstBlock()

	gp, err := NewGlobalBlockPersister(utils.Some(dir), committee)
	require.NoError(t, err)
	require.NotNil(t, gp)
	require.Equal(t, 0, len(gp.ConsumeLoaded()))
	require.Equal(t, fb, gp.Next())
	require.NoError(t, gp.Close())
}

func TestNewGlobalBlockPersisterNoop(t *testing.T) {
	rng := utils.TestRng()
	committee, _ := types.GenCommittee(rng, 3)
	fb := committee.FirstBlock()
	blocks := makeGlobalBlocks(rng, 5)

	gp, err := NewGlobalBlockPersister(utils.None[string](), committee)
	require.NoError(t, err)
	require.NotNil(t, gp)
	require.Equal(t, 0, len(gp.ConsumeLoaded()))

	for i, b := range blocks {
		require.NoError(t, gp.PersistBlock(fb+types.GlobalBlockNumber(i), b))
	}
	require.Equal(t, fb+5, gp.Next())

	// Truncate in no-op mode. Before the fix, truncateBefore wouldn't
	// advance s.next, so subsequent PersistBlock calls at higher numbers
	// would fail with "out of sequence".
	require.NoError(t, gp.TruncateBefore(fb+10))
	require.Equal(t, fb+10, gp.Next())

	newBlock := types.GenBlock(rng)
	require.NoError(t, gp.PersistBlock(fb+10, newBlock))
	require.Equal(t, fb+11, gp.Next())
	require.NoError(t, gp.Close())
}

func TestGlobalBlockPersistAndReload(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	committee, _ := types.GenCommittee(rng, 3)
	fb := committee.FirstBlock()
	blocks := makeGlobalBlocks(rng, 5)

	gp, err := NewGlobalBlockPersister(utils.Some(dir), committee)
	require.NoError(t, err)
	for i, b := range blocks {
		require.NoError(t, gp.PersistBlock(fb+types.GlobalBlockNumber(i), b))
	}
	require.Equal(t, fb+5, gp.Next())
	require.NoError(t, gp.Close())

	gp2, err := NewGlobalBlockPersister(utils.Some(dir), committee)
	require.NoError(t, err)
	loaded := gp2.ConsumeLoaded()
	require.Equal(t, 5, len(loaded))
	for i, lb := range loaded {
		require.Equal(t, fb+types.GlobalBlockNumber(i), lb.Number)
	}
	require.Equal(t, fb+5, gp2.Next())
	require.NoError(t, gp2.Close())
}

func TestGlobalBlockTruncateAndReload(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	committee, _ := types.GenCommittee(rng, 3)
	fb := committee.FirstBlock()
	blocks := makeGlobalBlocks(rng, 10)

	gp, err := NewGlobalBlockPersister(utils.Some(dir), committee)
	require.NoError(t, err)
	for i, b := range blocks {
		require.NoError(t, gp.PersistBlock(fb+types.GlobalBlockNumber(i), b))
	}
	require.NoError(t, gp.TruncateBefore(fb+5))
	require.NoError(t, gp.Close())

	gp2, err := NewGlobalBlockPersister(utils.Some(dir), committee)
	require.NoError(t, err)
	loaded := gp2.ConsumeLoaded()
	require.Equal(t, 5, len(loaded))
	require.Equal(t, fb+5, loaded[0].Number)
	require.Equal(t, fb+9, loaded[4].Number)
	require.Equal(t, fb+10, gp2.Next())
	require.NoError(t, gp2.Close())
}

func TestGlobalBlockTruncateAll(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	committee, _ := types.GenCommittee(rng, 3)
	fb := committee.FirstBlock()
	blocks := makeGlobalBlocks(rng, 5)

	gp, err := NewGlobalBlockPersister(utils.Some(dir), committee)
	require.NoError(t, err)
	for i, b := range blocks {
		require.NoError(t, gp.PersistBlock(fb+types.GlobalBlockNumber(i), b))
	}
	require.NoError(t, gp.TruncateBefore(fb+10))
	require.Equal(t, fb+10, gp.Next())
	require.NoError(t, gp.Close())

	gp2, err := NewGlobalBlockPersister(utils.Some(dir), committee)
	require.NoError(t, err)
	require.Equal(t, 0, len(gp2.ConsumeLoaded()))
	require.Equal(t, fb, gp2.Next())
	require.NoError(t, gp2.Close())
}

func TestGlobalBlockDuplicateIgnored(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	committee, _ := types.GenCommittee(rng, 3)
	fb := committee.FirstBlock()
	block := types.GenBlock(rng)

	gp, err := NewGlobalBlockPersister(utils.Some(dir), committee)
	require.NoError(t, err)
	require.NoError(t, gp.PersistBlock(fb, block))
	require.NoError(t, gp.PersistBlock(fb, block))
	require.Equal(t, fb+1, gp.Next())
	require.NoError(t, gp.Close())
}

func TestGlobalBlockGapError(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	committee, _ := types.GenCommittee(rng, 3)
	fb := committee.FirstBlock()
	block := types.GenBlock(rng)

	gp, err := NewGlobalBlockPersister(utils.Some(dir), committee)
	require.NoError(t, err)
	err = gp.PersistBlock(fb+2, block)
	require.Error(t, err)
	require.Contains(t, err.Error(), "out of sequence")
	require.NoError(t, gp.Close())
}

func TestGlobalBlockTruncateBeforeNoop(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	committee, _ := types.GenCommittee(rng, 3)
	fb := committee.FirstBlock()
	blocks := makeGlobalBlocks(rng, 5)

	gp, err := NewGlobalBlockPersister(utils.Some(dir), committee)
	require.NoError(t, err)
	for i, b := range blocks {
		require.NoError(t, gp.PersistBlock(fb+types.GlobalBlockNumber(i), b))
	}
	require.NoError(t, gp.TruncateBefore(0))
	require.Equal(t, fb+5, gp.Next())
	require.NoError(t, gp.Close())

	gp2, err := NewGlobalBlockPersister(utils.Some(dir), committee)
	require.NoError(t, err)
	require.Equal(t, 5, len(gp2.ConsumeLoaded()))
	require.NoError(t, gp2.Close())
}

func TestGlobalBlockContinueAfterReload(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	committee, _ := types.GenCommittee(rng, 3)
	fb := committee.FirstBlock()
	blocks := makeGlobalBlocks(rng, 10)

	gp, err := NewGlobalBlockPersister(utils.Some(dir), committee)
	require.NoError(t, err)
	for i := range 5 {
		require.NoError(t, gp.PersistBlock(fb+types.GlobalBlockNumber(i), blocks[i]))
	}
	require.NoError(t, gp.Close())

	gp2, err := NewGlobalBlockPersister(utils.Some(dir), committee)
	require.NoError(t, err)
	require.Equal(t, 5, len(gp2.ConsumeLoaded()))
	for i := 5; i < 10; i++ {
		require.NoError(t, gp2.PersistBlock(fb+types.GlobalBlockNumber(i), blocks[i]))
	}
	require.Equal(t, fb+10, gp2.Next())
	require.NoError(t, gp2.Close())

	gp3, err := NewGlobalBlockPersister(utils.Some(dir), committee)
	require.NoError(t, err)
	require.Equal(t, 10, len(gp3.ConsumeLoaded()))
	require.NoError(t, gp3.Close())
}

func TestGlobalBlockTruncateAfterMiddle(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	committee, _ := types.GenCommittee(rng, 3)
	fb := committee.FirstBlock()
	blocks := makeGlobalBlocks(rng, 5)

	// First session: persist 5 blocks.
	gp, err := NewGlobalBlockPersister(utils.Some(dir), committee)
	require.NoError(t, err)
	for i, b := range blocks {
		require.NoError(t, gp.PersistBlock(fb+types.GlobalBlockNumber(i), b))
	}
	require.NoError(t, gp.Close())

	// Second session: reload, then TruncateAfter trims WAL and loaded.
	gp2, err := NewGlobalBlockPersister(utils.Some(dir), committee)
	require.NoError(t, err)
	require.NoError(t, gp2.TruncateAfter(fb+3))
	require.Equal(t, fb+3, gp2.Next())
	loaded := gp2.ConsumeLoaded()
	require.Equal(t, 3, len(loaded))
	require.Equal(t, fb, loaded[0].Number)
	require.Equal(t, fb+2, loaded[2].Number)
	require.NoError(t, gp2.Close())

	// Third session: verify WAL persistence.
	gp3, err := NewGlobalBlockPersister(utils.Some(dir), committee)
	require.NoError(t, err)
	loaded = gp3.ConsumeLoaded()
	require.Equal(t, 3, len(loaded))
	require.Equal(t, fb, loaded[0].Number)
	require.Equal(t, fb+2, loaded[2].Number)
	require.NoError(t, gp3.Close())
}

func TestGlobalBlockTruncateAfterNoop(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	committee, _ := types.GenCommittee(rng, 3)
	fb := committee.FirstBlock()
	blocks := makeGlobalBlocks(rng, 3)

	gp, err := NewGlobalBlockPersister(utils.Some(dir), committee)
	require.NoError(t, err)
	for i, b := range blocks {
		require.NoError(t, gp.PersistBlock(fb+types.GlobalBlockNumber(i), b))
	}
	// TruncateAfter at or past the last block is a no-op.
	require.NoError(t, gp.TruncateAfter(fb+11))
	require.Equal(t, fb+3, gp.Next())
	require.NoError(t, gp.Close())
}

func TestGlobalBlockTruncateAfterBeforeFirst(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	committee, _ := types.GenCommittee(rng, 3)
	fb := committee.FirstBlock()
	blocks := makeGlobalBlocks(rng, 5)

	gp, err := NewGlobalBlockPersister(utils.Some(dir), committee)
	require.NoError(t, err)
	for i, b := range blocks {
		require.NoError(t, gp.PersistBlock(fb+types.GlobalBlockNumber(i), b))
	}
	// Truncate first 2 blocks, leaving [fb+2, fb+5).
	require.NoError(t, gp.TruncateBefore(fb+2))
	require.Equal(t, fb+5, gp.Next())

	// TruncateAfter at or before the first remaining block removes everything.
	require.NoError(t, gp.TruncateAfter(fb+2))
	require.Equal(t, fb+2, gp.Next()) // cursor stays at firstGlobal
	require.NoError(t, gp.Close())

	// Reload — should be empty.
	gp2, err := NewGlobalBlockPersister(utils.Some(dir), committee)
	require.NoError(t, err)
	require.Equal(t, 0, len(gp2.ConsumeLoaded()))
	require.NoError(t, gp2.Close())
}
