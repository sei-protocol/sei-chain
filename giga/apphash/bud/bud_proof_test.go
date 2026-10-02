package bud

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/apphash"
)

func TestBUDProofVerifiesEveryLeaf(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for count := 1; count <= 17; count++ {
		tree, err := NewBUDTree(randomBudlets(t, rng, count))
		require.NoError(t, err)
		bud := tree.BUD()

		for index, budlet := range tree.Budlets() {
			proof, found := tree.BuildBUDProof(budlet.Key())
			require.True(t, found, "count %d index %d", count, index)
			require.Equal(t, budlet, proof.Budlet())
			require.Equal(t, uint64(count), proof.Count())
			require.Equal(t, uint64(index), proof.Index())
			require.Equal(t, bud, proof.ComputeBUD(), "count %d index %d", count, index)

			deserialized, err := DeserializeBUDProof(proof.Serialize())
			require.NoError(t, err, "count %d index %d", count, index)
			require.Equal(t, proof, deserialized, "count %d index %d", count, index)
		}
	}
}

// TestBUDProofRejectsTampering requires every single-field alteration of a serialized BUD proof either to fail to
// deserialize or to compute a BUD other than the one claimed.
func TestBUDProofRejectsTampering(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for _, count := range []int{1, 2, 3, 5, 8, 13} {
		tree, err := NewBUDTree(randomBudlets(t, rng, count))
		require.NoError(t, err)

		for index, budlet := range tree.Budlets() {
			proof, found := tree.BuildBUDProof(budlet.Key())
			require.True(t, found)
			for name, tampered := range tamperedBUDProofClaims(tree.BUD(), proof) {
				deserialized, err := DeserializeBUDProof(tampered.proof.Serialize())
				if err != nil {
					continue
				}
				require.NotEqual(t, tampered.bud, deserialized.ComputeBUD(),
					"count %d index %d: %s", count, index, name)
			}
		}
	}
}

// budProofClaim is a BUD proof and the BUD it is claimed to compute.
type budProofClaim struct {
	// The BUD the proof's budlet is claimed to be under.
	bud apphash.BUD

	// The proof of the claim.
	proof *BUDProof
}

// tamperedBUDProofClaims returns, by name, every single-field alteration of a valid claim.
func tamperedBUDProofClaims(bud apphash.BUD, proof *BUDProof) map[string]budProofClaim {
	claims := make(map[string]budProofClaim)
	alter := func(name string, change func(claim *budProofClaim)) {
		proofCopy := *proof
		proofCopy.budlet.key = bytes.Clone(proof.budlet.key)
		// bytes.Clone keeps a nil value nil and an empty one non-nil, so deletions and empty writes stay apart.
		proofCopy.budlet.value = bytes.Clone(proof.budlet.value)
		proofCopy.siblings = append([][32]byte(nil), proof.siblings...)
		claim := budProofClaim{bud: bud, proof: &proofCopy}
		change(&claim)
		claims[name] = claim
	}

	alter("bud", func(claim *budProofClaim) { claim.bud[0] ^= 1 })
	alter("key", func(claim *budProofClaim) { claim.proof.budlet.key[0] ^= 1 })
	alter("previous height", func(claim *budProofClaim) { claim.proof.budlet.previousHeight ^= 1 })
	if proof.budlet.value == nil {
		alter("empty write", func(claim *budProofClaim) { claim.proof.budlet.value = []byte{} })
	} else {
		alter("deletion", func(claim *budProofClaim) { claim.proof.budlet.value = nil })
		alter("value", func(claim *budProofClaim) {
			claim.proof.budlet.value = append(claim.proof.budlet.value, 0)
		})
	}
	alter("count up", func(claim *budProofClaim) { claim.proof.count++ })
	if proof.count > proof.index+1 {
		alter("count down", func(claim *budProofClaim) { claim.proof.count-- })
		alter("index up", func(claim *budProofClaim) { claim.proof.index++ })
	}
	if proof.index > 0 {
		alter("index down", func(claim *budProofClaim) { claim.proof.index-- })
	}
	for i := range proof.siblings {
		alter(fmt.Sprintf("sibling %d", i), func(claim *budProofClaim) { claim.proof.siblings[i][0] ^= 1 })
	}
	alter("extra sibling", func(claim *budProofClaim) {
		claim.proof.siblings = append(claim.proof.siblings, [32]byte{})
	})
	if len(proof.siblings) > 0 {
		alter("missing sibling", func(claim *budProofClaim) {
			claim.proof.siblings = claim.proof.siblings[:len(claim.proof.siblings)-1]
		})
	}
	return claims
}

func TestDeserializeBUDProofRejectsMalformedInput(t *testing.T) {
	budlet := newTestBudlet(t, "evm/a", []byte{0x01}, 0).Serialize()
	serialize := func(version uint8, budVersion uint8, count uint64, index uint64, siblings int) []byte {
		data := append([]byte{version, budVersion}, budlet...)
		data = binary.BigEndian.AppendUint64(data, count)
		data = binary.BigEndian.AppendUint64(data, index)
		return append(data, make([]byte, 32*siblings)...)
	}
	valid := serialize(budProofVersion, budVersion, 3, 0, 2)
	_, err := DeserializeBUDProof(valid)
	require.NoError(t, err)

	testCases := map[string][]byte{
		"empty":                   nil,
		"unsupported version":     serialize(budProofVersion+1, budVersion, 3, 0, 2),
		"unsupported BUD version": serialize(budProofVersion, budVersion+1, 3, 0, 2),
		"malformed budlet":        append([]byte{budProofVersion, budVersion}, 0xff, 0xff, 0xff, 0xff),
		"index equals count":      serialize(budProofVersion, budVersion, 3, 3, 2),
		"zero count":              serialize(budProofVersion, budVersion, 0, 0, 0),
		"missing sibling":         serialize(budProofVersion, budVersion, 3, 0, 1),
		"extra sibling":           serialize(budProofVersion, budVersion, 3, 0, 3),
		"partial sibling":         valid[:len(valid)-1],
	}
	for length := 0; length < budProofBudletOffset+len(budlet)+budProofPositionSize; length++ {
		testCases[fmt.Sprintf("truncated to %d bytes", length)] = valid[:length]
	}
	for name, data := range testCases {
		t.Run(name, func(t *testing.T) {
			_, err := DeserializeBUDProof(data)
			require.Error(t, err)
		})
	}
}
