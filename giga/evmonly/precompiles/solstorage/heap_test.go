package solstorage

import (
	"math/rand"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHeapKeepsMinimumAndPositions(t *testing.T) {
	state := mapState{}
	h := NewHeap(Store{State: state}, Slot(4))
	rng := rand.New(rand.NewSource(1)) //nolint:gosec // deterministic test data
	var live []uint64
	for range 500 {
		if len(live) > 0 && rng.Intn(3) == 0 {
			i := rng.Intn(len(live))
			require.True(t, h.Remove(Slot(live[i])))
			require.False(t, h.Remove(Slot(live[i])), "already removed")
			live = slices.Delete(live, i, i+1)
		} else {
			v := rng.Uint64() % 1000
			require.Equal(t, !slices.Contains(live, v), h.Push(Slot(v)))
			if !slices.Contains(live, v) {
				live = append(live, v)
			}
		}
		require.Equal(t, uint64(len(live)), h.Len())
		for i := range h.Len() {
			require.Equal(t, i+1, h.set.position(h.At(i)), "position of index %d", i)
			if i > 0 {
				require.False(t, h.less(i, (i-1)/2), "index %d is below its parent", i)
			}
		}
		if len(live) > 0 {
			require.Equal(t, Slot(slices.Min(live)), h.At(0))
		}
	}
	for _, v := range live {
		require.True(t, h.Remove(Slot(v)))
	}
	require.Empty(t, state, "an emptied heap leaves no storage")
}
