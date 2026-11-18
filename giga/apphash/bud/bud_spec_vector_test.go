package bud

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/giga/apphash/merkle"
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
	computedInner01 := merkle.MerkleInnerHash(leaves[0], leaves[1])
	require.Equal(t, inner01, hex.EncodeToString(computedInner01[:]))
	computedRoot, err := ComputeBUDTreeHash(budlets)
	require.NoError(t, err)
	require.Equal(t, root, hex.EncodeToString(computedRoot[:]))
	computedBUDHash := ComputeBUDHash(uint64(len(budlets)), computedRoot)
	require.Equal(t, budHash, hex.EncodeToString(computedBUDHash[:]))

	for i, budlet := range budlets {
		deserialized, err := DeserializeBudlet(decodeSpecHex(t, serializations[i]))
		require.NoError(t, err, "budlet %d", i)
		require.Equal(t, budlet, deserialized, "budlet %d", i)
	}

	for i, budlet := range budlets {
		proof := mustNewBUDProof(t, budlets, budlet.Key())
		require.Equal(t, proofs[i], hex.EncodeToString(proof.Serialize()), "budlet %d proof", i)

		deserializedProof, err := DeserializeBUDProof(decodeSpecHex(t, proofs[i]))
		require.NoError(t, err, "budlet %d proof", i)
		require.Equal(t, proof, deserializedProof, "budlet %d proof", i)
		deserializedBUDHash, err := deserializedProof.ComputeBUDHash()
		require.NoError(t, err, "budlet %d proof", i)
		require.Equal(t, budHash, hex.EncodeToString(deserializedBUDHash[:]), "budlet %d proof", i)
	}
}

// TestBUDSpecEmptyTreeVector pins the BUD tree root and the BUD hash of a block with no budlets in bud_spec.md.
func TestBUDSpecEmptyTreeVector(t *testing.T) {
	budHash := "9725d028d186ff14d54a60658eac9ea435e5bace76d9aa01df155e4e89f1678f"

	computedRoot, err := ComputeBUDTreeHash(nil)
	require.NoError(t, err)
	require.Equal(t, sha256.Sum256(nil), computedRoot)
	computedBUDHash := ComputeBUDHash(0, computedRoot)
	require.Equal(t, budHash, hex.EncodeToString(computedBUDHash[:]))
}

// TestBUDSpecStateProofVector pins every value of the BUD state proof test vector in bud_spec.md.
func TestBUDSpecStateProofVector(t *testing.T) {
	const chainID = 0x1112131415161718
	filled := func(b byte) [32]byte {
		return [32]byte(bytes.Repeat([]byte{b}, 32))
	}

	budlets := []*Budlet{
		newTestBudlet(t, "evm/a", []byte{0x00, 0xaa, 0xbb}, []byte{0x00, 0xaa}, 0),
		newTestBudlet(t, "evm/b", []byte{0x00, 0xbb, 0xcc}, []byte{0x00, 0xbb}, 7),
		newTestBudlet(t, "evm/c", []byte{0x00, 0xcc}, []byte{}, 8),
	}
	appHashData := apphash.NewAppHashData(
		chainID, 9, filled(0xa9), filled(0xb9), budHashOf(t, budlets), filled(0xd9), filled(0xe9))
	budProof := mustNewBUDProof(t, budlets, []byte("evm/b"))
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

	require.Equal(t, serializedAppHashData, hex.EncodeToString(mustSerializeAppHashData(t, appHashData)))
	require.Equal(t, serializedBUDProof, hex.EncodeToString(budProof.Serialize()))
	require.Equal(t, serializedStateProof, hex.EncodeToString(mustSerializeBUDStateProof(t, stateProof)))

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
