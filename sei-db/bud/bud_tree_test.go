package bud

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

// referenceBUDTreeRoot returns the root of the BUD tree over leaves using the recursive RFC 6962 definition.
func referenceBUDTreeRoot(leaves [][32]byte) [32]byte {
	if len(leaves) == 1 {
		return leaves[0]
	}
	split := 1
	for split*2 < len(leaves) {
		split *= 2
	}
	return budInnerHash(referenceBUDTreeRoot(leaves[:split]), referenceBUDTreeRoot(leaves[split:]))
}

func TestBUDTreeRootMatchesRFC6962(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for count := 1; count <= 64; count++ {
		leaves := budLeafHashes(randomBudlets(t, rng, count))
		require.Equal(t, referenceBUDTreeRoot(leaves), budTreeRoot(leaves), "count %d", count)
	}
}

func TestNewBUDTreeRejectsUnsortedBudlets(t *testing.T) {
	testCases := map[string][]*Budlet{
		"unsorted": {
			newTestBudlet(t, "evm/b", []byte{0x01}, 0),
			newTestBudlet(t, "evm/a", []byte{0x01}, 0),
		},
		"duplicate key": {
			newTestBudlet(t, "evm/a", []byte{0x01}, 0),
			newTestDeletionBudlet(t, "evm/a", 0),
		},
	}
	for name, budlets := range testCases {
		t.Run(name, func(t *testing.T) {
			_, err := NewBUDTree(budlets)
			require.Error(t, err)
		})
	}
}

func TestBUDTreeSerializationRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	for count := 0; count <= 17; count++ {
		tree, err := NewBUDTree(randomBudlets(t, rng, count))
		require.NoError(t, err)

		deserialized, err := DeserializeBUDTree(tree.Serialize())
		require.NoError(t, err, "count %d", count)
		require.Equal(t, tree.BUD(), deserialized.BUD(), "count %d", count)
		require.Equal(t, tree.Budlets(), deserialized.Budlets(), "count %d", count)
	}
}

func TestDeserializeBUDTreeRejectsMalformedInput(t *testing.T) {
	serialize := func(version uint8, count uint64, budlets ...*Budlet) []byte {
		data := binary.BigEndian.AppendUint64([]byte{version}, count)
		for _, budlet := range budlets {
			data = append(data, budlet.Serialize()...)
		}
		return data
	}
	first := newTestBudlet(t, "evm/a", []byte{0x01}, 0)
	second := newTestDeletionBudlet(t, "evm/b", 4)
	valid := serialize(budVersion, 2, first, second)
	_, err := DeserializeBUDTree(valid)
	require.NoError(t, err)

	testCases := map[string][]byte{
		"empty":               nil,
		"unsupported version": serialize(budVersion+1, 2, first, second),
		"truncated header":    valid[:budTreeBudletsOffset-1],
		"count too high":      serialize(budVersion, 3, first, second),
		"count too low":       serialize(budVersion, 1, first, second),
		"huge count":          serialize(budVersion, ^uint64(0), first, second),
		"unsorted budlets":    serialize(budVersion, 2, second, first),
		"truncated budlet":    valid[:len(valid)-1],
		"malformed budlet":    append(serialize(budVersion, 1), bytes.Repeat([]byte{0xff}, 4)...),
	}
	for name, data := range testCases {
		t.Run(name, func(t *testing.T) {
			_, err := DeserializeBUDTree(data)
			require.Error(t, err)
		})
	}
}

func TestBuildBUDProofOfAbsentKey(t *testing.T) {
	tree, err := NewBUDTree(threeBudlets(t))
	require.NoError(t, err)
	proof, found := tree.BuildBUDProof([]byte("evm/bb"))
	require.False(t, found)
	require.Nil(t, proof)
}
