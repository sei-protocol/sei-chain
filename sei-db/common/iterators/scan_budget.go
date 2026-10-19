package iterators

import (
	"context"
	"errors"

	dbm "github.com/tendermint/tm-db"
)

var _ dbm.Iterator = (*budgetedIterator)(nil)

// ErrScanBudgetExceeded is reported by an iterator whose scan read more rows than its ScanBudget allows.
var ErrScanBudgetExceeded = errors.New("scan read more rows than its budget allows")

// ScanBudget bounds the rows the store iterators of one scan may return, and ends the scan when its
// context ends. Rows are charged as the store iterator returns them, before any lane filtering, so rows
// a scan reads and never yields are charged too. Rows the store iterator skips itself, such as deletions
// and tombstones, are not charged. Every iterator opened for one scan shares its budget. A nil
// *ScanBudget imposes no bound. A ScanBudget is not safe for concurrent use.
type ScanBudget struct {
	ctx       context.Context
	remaining int
}

// NewScanBudget returns a budget that allows maxRows rows to be read while ctx is live.
func NewScanBudget(ctx context.Context, maxRows int) *ScanBudget {
	if ctx == nil {
		ctx = context.Background()
	}
	return &ScanBudget{ctx: ctx, remaining: maxRows}
}

// spend charges one row read. It returns the context's error once the context ends, and
// ErrScanBudgetExceeded once the budget has no rows left.
func (b *ScanBudget) spend() error {
	if err := b.ctx.Err(); err != nil {
		return err
	}
	if b.remaining <= 0 {
		return ErrScanBudgetExceeded
	}
	b.remaining--
	return nil
}

// budgetedIterator charges each row its parent positions on to a ScanBudget, and goes invalid with the
// budget's error once a charge is refused.
type budgetedIterator struct {
	parent dbm.Iterator
	budget *ScanBudget
	err    error
}

// NewBudgetedIterator returns an iterator that yields parent's rows and charges each one to budget,
// including the row parent is positioned on at construction. Once budget refuses a row, the iterator
// is invalid and Error reports why. A nil budget returns parent unchanged.
func NewBudgetedIterator(parent dbm.Iterator, budget *ScanBudget) dbm.Iterator {
	if budget == nil {
		return parent
	}
	it := &budgetedIterator{parent: parent, budget: budget}
	it.charge()
	return it
}

// charge spends one row for the parent's current position, if it has one.
func (it *budgetedIterator) charge() {
	if it.err != nil || !it.parent.Valid() {
		return
	}
	it.err = it.budget.spend()
}

func (it *budgetedIterator) Domain() ([]byte, []byte) {
	return it.parent.Domain()
}

func (it *budgetedIterator) Valid() bool {
	return it.err == nil && it.parent.Valid()
}

func (it *budgetedIterator) Next() {
	if !it.Valid() {
		return
	}
	it.parent.Next()
	it.charge()
}

func (it *budgetedIterator) Key() []byte {
	if !it.Valid() {
		return nil
	}
	return it.parent.Key()
}

func (it *budgetedIterator) Value() []byte {
	if !it.Valid() {
		return nil
	}
	return it.parent.Value()
}

func (it *budgetedIterator) Error() error {
	if it.err != nil {
		return it.err
	}
	return it.parent.Error()
}

func (it *budgetedIterator) Close() error {
	return it.parent.Close()
}
