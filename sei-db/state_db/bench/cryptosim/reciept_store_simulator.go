package cryptosim

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	dbLogger "github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
	receiptpkg "github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// A simulated reciept store.
type RecieptStoreSimulator struct {
	ctx    context.Context
	cancel context.CancelFunc

	config *CryptoSimConfig

	recieptsChan chan *block

	store   *parquet.Store
	metrics *CryptosimMetrics
}

// Creates a new reciept store simulator.
func NewRecieptStoreSimulator(
	ctx context.Context,
	config *CryptoSimConfig,
	recieptsChan chan *block,
	metrics *CryptosimMetrics,
) (*RecieptStoreSimulator, error) {
	derivedCtx, cancel := context.WithCancel(ctx)

	storeCfg := parquet.StoreConfig{
		DBDirectory:          filepath.Join(config.DataDir, "receipts"),
		BlockFlushInterval:   25,
		MaxBlocksPerFile:     500,
		KeepRecent:           0,
		PruneIntervalSeconds: 0,
	}
	store, err := parquet.NewStore(dbLogger.NewNopLogger(), storeCfg)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create parquet receipt store: %w", err)
	}

	r := &RecieptStoreSimulator{
		ctx:          derivedCtx,
		cancel:       cancel,
		config:       config,
		recieptsChan: recieptsChan,
		store:        store,
		metrics:      metrics,
	}
	go r.mainLoop()
	return r, nil
}

func (r *RecieptStoreSimulator) mainLoop() {
	defer r.store.Close()
	for {
		select {
		case <-r.ctx.Done():
			return
		case blk := <-r.recieptsChan:
			r.processBlock(blk)
		}
	}
}

// Processes a block of reciepts.
func (r *RecieptStoreSimulator) processBlock(blk *block) {
	blockNumber := blk.BlockNumber()
	blockHash := common.Hash{}

	inputs := make([]parquet.ReceiptInput, 0, len(blk.reciepts))

	var logStartIndex uint
	var marshalErrors int64

	for _, receipt := range blk.reciepts {
		if receipt == nil {
			continue
		}

		receiptBytes, err := receipt.Marshal()
		if err != nil {
			fmt.Printf("failed to marshal receipt: %v\n", err)
			marshalErrors++
			continue
		}

		txHash := common.HexToHash(receipt.TxHashHex)
		txLogs := convertLogsForTx(receipt, logStartIndex)
		logStartIndex += uint(len(txLogs))
		for _, lg := range txLogs {
			lg.BlockHash = blockHash
		}

		inputs = append(inputs, parquet.ReceiptInput{
			BlockNumber: blockNumber,
			Receipt: parquet.ReceiptRecord{
				TxHash:       parquet.CopyBytes(txHash[:]),
				BlockNumber:  blockNumber,
				ReceiptBytes: parquet.CopyBytesOrEmpty(receiptBytes),
			},
			Logs:         receiptpkg.BuildParquetLogRecords(txLogs, blockHash),
			ReceiptBytes: parquet.CopyBytesOrEmpty(receiptBytes),
		})
	}

	for range marshalErrors {
		r.metrics.ReportReceiptError()
	}

	if len(inputs) > 0 {
		start := time.Now()
		if err := r.store.WriteReceipts(inputs); err != nil {
			fmt.Printf("failed to write receipts for block %d: %v\n", blockNumber, err)
			r.metrics.ReportReceiptError()
			return
		}
		r.metrics.RecordReceiptBlockWriteDuration(time.Since(start).Seconds())
		r.metrics.ReportReceiptsWritten(int64(len(inputs)))
	}
	r.store.UpdateLatestVersion(int64(blockNumber)) //nolint:gosec // block numbers won't exceed int64 max
}

// convertLogsForTx converts evmtypes.Log entries to ethtypes.Log entries.
// Mirrors receipt.getLogsForTx.
func convertLogsForTx(receipt *evmtypes.Receipt, logStartIndex uint) []*ethtypes.Log {
	logs := make([]*ethtypes.Log, 0, len(receipt.Logs))
	for _, l := range receipt.Logs {
		logs = append(logs, convertLogEntry(l, receipt, logStartIndex))
	}
	return logs
}

// convertLogEntry converts a single evmtypes.Log to an ethtypes.Log.
// Mirrors receipt.convertLog.
func convertLogEntry(l *evmtypes.Log, receipt *evmtypes.Receipt, logStartIndex uint) *ethtypes.Log {
	return &ethtypes.Log{
		Address:     common.HexToAddress(l.Address),
		Topics:      mapTopics(l.Topics),
		Data:        l.Data,
		BlockNumber: receipt.BlockNumber,
		TxHash:      common.HexToHash(receipt.TxHashHex),
		TxIndex:     uint(receipt.TransactionIndex),
		Index:       uint(l.Index) + logStartIndex,
	}
}

// mapTopics converts hex-encoded topic strings to common.Hash values.
func mapTopics(topics []string) []common.Hash {
	result := make([]common.Hash, len(topics))
	for i, t := range topics {
		result[i] = common.HexToHash(t)
	}
	return result
}
