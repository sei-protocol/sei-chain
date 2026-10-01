package bud

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBUDSpecVector pins every value of the test vector in bud_spec.md.
func TestBUDSpecVector(t *testing.T) {
	budlets := []*Budlet{
		newTestBudlet(t, "evm/a", []byte{0xaa, 0xbb}, 0),
		newTestDeletionBudlet(t, "evm/b", 7),
		newTestBudlet(t, "evm/c", []byte{0xcc}, 0x0102030405060708),
	}
	serializations := []string{
		"00000005" + "65766d2f61" + "00" + "00000002" + "aabb" + "0000000000000000",
		"00000005" + "65766d2f62" + "01" + "00000000" + "" + "0000000000000007",
		"00000005" + "65766d2f63" + "00" + "00000001" + "cc" + "0102030405060708",
	}
	leafHashes := []string{
		"35d10b1d4b1150c9e192fd6e3990c335059a082b034552b0c8c5fb84b18b8120",
		"658e7daf30805c8bea7e7128f4776983b4bfcb2baed616d2ce4aa72c5aa6bb02",
		"c28796d5c82fad7b487cb8abf8141c7a70b593fca1ed6066f910bef4aa873fd3",
	}
	inner01 := "ba10a4fe9d2471b43c2962f3316edc63cca7affe2b6cf3b5d26b2b5dd34356fc"
	root := "9e7ca3a63b3fe038f0eccb200772166d2c8a4cdaaf9370f8cd19dcfe588944da"
	bud := "84f9b7bcac72e99f8e8da42848209a1ec0587f8b8d2b7c4c1e7ba7da9e3bd29f"
	serializedTree := "01" + "0000000000000003" + serializations[0] + serializations[1] + serializations[2]
	proofs := []string{
		"01" + "01" + serializations[0] + "0000000000000003" + "0000000000000000" +
			leafHashes[1] + leafHashes[2],
		"01" + "01" + serializations[1] + "0000000000000003" + "0000000000000001" +
			leafHashes[0] + leafHashes[2],
		"01" + "01" + serializations[2] + "0000000000000003" + "0000000000000002" +
			inner01,
	}

	leaves := budLeafHashes(budlets)
	for i, budlet := range budlets {
		require.Equal(t, serializations[i], hex.EncodeToString(budlet.Serialize()),
			"budlet %d serialization", i)
		require.Equal(t, leafHashes[i], hex.EncodeToString(leaves[i][:]), "budlet %d leaf hash", i)
	}
	computedInner01 := budInnerHash(leaves[0], leaves[1])
	require.Equal(t, inner01, hex.EncodeToString(computedInner01[:]))
	computedRoot := budTreeRoot(leaves)
	require.Equal(t, root, hex.EncodeToString(computedRoot[:]))

	tree, err := NewBUDTree(budlets)
	require.NoError(t, err)
	computedBUD := tree.BUD()
	require.Equal(t, bud, hex.EncodeToString(computedBUD[:]))
	require.Equal(t, serializedTree, hex.EncodeToString(tree.Serialize()))

	for i, budlet := range budlets {
		proof, found := tree.BuildBUDProof(budlet.Key())
		require.True(t, found)
		require.Equal(t, proofs[i], hex.EncodeToString(proof.Serialize()), "budlet %d proof", i)
	}
}
