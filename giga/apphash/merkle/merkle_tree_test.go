package merkle

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	tmmerkle "github.com/sei-protocol/sei-chain/sei-tendermint/crypto/merkle"
)

// randomPreimages returns count leaf preimages of 0 to 64 random bytes, and their leaf hashes.
func randomPreimages(rng *rand.Rand, count int) ([][]byte, [][32]byte) {
	preimages := make([][]byte, 0, count)
	leafHashes := make([][32]byte, 0, count)
	for range count {
		preimage := make([]byte, rng.Intn(65))
		_, _ = rng.Read(preimage)
		preimages = append(preimages, preimage)
		leafHash := MerkleLeafHash(preimage)
		leafHashes = append(leafHashes, leafHash)
	}
	return preimages, leafHashes
}

func TestMerkleRootMatchesRFC6962(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for count := 0; count <= 64; count++ {
		preimages, leafHashes := randomPreimages(rng, count)
		root := MerkleRoot(leafHashes)
		require.Equal(t, tmmerkle.HashFromByteSlices(preimages), root[:], "count %d", count)
	}
}

func TestMerkleSiblingsLeadToRoot(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for count := 1; count <= 33; count++ {
		_, leafHashes := randomPreimages(rng, count)
		root := MerkleRoot(leafHashes)
		for index := range leafHashes {
			leafCount := uint64(count)
			leafIndex := uint64(index)
			siblings := MerkleSiblings(leafHashes, index)
			require.Len(t, siblings, MerkleSiblingCount(leafCount, leafIndex),
				"count %d index %d", count, index)
			computedRoot, err := MerkleRootFromSiblings(leafCount, leafIndex, leafHashes[index], siblings)
			require.NoError(t, err, "count %d index %d", count, index)
			require.Equal(t, root, computedRoot, "count %d index %d", count, index)
		}
	}
}

func TestMerkleRootFromSiblingsRejectsMalformedProofs(t *testing.T) {
	_, leafHashes := randomPreimages(rand.New(rand.NewSource(3)), 5)
	siblings := MerkleSiblings(leafHashes, 1)
	require.NotEmpty(t, siblings)

	testCases := map[string]struct {
		leafCount uint64
		leafIndex uint64
		siblings  [][32]byte
	}{
		"index equals count": {5, 5, siblings},
		"index past count":   {5, 6, siblings},
		"zero count":         {0, 0, nil},
		"missing sibling":    {5, 1, siblings[:len(siblings)-1]},
		"extra sibling":      {5, 1, append(append([][32]byte(nil), siblings...), [32]byte{})},
		"siblings for one":   {1, 0, siblings},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			_, err := MerkleRootFromSiblings(testCase.leafCount, testCase.leafIndex, leafHashes[1],
				testCase.siblings)
			require.Error(t, err)
		})
	}
}
