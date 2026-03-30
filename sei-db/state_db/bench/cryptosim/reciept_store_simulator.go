package cryptosim

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	// Must be larger than cacheWindow * TransactionsPerBlock so that the
	// oldest ring entries have aged past the cache window, enabling duckdb-
	// only reads to find targets.  With the default cache window of ~1000
	// blocks and 1024 txns/block the minimum is ~1.02M; 3M gives comfortable
	// headroom for both cache and duckdb read modes.
	defaultTxHashRingSize = 3_000_000
)

// txHashEntry stores a written tx hash along with its block and contract address,
// used by reader goroutines to generate realistic log filter queries.
type txHashEntry struct {
	txHash          common.Hash
	blockNumber     uint64
	contractAddress common.Address
}

// txHashRing is a fixed-size ring buffer of recently written tx hashes.
// Writers call Push from the main loop; readers call RandomEntry from goroutines.
type txHashRing struct {
	mu      sync.RWMutex
	entries []txHashEntry
	size    int
	head    int
	count   int
}

func newTxHashRing(size int) *txHashRing {
	return &txHashRing{
		entries: make([]txHashEntry, size),
		size:    size,
	}
}

// Push appends a tx hash entry to the ring, overwriting the oldest entry when full.
func (r *txHashRing) Push(txHash common.Hash, blockNumber uint64, contractAddress common.Address) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[r.head] = txHashEntry{
		txHash:          txHash,
		blockNumber:     blockNumber,
		contractAddress: contractAddress,
	}
	r.head = (r.head + 1) % r.size
	if r.count < r.size {
		r.count++
	}
}

// RandomEntry returns a random entry from the ring, using CannedRandom to
// avoid potential rand.Rand hotspots under high-concurrency benchmarks.
func (r *txHashRing) RandomEntry(crand *CannedRandom) *txHashEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.count == 0 {
		return nil
	}
	idx := int(crand.Int64Range(0, int64(r.count)))
	entry := r.entries[idx]
	return &entry
}

const maxRingSampleAttempts = 100

// RandomEntryInBlockRange samples a random entry whose blockNumber falls within
// [minBlock, maxBlock]. Returns nil if no matching entry is found after a
// bounded number of attempts.
func (r *txHashRing) RandomEntryInBlockRange(crand *CannedRandom, minBlock, maxBlock uint64) *txHashEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.count == 0 {
		return nil
	}
	for range maxRingSampleAttempts {
		idx := int(crand.Int64Range(0, int64(r.count)))
		entry := r.entries[idx]
		if entry.blockNumber >= minBlock && entry.blockNumber <= maxBlock {
			return &entry
		}
	}
	return nil
}

// A simulated receipt store with concurrent reads, writes, and pruning
// backed by the production receipt.ReceiptStore (parquet + ledger cache).
type RecieptStoreSimulator struct {
	ctx    context.Context
	cancel context.CancelFunc

	config *CryptoSimConfig

	recieptsChan chan *block

	store                    receipt.ReceiptStore
	crand                    *CannedRandom
	txRing                   *txHashRing
	metrics                  *CryptosimMetrics
	receiptCacheWindowBlocks uint64
}

