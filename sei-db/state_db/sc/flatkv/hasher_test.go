package flatkv

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
)

// hashLagWorkloadBlocks is how many blocks the hash-lag comparison writes. Enough that the lagging store's
// hasher is genuinely several blocks behind, since a hash is only interesting here if it was produced while
// the execution thread had moved on.
const hashLagWorkloadBlocks = 24

// TestBlockHashesDoNotDependOnHashLag pins the claim this whole mechanism rests on: a block's lattice hash is
// the same value whether the hasher produced it while execution waited or many blocks later.
//
// One store waits for each block's hash before committing the next, the other never waits until the end. Both
// see the same writes, so every block's hash must agree, block by block.
func TestBlockHashesDoNotDependOnHashLag(t *testing.T) {
	synchronous := hashesForWorkload(t, true)
	lagging := hashesForWorkload(t, false)

	require.Len(t, lagging, len(synchronous))
	for version, want := range synchronous {
		require.Equal(t, want, lagging[version], "hash for block %d depends on when it was hashed", version)
	}
}

// hashesForWorkload runs the same workload against a fresh store and returns each block's hash keyed by
// version. When waitPerBlock is set the hasher is drained after every commit, keeping it at most one block
// behind; otherwise it runs as far behind as it likes and the hashes are collected at the end.
func hashesForWorkload(t *testing.T, waitPerBlock bool) map[int64][]byte {
	t.Helper()
	store := setupTestStoreWithConfig(t, config.DefaultTestConfig(t))
	defer func() { require.NoError(t, store.Close()) }()

	for block := 1; block <= hashLagWorkloadBlocks; block++ {
		version := store.Version() + 1
		require.NoError(t, store.ApplyChangeSets(version, hashLagChangeSets(block)))
		_, err := store.Commit(version)
		require.NoError(t, err)
		if waitPerBlock {
			require.NoError(t, store.FlushHashes())
		}
	}
	require.NoError(t, store.FlushHashes())

	hashes := make(map[int64][]byte, hashLagWorkloadBlocks)
	for len(hashes) < hashLagWorkloadBlocks {
		published := <-store.HashChan()
		hashes[published.BlockHeight] = published.Hash
	}
	return hashes
}

// hashLagChangeSets returns one block's writes: a new storage slot, an overwrite of the slot the previous
// block wrote, and an account nonce. The overwrite is the part that matters — it is the case that reads the
// previous block's value, which is what a lagging hasher has to get right.
func hashLagChangeSets(block int) []*proto.NamedChangeSet {
	//nolint:gosec // G115 - block counts in this test are tiny
	current := byte(block)
	pairs := []*proto.KVPair{
		storagePair(addrN(current), slotN(current), padLeft32(current)),
		noncePair(addrN(current), uint64(block)),
	}
	if block > 1 {
		previous := current - 1
		pairs = append(pairs, storagePair(addrN(previous), slotN(previous), padLeft32(previous, 0xFF)))
	}
	return []*proto.NamedChangeSet{namedCS(pairs...)}
}
