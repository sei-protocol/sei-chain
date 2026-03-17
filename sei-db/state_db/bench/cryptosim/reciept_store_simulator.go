package cryptosim

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// A simulated receipt store using the real production receipt.ReceiptStore
// (cached parquet backend with WAL, flush, rotation, and pruning).
type RecieptStoreSimulator struct {
	ctx    context.Context
	cancel context.CancelFunc

	config *CryptoSimConfig

	recieptsChan chan *block

	store   receipt.ReceiptStore
	metrics *CryptosimMetrics
}

// Creates a new receipt store simulator backed by the production ReceiptStore
// (parquet backend + ledger cache), matching the real node write path.
func NewRecieptStoreSimulator(
	ctx context.Context,
	config *CryptoSimConfig,
	recieptsChan chan *block,
	metrics *CryptosimMetrics,
) (*RecieptStoreSimulator, error) {
	derivedCtx, cancel := context.WithCancel(ctx)

	storeCfg := dbconfig.ReceiptStoreConfig{
		DBDirectory:          filepath.Join(config.DataDir, "receipts"),
		Backend:              "parquet",
		KeepRecent:           int(config.ReceiptKeepRecent),
		PruneIntervalSeconds: int(config.ReceiptPruneIntervalSeconds),
	}

	// nil StoreKey is safe: the parquet write path never touches the legacy KV store.
	store, err := receipt.NewReceiptStore(storeCfg, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create receipt store: %w", err)
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
	defer func() {
		if err := r.store.Close(); err != nil {
			fmt.Printf("failed to close receipt store: %v\n", err)
		}
	}()
	for {
		select {
		case <-r.ctx.Done():
			return
		case blk := <-r.recieptsChan:
			r.processBlock(blk)
		}
	}
}

// Processes a block of receipts using the production ReceiptStore.SetReceipts path,
// which writes to parquet (WAL + buffer + rotation) and populates the ledger cache.
func (r *RecieptStoreSimulator) processBlock(blk *block) {
	blockNumber := uint64(blk.BlockNumber()) //nolint:gosec

	records := make([]receipt.ReceiptRecord, 0, len(blk.reciepts))
	var marshalErrors int64

	for _, rcpt := range blk.reciepts {
		if rcpt == nil {
			continue
		}

		receiptBytes, err := rcpt.Marshal()
		if err != nil {
			fmt.Printf("failed to marshal receipt: %v\n", err)
			marshalErrors++
			continue
		}

		txHash := common.HexToHash(rcpt.TxHashHex)
		records = append(records, receipt.ReceiptRecord{
			TxHash:       txHash,
			Receipt:      rcpt,
			ReceiptBytes: receiptBytes,
		})
	}

	for range marshalErrors {
		r.metrics.ReportReceiptError()
	}

	if len(records) > 0 {
		// Build a minimal sdk.Context with the block height set.
		// The parquet write path only uses ctx.BlockHeight() and ctx.Context().
		sdkCtx := sdk.NewContext(nil, tmproto.Header{Height: int64(blockNumber)}, false) //nolint:gosec

		start := time.Now()
		if err := r.store.SetReceipts(sdkCtx, records); err != nil {
			fmt.Printf("failed to write receipts for block %d: %v\n", blockNumber, err)
			r.metrics.ReportReceiptError()
			return
		}
		r.metrics.RecordReceiptBlockWriteDuration(time.Since(start).Seconds())
		r.metrics.ReportReceiptsWritten(int64(len(records)))
	}

	if err := r.store.SetLatestVersion(int64(blockNumber)); err != nil { //nolint:gosec
		fmt.Printf("failed to update latest version for block %d: %v\n", blockNumber, err)
	}
}

// convertLogsForTx converts evmtypes.Log entries to ethtypes.Log entries.
// Mirrors receipt.getLogsForTx.
func convertLogsForTx(rcpt *evmtypes.Receipt, logStartIndex uint) []*ethtypes.Log {
	logs := make([]*ethtypes.Log, 0, len(rcpt.Logs))
	for _, l := range rcpt.Logs {
		logs = append(logs, convertLogEntry(l, rcpt, logStartIndex))
	}
	return logs
}

// convertLogEntry converts a single evmtypes.Log to an ethtypes.Log.
// Mirrors receipt.convertLog.
func convertLogEntry(l *evmtypes.Log, rcpt *evmtypes.Receipt, logStartIndex uint) *ethtypes.Log {
	return &ethtypes.Log{
		Address:     common.HexToAddress(l.Address),
		Topics:      mapTopics(l.Topics),
		Data:        l.Data,
		BlockNumber: rcpt.BlockNumber,
		TxHash:      common.HexToHash(rcpt.TxHashHex),
		TxIndex:     uint(rcpt.TransactionIndex),
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
