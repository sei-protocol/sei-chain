package persist

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func makeGlobalBlock(rng utils.Rng) *types.Block {
	lane := types.GenSecretKey(rng).Public()
	return types.NewBlock(lane, 0, types.GenBlockHeaderHash(rng), types.GenPayload(rng))
}

func makeGlobalBlocks(rng utils.Rng, n int) []*types.Block {
	blocks := make([]*types.Block, n)
	for i := range blocks {
		blocks[i] = makeGlobalBlock(rng)
	}
	return blocks
}

func TestNewGlobalBlockPersisterEmptyDir(t *testing.T) {
	dir := t.TempDir()
	gp, err := NewGlobalBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	require.NotNil(t, gp)
	require.Equal(t, 0, len(gp.LoadedBlocks()))
	require.Equal(t, types.GlobalBlockNumber(0), gp.LoadNext())
	require.NoError(t, gp.Close())
}

func TestNewGlobalBlockPersisterNoop(t *testing.T) {
	gp, err := NewGlobalBlockPersister(utils.None[string]())
	require.NoError(t, err)
	require.NotNil(t, gp)
	require.Equal(t, 0, len(gp.LoadedBlocks()))

	rng := utils.TestRng()
	block := makeGlobalBlock(rng)
	require.NoError(t, gp.PersistBlock(0, block))
	require.Equal(t, types.GlobalBlockNumber(1), gp.LoadNext())
	require.NoError(t, gp.Close())
}

func TestGlobalBlockPersistAndReload(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	blocks := makeGlobalBlocks(rng, 5)

	gp, err := NewGlobalBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	_ = gp.LoadedBlocks()
	for i, b := range blocks {
		require.NoError(t, gp.PersistBlock(types.GlobalBlockNumber(i), b))
	}
	require.Equal(t, types.GlobalBlockNumber(5), gp.LoadNext())
	require.NoError(t, gp.Close())

	gp2, err := NewGlobalBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	loaded := gp2.LoadedBlocks()
	require.Equal(t, 5, len(loaded))
	for i, lb := range loaded {
		require.Equal(t, types.GlobalBlockNumber(i), lb.Number)
	}
	require.Equal(t, types.GlobalBlockNumber(5), gp2.LoadNext())
	require.NoError(t, gp2.Close())
}

func TestGlobalBlockTruncateAndReload(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	blocks := makeGlobalBlocks(rng, 10)

	gp, err := NewGlobalBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	_ = gp.LoadedBlocks()
	for i, b := range blocks {
		require.NoError(t, gp.PersistBlock(types.GlobalBlockNumber(i), b))
	}
	require.NoError(t, gp.TruncateBefore(5))
	require.NoError(t, gp.Close())

	gp2, err := NewGlobalBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	loaded := gp2.LoadedBlocks()
	require.Equal(t, 5, len(loaded))
	require.Equal(t, types.GlobalBlockNumber(5), loaded[0].Number)
	require.Equal(t, types.GlobalBlockNumber(9), loaded[4].Number)
	require.Equal(t, types.GlobalBlockNumber(10), gp2.LoadNext())
	require.NoError(t, gp2.Close())
}

func TestGlobalBlockTruncateAll(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	blocks := makeGlobalBlocks(rng, 5)

	gp, err := NewGlobalBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	_ = gp.LoadedBlocks()
	for i, b := range blocks {
		require.NoError(t, gp.PersistBlock(types.GlobalBlockNumber(i), b))
	}
	require.NoError(t, gp.TruncateBefore(10))
	require.Equal(t, types.GlobalBlockNumber(10), gp.LoadNext())
	require.NoError(t, gp.Close())

	gp2, err := NewGlobalBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	loaded := gp2.LoadedBlocks()
	require.Equal(t, 0, len(loaded))
	require.Equal(t, types.GlobalBlockNumber(0), gp2.LoadNext())
	require.NoError(t, gp2.Close())
}

func TestGlobalBlockDuplicateIgnored(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	block := makeGlobalBlock(rng)

	gp, err := NewGlobalBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	_ = gp.LoadedBlocks()
	require.NoError(t, gp.PersistBlock(0, block))
	require.NoError(t, gp.PersistBlock(0, block))
	require.Equal(t, types.GlobalBlockNumber(1), gp.LoadNext())
	require.NoError(t, gp.Close())
}

func TestGlobalBlockGapError(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	block := makeGlobalBlock(rng)

	gp, err := NewGlobalBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	_ = gp.LoadedBlocks()
	err = gp.PersistBlock(2, block)
	require.Error(t, err)
	require.Contains(t, err.Error(), "out of sequence")
	require.NoError(t, gp.Close())
}

func TestGlobalBlockTruncateBeforeNoop(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	blocks := makeGlobalBlocks(rng, 5)

	gp, err := NewGlobalBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	_ = gp.LoadedBlocks()
	for i, b := range blocks {
		require.NoError(t, gp.PersistBlock(types.GlobalBlockNumber(i), b))
	}
	require.NoError(t, gp.TruncateBefore(0))
	require.Equal(t, types.GlobalBlockNumber(5), gp.LoadNext())
	require.NoError(t, gp.Close())

	gp2, err := NewGlobalBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	loaded := gp2.LoadedBlocks()
	require.Equal(t, 5, len(loaded))
	require.NoError(t, gp2.Close())
}

func TestGlobalBlockContinueAfterReload(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	blocks := makeGlobalBlocks(rng, 10)

	gp, err := NewGlobalBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	_ = gp.LoadedBlocks()
	for i := range 5 {
		require.NoError(t, gp.PersistBlock(types.GlobalBlockNumber(i), blocks[i]))
	}
	require.NoError(t, gp.Close())

	gp2, err := NewGlobalBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	loaded := gp2.LoadedBlocks()
	require.Equal(t, 5, len(loaded))
	for i := 5; i < 10; i++ {
		require.NoError(t, gp2.PersistBlock(types.GlobalBlockNumber(i), blocks[i]))
	}
	require.Equal(t, types.GlobalBlockNumber(10), gp2.LoadNext())
	require.NoError(t, gp2.Close())

	gp3, err := NewGlobalBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	loaded = gp3.LoadedBlocks()
	require.Equal(t, 10, len(loaded))
	require.NoError(t, gp3.Close())
}
