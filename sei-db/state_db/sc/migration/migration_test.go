package migration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// Do operations on regular flatKV and memIAVL databases to verify that the test framework is sane.
func TestBasisCase(t *testing.T) {

	stores := []string{"bank", "evm", "aaa", "bbb", "ccc", "ddd"}

	memiavlDB := NewTestMemIAVLCommitStore(t, stores)
	memiavlRouter := NewTestMemIAVLRouter(t, memiavlDB)

	flatKVDB := NewTestFlatKVCommitStore(t)
	flatKVRouter := NewTestFlatKVRouter(t, flatKVDB)

	inMemoryRouter := NewTestInMemoryRouter()

	keysInUse := make(map[keyPair]struct{})

	multiRouter := NewTestMultiRouter(t, inMemoryRouter, memiavlRouter, flatKVRouter)

	SimulateBlocks(t,
		multiRouter,
		keysInUse,
		stores,
		100, // reads per block
		100, // updates per block
		20,  // deletes per block
		100, // new keys per block
		100, // blocks to simulate
	)

	// Verify that both backends contain all the data the oracle knows about.
	inMemoryRouter.VerifyContainsSameData(t, memiavlRouter)
	inMemoryRouter.VerifyContainsSameData(t, flatKVRouter)

	// Key count check: the oracle knows the exact number of live logical keys.
	// Both backends must contain exactly that many keys. This rules out any
	// phantom keys (extra rows) that VerifyContainsSameData cannot detect.
	expectedKeyCount := int64(len(keysInUse))
	require.Equal(t, expectedKeyCount, GetMemIAVLKeyCount(t, memiavlDB), "memiavl key count")
	require.Equal(t, expectedKeyCount, GetFlatKVKeyCount(t, flatKVDB), "flatkv key count")

	// Hash consistency check: bulk-load the oracle's final state into fresh
	// backends and confirm they produce identical hashes to the incrementally-
	// built instances. This validates that the hash functions are
	// order-independent (LtHash for flatKV, Merkle for memiavl).
	fullCS := inMemoryRouter.ToChangeSets()
	ctx := context.Background()

	freshFlatKVDB := NewTestFlatKVCommitStore(t)
	freshFlatKVRouter := NewTestFlatKVRouter(t, freshFlatKVDB)
	require.NoError(t, freshFlatKVRouter.ApplyChangeSets(ctx, fullCS), "fresh flatKV apply")

	freshMemIAVLDB := NewTestMemIAVLCommitStore(t, stores)
	freshMemIAVLRouter := NewTestMemIAVLRouter(t, freshMemIAVLDB)
	require.NoError(t, freshMemIAVLRouter.ApplyChangeSets(ctx, fullCS), "fresh memiavl apply")

	require.Equal(t, flatKVDB.CommittedRootHash(), freshFlatKVDB.CommittedRootHash(),
		"flatKV hash mismatch between incremental and bulk-loaded instances")
	require.Equal(t, GetMemIAVLStoreHashes(t, memiavlDB), GetMemIAVLStoreHashes(t, freshMemIAVLDB),
		"memiavl store hash mismatch between incremental and bulk-loaded instances")

}
