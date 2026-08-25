package query

import (
	"context"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/mem"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/kv"
	"github.com/stretchr/testify/require"
)

func seedStore(t *testing.T, n int, valueSize int) *mem.Store {
	t.Helper()
	store := mem.NewStore()
	val := make([]byte, valueSize)
	prefix := []byte("p")
	for i := range n {
		key := append(append([]byte{}, prefix...), byte(i))
		store.Set(key, val)
	}
	return store
}

func TestScanSubspace_CapAtPairs(t *testing.T) {
	store := seedStore(t, 5, 1)

	_, err := ScanSubspace(t.Context(), store, []byte("p"), Limits{MaxPairs: 3, MaxBytes: DefaultMaxSubspaceBytes})
	require.Error(t, err)
	require.True(t, storetypes.ErrSubspaceCapExceeded.Is(err))
}

func TestScanSubspace_CapAtBytes(t *testing.T) {
	store := seedStore(t, 3, 10)

	_, err := ScanSubspace(t.Context(), store, []byte("p"), Limits{MaxPairs: 100, MaxBytes: 15})
	require.Error(t, err)
	require.True(t, storetypes.ErrSubspaceCapExceeded.Is(err))
}

func TestScanSubspace_NarrowPrefixSucceeds(t *testing.T) {
	store := mem.NewStore()
	store.Set([]byte("abc"), []byte("v1"))
	store.Set([]byte("abd"), []byte("v2"))
	store.Set([]byte("xyz"), []byte("v3"))

	bz, err := ScanSubspace(t.Context(), store, []byte("ab"), Limits{MaxPairs: 10, MaxBytes: DefaultMaxSubspaceBytes})
	require.NoError(t, err)

	var pairs kv.Pairs
	require.NoError(t, pairs.Unmarshal(bz))
	require.Len(t, pairs.Pairs, 2)
}

func TestScanSubspace_ContextCancelStopsAllocation(t *testing.T) {
	store := seedStore(t, 100, 64)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := ScanSubspace(ctx, store, []byte("p"), Limits{MaxPairs: 1000, MaxBytes: DefaultMaxSubspaceBytes})
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, storetypes.ErrSubspaceCapExceeded.Is(err))
}

func TestScanSubspace_ContextDeadline(t *testing.T) {
	store := seedStore(t, 100, 64)

	ctx, cancel := context.WithTimeout(t.Context(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)

	_, err := ScanSubspace(ctx, store, []byte("p"), Limits{MaxPairs: 1000, MaxBytes: DefaultMaxSubspaceBytes})
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestIsCapExceededResponse(t *testing.T) {
	_, err := ScanSubspace(t.Context(), seedStore(t, 5, 1), []byte("p"), Limits{MaxPairs: 1, MaxBytes: DefaultMaxSubspaceBytes})
	require.True(t, IsCapExceeded(err))

	res := sdkerrors.QueryResult(err)
	require.True(t, IsCapExceededResponse(res))
}
