package bud

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/apphash"
)

// TestBUDSpecVector pins every value of the budlet, BUD tree, BUD hash, and BUD proof test vectors in bud_spec.md.
func TestBUDSpecVector(t *testing.T) {
	budlets := []*Budlet{
		newTestBudlet(t, "evm/a", []byte{0x00, 0xaa, 0xbb}, []byte{0x00, 0xaa}, 0),
		newTestBudlet(t, "evm/b", []byte{0x00, 0xbb, 0xcc}, []byte{0x00, 0xbb}, 7),
		newTestBudlet(t, "evm/c", []byte{0x00, 0xcc}, []byte{}, 8),
	}
	serializations := []string{
		"00000005" + "65766d2f61" + "00000003" + "00aabb" + "00000002" + "00aa" + "0000000000000000",
		"00000005" + "65766d2f62" + "00000003" + "00bbcc" + "00000002" + "00bb" + "0000000000000007",
		"00000005" + "65766d2f63" + "00000002" + "00cc" + "00000000" + "" + "0000000000000008",
	}
	leafHashes := []string{
		"0473228ae7de8d729da1c3829911e44bd8132bb70231601c81284c479194dd60",
		"e40d5c36c1f33b62926219ab8f643dcb540b86ea6af98731027109764392804c",
		"905f946a6e3487f06c7b1b525d651e8ee7a48fa5fbe481e50a40d09e79939d7e",
	}
	inner01 := "329d1fc527f7bc747989903c7a8dc0778cec41b98a695acab12887c02f157b25"
	root := "bc19858156c6b699dd78dd68213e06fbf347d30a6c5596cb3686902775148282"
	budHash := "4f3428944c0a960bb22d615446c3badd7790eaa96a2dd4519c14abc1c48cf086"
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
	computedBUDHash := tree.BUDHash()
	require.Equal(t, budHash, hex.EncodeToString(computedBUDHash[:]))
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
		deserializedBUDHash := deserializedProof.ComputeBUDHash()
		require.Equal(t, budHash, hex.EncodeToString(deserializedBUDHash[:]), "budlet %d proof", i)
	}
}

// TestBUDSpecEmptyTreeVector pins the serialized BUD tree and the BUD hash of a block with no budlets in bud_spec.md.
func TestBUDSpecEmptyTreeVector(t *testing.T) {
	serializedTree := "01" + "0000000000000000"
	budHash := "3eeeba2dfb311dad9e9e46eba984fa855be2b872496b921b19da52b3eb09d5e2"

	tree, err := NewBUDTree(nil)
	require.NoError(t, err)
	require.Equal(t, serializedTree, hex.EncodeToString(tree.Serialize()))
	computedBUDHash := tree.BUDHash()
	require.Equal(t, budHash, hex.EncodeToString(computedBUDHash[:]))

	deserialized, err := DeserializeBUDTree(decodeSpecHex(t, serializedTree))
	require.NoError(t, err)
	require.Empty(t, deserialized.Budlets())
	deserializedBUDHash := deserialized.BUDHash()
	require.Equal(t, budHash, hex.EncodeToString(deserializedBUDHash[:]))
}

// TestBUDSpecStateProofVector pins every value of the BUD state proof test vector in bud_spec.md.
func TestBUDSpecStateProofVector(t *testing.T) {
	const chainID = 0x1112131415161718
	filled := func(b byte) [32]byte {
		return [32]byte(bytes.Repeat([]byte{b}, 32))
	}

	tree, err := NewBUDTree([]*Budlet{
		newTestBudlet(t, "evm/a", []byte{0x00, 0xaa, 0xbb}, []byte{0x00, 0xaa}, 0),
		newTestBudlet(t, "evm/b", []byte{0x00, 0xbb, 0xcc}, []byte{0x00, 0xbb}, 7),
		newTestBudlet(t, "evm/c", []byte{0x00, 0xcc}, []byte{}, 8),
	})
	require.NoError(t, err)
	appHashData := apphash.NewAppHashData(
		chainID, 9, filled(0xa9), filled(0xb9), tree.BUDHash(), filled(0xd9), filled(0xe9))
	budProof, found := tree.BuildBUDProof([]byte("evm/b"))
	require.True(t, found)
	stateProof, err := NewBUDStateProof(appHashData, budProof)
	require.NoError(t, err)

	budHash9 := "4f3428944c0a960bb22d615446c3badd7790eaa96a2dd4519c14abc1c48cf086"
	serializedAppHashData := "01" + "1112131415161718" + "0000000000000009" + strings.Repeat("a9", 32) +
		strings.Repeat("b9", 32) + budHash9 + strings.Repeat("d9", 32) + strings.Repeat("e9", 32)
	appHash9 := "de4690aca7eee0df79120d1711a1494be8a19bd8e8ae1318333c08cc3ab8a189"
	serializedBUDProof := "01" + "01" +
		"00000005" + "65766d2f62" + "00000003" + "00bbcc" + "00000002" + "00bb" + "0000000000000007" +
		"0000000000000003" + "0000000000000001" +
		"0473228ae7de8d729da1c3829911e44bd8132bb70231601c81284c479194dd60" +
		"905f946a6e3487f06c7b1b525d651e8ee7a48fa5fbe481e50a40d09e79939d7e"
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
		require.Equal(t, []byte{0x00, 0xbb}, value, "height %d", height)
	}
	value, covered := deserialized.ValueAt(9)
	require.True(t, covered)
	require.Equal(t, []byte{0x00, 0xbb, 0xcc}, value)
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
