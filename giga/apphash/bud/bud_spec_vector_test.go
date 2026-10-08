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
		newTestBudlet(t, "evm/a", []byte{0xaa, 0xbb}, nil, 0),
		newTestDeletionBudlet(t, "evm/b", []byte{0xbb}, 7),
		newTestBudlet(t, "evm/c", []byte{0xcc}, []byte{}, 8),
	}
	serializations := []string{
		"00000005" + "65766d2f61" + "00" + "00000002" + "aabb" + "01" + "00000000" + "" + "0000000000000000",
		"00000005" + "65766d2f62" + "01" + "00000000" + "" + "00" + "00000001" + "bb" + "0000000000000007",
		"00000005" + "65766d2f63" + "00" + "00000001" + "cc" + "00" + "00000000" + "" + "0000000000000008",
	}
	leafHashes := []string{
		"377d389fc54fdd44a82596916704d7a16b64d58aeb1338ad26c9e1fff1600d13",
		"6eddccfbc096698bc35e0a76ca7d48b09b1dc6962705dd9cc12ec193fd7707f2",
		"a71d74fa15dd6ba2179fe0f1f1ba64ace69008694375dd4454579aa75681c7f4",
	}
	inner01 := "01c14ff18fc0f5d92d66e7ae9bac8da08f8e4e37443856314bfb2c4be4bb05bf"
	root := "42826a8e6a7b26f4a8d61db2694f601b3c1e33238aa47b36cbcaa299eef0ee12"
	bud := "cb9f3601d13115bd8f430f469e52d64709857df23f068bfa2aba3128eea8ebef"
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

	tree, err := NewBUDTree([]*Budlet{
		newTestBudlet(t, "evm/a", []byte{0xaa, 0xbb}, nil, 0),
		newTestDeletionBudlet(t, "evm/b", []byte{0xbb}, 7),
		newTestBudlet(t, "evm/c", []byte{0xcc}, []byte{}, 8),
	})
	require.NoError(t, err)
	appHashData := apphash.NewAppHashData(
		chainID, 9, filled(0xa9), filled(0xb9), tree.BUD(), filled(0xd9), filled(0xe9))
	budProof, found := tree.BuildBUDProof([]byte("evm/b"))
	require.True(t, found)
	stateProof, err := NewBUDStateProof(appHashData, budProof)
	require.NoError(t, err)

	bud9 := "cb9f3601d13115bd8f430f469e52d64709857df23f068bfa2aba3128eea8ebef"
	serializedAppHashData := "01" + "1112131415161718" + "0000000000000009" + strings.Repeat("a9", 32) +
		strings.Repeat("b9", 32) + bud9 + strings.Repeat("d9", 32) + strings.Repeat("e9", 32)
	appHash9 := "b5b0ba5ff115aca33794926156e3aeae5c04d2b9a2151d4cea0b3356ea6e3b56"
	serializedBUDProof := "01" + "01" +
		"00000005" + "65766d2f62" + "01" + "00000000" + "" + "00" + "00000001" + "bb" + "0000000000000007" +
		"0000000000000003" + "0000000000000001" +
		"377d389fc54fdd44a82596916704d7a16b64d58aeb1338ad26c9e1fff1600d13" +
		"a71d74fa15dd6ba2179fe0f1f1ba64ace69008694375dd4454579aa75681c7f4"
	serializedStateProof := "01" + "000000b1" + serializedAppHashData + serializedBUDProof

	require.Equal(t, serializedAppHashData, hex.EncodeToString(appHashData.Serialize()))
	require.Equal(t, serializedBUDProof, hex.EncodeToString(budProof.Serialize()))
	require.Equal(t, serializedStateProof, hex.EncodeToString(stateProof.Serialize()))

	deserialized, err := DeserializeBUDStateProof(decodeSpecHex(t, serializedStateProof))
	require.NoError(t, err)
	require.Equal(t, stateProof, deserialized)
	require.Equal(t, uint64(chainID), deserialized.ChainID())
	require.Equal(t, uint64(7), deserialized.StartHeight())
	require.Equal(t, uint64(9), deserialized.EndHeight())
	appHash := deserialized.AppHash()
	require.Equal(t, appHash9, hex.EncodeToString(appHash[:]))

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
