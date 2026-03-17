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
	crand   *CannedRandom
	txRing  *txHashRing
	metrics *CryptosimMetrics
}

// Creates a new receipt store simulator backed by the production ReceiptStore
// (parquet backend + ledger cache), with optional concurrent reader goroutines.
//
// Receipt-by-hash reads reconstruct tx hashes on the fly via SyntheticTxHash
// (no storage needed). Log filter reads use the ring buffer to sample contract
// addresses written by the write path. See SyntheticTxHash in receipt.go for details.
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

	// CannedRandom with the same (seed, bufferSize) as the block builder so that
	// SyntheticTxHash produces the same hashes the write path stored.
	// Only SeededBytes is called, which is a read-only operation safe for concurrent use.
	crand := NewCannedRandom(config.CannedRandomSize, config.Seed)

	txRing := newTxHashRing(defaultTxHashRingSize)

	r := &RecieptStoreSimulator{
		ctx:          derivedCtx,
		cancel:       cancel,
		config:       config,
		recieptsChan: recieptsChan,
		store:        store,
		crand:        crand,
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

	fmt.Printf("Started %d receipt reader goroutines (%d reads/sec each)\n",
		readerCount, readsPerReader)
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

// executeRead picks a read type: receipt-by-hash (via SyntheticTxHash) or log filter
// (via the ring buffer for contract addresses), weighted by ReceiptLogFilterRatio.
func (r *RecieptStoreSimulator) executeRead(rng *rand.Rand) {
	if r.config.ReceiptLogFilterRatio > 0 && rng.Float64() < r.config.ReceiptLogFilterRatio {
		r.executeLogFilterRead(rng)
		return
	}
	r.executeReceiptRead(rng)
}

// executeReceiptRead reconstructs a tx hash from a random (block, txIndex) pair and queries it.
//
// The valid block range is [max(1, latest - KeepRecent + 1), latest]. Hashes outside this
// range may have been pruned — that's fine, the query simply returns no result and we
// count it as a miss. No ring buffer or hash storage is needed because SyntheticTxHash
// can recompute any hash from its coordinates alone (see receipt.go).
func (r *RecieptStoreSimulator) executeReceiptRead(rng *rand.Rand) {
	latestBlock := r.store.LatestVersion()
	if latestBlock <= 0 {
		return
	}

	earliestBlock := int64(1)
	if r.config.ReceiptKeepRecent > 0 && latestBlock > r.config.ReceiptKeepRecent {
		earliestBlock = latestBlock - r.config.ReceiptKeepRecent + 1
	}

	blockRange := latestBlock - earliestBlock + 1
	randomBlock := earliestBlock + rng.Int63n(blockRange)
	randomTxIdx := rng.Intn(r.config.TransactionsPerBlock)

	hashBytes := SyntheticTxHash(r.crand, uint64(randomBlock), uint32(randomTxIdx)) //nolint:gosec
	txHash := common.BytesToHash(hashBytes)

	r.metrics.ReportReceiptRead()

	sdkCtx := sdk.NewContext(nil, tmproto.Header{}, false)
	start := time.Now()
	_, err := r.store.GetReceipt(sdkCtx, txHash)
	r.metrics.RecordReceiptReadDuration(time.Since(start).Seconds())

	if err != nil {
		r.metrics.ReportReceiptError()
	}
}

// executeLogFilterRead simulates an eth_getLogs query filtering by contract address
// over a small block range, which is the typical RPC pattern. Contract addresses
// come from the ring buffer since they aren't deterministically derivable from a tx ID.
func (r *RecieptStoreSimulator) executeLogFilterRead(rng *rand.Rand) {
	entry := r.txRing.RandomEntry(rng)
	if entry == nil {
		return
	}

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
