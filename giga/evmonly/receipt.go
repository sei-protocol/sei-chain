package evmonly

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

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
	records := make([]receipt.ReceiptRecord, 0, len(result.Receipts))
	for i := range result.Receipts {
		record, keep, err := encodeReceiptRecord(blockNumber, result, i)
		if err != nil {
			return nil, err
		}
		if keep {
			records = append(records, record)
		}
	}
	indexReceiptRecords(records)
	return records, nil
}

// encodeReceiptRecord builds the record for transaction i, and reports false when the transaction
// stores no receipt. The record's TransactionIndex is left for indexReceiptRecords. Records are
// independent of each other, so a caller holding a worker pool may build them in parallel.
func encodeReceiptRecord(blockNumber uint64, result *BlockResult, i int) (receipt.ReceiptRecord, bool, error) {
	ethReceipt := result.Receipts[i]
	if ethReceipt == nil {
		return receipt.ReceiptRecord{}, false, fmt.Errorf("receipt %d is nil", i)
	}
	txResult := result.Txs[i]
	// A rejected transaction never ran, and an Ethereum block could not have included it, so it
	// stores no receipt: its hash keeps the receipt of the block that does execute it, earlier or
	// later.
	if txResult.Rejected {
		return receipt.ReceiptRecord{}, false, nil
	}
	unadjustedIndex, ok := utils.SafeCast[uint32](ethReceipt.TransactionIndex)
	if !ok {
		return receipt.ReceiptRecord{}, false, fmt.Errorf("receipt %d transaction index %d exceeds uint32", i, ethReceipt.TransactionIndex)
	}
	status, ok := utils.SafeCast[uint32](ethReceipt.Status)
	if !ok {
		return receipt.ReceiptRecord{}, false, fmt.Errorf("receipt %d status %d exceeds uint32", i, ethReceipt.Status)
	}
	stored := &evmtypes.Receipt{
		TxType:                     uint32(ethReceipt.Type),
		CumulativeGasUsed:          ethReceipt.CumulativeGasUsed,
		TxHashHex:                  ethReceipt.TxHash.Hex(),
		GasUsed:                    ethReceipt.GasUsed,
		BlockNumber:                blockNumber,
		UnadjustedTransactionIndex: unadjustedIndex,
		Status:                     status,
		From:                       txResult.Sender.Hex(),
		Logs:                       evmtypes.NewLogsFromEth(ethReceipt.Logs),
		LogsBloom:                  append([]byte(nil), ethReceipt.Bloom[:]...),
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
	// Reward is the tx's priority fee unfiltered: giga's base fee is always zero
	// (evmOnlyBaseFee), and its admission floor already guarantees a positive effective price.
	var reward *big.Int
	if ethReceipt.EffectiveGasPrice != nil {
		reward = new(big.Int).SetUint64(stored.EffectiveGasPrice)
	}
	return receipt.ReceiptRecord{TxHash: ethReceipt.TxHash, Receipt: stored, Reward: reward}, true, nil
}

// indexReceiptRecords sets each record's TransactionIndex to its position among a block's records,
// skipping the transactions that store no receipt as if the block did not hold them.
func indexReceiptRecords(records []receipt.ReceiptRecord) {
	var index uint32
	for i := range records {
		records[i].Receipt.TransactionIndex = index
		index++
	}
}

// receiptRecordsParallel builds the block's records across the executor's worker pool, returning
// the same records in the same order as receiptRecords.
func (e *Executor) receiptRecordsParallel(ctx context.Context, blockNumber uint64, result *BlockResult) ([]receipt.ReceiptRecord, error) {
	count := len(result.Receipts)
	if e.occPool == nil || e.closed.Load() || count < occParallelReceiptThreshold {
		return receiptRecords(blockNumber, result)
	}
	if count != len(result.Txs) {
		return nil, fmt.Errorf("receipt count %d does not match transaction result count %d", count, len(result.Txs))
	}
	records := make([]receipt.ReceiptRecord, count)
	kept := make([]bool, count)
	chunk := occChunkSize(count, e.cfg.OCCWorkers)
	ranges := occRanges(count, chunk)
	err := e.occPool.Run(ctx, len(ranges), func(workerCtx context.Context, workerID int, workers int) error {
		for rangeIndex := workerID; rangeIndex < len(ranges); rangeIndex += workers {
			for i := ranges[rangeIndex].start; i < ranges[rangeIndex].end; i++ {
				if err := workerCtx.Err(); err != nil {
					return err
				}
				record, keep, err := encodeReceiptRecord(blockNumber, result, i)
				if err != nil {
					return err
				}
				records[i], kept[i] = record, keep
			}
		}
		return nil
	})
	if errors.Is(err, errOCCWorkerPoolClosed) {
		return receiptRecords(blockNumber, result)
	}
	if err != nil {
		return nil, err
	}
	compacted := records[:0]
	for i := range records {
		if kept[i] {
			compacted = append(compacted, records[i])
		}
	}
	indexReceiptRecords(compacted)
	return compacted, nil
}

// occParallelReceiptThreshold is the block size below which fanning receipt encoding out costs more
// than it saves.
const occParallelReceiptThreshold = 64

func newReceiptContext(ctx context.Context, blockHeight int64) sdk.Context {
	return sdk.NewContext(nil, tmproto.Header{Height: blockHeight}, false).WithContext(ctx)
}
