package bud

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/apphash"
)

// budHashOf returns the BUD hash of the BUD tree over budlets, failing the test if they are not sorted.
func budHashOf(t *testing.T, budlets []*Budlet) apphash.BUDHash {
	t.Helper()

	tree, err := NewBUDTree(budlets)
	require.NoError(t, err)
	return tree.BUDHash()
}

func TestBUDHashOfEmptyTree(t *testing.T) {
	require.Equal(t, computeBUDHash(0, [32]byte{}), budHashOf(t, nil))
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

	var tree BUDTree
	_ = tree.BUDHash()
	require.Empty(t, tree.Budlets())
	_, found := tree.BuildBUDProof([]byte("evm/a"))
	require.False(t, found)
	_ = tree.Serialize()

	var proof BUDProof
	_ = proof.ComputeBUDHash()
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
	_, err = DeserializeBUDStateProof(stateProof.Serialize())
	require.Error(t, err)
}
