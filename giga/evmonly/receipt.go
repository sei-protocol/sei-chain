package evmonly

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

func receiptRecords(blockNumber uint64, result *BlockResult) ([]receipt.ReceiptRecord, error) {
	if len(result.Receipts) != len(result.Txs) {
		return nil, fmt.Errorf("receipt count %d does not match transaction result count %d", len(result.Receipts), len(result.Txs))
	}
	records := make([]receipt.ReceiptRecord, len(result.Receipts))
	for i := range result.Receipts {
		if err := encodeReceiptRecord(blockNumber, result, i, records); err != nil {
			return nil, err
		}
	}
	return executedReceiptRecords(records), nil
}

// encodeReceiptRecord writes the record for one transaction. Records are independent of each other,
// so a caller holding a worker pool may fill the slice in parallel.
func encodeReceiptRecord(blockNumber uint64, result *BlockResult, i int, records []receipt.ReceiptRecord) error {
	{
		ethReceipt := result.Receipts[i]
		if ethReceipt == nil {
			return fmt.Errorf("receipt %d is nil", i)
		}
		transactionIndex, ok := utils.SafeCast[uint32](ethReceipt.TransactionIndex)
		if !ok {
			return fmt.Errorf("receipt %d transaction index %d exceeds uint32", i, ethReceipt.TransactionIndex)
		}
		status, ok := utils.SafeCast[uint32](ethReceipt.Status)
		if !ok {
			return fmt.Errorf("receipt %d status %d exceeds uint32", i, ethReceipt.Status)
		}
		txResult := result.Txs[i]
		// A replay must not replace the receipt of its earlier execution.
		if errors.Is(txResult.Err, core.ErrNonceTooLow) {
			return nil
		}
		stored := &evmtypes.Receipt{
			TxType:            uint32(ethReceipt.Type),
			CumulativeGasUsed: ethReceipt.CumulativeGasUsed,
			TxHashHex:         ethReceipt.TxHash.Hex(),
			GasUsed:           ethReceipt.GasUsed,
			BlockNumber:       blockNumber,
			TransactionIndex:  transactionIndex,
			Status:            status,
			From:              txResult.Sender.Hex(),
			Logs:              evmtypes.NewLogsFromEth(ethReceipt.Logs),
			LogsBloom:         append([]byte(nil), ethReceipt.Bloom[:]...),
		}
		if ethReceipt.EffectiveGasPrice != nil {
			stored.EffectiveGasPrice = ethReceipt.EffectiveGasPrice.Uint64()
		}
		if txResult.To != nil {
			stored.To = txResult.To.Hex()
		}
		if txResult.ContractAddress != (common.Address{}) {
			stored.ContractAddress = txResult.ContractAddress.Hex()
		}
		if txResult.Err != nil {
			stored.VmError = txResult.Err.Error()
		}
		records[i] = receipt.ReceiptRecord{TxHash: ethReceipt.TxHash, Receipt: stored}
	}
	return nil
}

// executedReceiptRecords excludes transactions rejected before execution.
func executedReceiptRecords(records []receipt.ReceiptRecord) []receipt.ReceiptRecord {
	return slices.DeleteFunc(records, func(record receipt.ReceiptRecord) bool {
		return record.Receipt == nil
	})
}

// receiptRecordsParallel fills the block's records across the executor's worker pool. Encoding is a
// per-transaction transform of a finished result, so it parallelizes cleanly, and it is otherwise
// one of the larger serial stretches between a block's execution and its commit.
func (e *Executor) receiptRecordsParallel(ctx context.Context, blockNumber uint64, result *BlockResult) ([]receipt.ReceiptRecord, error) {
	count := len(result.Receipts)
	if e.occPool == nil || count < occParallelReceiptThreshold {
		return receiptRecords(blockNumber, result)
	}
	if count != len(result.Txs) {
		return nil, fmt.Errorf("receipt count %d does not match transaction result count %d", count, len(result.Txs))
	}
	records := make([]receipt.ReceiptRecord, count)
	chunk := occChunkSize(count, e.cfg.OCCWorkers)
	ranges := occRanges(count, chunk)
	err := e.occPool.Run(ctx, len(ranges), func(workerCtx context.Context, workerID int, workers int) error {
		for rangeIndex := workerID; rangeIndex < len(ranges); rangeIndex += workers {
			for i := ranges[rangeIndex].start; i < ranges[rangeIndex].end; i++ {
				if err := workerCtx.Err(); err != nil {
					return err
				}
				if err := encodeReceiptRecord(blockNumber, result, i, records); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return executedReceiptRecords(records), nil
}

// occParallelReceiptThreshold is the block size below which fanning receipt encoding out costs more
// than it saves.
const occParallelReceiptThreshold = 64

func newReceiptContext(ctx context.Context, blockHeight int64) sdk.Context {
	return sdk.NewContext(nil, tmproto.Header{Height: blockHeight}, false).WithContext(ctx)
}
