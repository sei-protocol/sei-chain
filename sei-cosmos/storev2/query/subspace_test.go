package query

import (
	"context"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/mem"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/kv"
	"github.com/sei-protocol/sei-chain/sei-db/common/iterators"
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

// panicOnCancelledCtxStore implements ContextIterator the way state.Store does:
// iterator construction on an already-cancelled context is fatal.
type panicOnCancelledCtxStore struct {
	*mem.Store
}

func (s *panicOnCancelledCtxStore) IteratorWithContext(ctx context.Context, start, end []byte) storetypes.Iterator {
	if err := ctx.Err(); err != nil {
		panic(err)
	}
	return s.Store.Iterator(start, end)
}

func (s *panicOnCancelledCtxStore) ReverseIteratorWithContext(ctx context.Context, start, end []byte) storetypes.Iterator {
	if err := ctx.Err(); err != nil {
		panic(err)
	}
	return s.Store.ReverseIterator(start, end)
}

func TestScanSubspace_ContextCancelBeforeIteratorOpen(t *testing.T) {
	store := &panicOnCancelledCtxStore{Store: seedStore(t, 1, 1)}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := ScanSubspace(ctx, store, []byte("p"), Limits{MaxPairs: 1000, MaxBytes: DefaultMaxSubspaceBytes})
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
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

// abortingIterator yields a fixed number of pairs and then goes invalid with an
// error set, the way an SS iterator reports a scan it gave up on mid-prefix.
type abortingIterator struct {
	storetypes.Iterator
	remaining int
	err       error
}

func (it *abortingIterator) Valid() bool {
	return it.err == nil && it.Iterator.Valid()
}

func (it *abortingIterator) Next() {
	it.remaining--
	if it.remaining <= 0 {
		it.err = context.Canceled
		return
	}
	it.Iterator.Next()
}

func (it *abortingIterator) Error() error {
	return it.err
}

type abortingStore struct {
	*mem.Store
	pairsBeforeAbort int
}

func (s *abortingStore) Iterator(start, end []byte) storetypes.Iterator {
	return &abortingIterator{Iterator: s.Store.Iterator(start, end), remaining: s.pairsBeforeAbort}
}

// hiddenRowStore implements BudgetedIterable the way a flatkv projected lane does: it reads every row
// in range and yields only those its filter keeps.
type hiddenRowStore struct {
	*mem.Store
	keep func(key []byte) bool
}

func (s *hiddenRowStore) BudgetedIterator(budget *iterators.ScanBudget, start, end []byte) (storetypes.Iterator, error) {
	parent := iterators.NewBudgetedIterator(s.Store.Iterator(start, end), budget)
	return iterators.NewTransformingIterator(parent, func(key, value []byte) ([]byte, []byte, bool, error) {
		return key, value, !s.keep(key), nil
	})
}

func TestScanSubspace_RowsReadAndNotYieldedAreCapped(t *testing.T) {
	store := &hiddenRowStore{Store: seedStore(t, 10, 8), keep: func([]byte) bool { return false }}

	_, err := ScanSubspace(t.Context(), store, []byte("p"), Limits{MaxScannedRows: 9})
	require.True(t, storetypes.ErrSubspaceCapExceeded.Is(err), "got %v", err)

	bz, err := ScanSubspace(t.Context(), store, []byte("p"), Limits{MaxScannedRows: 10})
	require.NoError(t, err)
	var pairs kv.Pairs
	require.NoError(t, pairs.Unmarshal(bz))
	require.Empty(t, pairs.Pairs)
}

func TestScanSubspace_ScanBudgetEndsAScanMidIteration(t *testing.T) {
	everyThird := func(key []byte) bool { return key[1]%3 == 0 }
	store := &hiddenRowStore{Store: seedStore(t, 10, 8), keep: everyThird}

	_, err := ScanSubspace(t.Context(), store, []byte("p"), Limits{MaxScannedRows: 5})
	require.True(t, storetypes.ErrSubspaceCapExceeded.Is(err), "got %v", err)
	require.True(t, IsCapExceededResponse(sdkerrors.QueryResult(err)))
}

func TestLimits_DefaultScanBudgetIsNeverBelowThePairCap(t *testing.T) {
	require.Equal(t, DefaultMaxSubspaceScannedRows, Limits{}.effective().MaxScannedRows)
	require.Equal(t, 2*DefaultMaxSubspaceScannedRows,
		Limits{MaxPairs: 2 * DefaultMaxSubspaceScannedRows}.effective().MaxScannedRows)
	require.Equal(t, 7, Limits{MaxPairs: 2 * DefaultMaxSubspaceScannedRows, MaxScannedRows: 7}.effective().MaxScannedRows)
}

func TestScanSubspace_AbortedIterationIsNotSuccess(t *testing.T) {
	store := &abortingStore{Store: seedStore(t, 10, 8), pairsBeforeAbort: 3}

	_, err := ScanSubspace(t.Context(), store, []byte("p"), Limits{MaxPairs: 1000, MaxBytes: DefaultMaxSubspaceBytes})
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, storetypes.ErrSubspaceCapExceeded.Is(err))
}
