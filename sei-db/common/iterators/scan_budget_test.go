package iterators_test

import (
	"context"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/iterators"
	"github.com/stretchr/testify/require"
)

func TestBudgetedIterator_AllowsExactlyTheBudget(t *testing.T) {
	budget := iterators.NewScanBudget(t.Context(), 3)
	it := iterators.NewBudgetedIterator(memIter(t, []byte("a"), []byte("b"), []byte("c")), budget)

	require.Len(t, collect(t, it), 3)
	require.NoError(t, it.Close())
}

func TestBudgetedIterator_RefusesTheRowPastTheBudget(t *testing.T) {
	budget := iterators.NewScanBudget(t.Context(), 2)
	it := iterators.NewBudgetedIterator(memIter(t, []byte("a"), []byte("b"), []byte("c")), budget)

	require.True(t, it.Valid())
	it.Next()
	require.True(t, it.Valid())
	it.Next()
	require.False(t, it.Valid())
	require.Nil(t, it.Key())
	require.ErrorIs(t, it.Error(), iterators.ErrScanBudgetExceeded)
	require.NoError(t, it.Close())
}

func TestBudgetedIterator_ChargesTheRowPositionedAtConstruction(t *testing.T) {
	budget := iterators.NewScanBudget(t.Context(), 0)
	it := iterators.NewBudgetedIterator(memIter(t, []byte("a")), budget)

	require.False(t, it.Valid())
	require.ErrorIs(t, it.Error(), iterators.ErrScanBudgetExceeded)
	require.NoError(t, it.Close())
}

func TestBudgetedIterator_EmptyParentSpendsNothing(t *testing.T) {
	budget := iterators.NewScanBudget(t.Context(), 0)
	it := iterators.NewBudgetedIterator(memIter(t), budget)

	require.False(t, it.Valid())
	require.NoError(t, it.Error())
	require.NoError(t, it.Close())
}

func TestBudgetedIterator_SharesTheBudgetAcrossIterators(t *testing.T) {
	budget := iterators.NewScanBudget(t.Context(), 3)
	first := iterators.NewBudgetedIterator(memIter(t, []byte("a"), []byte("b")), budget)
	second := iterators.NewBudgetedIterator(memIter(t, []byte("c"), []byte("d")), budget)

	merged, err := iterators.NewMergingIterator(true, first, second)
	require.NoError(t, err)
	for merged.Valid() {
		merged.Next()
	}
	require.ErrorIs(t, merged.Error(), iterators.ErrScanBudgetExceeded)
	require.NoError(t, merged.Close())
}

func TestBudgetedIterator_StopsWhenTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	budget := iterators.NewScanBudget(ctx, 100)
	it := iterators.NewBudgetedIterator(memIter(t, []byte("a"), []byte("b")), budget)

	require.True(t, it.Valid())
	cancel()
	it.Next()
	require.False(t, it.Valid())
	require.ErrorIs(t, it.Error(), context.Canceled)
	require.NoError(t, it.Close())
}

func TestBudgetedIterator_NilBudgetReturnsTheParent(t *testing.T) {
	parent := memIter(t, []byte("a"))
	require.Same(t, parent, iterators.NewBudgetedIterator(parent, nil))
	require.NoError(t, parent.Close())
}

// TestBudgetedIterator_BoundsRowsATransformSkips pins that a transforming iterator over a budgeted parent
// cannot read past the budget while it searches for a row to yield, including during its construction.
func TestBudgetedIterator_BoundsRowsATransformSkips(t *testing.T) {
	skipAll := func(_, _ []byte) ([]byte, []byte, bool, error) { return nil, nil, true, nil }

	budget := iterators.NewScanBudget(t.Context(), 2)
	parent := iterators.NewBudgetedIterator(memIter(t, []byte("a"), []byte("b"), []byte("c")), budget)
	_, err := iterators.NewTransformingIterator(parent, skipAll)
	require.ErrorIs(t, err, iterators.ErrScanBudgetExceeded)

	budget = iterators.NewScanBudget(t.Context(), 3)
	parent = iterators.NewBudgetedIterator(memIter(t, []byte("a"), []byte("b"), []byte("c")), budget)
	it, err := iterators.NewTransformingIterator(parent, skipAll)
	require.NoError(t, err)
	require.False(t, it.Valid())
	require.NoError(t, it.Close())
}
