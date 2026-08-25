package query

import (
	"context"
	"fmt"

	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/kv"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
)

const (
	DefaultMaxSubspacePairs = 1_000
	DefaultMaxSubspaceBytes = 4 * 1024 * 1024 // 4 MiB
)

// Limits bounds how many pairs and bytes a /subspace scan may materialize.
type Limits struct {
	MaxPairs int
	MaxBytes int
}

// effective returns limits with non-positive fields replaced by package defaults.
func (l Limits) effective() Limits {
	if l.MaxPairs <= 0 {
		l.MaxPairs = DefaultMaxSubspacePairs
	}
	if l.MaxBytes <= 0 {
		l.MaxBytes = DefaultMaxSubspaceBytes
	}
	return l
}

// ScanSubspace walks prefix in st and returns marshaled kv.Pairs.
// It stops with ErrSubspaceCapExceeded when either limit would be exceeded.
func ScanSubspace(ctx context.Context, st storetypes.KVStore, prefix []byte, limits Limits) ([]byte, error) {
	limits = limits.effective()

	pairs := kv.Pairs{
		Pairs: make([]kv.Pair, 0),
	}
	totalBytes := 0

	iterator := storetypes.KVStorePrefixIterator(st, prefix)
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

	bz, err := pairs.Marshal()
	if err != nil {
		panic(fmt.Errorf("failed to marshal KV pairs: %w", err))
	}
	return bz, nil
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
