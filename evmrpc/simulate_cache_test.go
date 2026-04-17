package evmrpc

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/stretchr/testify/require"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

func newTestBackend(size int, ttl time.Duration) *Backend {
	return &Backend{
		replayStateCacheMu: &sync.Mutex{},
		replayStateCache:   expirable.NewLRU[string, *blockReplayState](size, nil, ttl),
	}
}

// TestReplayStateCache_GetPut exercises the basic round-trip and the
// "best checkpoint <= txIndex" selection logic used to resume a trace.
func TestReplayStateCache_GetPut(t *testing.T) {
	b := newTestBackend(replayStateCacheBlocks, replayStateCacheTTL)
	hash := "0xabc"

	ctx0 := sdk.Context{}.WithBlockHeight(100)
	ctx5 := sdk.Context{}.WithBlockHeight(105)
	ctx10 := sdk.Context{}.WithBlockHeight(110)

	b.putReplayState(hash, -1, ctx0)
	b.putReplayState(hash, 5, ctx5)
	b.putReplayState(hash, 10, ctx10)

	// Asking for txIndex=7 should return the checkpoint at idx=5.
	got, idx, ok := b.getReplayState(hash, 7)
	require.True(t, ok)
	require.Equal(t, 5, idx)
	require.Equal(t, int64(105), got.BlockHeight())

	// Asking for txIndex=0 should return the -1 checkpoint.
	got, idx, ok = b.getReplayState(hash, 0)
	require.True(t, ok)
	require.Equal(t, -1, idx)
	require.Equal(t, int64(100), got.BlockHeight())

	// Unknown block returns false.
	_, _, ok = b.getReplayState("0xmissing", 0)
	require.False(t, ok)
}

// TestReplayStateCache_EvictsOldBlocks is the regression test for the
// unbounded-memory-growth bug: distinct blocks beyond the cache size must
// be evicted, not retained forever.
func TestReplayStateCache_EvictsOldBlocks(t *testing.T) {
	const size = 4
	b := newTestBackend(size, time.Hour)

	for i := 0; i < size*3; i++ {
		hash := fmt.Sprintf("0x%d", i)
		b.putReplayState(hash, 0, sdk.Context{}.WithBlockHeight(int64(i)))
	}

	require.LessOrEqual(t, b.replayStateCache.Len(), size,
		"cache must not grow beyond its configured size")

	_, _, ok := b.getReplayState("0x0", 0)
	require.False(t, ok, "oldest block must have been evicted")

	_, _, ok = b.getReplayState(fmt.Sprintf("0x%d", size*3-1), 0)
	require.True(t, ok, "most recently added block must still be cached")
}

// TestReplayStateCache_Concurrent runs parallel puts/gets to catch
// data races on the per-block inner map. Run with `go test -race`.
func TestReplayStateCache_Concurrent(t *testing.T) {
	b := newTestBackend(replayStateCacheBlocks, replayStateCacheTTL)

	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			hash := fmt.Sprintf("block-%d", w%4) // 4 distinct blocks, contention on each
			for i := 0; i < 100; i++ {
				b.putReplayState(hash, i, sdk.Context{}.WithBlockHeight(int64(i)))
				_, _, _ = b.getReplayState(hash, i)
			}
		}(w)
	}
	wg.Wait()
}
