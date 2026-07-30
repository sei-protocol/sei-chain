package indexer_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer"
)

func TestScanBudget_NilAndZeroAreUnlimited(t *testing.T) {
	// A nil *ScanBudget behaves as unlimited: its lease never rejects.
	var nilBudget *indexer.ScanBudget
	require.Equal(t, int64(0), nilBudget.InFlight())

	lease := nilBudget.Lease()
	require.NoError(t, lease.Visit(1<<30))
	require.Equal(t, int64(0), nilBudget.InFlight())
	lease.Release() // must not panic

	// A zero/non-positive max also disables the cap.
	for _, maxBudgets := range []int{0, -1} {
		b := indexer.NewScanBudget(maxBudgets)
		l := b.Lease()
		require.NoError(t, l.Visit(1<<30))
		require.Equal(t, int64(0), b.InFlight(), "unlimited budget should not track charges")
	}
}

func TestScanBudget_ChargesAndReleases(t *testing.T) {
	b := indexer.NewScanBudget(100)
	lease := b.Lease()

	require.NoError(t, lease.Visit(10))
	require.Equal(t, int64(10), b.InFlight())

	require.NoError(t, lease.Visit(40))
	require.Equal(t, int64(50), b.InFlight())

	lease.Release()
	require.Equal(t, int64(0), b.InFlight())

	// Release is idempotent: a second call returns nothing further.
	lease.Release()
	require.Equal(t, int64(0), b.InFlight())
}

func TestScanBudget_VisitZeroIsNoop(t *testing.T) {
	b := indexer.NewScanBudget(10)
	lease := b.Lease()

	require.NoError(t, lease.Visit(0))
	require.Equal(t, int64(0), b.InFlight())
}

func TestScanBudget_ExceedReturnsError(t *testing.T) {
	b := indexer.NewScanBudget(10)
	lease := b.Lease()

	// Reaching exactly the ceiling is allowed; only exceeding it fails.
	require.NoError(t, lease.Visit(10))
	require.Equal(t, int64(10), b.InFlight())

	err := lease.Visit(1)
	require.ErrorIs(t, err, indexer.ErrScanBudgetExceeded)

	// The over-limit entries are still charged until Release (the search
	// aborts and releases via defer), so the caller sees the overshoot.
	require.Equal(t, int64(11), b.InFlight())

	lease.Release()
	require.Equal(t, int64(0), b.InFlight())
}

func TestScanBudget_SharedAcrossLeases(t *testing.T) {
	b := indexer.NewScanBudget(10)
	a := b.Lease()
	c := b.Lease()

	require.NoError(t, a.Visit(6))
	require.NoError(t, c.Visit(4))
	require.Equal(t, int64(10), b.InFlight())

	// The next visit on either lease exceeds the shared ceiling.
	require.ErrorIs(t, c.Visit(1), indexer.ErrScanBudgetExceeded)

	// Releasing one lease returns only its own charge to the pool.
	a.Release()
	require.Equal(t, int64(5), b.InFlight())
	c.Release()
	require.Equal(t, int64(0), b.InFlight())
}

func TestScanBudget_NilLeaseSafe(t *testing.T) {
	var lease *indexer.ScanLease
	require.NoError(t, lease.Visit(5))
	require.NotPanics(t, func() { lease.Release() })
}

// TestScanBudget_ConcurrentReleaseBalances exercises the shared counter under
// concurrency: every charged entry must be returned, so InFlight settles back
// to zero once all leases release regardless of interleaving.
func TestScanBudget_ConcurrentReleaseBalances(t *testing.T) {
	const workers = 50
	const perWorker = 20

	// Large ceiling so no worker trips the cap; we're checking accounting.
	b := indexer.NewScanBudget(workers * perWorker)

	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			lease := b.Lease()
			defer lease.Release()
			for range perWorker {
				_ = lease.Visit(1)
			}
		})
	}
	wg.Wait()

	require.Equal(t, int64(0), b.InFlight(), "all charges must be released")
}

// TestScanBudget_SoftCapOvershoot documents the intentionally soft cap: because
// each lease charges before observing the ceiling, concurrent leases can push
// the aggregate past max, and every one of them may still get an error.
func TestScanBudget_SoftCapOvershoot(t *testing.T) {
	const leases = 8
	// Ceiling of 1 with each lease charging 1: the first Add already reaches
	// the ceiling, so every concurrent lease charging 1 overshoots.
	b := indexer.NewScanBudget(1)

	var wg sync.WaitGroup
	errs := make([]error, leases)
	start := make(chan struct{})
	for i := range leases {
		wg.Go(func() {
			lease := b.Lease()
			<-start
			errs[i] = lease.Visit(1)
		})
	}
	close(start)
	wg.Wait()

	// The aggregate overshot the ceiling of 1 by the number of leases.
	require.Equal(t, int64(leases), b.InFlight())

	// At least one lease saw the budget exceeded (all but possibly the first).
	exceeded := 0
	for _, err := range errs {
		if errors.Is(err, indexer.ErrScanBudgetExceeded) {
			exceeded++
		}
	}
	require.GreaterOrEqual(t, exceeded, leases-1)
}
