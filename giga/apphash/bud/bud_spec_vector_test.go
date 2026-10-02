package bud

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/apphash"
)

// TestBUDSpecVector pins every value of the budlet, BUD tree, BUD, and BUD proof test vectors in bud_spec.md.
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
		deserialized, err := DeserializeBudlet(decodeSpecHex(t, serializations[i]))
		require.NoError(t, err, "budlet %d", i)
		require.Equal(t, budlet, deserialized, "budlet %d", i)
	}
	deserializedTree, err := DeserializeBUDTree(decodeSpecHex(t, serializedTree))
	require.NoError(t, err)
	require.Equal(t, tree, deserializedTree)

	for i, budlet := range budlets {
		proof, found := tree.BuildBUDProof(budlet.Key())
		require.True(t, found)
		require.Equal(t, proofs[i], hex.EncodeToString(proof.Serialize()), "budlet %d proof", i)

		deserializedProof, err := DeserializeBUDProof(decodeSpecHex(t, proofs[i]))
		require.NoError(t, err, "budlet %d proof", i)
		require.Equal(t, proof, deserializedProof, "budlet %d proof", i)
		deserializedBUD := deserializedProof.ComputeBUD()
		require.Equal(t, bud, hex.EncodeToString(deserializedBUD[:]), "budlet %d proof", i)
	}
}

// TestBUDSpecEmptyTreeVector pins the serialized BUD tree and the BUD of a block with no budlets in bud_spec.md.
func TestBUDSpecEmptyTreeVector(t *testing.T) {
	serializedTree := "01" + "0000000000000000"
	bud := "3eeeba2dfb311dad9e9e46eba984fa855be2b872496b921b19da52b3eb09d5e2"

	tree, err := NewBUDTree(nil)
	require.NoError(t, err)
	require.Equal(t, serializedTree, hex.EncodeToString(tree.Serialize()))
	computedBUD := tree.BUD()
	require.Equal(t, bud, hex.EncodeToString(computedBUD[:]))

	deserialized, err := DeserializeBUDTree(decodeSpecHex(t, serializedTree))
	require.NoError(t, err)
	require.Empty(t, deserialized.Budlets())
	deserializedBUD := deserialized.BUD()
	require.Equal(t, bud, hex.EncodeToString(deserializedBUD[:]))
}

