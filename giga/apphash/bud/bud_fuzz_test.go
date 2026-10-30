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

	// Serialized BUD proofs.
	proofs [][]byte

	// Serialized BUD state proofs.
	stateProofs [][]byte
}

// newBUDFuzzSeeds returns the serialized budlets, BUD proofs, and BUD state proofs of the bud_spec.md
// test vectors.
func newBUDFuzzSeeds(f *testing.F) budFuzzSeeds {
	f.Helper()

	newBudlet := func(key string, value []byte, previousValue []byte, notModifiedSince uint64) *Budlet {
		budlet, err := NewBudlet([]byte(key), value, previousValue, notModifiedSince)
		require.NoError(f, err)
		return budlet
	}
	buildProof := func(budlets []*Budlet, key string) *BUDProof {
		proof, found, err := NewBUDProof(budlets, []byte(key))
		require.NoError(f, err)
		require.True(f, found)
		return proof
	}
	filled := func(b byte) [32]byte {
		return [32]byte(bytes.Repeat([]byte{b}, 32))
	}

	budlets := []*Budlet{
		newBudlet("evm/a", []byte{0x00, 0xaa, 0xbb}, []byte{0x00, 0xaa}, 0),
		newBudlet("evm/b", []byte{0x00, 0xbb, 0xcc}, []byte{0x00, 0xbb}, 7),
		newBudlet("evm/c", []byte{0x00, 0xcc}, []byte{}, 8),
	}
	treeHash, err := ComputeBUDTreeHash(budlets)
	require.NoError(f, err)
	budHash := ComputeBUDHash(uint64(len(budlets)), treeHash)
	appHashData := apphash.NewAppHashData(
		0x1112131415161718, 9, filled(0xa9), filled(0xb9), budHash, filled(0xd9), filled(0xe9))
	stateProof, err := NewBUDStateProof(appHashData, buildProof(budlets, "evm/b"))
	require.NoError(f, err)

	var seeds budFuzzSeeds
	for _, budlet := range budlets {
		seeds.budlets = append(seeds.budlets, budlet.Serialize())
		seeds.proofs = append(seeds.proofs, buildProof(budlets, string(budlet.Key())).Serialize())
	}
	seeds.stateProofs = [][]byte{mustSerializeBUDStateProof(f, stateProof)}
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

// FuzzDeserializeBUDProof requires DeserializeBUDProof() never to panic, and every BUD proof it accepts to
// serialize back to the same bytes and compute a BUD hash.
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
		_, err = proof.ComputeBUDHash()
		require.NoError(t, err)
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
		require.Equal(t, data, mustSerializeBUDStateProof(t, stateProof))
		_ = stateProof.AppHash()
		for _, height := range []uint64{stateProof.StartHeight(), stateProof.EndHeight()} {
			_, covered := stateProof.ValueAt(height)
			require.True(t, covered, "height %d", height)
		}
	})
}
