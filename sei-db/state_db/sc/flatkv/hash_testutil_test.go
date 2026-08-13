package flatkv

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// awaitRootHash returns the lattice hash of the latest block the store has committed, waiting for the
// hasher to reach it.
//
// The hasher runs behind the execution thread, so a hash asked for right after a commit may not exist yet.
// Tests want the hash of the block they just wrote, so they wait for it.
func awaitRootHash(t testing.TB, store Store) []byte {
	t.Helper()
	require.NoError(t, store.FlushHashes())
	return store.PublishedHash().Hash
}

// awaitHashSeed returns the store's accumulated lattice state once the hasher has caught up with every block
// committed so far.
//
// A store with no hasher — a read-only store, or one that has not been loaded — answers with the state it read
// at load time, which describes the height it loaded.
func awaitHashSeed(t testing.TB, s *CommitStore) hasherSeed {
	t.Helper()
	if s.hasher == nil {
		return s.hashSeed
	}
	require.NoError(t, s.FlushHashes())
	seed, err := s.hasher.Seed()
	require.NoError(t, err)
	return seed
}

// awaitWorkingLtHash returns the store-wide lattice hash covering every block committed so far: the sum of
// every data database's root, which is how the store-wide root is derived.
func awaitWorkingLtHash(t testing.TB, s *CommitStore) *lthash.LtHash {
	t.Helper()
	seed := awaitHashSeed(t, s)
	global := lthash.New()
	for _, dir := range dataDBDirs {
		global.MixIn(seed.perDBLtHash[dir])
	}
	return global
}
