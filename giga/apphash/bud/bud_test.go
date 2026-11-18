package bud

import (
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/apphash"
)

// budHashOf returns the BUD hash of the BUD tree over budlets, failing the test if they are not sorted.
func budHashOf(t *testing.T, budlets []*Budlet) apphash.BUDHash {
	t.Helper()

	treeHash, err := ComputeBUDTreeHash(budlets)
	require.NoError(t, err)
	return ComputeBUDHash(uint64(len(budlets)), treeHash)
}

// mustNewBUDProof returns the BUD proof of the budlet with key, failing the test if the budlets are invalid or none
// has that key.
func mustNewBUDProof(t *testing.T, budlets []*Budlet, key []byte) *BUDProof {
	t.Helper()

	proof, found, err := NewBUDProof(budlets, key)
	require.NoError(t, err)
	require.True(t, found, "key %x", key)
	return proof
}

func TestBUDHashOfEmptyTree(t *testing.T) {
	require.Equal(t, ComputeBUDHash(0, sha256.Sum256(nil)), budHashOf(t, nil))
}

func TestBUDHashCommitsToEveryField(t *testing.T) {
	original := budHashOf(t, threeBudlets(t))

	replacements := map[string]*Budlet{
		"key":                       newTestBudlet(t, "evm/bb", []byte{0x02, 0x03}, []byte{}, 0),
		"value":                     newTestBudlet(t, "evm/b", []byte{0x02, 0x04}, []byte{}, 0),
		"empty value":               newTestBudlet(t, "evm/b", []byte{}, []byte{}, 0),
		"previous value":            newTestBudlet(t, "evm/b", []byte{0x02, 0x03}, []byte{0x05}, 0),
		"not-modified-since height": newTestBudlet(t, "evm/b", []byte{0x02, 0x03}, []byte{}, 1),
	}
	for name, replacement := range replacements {
		t.Run(name, func(t *testing.T) {
			budlets := threeBudlets(t)
			budlets[1] = replacement
			require.NotEqual(t, original, budHashOf(t, budlets))
		})
	}

	t.Run("count", func(t *testing.T) {
		require.NotEqual(t, original, budHashOf(t, threeBudlets(t)[:2]))
	})
}

func TestZeroValuesDoNotPanic(t *testing.T) {
	var budlet Budlet
	require.Nil(t, budlet.Key())
	require.Nil(t, budlet.Value())
	require.Nil(t, budlet.PreviousValue())
	require.Zero(t, budlet.NotModifiedSince())
	_, err := DeserializeBudlet(budlet.Serialize())
	require.Error(t, err)

	_, found, err := NewBUDProof(nil, []byte("evm/a"))
	require.NoError(t, err)
	require.False(t, found)

	var proof BUDProof
	_, err = proof.ComputeBUDHash()
	require.Error(t, err)
	require.NotNil(t, proof.Budlet())
	require.Zero(t, proof.Count())
	require.Zero(t, proof.Index())
	require.Empty(t, proof.Siblings())
	_, err = DeserializeBUDProof(proof.Serialize())
	require.Error(t, err)

	var stateProof BUDStateProof
	require.Nil(t, stateProof.Key())
	require.Zero(t, stateProof.ChainID())
	require.Zero(t, stateProof.StartHeight())
	require.Zero(t, stateProof.EndHeight())
	_, covered := stateProof.ValueAt(0)
	require.False(t, covered)
	_ = stateProof.AppHash()
	_, err = DeserializeBUDStateProof(mustSerializeBUDStateProof(t, &stateProof))
	require.Error(t, err)
}
