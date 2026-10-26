package receipthash

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/merkle"
)

// randomPreimages returns count leaf preimages of 0 to 64 random bytes, and their leaf hashes.
func randomPreimages(rng *rand.Rand, count int) ([][]byte, [][32]byte) {
	preimages := make([][]byte, 0, count)
	leafHashes := make([][32]byte, 0, count)
	for range count {
		preimage := make([]byte, rng.Intn(65))
		_, _ = rng.Read(preimage)
		preimages = append(preimages, preimage)
		leafHash := merkleLeafHash(preimage)
		leafHashes = append(leafHashes, leafHash)
	}
	return preimages, leafHashes
}

func TestMerkleRootMatchesRFC6962(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for count := 1; count <= 64; count++ {
		preimages, leafHashes := randomPreimages(rng, count)
		root := merkleRoot(leafHashes)
		require.Equal(t, merkle.HashFromByteSlices(preimages), root[:], "count %d", count)
	}
}

func TestMerkleRootOfNoLeaves(t *testing.T) {
	require.Equal(t, [32]byte{}, merkleRoot(nil))
}

func TestMerklePathLeadsToRoot(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for count := 1; count <= 33; count++ {
		_, leafHashes := randomPreimages(rng, count)
		root := merkleRoot(leafHashes)
		for index := range leafHashes {
			path := newMerklePath(leafHashes, index)
			require.Equal(t, root, path.root(uint64(count), leafHashes[index]),
				"count %d index %d", count, index)
		}
	}
}
