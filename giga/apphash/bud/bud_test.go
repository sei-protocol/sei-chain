package bud

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/apphash"
)

// budOf returns the BUD of the BUD tree over budlets, failing the test if they are not sorted.
func budOf(t *testing.T, budlets []*Budlet) apphash.BUD {
	t.Helper()

	tree, err := NewBUDTree(budlets)
	require.NoError(t, err)
	return tree.BUD()
}

func TestBUDOfEmptyTree(t *testing.T) {
	require.Equal(t, hashBUD(0, [32]byte{}), budOf(t, nil))
}

func TestBUDCommitsToEveryField(t *testing.T) {
	original := budOf(t, threeBudlets(t))

	replacements := map[string]*Budlet{
		"key":                       newTestBudlet(t, "evm/bb", []byte{0x02, 0x03}, nil, 0),
		"value":                     newTestBudlet(t, "evm/b", []byte{0x02, 0x04}, nil, 0),
		"deletion":                  newTestDeletionBudlet(t, "evm/b", nil, 0),
		"previous value":            newTestBudlet(t, "evm/b", []byte{0x02, 0x03}, []byte{0x05}, 0),
		"empty previous value":      newTestBudlet(t, "evm/b", []byte{0x02, 0x03}, []byte{}, 0),
		"not-modified-since height": newTestBudlet(t, "evm/b", []byte{0x02, 0x03}, nil, 1),
	}
	for name, replacement := range replacements {
		t.Run(name, func(t *testing.T) {
			budlets := threeBudlets(t)
			budlets[1] = replacement
			require.NotEqual(t, original, budOf(t, budlets))
		})
	}

	t.Run("count", func(t *testing.T) {
		require.NotEqual(t, original, budOf(t, threeBudlets(t)[:2]))
	})
}

func TestBUDDistinguishesDeletionFromEmptyValue(t *testing.T) {
	deleted := budOf(t, []*Budlet{newTestDeletionBudlet(t, "evm/a", nil, 0)})
	empty := budOf(t, []*Budlet{newTestBudlet(t, "evm/a", []byte{}, nil, 0)})
	require.NotEqual(t, deleted, empty)
}

func TestBUDDistinguishesAbsentFromEmptyPreviousValue(t *testing.T) {
	absent := budOf(t, []*Budlet{newTestBudlet(t, "evm/a", []byte{0x01}, nil, 0)})
	empty := budOf(t, []*Budlet{newTestBudlet(t, "evm/a", []byte{0x01}, []byte{}, 0)})
	require.NotEqual(t, absent, empty)
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
	_ = tree.BUD()
	require.Empty(t, tree.Budlets())
	_, found := tree.BuildBUDProof([]byte("evm/a"))
	require.False(t, found)
	_ = tree.Serialize()

	var proof BUDProof
	_ = proof.ComputeBUD()
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
