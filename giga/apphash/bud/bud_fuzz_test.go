package bud

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/apphash"
)

// budFuzzSeeds holds serialized objects of each kind, built from the bud_spec.md test vectors.
type budFuzzSeeds struct {
	// Serialized budlets.
	budlets [][]byte

	// Serialized BUD trees.
	trees [][]byte

	// Serialized BUD proofs.
	proofs [][]byte

	// Serialized BUD state proofs.
	stateProofs [][]byte
}

// newBUDFuzzSeeds returns the serialized budlets, BUD trees, BUD proofs, and BUD state proofs of the bud_spec.md
// test vectors.
func newBUDFuzzSeeds(f *testing.F) budFuzzSeeds {
	f.Helper()

	newBudlet := func(key string, value []byte, previousValue []byte, notModifiedSince uint64) *Budlet {
		budlet, err := NewBudlet([]byte(key), value, previousValue, notModifiedSince)
		require.NoError(f, err)
		return budlet
	}
	newTree := func(budlets ...*Budlet) *BUDTree {
		tree, err := NewBUDTree(budlets)
		require.NoError(f, err)
		return tree
	}
	buildProof := func(tree *BUDTree, key string) *BUDProof {
		proof, found := tree.BuildBUDProof([]byte(key))
		require.True(f, found)
		return proof
	}
	filled := func(b byte) [32]byte {
		return [32]byte(bytes.Repeat([]byte{b}, 32))
	}

	budlets := []*Budlet{
		newBudlet("evm/a", []byte{0xaa, 0xbb}, nil, 0),
		newBudlet("evm/b", nil, []byte{0xbb}, 7),
		newBudlet("evm/c", []byte{0xcc}, []byte{}, 8),
	}
	tree := newTree(budlets...)
	appHashData := apphash.NewAppHashData(
		0x1112131415161718, 9, filled(0xa9), filled(0xb9), tree.BUD(), filled(0xd9), filled(0xe9))
	stateProof, err := NewBUDStateProof(appHashData, buildProof(tree, "evm/b"))
	require.NoError(f, err)

	var seeds budFuzzSeeds
	for _, budlet := range budlets {
		seeds.budlets = append(seeds.budlets, budlet.Serialize())
		seeds.proofs = append(seeds.proofs, buildProof(tree, string(budlet.Key())).Serialize())
	}
	seeds.trees = [][]byte{newTree().Serialize(), tree.Serialize()}
	seeds.stateProofs = [][]byte{stateProof.Serialize()}
	return seeds
}

// FuzzDeserializeBudlet requires DeserializeBudlet() never to panic, and every budlet it accepts to serialize back
// to the same bytes.
func FuzzDeserializeBudlet(f *testing.F) {
	for _, seed := range newBUDFuzzSeeds(f).budlets {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		budlet, err := DeserializeBudlet(data)
		if err != nil {
			return
		}
		require.Equal(t, data, budlet.Serialize())
	})
}

// FuzzDeserializeBUDTree requires DeserializeBUDTree() never to panic, and every BUD tree it accepts to serialize
// back to the same bytes and prove each of its budlets.
func FuzzDeserializeBUDTree(f *testing.F) {
	for _, seed := range newBUDFuzzSeeds(f).trees {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		tree, err := DeserializeBUDTree(data)
		if err != nil {
			return
		}
		require.Equal(t, data, tree.Serialize())
		for _, budlet := range tree.Budlets() {
			proof, found := tree.BuildBUDProof(budlet.Key())
			require.True(t, found)
			require.Equal(t, tree.BUD(), proof.ComputeBUD())
		}
	})
}

// FuzzDeserializeBUDProof requires DeserializeBUDProof() never to panic, and every BUD proof it accepts to
// serialize back to the same bytes and compute a BUD.
func FuzzDeserializeBUDProof(f *testing.F) {
	for _, seed := range newBUDFuzzSeeds(f).proofs {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		proof, err := DeserializeBUDProof(data)
		if err != nil {
			return
		}
		require.Equal(t, data, proof.Serialize())
		_ = proof.ComputeBUD()
	})
}

// FuzzDeserializeBUDStateProof requires DeserializeBUDStateProof() never to panic, and every BUD state proof it
// accepts to serialize back to the same bytes and answer for the heights it covers.
func FuzzDeserializeBUDStateProof(f *testing.F) {
	for _, seed := range newBUDFuzzSeeds(f).stateProofs {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		stateProof, err := DeserializeBUDStateProof(data)
		if err != nil {
			return
		}
		require.Equal(t, data, stateProof.Serialize())
		_ = stateProof.AppHash()
		for _, height := range []uint64{stateProof.StartHeight(), stateProof.EndHeight()} {
			_, covered := stateProof.ValueAt(height)
			require.True(t, covered, "height %d", height)
		}
	})
}
