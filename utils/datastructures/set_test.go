package datastructures_test

import (
	"sync"
	"testing"

	"github.com/sei-protocol/sei-chain/utils/datastructures"
	"github.com/stretchr/testify/require"
)

func TestSyncSetSequantial(t *testing.T) {
	set := datastructures.NewSyncSet([]uint64{1, 2, 3})
	require.Equal(t, 3, set.Size())
	set.Add(4)
	require.Equal(t, 4, set.Size())
	set.Add(1)
	require.Equal(t, 4, set.Size())
	for _, i := range []uint64{1, 2, 3, 4} {
		require.True(t, set.Contains(i))
	}
	set.Remove(4)
	require.Equal(t, 3, set.Size())
	set.RemoveAll([]uint64{2, 3})
	require.Equal(t, 1, set.Size())
}

func TestSyncSetParallel(t *testing.T) {
	set := datastructures.NewSyncSet([]uint64{})
	wg := sync.WaitGroup{}
	// a parallelism of 100 is likely to expose any potential data corruption
	for i := uint64(0); i < uint64(100); i++ {
		wg.Add(1)
		i := i
		go func() {
			set.Add(i)
			wg.Done()
		}()
	}
	wg.Wait()
	require.Equal(t, 100, set.Size())

	for i := uint64(0); i < uint64(100); i++ {
		wg.Add(1)
		i := i
		go func() {
			set.Remove(i)
			wg.Done()
		}()
	}
	wg.Wait()
	require.Equal(t, 0, set.Size())
}