// TestBUDSpecStateProofVector pins every value of the BUD state proof test vector in bud_spec.md.
func TestBUDSpecStateProofVector(t *testing.T) {
	const chainID = 0x1112131415161718
	filled := func(b byte) [32]byte {
		return [32]byte(bytes.Repeat([]byte{b}, 32))
	}

	tree7, err := NewBUDTree([]*Budlet{newTestBudlet(t, "evm/b", []byte{0xbb}, 0)})
	require.NoError(t, err)
	tree9, err := NewBUDTree([]*Budlet{
		newTestBudlet(t, "evm/a", []byte{0xaa, 0xbb}, 0),
		newTestDeletionBudlet(t, "evm/b", 7),
		newTestBudlet(t, "evm/c", []byte{0xcc}, 0x0102030405060708),
	})
	require.NoError(t, err)
	appHashData7 := apphash.NewAppHashData(
		chainID, 7, filled(0xa7), filled(0xb7), tree7.BUD(), filled(0xd7), filled(0xe7))
	appHashData9 := apphash.NewAppHashData(
		chainID, 9, filled(0xa9), filled(0xb9), tree9.BUD(), filled(0xd9), filled(0xe9))
	proof7, found := tree7.BuildBUDProof([]byte("evm/b"))
	require.True(t, found)
	proof9, found := tree9.BuildBUDProof([]byte("evm/b"))
	require.True(t, found)
	stateProof, err := NewBUDStateProof(
		[]*apphash.AppHashData{appHashData7, appHashData9}, []*BUDProof{proof7, proof9})
	require.NoError(t, err)

	budlet7 := "00000005" + "65766d2f62" + "00" + "00000001" + "bb" + "0000000000000000"
	leafHash7 := "1972da2c96a40269ddcd0ba29f92f10524fb0c2fc887d8fd5f92d5092c6ce323"
	bud7 := "8e8700ec280c2c50dab3033731c23e42ed19f966045a78846345e0b9faf069f4"
	bud9 := "84f9b7bcac72e99f8e8da42848209a1ec0587f8b8d2b7c4c1e7ba7da9e3bd29f"
	serializedAppHashData7 := "01" + "1112131415161718" + "0000000000000007" + strings.Repeat("a7", 32) +
		strings.Repeat("b7", 32) + bud7 + strings.Repeat("d7", 32) + strings.Repeat("e7", 32)
	serializedAppHashData9 := "01" + "1112131415161718" + "0000000000000009" + strings.Repeat("a9", 32) +
		strings.Repeat("b9", 32) + bud9 + strings.Repeat("d9", 32) + strings.Repeat("e9", 32)
	appHash7 := "6b0549eb2057bb970ef17a2ace1feeec1aa592e0c2cc1667744b89958204a943"
	appHash9 := "84016c34d2bc2ca0bed325790ab94d8a7838fbf6066c252f94cae1731e49dab4"
	serializedProof7 := "01" + "01" + budlet7 + "0000000000000001" + "0000000000000000"
	serializedProof9 := "01" + "01" +
		"00000005" + "65766d2f62" + "01" + "00000000" + "" + "0000000000000007" +
		"0000000000000003" + "0000000000000001" +
		"35d10b1d4b1150c9e192fd6e3990c335059a082b034552b0c8c5fb84b18b8120" +
		"c28796d5c82fad7b487cb8abf8141c7a70b593fca1ed6066f910bef4aa873fd3"
	serializedStateProof := "01" + "02" +
		"000000b1" + serializedAppHashData7 + serializedProof7 +
		"000000b1" + serializedAppHashData9 + serializedProof9

	require.Equal(t, budlet7, hex.EncodeToString(tree7.Budlets()[0].Serialize()))
	leaves7 := budLeafHashes(tree7.Budlets())
	require.Equal(t, leafHash7, hex.EncodeToString(leaves7[0][:]))
	computedBUD7 := tree7.BUD()
	require.Equal(t, bud7, hex.EncodeToString(computedBUD7[:]))
	require.Equal(t, serializedAppHashData7, hex.EncodeToString(appHashData7.Serialize()))
	require.Equal(t, serializedAppHashData9, hex.EncodeToString(appHashData9.Serialize()))
	require.Equal(t, serializedProof7, hex.EncodeToString(proof7.Serialize()))
	require.Equal(t, serializedProof9, hex.EncodeToString(proof9.Serialize()))
	require.Equal(t, serializedStateProof, hex.EncodeToString(stateProof.Serialize()))

	deserialized, err := DeserializeBUDStateProof(decodeSpecHex(t, serializedStateProof))
	require.NoError(t, err)
	require.Equal(t, stateProof, deserialized)
	require.Equal(t, uint64(chainID), deserialized.ChainID())
	appHashes := deserialized.AppHashes()
	require.Equal(t, appHash7, hex.EncodeToString(appHashes[0][:]))
	require.Equal(t, appHash9, hex.EncodeToString(appHashes[1][:]))

	for _, height := range []uint64{7, 8} {
		value, covered := deserialized.ValueAt(height)
		require.True(t, covered, "height %d", height)
		require.Equal(t, []byte{0xbb}, value, "height %d", height)
	}
	value, covered := deserialized.ValueAt(9)
	require.True(t, covered)
	require.Nil(t, value)
	for _, height := range []uint64{6, 10} {
		_, covered := deserialized.ValueAt(height)
		require.False(t, covered, "height %d", height)
	}
}

// decodeSpecHex decodes a hex string from bud_spec.md, failing the test if it does not parse.
func decodeSpecHex(t *testing.T, encoded string) []byte {
	t.Helper()

	decoded, err := hex.DecodeString(encoded)
	require.NoError(t, err)
	return decoded
}
