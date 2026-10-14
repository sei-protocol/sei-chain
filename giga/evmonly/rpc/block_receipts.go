package rpc

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	ethrpc "github.com/ethereum/go-ethereum/rpc"

	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

var (
	errBlockReceiptsNoBlock = errors.New("eth_getBlockReceipts requires a block number or hash")
	errBlockReceiptsPruned  = errors.New("eth_getBlockReceipts block receipts are pruned")
)

// GetBlockReceipts returns the receipts stored for a finalized block, in
// transaction index order. It returns nil for an unknown hash, a
// zero/negative or future height, or a block whose receipts are not yet
// indexed, and an error for a pruned height or pruned receipts. Current-state
// tags resolve to the latest block whose receipts are indexed.
func (api *blockAPI) GetBlockReceipts(ctx context.Context, blockNrOrHash ethrpc.BlockNumberOrHash) ([]map[string]any, error) {
	block, err := api.resolveReceiptsBlock(ctx, blockNrOrHash)
	if err != nil || block == nil {
		return nil, err
	}
	height := uint64(block.Block.Height) //nolint:gosec // G115: a resolved block's height is positive.
	if _, latest := indexedRange(api.backend, api.store); height > latest {
		return nil, nil
	}
	records, err := blockReceiptRecords(ctx, api.store, block)
	if err != nil {
		return nil, err
	}
	// Checked after the read, so a retention floor that rose during it is caught too.
	if earliest, _ := indexedRange(api.backend, api.store); height < earliest {
		return nil, fmt.Errorf("%w: block %d; earliest available block is %d", errBlockReceiptsPruned, height, earliest)
	}
	blockHash := common.BytesToHash(block.BlockID.Hash)
	receipts := make([]map[string]any, len(records))
	for i, record := range records {
		receipts[i] = encodeReceipt(record.TxHash, record.Receipt, blockHash)
	}
	return receipts, nil
}

// resolveReceiptsBlock returns the block blockNrOrHash names, or a nil block
// when there is none. Current-state tags name the latest block whose receipts
// are indexed rather than the committed head.
func (api *blockAPI) resolveReceiptsBlock(ctx context.Context, blockNrOrHash ethrpc.BlockNumberOrHash) (*coretypes.ResultBlock, error) {
	if hash, ok := blockNrOrHash.Hash(); ok {
		return api.resolveBlockByHash(ctx, hash)
	}
	number, ok := blockNrOrHash.Number()
	if !ok {
		return nil, &invalidParamsError{error: errBlockReceiptsNoBlock}
	}
	switch number {
	case ethrpc.LatestBlockNumber, ethrpc.SafeBlockNumber, ethrpc.FinalizedBlockNumber, ethrpc.PendingBlockNumber:
		_, latest := indexedRange(api.backend, api.store)
		number = ethrpc.BlockNumber(latest) //nolint:gosec // G115: an indexed height fits in int64.
	}
	return api.resolveBlockByNumber(ctx, number)
}
