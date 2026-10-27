package bud

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewBUDTreeRejectsUnsortedBudlets(t *testing.T) {
	testCases := map[string][]*Budlet{
		"unsorted": {
			newTestBudlet(t, "evm/b", []byte{0x01}, []byte{}, 0),
			newTestBudlet(t, "evm/a", []byte{0x01}, []byte{}, 0),
		},
		"duplicate key": {
			newTestBudlet(t, "evm/a", []byte{0x01}, []byte{}, 0),
			newTestBudlet(t, "evm/a", []byte{0x02}, []byte{0x01}, 0),
		},
	}
	for name, budlets := range testCases {
		t.Run(name, func(t *testing.T) {
			_, err := NewBUDTree(budlets)
			require.Error(t, err)
		})
	}
}

func TestNewBUDTreeRejectsNilAndInvalidBudlets(t *testing.T) {
	valid := newTestBudlet(t, "evm/a", []byte{0x01}, []byte{}, 0)
	testCases := map[string][]*Budlet{
		"nil budlet":          {nil},
		"nil after a budlet":  {valid, nil},
		"zero budlet":         {&Budlet{}},
		"zero after a budlet": {valid, &Budlet{}},
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
		require.Equal(t, tree.BUDHash(), deserialized.BUDHash(), "count %d", count)
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
	first := newTestBudlet(t, "evm/a", []byte{0x01}, []byte{}, 0)
	second := newTestBudlet(t, "evm/b", []byte{}, []byte{0x02}, 4)
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