// Creates a new receipt store simulator backed by the production ReceiptStore
// (parquet backend + ledger cache), with optional concurrent reader goroutines.
//
// The caller must supply a CannedRandom instance (typically via Clone) that
// shares the same (seed, bufferSize) as the block builder so that
// SyntheticTxHash reproduces the hashes the write path stored.
//
// Receipt-by-hash reads reconstruct tx hashes on the fly via SyntheticTxHash
// (no storage needed). Log filter reads use the ring buffer to sample contract
// addresses written by the write path. See SyntheticTxHash in receipt.go for details.
func NewRecieptStoreSimulator(
	ctx context.Context,
	config *CryptoSimConfig,
	recieptsChan chan *block,
	metrics *CryptosimMetrics,
	crand *CannedRandom,
) (*RecieptStoreSimulator, error) {
	derivedCtx, cancel := context.WithCancel(ctx)

	storeCfg := dbconfig.ReceiptStoreConfig{
		DBDirectory:          filepath.Join(config.DataDir, "receipts"),
		Backend:              "parquet",
		KeepRecent:           int(config.ReceiptKeepRecent),
		PruneIntervalSeconds: int(config.ReceiptPruneIntervalSeconds),
	}

	// nil StoreKey is safe: the parquet write path never touches the legacy KV store.
	// Cryptosim passes its metrics as a read observer so cache hits/misses are measured
	// at the cache wrapper, which is the only layer that can distinguish them reliably.
	store, err := receipt.NewReceiptStoreWithReadObserver(storeCfg, nil, metrics)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create receipt store: %w", err)
	}

	txRing := newTxHashRing(defaultTxHashRingSize)

	r := &RecieptStoreSimulator{
		ctx:                      derivedCtx,
		cancel:                   cancel,
		config:                   config,
		recieptsChan:             recieptsChan,
		store:                    store,
		crand:                    crand,
		txRing:                   txRing,
		metrics:                  metrics,
		receiptCacheWindowBlocks: receipt.EstimatedReceiptCacheWindowBlocks(store),
	}
	go r.mainLoop()

	if config.ReceiptReadConcurrency > 0 && config.ReceiptReadsPerSecond > 0 {
		r.startReceiptReaders()
	}
	if config.ReceiptLogFilterReadConcurrency > 0 && config.ReceiptLogFilterReadsPerSecond > 0 {
		r.startLogFilterReaders()
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

// Processes a block of receipts using the production ReceiptStore.SetReceipts path,
// then populates the ring buffer with contract addresses for log filter reads.
func (r *RecieptStoreSimulator) processBlock(blk *block) {
	blockNumber := uint64(blk.BlockNumber()) //nolint:gosec

	records := make([]receipt.ReceiptRecord, 0, len(blk.reciepts))
	var marshalErrors int64

	type ringEntry struct {
		txHash          common.Hash
		contractAddress common.Address
	}
	ringEntries := make([]ringEntry, 0, len(blk.reciepts))

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

		ringEntries = append(ringEntries, ringEntry{
			txHash:          txHash,
			contractAddress: common.HexToAddress(rcpt.ContractAddress),
		})
	}

	for range marshalErrors {
		r.metrics.ReportReceiptError()
	}

	if len(records) > 0 {
		sdkCtx := sdk.NewContext(nil, tmproto.Header{Height: int64(blockNumber)}, false) //nolint:gosec

		start := time.Now()
		if err := r.store.SetReceipts(sdkCtx, records); err != nil {
			fmt.Printf("failed to write receipts for block %d: %v\n", blockNumber, err)
			r.metrics.ReportReceiptError()
			return
		}
		r.metrics.RecordReceiptBlockWriteDuration(time.Since(start))
		r.metrics.ReportReceiptsWritten(int64(len(records)))
	}

	for _, entry := range ringEntries {
		r.txRing.Push(entry.txHash, blockNumber, entry.contractAddress)
	}

	if err := r.store.SetLatestVersion(int64(blockNumber)); err != nil { //nolint:gosec
		fmt.Printf("failed to update latest version for block %d: %v\n", blockNumber, err)
	}
}

// startReceiptReaders launches dedicated goroutines for receipt-by-hash lookups.
func (r *RecieptStoreSimulator) startReceiptReaders() {
	readerCount := r.config.ReceiptReadConcurrency
	totalReadsPerSec := r.config.ReceiptReadsPerSecond
	if totalReadsPerSec <= 0 {
		totalReadsPerSec = 1000
	}

	readsPerReader := totalReadsPerSec / readerCount
	if readsPerReader < 1 {
		readsPerReader = 1
	}

	for i := 0; i < readerCount; i++ {
		readerCrand := r.crand.Clone(true)
		go r.tickerLoop(readsPerReader, readerCrand, r.executeReceiptRead)
	}

	fmt.Printf("Started %d receipt reader goroutines (%d reads/sec each)\n",
		readerCount, readsPerReader)
}

// startLogFilterReaders launches dedicated goroutines for log filter (eth_getLogs) queries.
func (r *RecieptStoreSimulator) startLogFilterReaders() {
	readerCount := r.config.ReceiptLogFilterReadConcurrency
	totalReadsPerSec := r.config.ReceiptLogFilterReadsPerSecond
	if totalReadsPerSec <= 0 {
		totalReadsPerSec = 100
	}

	readsPerReader := totalReadsPerSec / readerCount
	if readsPerReader < 1 {
		readsPerReader = 1
	}

	for i := 0; i < readerCount; i++ {
		readerCrand := r.crand.Clone(true)
		go r.tickerLoop(readsPerReader, readerCrand, r.executeLogFilterRead)
	}

	fmt.Printf("Started %d log filter reader goroutines (%d reads/sec each)\n",
		readerCount, readsPerReader)
}

func (r *RecieptStoreSimulator) tickerLoop(readsPerSecond int, crand *CannedRandom, fn func(*CannedRandom)) {
	interval := time.Second / time.Duration(readsPerSecond)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			fn(crand)
		}
	}
}

