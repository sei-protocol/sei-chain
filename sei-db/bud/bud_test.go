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
		"key":             newTestBudlet(t, "evm/bb", []byte{0x02, 0x03}, 0),
		"value":           newTestBudlet(t, "evm/b", []byte{0x02, 0x04}, 0),
		"deletion":        newTestDeletionBudlet(t, "evm/b", 0),
		"previous height": newTestBudlet(t, "evm/b", []byte{0x02, 0x03}, 1),
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
	deleted := budOf(t, []*Budlet{newTestDeletionBudlet(t, "evm/a", 0)})
	empty := budOf(t, []*Budlet{newTestBudlet(t, "evm/a", []byte{}, 0)})
	require.NotEqual(t, deleted, empty)
}
