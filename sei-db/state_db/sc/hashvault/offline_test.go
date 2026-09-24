package hashvault

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A vault that was never created records nothing, which is what a node booting for the first time finds.
func TestStoredRangeOfAMissingVault(t *testing.T) {
	_, _, recorded, err := StoredRange(testConfig(t, true))
	require.NoError(t, err)
	require.False(t, recorded)
}

// A vault that exists but holds no hashes records nothing either.
func TestStoredRangeOfAnEmptyVault(t *testing.T) {
	cfg := testConfig(t, true)
	v, err := Open(cfg)
	require.NoError(t, err)
	require.NoError(t, v.Close())

	_, _, recorded, err := StoredRange(cfg)
	require.NoError(t, err)
	require.False(t, recorded)
}

// The range read offline is the one the vault holds when open.
func TestStoredRangeOfAPopulatedVault(t *testing.T) {
	cfg := testConfig(t, true)
	v, err := Open(cfg)
	require.NoError(t, err)
	commitRange(t, v, 4, 9)
	require.NoError(t, v.Close())

	oldest, newest, recorded, err := StoredRange(cfg)
	require.NoError(t, err)
	require.True(t, recorded)
	require.Equal(t, uint64(4), oldest)
	require.Equal(t, uint64(9), newest)
}

// PruneAfter keeps the hashes up to its block and drops the rest, and the vault reopens on what is kept.
func TestPruneAfterKeepsThePrefix(t *testing.T) {
	cfg := testConfig(t, true)
	v, err := Open(cfg)
	require.NoError(t, err)
	commitRange(t, v, 1, 9)
	require.NoError(t, v.Close())

	require.NoError(t, PruneAfter(cfg, 5))

	_, newest, recorded, err := StoredRange(cfg)
	require.NoError(t, err)
	require.True(t, recorded)
	require.Equal(t, uint64(5), newest)

	reopened := openVault(t, cfg)
	requireHash(t, reopened, 5, hashOf(5))
	require.NoError(t, reopened.Commit(6, hashOf(0xEE)), "commits carry on from the kept prefix")
}

// Pruning below every recorded hash leaves an empty vault, and pruning a missing one does nothing.
func TestPruneAfterBelowEveryHashEmptiesTheVault(t *testing.T) {
	cfg := testConfig(t, true)
	require.NoError(t, PruneAfter(cfg, 0), "a vault that was never created has nothing to prune")

	v, err := Open(cfg)
	require.NoError(t, err)
	commitRange(t, v, 3, 5)
	require.NoError(t, v.Close())

	require.NoError(t, PruneAfter(cfg, 2))
	_, _, recorded, err := StoredRange(cfg)
	require.NoError(t, err)
	require.False(t, recorded)
}