// executeReceiptRead samples a tx hash from the ring and queries GetReceipt.
//
// ReceiptReadMode controls which blocks are targeted:
//   - "cache": only blocks within the cache window (guaranteed cache hit).
//   - "duckdb": only blocks older than the cache window (guaranteed cache miss).
func (r *RecieptStoreSimulator) executeReceiptRead(crand *CannedRandom) {
	latestBlock := r.store.LatestVersion()
	if latestBlock <= 0 {
		return
	}
	latest := uint64(latestBlock) //nolint:gosec
	cacheWindow := r.receiptCacheWindowBlocks

	var entry *txHashEntry
	switch r.config.ReceiptReadMode {
	case "cache":
		minBlock := uint64(0)
		if latest > cacheWindow {
			minBlock = latest - cacheWindow
		}
		entry = r.txRing.RandomEntryInBlockRange(crand, minBlock, latest)
	case "duckdb":
		maxBlock := uint64(0)
		if latest > cacheWindow {
			maxBlock = latest - cacheWindow - 1
		}
		entry = r.txRing.RandomEntryInBlockRange(crand, 0, maxBlock)
	}
	if entry == nil {
		return
	}

	r.metrics.ReportReceiptRead()

	sdkCtx := sdk.NewContext(nil, tmproto.Header{}, false)
	start := time.Now()
	rcpt, err := r.store.GetReceipt(sdkCtx, entry.txHash)
	r.metrics.RecordReceiptReadDuration(time.Since(start).Seconds())

	if err != nil {
		r.metrics.ReportReceiptError()
		return
	}
	if rcpt != nil {
		r.metrics.ReportReceiptReadFound()
		return
	}
	r.metrics.ReportReceiptReadNotFound()
}

// executeLogFilterRead simulates an eth_getLogs query filtering by contract address
// over a configurable block range. Contract addresses come from the ring buffer.
//
// ReceiptLogFilterReadMode controls which blocks are targeted:
//   - "cache": block range falls entirely within the cache window (DuckDB skipped).
//   - "duckdb": block range falls entirely before the cache window (cache miss).
func (r *RecieptStoreSimulator) executeLogFilterRead(crand *CannedRandom) {
	entry := r.txRing.RandomEntry(crand)
	if entry == nil {
		return
	}

	latestVersion := r.store.LatestVersion()
	if latestVersion <= 0 {
		return
	}

	rangeSize := uint64(crand.Int64Range(
		int64(r.config.ReceiptLogFilterMinBlockRange),
		int64(r.config.ReceiptLogFilterMaxBlockRange)+1,
	)) //nolint:gosec
	latest := uint64(latestVersion) //nolint:gosec
	cacheWindow := r.receiptCacheWindowBlocks

	var fromBlock, toBlock uint64

	switch r.config.ReceiptLogFilterReadMode {
	case "cache":
		cacheMin := uint64(0)
		if latest > cacheWindow {
			cacheMin = latest - cacheWindow
		}
		if latest <= cacheMin {
			return
		}
		fromBlock = uint64(crand.Int64Range(int64(cacheMin), int64(latest)+1)) //nolint:gosec
		toBlock = fromBlock + rangeSize
		if toBlock > latest {
			toBlock = latest
		}
	case "duckdb":
		if latest <= cacheWindow {
			return
		}
		coldMax := latest - cacheWindow - 1
		earliestBlock := uint64(1)
		if r.config.ReceiptKeepRecent > 0 && latest > uint64(r.config.ReceiptKeepRecent) { //nolint:gosec
			earliestBlock = latest - uint64(r.config.ReceiptKeepRecent) + 1 //nolint:gosec
		}
		if coldMax < earliestBlock {
			return
		}
		fromBlock = uint64(crand.Int64Range(int64(earliestBlock), int64(coldMax)+1)) //nolint:gosec
		toBlock = fromBlock + rangeSize
		if toBlock > coldMax {
			toBlock = coldMax
		}
	}

	crit := filters.FilterCriteria{
		Addresses: []common.Address{entry.contractAddress},
	}

	sdkCtx := sdk.NewContext(nil, tmproto.Header{}, false)
	start := time.Now()
	logs, err := r.store.FilterLogs(sdkCtx, fromBlock, toBlock, crit)
	r.metrics.RecordReceiptLogFilterDuration(time.Since(start).Seconds())
	r.metrics.RecordLogFilterLogsReturned(int64(len(logs)))

	if err != nil {
		r.metrics.ReportReceiptError()
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
