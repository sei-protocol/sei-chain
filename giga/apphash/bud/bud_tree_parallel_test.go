package bud

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// A tree large enough to hash on several goroutines must hash exactly as one hashed serially.
func TestLargeTreeHashesAsASerialPassDoes(t *testing.T) {
	const n = 3*parallelHashThreshold + 7
	budlets := make([]*Budlet, n)
	for i := range budlets {
		key := binary.BigEndian.AppendUint64(nil, uint64(i))
		budlet, err := NewBudlet(key, []byte{byte(i), 1}, []byte{byte(i)}, 1)
		require.NoError(t, err)
		budlets[i] = budlet
	}

	want := make([][32]byte, n)
	var scratch []byte
	for i, budlet := range budlets {
		want[i], scratch = budLeafHash(budlet, scratch)
	}
	require.Equal(t, want, budLeafHashes(budlets))

	level := want
	for len(level) > 1 {
		next := make([][32]byte, 0, (len(level)+1)/2)
		for i := 0; i+1 < len(level); i += 2 {
			next = append(next, budInnerHash(level[i], level[i+1]))
		}
		if len(level)%2 == 1 {
			next = append(next, level[len(level)-1])
		}
		level = next
	}
	require.Equal(t, level[0], budTreeRoot(want))
}
