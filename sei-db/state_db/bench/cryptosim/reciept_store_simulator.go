package cryptosim

import (
	"context"
	"fmt"
	"math/rand"
	"path/filepath"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
	receiptpkg "github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	// Size of the ring buffer for tracking written tx hashes.
	defaultTxHashRingSize = 1_000_000
)

// A simulated receipt store with concurrent reads, writes, cache, WAL, and pruning.
type RecieptStoreSimulator struct {
	ctx    context.Context
	cancel context.CancelFunc

	config *CryptoSimConfig

	recieptsChan chan *block

	store   *parquet.Store
	cache   *receiptSimCache
	txRing  *txHashRing
	metrics *CryptosimMetrics
}

// Creates a new receipt store simulator with the full production-like stack:
// WAL, flush, rotation, pruning, ledger cache, and concurrent readers.
func NewRecieptStoreSimulator(
	ctx context.Context,
	config *CryptoSimConfig,
	recieptsChan chan *block,
	metrics *CryptosimMetrics,
) (*RecieptStoreSimulator, error) {
	derivedCtx, cancel := context.WithCancel(ctx)

	maxBlocksPerFile := uint64(max(config.ReceiptMaxBlocksPerFile, 0)) //nolint:gosec // validated non-negative
	if maxBlocksPerFile == 0 {
		maxBlocksPerFile = 500
	}
	blockFlushInterval := uint64(max(config.ReceiptBlockFlushInterval, 0)) //nolint:gosec // validated non-negative
	if blockFlushInterval == 0 {
		blockFlushInterval = 1
	}

	storeCfg := parquet.StoreConfig{
		DBDirectory:          filepath.Join(config.DataDir, "receipts"),
		BlockFlushInterval:   blockFlushInterval,
		MaxBlocksPerFile:     maxBlocksPerFile,
		KeepRecent:           config.ReceiptKeepRecent,
		PruneIntervalSeconds: config.ReceiptPruneIntervalSeconds,
	}
	store, err := parquet.NewStore(storeCfg)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create parquet receipt store: %w", err)
	}

	cache := newReceiptSimCache(maxBlocksPerFile)
	txRing := newTxHashRing(defaultTxHashRingSize, config.Seed+42)

	r := &RecieptStoreSimulator{
		ctx:          derivedCtx,
		cancel:       cancel,
		config:       config,
		recieptsChan: recieptsChan,
		store:        store,
		cache:        cache,
		txRing:       txRing,
		metrics:      metrics,
	}
	go r.mainLoop()

	if config.ReceiptReadConcurrency > 0 && config.ReceiptReadsPerSecond > 0 {
		r.startReaders()
	}

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

// Processes a block of receipts: marshal, write to parquet (WAL + buffer), populate cache.
func (r *RecieptStoreSimulator) processBlock(blk *block) {
	blockNumber := uint64(blk.BlockNumber()) //nolint:gosec
	blockHash := common.Hash{}

	inputs := make([]parquet.ReceiptInput, 0, len(blk.reciepts))

	type cacheEntry struct {
		txHash          common.Hash
		receiptBytes    []byte
		contractAddress common.Address
	}
	cacheEntries := make([]cacheEntry, 0, len(blk.reciepts))

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

		cacheEntries = append(cacheEntries, cacheEntry{
			txHash:          txHash,
			receiptBytes:    receiptBytes,
			contractAddress: common.HexToAddress(receipt.ContractAddress),
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

	// Populate cache and ring buffer after successful write (mirrors real cachedReceiptStore).
	r.cache.MaybeRotate(blockNumber)
	for _, entry := range cacheEntries {
		r.cache.Add(entry.txHash, entry.receiptBytes)
		r.txRing.Push(entry.txHash, blockNumber, entry.contractAddress)
	}

	r.store.UpdateLatestVersion(int64(blockNumber)) //nolint:gosec // block numbers won't exceed int64 max
}

// startReaders launches concurrent reader goroutines that simulate RPC receipt lookups.
func (r *RecieptStoreSimulator) startReaders() {
	readerCount := r.config.ReceiptReadConcurrency
	totalReadsPerSec := r.config.ReceiptReadsPerSecond
	if totalReadsPerSec <= 0 {
		totalReadsPerSec = 1000
	}

	// Distribute reads evenly across readers.
	readsPerReader := totalReadsPerSec / readerCount
	if readsPerReader < 1 {
		readsPerReader = 1
	}

	for i := 0; i < readerCount; i++ {
		//nolint:gosec // deterministic per-reader seed for benchmarks
		readerRng := rand.New(rand.NewSource(r.config.Seed + int64(i) + 100))
		go r.readerLoop(readsPerReader, readerRng)
	}

	fmt.Printf("Started %d receipt reader goroutines (%d reads/sec each, %.0f%% cold read ratio)\n",
		readerCount, readsPerReader, r.config.ReceiptColdReadRatio*100)
}

func (r *RecieptStoreSimulator) readerLoop(readsPerSecond int, rng *rand.Rand) {
	interval := time.Second / time.Duration(readsPerSecond)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			r.executeRead(rng)
		}
	}
}

func (r *RecieptStoreSimulator) executeRead(rng *rand.Rand) {
	entry := r.txRing.RandomEntry()
	if entry == nil {
		return // no entries yet
	}

	// Decide if this is a log filter query (eth_getLogs) or a receipt lookup.
	if r.config.ReceiptLogFilterRatio > 0 && rng.Float64() < r.config.ReceiptLogFilterRatio {
		r.executeLogFilterRead(entry, rng)
		return
	}

	r.metrics.ReportReceiptRead()

	// Decide whether this read bypasses the cache (cold read to DuckDB).
	forceCold := rng.Float64() < r.config.ReceiptColdReadRatio

	if !forceCold {
		if _, ok := r.cache.Get(entry.txHash); ok {
			r.metrics.ReportReceiptCacheHit()
			return
		}
	}

	// Cache miss (or forced cold read) — query DuckDB.
	r.metrics.ReportReceiptCacheMiss()
	start := time.Now()
	_, err := r.store.GetReceiptByTxHash(r.ctx, entry.txHash)
	r.metrics.RecordReceiptReadDuration(time.Since(start).Seconds())

	if err != nil {
		r.metrics.ReportReceiptError()
	}
}

// executeLogFilterRead simulates an eth_getLogs query filtering by contract address
// over a small block range, which is the typical RPC pattern.
func (r *RecieptStoreSimulator) executeLogFilterRead(entry *txHashEntry, rng *rand.Rand) {
	latestVersion := r.store.LatestVersion()
	if latestVersion <= 0 {
		return
	}

	// Query a range of 10-100 blocks around the entry's block number.
	rangeSize := uint64(10 + rng.Intn(91))
	fromBlock := entry.blockNumber
	toBlock := fromBlock + rangeSize
	if toBlock > uint64(latestVersion) { //nolint:gosec
		toBlock = uint64(latestVersion) //nolint:gosec
	}

	filter := parquet.LogFilter{
		FromBlock: &fromBlock,
		ToBlock:   &toBlock,
		Addresses: []common.Address{entry.contractAddress},
	}

	start := time.Now()
	_, err := r.store.GetLogs(r.ctx, filter)
	r.metrics.RecordReceiptLogFilterDuration(time.Since(start).Seconds())

	if err != nil {
		r.metrics.ReportReceiptError()
	}
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
