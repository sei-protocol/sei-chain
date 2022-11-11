package keeper_test

import (
	"bytes"
	"encoding/hex"
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

func TestAccountToValue(t *testing.T) {
	account := types.Account {
		Lamports: 123,
		RentEpoch: 456,
		Data: hex.EncodeToString([]byte("abc")),
	}
	expectedVal := []byte{149,84, 102, 209, 95, 175, 43, 23, 109, 170, 65, 141, 236, 74, 22, 129, 74, 243, 245, 225, 202, 22, 143, 202, 69, 254, 33, 247, 237, 138, 236, 175}

	value, err := keeper.AccountToValue(account)
	require.Nil(t, err)
	require.True(t, bytes.Equal(expectedVal, value))
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
