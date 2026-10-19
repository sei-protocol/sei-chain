package query

import (
	"context"
	"fmt"

	"cosmossdk.io/errors"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/kv"
	"github.com/sei-protocol/sei-chain/sei-db/common/iterators"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
)

const (
	DefaultMaxSubspacePairs       = 1_000
	DefaultMaxSubspaceBytes       = 4 * 1024 * 1024 // 4 MiB
	DefaultMaxSubspaceScannedRows = 100_000
)

// Limits bounds how many pairs and bytes a /subspace scan may materialize, and how many rows the
// store iterator may return to find them.
type Limits struct {
	MaxPairs       int
	MaxBytes       int
	MaxScannedRows int
}

// effective returns limits with non-positive fields replaced by package defaults. A non-positive
// MaxScannedRows resolves to the larger of DefaultMaxSubspaceScannedRows and MaxPairs.
func (l Limits) effective() Limits {
	if l.MaxPairs <= 0 {
		l.MaxPairs = DefaultMaxSubspacePairs
	}
	if l.MaxBytes <= 0 {
		l.MaxBytes = DefaultMaxSubspaceBytes
	}
	if l.MaxScannedRows <= 0 {
		// A store that yields one pair per row must reach the pair cap before the scan budget.
		l.MaxScannedRows = max(DefaultMaxSubspaceScannedRows, l.MaxPairs)
	}
	return l
}

// BudgetedIterable is implemented by stores that can read rows from storage without yielding them, and
// so can charge every row they read to a ScanBudget. The returned error reports a failed construction,
// including a budget exhausted before the first row was yielded.
type BudgetedIterable interface {
	BudgetedIterator(budget *iterators.ScanBudget, start, end []byte) (storetypes.Iterator, error)
}

// ScanSubspace walks prefix in st and returns marshaled kv.Pairs.
// It stops with ErrSubspaceCapExceeded when any limit would be exceeded.
func ScanSubspace(ctx context.Context, st storetypes.KVStore, prefix []byte, limits Limits) ([]byte, error) {
	if len(prefix) == 0 {
		return nil, errors.Wrap(sdkerrors.ErrInvalidRequest, "subspace prefix must not be empty")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	limits = limits.effective()

	pairs := kv.Pairs{
		Pairs: make([]kv.Pair, 0),
	}
	totalBytes := 0

	iterator, err := openSubspaceIterator(ctx, st, prefix, limits)
	if err != nil {
		return nil, scanError(err, limits)
	}
	defer func() { _ = iterator.Close() }()

	for ; iterator.Valid(); iterator.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		key := iterator.Key()
		value := iterator.Value()
		pairBytes := len(key) + len(value)

		if len(pairs.Pairs)+1 > limits.MaxPairs || totalBytes+pairBytes > limits.MaxBytes {
			return nil, storetypes.ErrSubspaceCapExceeded.Wrap("use a narrower prefix")
		}

		pairs.Pairs = append(pairs.Pairs, kv.Pair{Key: key, Value: value})
		totalBytes += pairBytes
	}

	if err := abortedScan(ctx, iterator); err != nil {
		return nil, scanError(err, limits)
	}

	bz, err := pairs.Marshal()
	if err != nil {
		panic(fmt.Errorf("failed to marshal KV pairs: %w", err))
	}
	return bz, nil
}

// openSubspaceIterator opens an ascending iterator over prefix. A store that can read rows it does not
// yield is opened with a scan budget, so that the rows it reads and skips are bounded too; the pair and
// byte limits see only the rows it yields.
func openSubspaceIterator(ctx context.Context, st storetypes.KVStore, prefix []byte, limits Limits) (storetypes.Iterator, error) {
	end := storetypes.PrefixEndBytes(prefix)
	if budgeted, ok := st.(BudgetedIterable); ok {
		return budgeted.BudgetedIterator(iterators.NewScanBudget(ctx, limits.MaxScannedRows), prefix, end)
	}
	return storetypes.IteratorOn(st, ctx, prefix, end, true), nil
}

// scanError reports an exhausted scan budget as a cap rejection, and passes every other error through.
func scanError(err error, limits Limits) error {
	if errors.IsOf(err, iterators.ErrScanBudgetExceeded) {
		return storetypes.ErrSubspaceCapExceeded.Wrapf(
			"scan read more than %d rows; use a narrower prefix", limits.MaxScannedRows)
	}
	return err
}

// abortedScan returns why iteration stopped before the prefix was exhausted, or
// nil when it was. An SS iterator that gives up inside an MVCC skip loop goes
// invalid and reports the reason on Error(), which is otherwise indistinguishable
// from a complete scan and would be marshaled as a successful truncated result.
func abortedScan(ctx context.Context, iterator storetypes.Iterator) error {
	if err := iterator.Error(); err != nil {
		return err
	}
	return ctx.Err()
}

// IsCapExceeded reports whether err is a subspace scan cap rejection.
func IsCapExceeded(err error) bool {
	return storetypes.ErrSubspaceCapExceeded.Is(err)
}

// IsCapExceededResponse reports whether res is a subspace scan cap rejection.
func IsCapExceededResponse(res abci.ResponseQuery) bool {
	return res.Codespace == storetypes.StoreCodespace &&
		res.Code == storetypes.ErrSubspaceCapExceeded.ABCICode()
}
