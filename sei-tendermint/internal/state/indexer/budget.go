package indexer

import (
	"errors"
	"sync/atomic"
)

// ErrScanBudgetExceeded is returned by a KV index Search when the aggregate
// number of index entries visited by all in-flight searches would exceed the
// configured scan budget. It signals overload: the caller should narrow the
// query or retry later.
var ErrScanBudgetExceeded = errors.New("kv indexer scan budget exceeded; narrow the query or retry later")

// ScanBudget bounds the total number of index entries that all in-flight KV
// index searches (tx_search and block_search) may visit at once.
// The zero value and a nil *ScanBudget both behave as "unlimited".
type ScanBudget struct {
	// max is the ceiling on the number of concurrently-charged entries.
	// A value <= 0 means unlimited.
	max int64
	// inFlight is the number of entries currently charged by open leases.
	inFlight atomic.Int64
}

// NewScanBudget returns a ScanBudget capped at max entries. A non-positive max
// disables the cap (unlimited).
func NewScanBudget(max int) *ScanBudget {
	return &ScanBudget{max: int64(max)}
}

// Lease opens a per-search charge against the budget. A search calls Visit as
// it walks index entries and Release (typically via defer) when it returns, so
// its entries are returned to the shared pool.
func (b *ScanBudget) Lease() *ScanLease {
	return &ScanLease{budget: b}
}

// InFlight returns the number of entries currently charged across all open
// leases. Intended for tests and metrics.
func (b *ScanBudget) InFlight() int64 {
	if b == nil {
		return 0
	}
	return b.inFlight.Load()
}

// ScanLease tracks the index entries a single search has charged against a
// ScanBudget. Each search must use its own lease; a ScanLease is not safe for
// concurrent use.
type ScanLease struct {
	budget *ScanBudget
	// held is the number of entries this lease has charged to the shared
	// counter and not yet released.
	held int64
}

// Visit records that the search has walked n more index entries and charges
// them to the shared budget. It returns ErrScanBudgetExceeded once the
// aggregate in-flight charge exceeds the budget's ceiling.
func (l *ScanLease) Visit(n int64) error {
	if l == nil || l.budget == nil || l.budget.max <= 0 || n == 0 {
		return nil
	}
	total := l.budget.inFlight.Add(n)
	l.held += n
	if total > l.budget.max {
		return ErrScanBudgetExceeded
	}
	return nil
}

// Release returns every entry this lease has charged to the shared budget.
func (l *ScanLease) Release() {
	if l == nil || l.budget == nil || l.held == 0 {
		return
	}
	l.budget.inFlight.Add(-l.held)
	l.held = 0
}
