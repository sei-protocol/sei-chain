package rootmulti

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestFlatKVOnlyHeightAdvancesOncePerBlock pins that a block advances the chain by exactly one height
// in flatkv_only mode.
//
// It is the assertion whose absence let a double commit live in this path. Taking a block's hash seals
// it on flatkv, because flatkv has no hash until a block is closed off; a commit that then derived its
// own next height from flatkv's state would land one block further on and seal an empty block nobody
// agreed to. An empty block moves no lattice hash, so nothing about the app hash looked wrong — only
// the height, which no flatkv_only test compared against the block count.
//
// The equivalent dual-write assertions already existed and passed, because memiavl supplies the height
// there. This covers the mode that has no memiavl to ask.
func TestFlatKVOnlyHeightAdvancesOncePerBlock(t *testing.T) {
	dir := t.TempDir()
	store, storeKeys := newTestRootMulti(t, dir, flatKVOnlyConfig())
	evmData := newEVMTestData(0x71)

	const blocks = 6
	for block := 1; block <= blocks; block++ {
		rec := simulateFlatKVOnlyBlock(t, store, storeKeys, block, evmData)
		require.Equalf(t, int64(block), rec.version,
			"block %d must commit as height %d; the working-hash request must not advance the height on its "+
				"own", block, block)
	}
	require.Equal(t, int64(blocks), store.LastCommitID().Version)

	// The height has to survive a reopen: a spurious extra block is durable, so a store that
	// double-committed would come back at twice the block count.
	require.NoError(t, store.Close())
	reopened, _ := newTestRootMulti(t, dir, flatKVOnlyConfig())
	defer func() { require.NoError(t, reopened.Close()) }()
	require.Equal(t, int64(blocks), reopened.LastCommitID().Version,
		"the committed height must be %d after reopening, not a doubled one", blocks)
}

// TestFlatKVOnlySeededChainBuildsItsInitialHeightFirst covers a chain whose first block is not 1.
//
// InitChain seeds the backends when the configured initial height is above 1, and the height the
// commit store is then asked for comes from the multistore rather than from a backend. The two cannot
// supply it themselves: flatkv reflects a seed immediately while memiavl does not apply it until its
// first commit, so between seeding and that commit they disagree.
func TestFlatKVOnlySeededChainBuildsItsInitialHeightFirst(t *testing.T) {
	store, storeKeys := newTestRootMulti(t, t.TempDir(), flatKVOnlyConfig())
	defer func() { require.NoError(t, store.Close()) }()
	evmData := newEVMTestData(0x82)

	const initialHeight = 50
	require.NoError(t, store.SetInitialVersion(initialHeight))

	rec := simulateFlatKVOnlyBlock(t, store, storeKeys, 1, evmData)
	require.Equal(t, int64(initialHeight), rec.version,
		"the first block of a chain seeded to start at %d must commit as %d", initialHeight, initialHeight)

	rec = simulateFlatKVOnlyBlock(t, store, storeKeys, 2, evmData)
	require.Equal(t, int64(initialHeight+1), rec.version, "and the next block continues from there")
}

// TestFlatKVOnlyEmptyBlocksAdvanceOnce pins the same property for blocks that write nothing.
//
// Empty blocks are where the defect hid: flatkv advances on an empty commit by design, so the spurious
// second commit looked exactly like a legitimate empty block and was accepted by the contiguity rule.
func TestFlatKVOnlyEmptyBlocksAdvanceOnce(t *testing.T) {
	store, _ := newTestRootMulti(t, t.TempDir(), flatKVOnlyConfig())
	defer func() { require.NoError(t, store.Close()) }()

	for block := 1; block <= 4; block++ {
		rec := finalizeBlock(t, store)
		require.Equalf(t, int64(block), rec.version,
			"an empty block must advance the height by one, not two (block %d)", block)
	}
	require.Equal(t, int64(4), store.LastCommitID().Version)
}
