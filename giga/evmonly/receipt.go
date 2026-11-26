package evmonly

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"

	receipttypes "github.com/sei-protocol/sei-chain/sei-db/receipt_db/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

func receiptRecords(block finalizedBlock) ([]receipttypes.ReceiptRecord, error) {
	records := make([]receipttypes.ReceiptRecord, len(block.receipts))
	for i := range block.receipts {
		record, err := encodeReceiptRecord(block, i)
		if err != nil {
			return nil, err
		}
		records[i] = record
	}
	return records, nil
}

// encodeReceiptRecord builds the stored receipt for transaction i of a finalized block.
func encodeReceiptRecord(block finalizedBlock, i int) (receipttypes.ReceiptRecord, error) {
	ethReceipt := block.receipts[i]
	txResult := block.results[i]
	transactionIndex, ok := utils.SafeCast[uint32](ethReceipt.TransactionIndex)
	if !ok {
		return receipttypes.ReceiptRecord{}, fmt.Errorf("receipt %d transaction index %d exceeds uint32", i, ethReceipt.TransactionIndex)
	}
	status, ok := utils.SafeCast[uint32](ethReceipt.Status)
	if !ok {
		return receipttypes.ReceiptRecord{}, fmt.Errorf("receipt %d status %d exceeds uint32", i, ethReceipt.Status)
	}
	stored := &evmtypes.Receipt{
		TxType:            uint32(ethReceipt.Type),
		CumulativeGasUsed: ethReceipt.CumulativeGasUsed,
		TxHashHex:         ethReceipt.TxHash.Hex(),
		GasUsed:           ethReceipt.GasUsed,
		BlockNumber:       block.context.Number,
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
	// Reward is the tx's priority fee unfiltered: giga's base fee is always zero
	// (evmOnlyBaseFee), and its admission floor already guarantees a positive effective price.
	var reward *big.Int
	if ethReceipt.EffectiveGasPrice != nil {
		reward = new(big.Int).SetUint64(stored.EffectiveGasPrice)
	}
	return receipttypes.ReceiptRecord{
		TxHash: ethReceipt.TxHash, Receipt: stored, Reward: reward,
	}, nil
}

// receiptRecordsParallel builds a finalized block's receipt records across the executor's worker pool.
func (e *Executor) receiptRecordsParallel(ctx context.Context, block finalizedBlock) ([]receipttypes.ReceiptRecord, error) {
	count := len(block.receipts)
	if e.occPool == nil || e.closed.Load() || count < occParallelReceiptThreshold {
		return receiptRecords(block)
	}
	records := make([]receipttypes.ReceiptRecord, count)
	chunk := occChunkSize(count, e.cfg.OCCWorkers)
	ranges := occRanges(count, chunk)
	err := e.occPool.Run(ctx, len(ranges), func(workerCtx context.Context, workerID int, workers int) error {
		for rangeIndex := workerID; rangeIndex < len(ranges); rangeIndex += workers {
			for i := ranges[rangeIndex].start; i < ranges[rangeIndex].end; i++ {
				if err := workerCtx.Err(); err != nil {
					return err
				}
				record, err := encodeReceiptRecord(block, i)
				if err != nil {
					return err
				}
				records[i] = record
			}
		}
		return nil
	})
	if errors.Is(err, errOCCWorkerPoolClosed) {
		return receiptRecords(block)
	}
	if err != nil {
		return nil, err
	}
	return records, nil
}

// occParallelReceiptThreshold is the block size below which fanning receipt encoding out costs more
// than it saves.
const occParallelReceiptThreshold = 64

func newReceiptContext(ctx context.Context, blockHeight int64) sdk.Context {
	return sdk.NewContext(nil, tmproto.Header{Height: blockHeight}, false).WithContext(ctx)
}
