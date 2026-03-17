package cryptosim

import (
	"context"
	"fmt"
	"math/rand"
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
	defaultTxHashRingSize = 1_000_000
)

// txHashEntry stores a written tx hash along with its block and contract address,
// used by reader goroutines to generate realistic read queries.
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

// RandomEntry returns a random entry from the ring. The caller's rng is used
// to avoid contention on a shared rng across reader goroutines.
func (r *txHashRing) RandomEntry(rng *rand.Rand) *txHashEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.count == 0 {
		return nil
	}
	idx := rng.Intn(r.count)
	entry := r.entries[idx]
	return &entry
}

// A simulated receipt store with concurrent reads, writes, and pruning
// backed by the production receipt.ReceiptStore (parquet + ledger cache).
type RecieptStoreSimulator struct {
	ctx    context.Context
	cancel context.CancelFunc

	config *CryptoSimConfig

	recieptsChan chan *block

	store   receipt.ReceiptStore
	txRing  *txHashRing
	metrics *CryptosimMetrics
}

// Creates a new receipt store simulator backed by the production ReceiptStore
// (parquet backend + ledger cache), with optional concurrent reader goroutines.
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

	txRing := newTxHashRing(defaultTxHashRingSize)

	r := &RecieptStoreSimulator{
		ctx:          derivedCtx,
		cancel:       cancel,
		config:       config,
		recieptsChan: recieptsChan,
		store:        store,
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

// Processes a block of receipts using the production ReceiptStore.SetReceipts path,
// then populates the tx hash ring for reader goroutines.
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
		r.metrics.RecordReceiptBlockWriteDuration(time.Since(start).Seconds())
		r.metrics.ReportReceiptsWritten(int64(len(records)))
	}

	for _, entry := range ringEntries {
		r.txRing.Push(entry.txHash, blockNumber, entry.contractAddress)
	}

	if err := r.store.SetLatestVersion(int64(blockNumber)); err != nil { //nolint:gosec
		fmt.Printf("failed to update latest version for block %d: %v\n", blockNumber, err)
	}
}

// startReaders launches concurrent reader goroutines that simulate RPC receipt lookups.
func (r *RecieptStoreSimulator) startReaders() {
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
	entry := r.txRing.RandomEntry(rng)
	if entry == nil {
		return
	}

	if r.config.ReceiptLogFilterRatio > 0 && rng.Float64() < r.config.ReceiptLogFilterRatio {
		r.executeLogFilterRead(entry, rng)
		return
	}

	r.metrics.ReportReceiptRead()

	sdkCtx := sdk.NewContext(nil, tmproto.Header{}, false)
	start := time.Now()
	_, err := r.store.GetReceipt(sdkCtx, entry.txHash)
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

	rangeSize := uint64(10 + rng.Intn(91))
	fromBlock := entry.blockNumber
	toBlock := fromBlock + rangeSize
	if toBlock > uint64(latestVersion) { //nolint:gosec
		toBlock = uint64(latestVersion) //nolint:gosec
	}

	crit := filters.FilterCriteria{
		Addresses: []common.Address{entry.contractAddress},
	}

	sdkCtx := sdk.NewContext(nil, tmproto.Header{}, false)
	start := time.Now()
	_, err := r.store.FilterLogs(sdkCtx, fromBlock, toBlock, crit)
	r.metrics.RecordReceiptLogFilterDuration(time.Since(start).Seconds())

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
