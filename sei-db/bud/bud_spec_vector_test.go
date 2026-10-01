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
		"2c04f8ef01f65fcf00309d0eb3b513fbd67f963c84bdced043becfa555a81984",
		"a9ffe6eeb707bdc90e75cedc80ddd41cea6b6d99b56a00b8deb832942f11406d",
		"e9d985609a1790ef720997688285b872e4e3b5b0767c7a55c8eb339bbd9e6a56",
	}
	inner01 := "c61a514b106ecf34b8371ad4af73a1d8d0c00962f9979128da71682bbb211ca2"
	root := "aa81ff328275dcf86485ec05be9a5a6eb47ebb242e3492e3be0d91fe0188f368"
	bud := "a60f1c2ddcd436250a3634bd33732a485610bfd1047283d6c5d63cad526cf86d"
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
