package keeper_test

import (
	"testing"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/nitro/keeper"
	"github.com/sei-protocol/sei-chain/x/nitro/types"
	"github.com/stretchr/testify/require"
)

func TestValidateValidProof(t *testing.T) {
	k, _ := keepertest.NitroKeeper(t)
	root, proof := createMockMerkleProof()
	require.Nil(t, k.Validate(root, proof))
}

func TestValidateInvalidProof(t *testing.T) {
	k, _ := keepertest.NitroKeeper(t)

	// wrong hash
	root, proof := createMockMerkleProof()
	proof.Hash[0] = "efg"
	require.NotNil(t, k.Validate(root, proof))

	// wrong direction
	root, proof = createMockMerkleProof()
	proof.Direction[0] = 1
	require.NotNil(t, k.Validate(root, proof))
}

func createMockMerkleProof() ([]byte, *types.MerkleProof) {
	node0 := "123"
	node1 := "abc"
	node2 := "efg"
	node3 := "hij"

	merkleRoot := keeper.Hash(keeper.Hash([]byte(node2), keeper.Hash([]byte(node1), []byte(node0))), []byte(node3))
	direction := []int64{-1, -1, 1}
	hash := []string{node1, node2, node3}

	return merkleRoot, &types.MerkleProof{
		Hash:       hash,
		Direction:  direction,
		Commitment: node0,
	}
}
