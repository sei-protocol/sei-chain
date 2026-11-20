package rpc

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	receiptdb "github.com/sei-protocol/sei-chain/sei-db/receipt_db"
)

// blockBaseFee returns the base fee the block at height ran at: the one store recorded with the
// block's stats, or unrecordedBaseFee's when the stats hold none.
func blockBaseFee(ctx context.Context, backend Backend, store receiptdb.ReceiptStore, height int64) (*big.Int, error) {
	if store != nil && height > 0 {
		stats, err := store.GetBlockStats(receiptContext(ctx), uint64(height))
		switch {
		case err == nil && stats.BaseFee != nil:
			return stats.BaseFee, nil
		case err != nil && !errors.Is(err, receiptdb.ErrNotFound) && !errors.Is(err, receiptdb.ErrBlockStatsNotSupported):
			return nil, fmt.Errorf("read the base fee of block %d: %w", height, err)
		}
	}
	return unrecordedBaseFee(backend, height), nil
}

// unrecordedBaseFee returns the backend's base fee for the block at height: the next block's past
// the latest committed block, and the latest committed block's otherwise. It is exact for those two
// blocks, and stands in for an older block whose stats hold no base fee.
func unrecordedBaseFee(backend Backend, height int64) *big.Int {
	if height > 0 && uint64(height) > backend.EvmBlockNumber() {
		return backend.EvmNextBaseFee()
	}
	return backend.EvmBaseFee()
}
