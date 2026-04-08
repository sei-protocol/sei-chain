package receipt

import (
	"bytes"
	"sort"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// Keep in sync with the parquet default max blocks per file to retain a similar cache window.
const defaultReceiptCacheRotateInterval = 500

type cacheRotateIntervalProvider interface {
	cacheRotateInterval() uint64
}

type cacheWarmupProvider interface {
	warmupReceipts() []ReceiptRecord
}

type cachedReceiptStore struct {
	backend             ReceiptStore
	cache               *ledgerCache
	cacheRotateInterval uint64
	cacheNextRotate     uint64
	cacheMu             sync.Mutex
	readMetrics         ReceiptReadMetrics
}

func newCachedReceiptStore(backend ReceiptStore, metrics ReceiptReadMetrics) ReceiptStore {
	if backend == nil {
		return nil
	}
	interval := uint64(defaultReceiptCacheRotateInterval)
	if provider, ok := backend.(cacheRotateIntervalProvider); ok {
		if v := provider.cacheRotateInterval(); v > 0 {
			interval = v
		}
	}
	store := &cachedReceiptStore{
		backend:             backend,
		cache:               newLedgerCache(),
		cacheRotateInterval: interval,
		readMetrics:         metrics,
	}
	if provider, ok := backend.(cacheWarmupProvider); ok {
		store.cacheReceipts(provider.warmupReceipts())
	}
	return store
}

// StableReceiptCacheWindowBlocks returns the near-tip block window that is
// guaranteed to stay in the active write chunk until the next rotation.
func StableReceiptCacheWindowBlocks(store ReceiptStore) uint64 {
	cached, ok := store.(*cachedReceiptStore)
	if !ok || cached.cacheRotateInterval == 0 {
		return 0
	}
	return cached.cacheRotateInterval
}

// EstimatedReceiptCacheWindowBlocks returns the approximate recent block window
// normally served by the in-memory receipt cache (current chunk + previous one).
func EstimatedReceiptCacheWindowBlocks(store ReceiptStore) uint64 {
	hotWindow := StableReceiptCacheWindowBlocks(store)
	if hotWindow == 0 {
		return 0
	}
	return hotWindow * uint64(numCacheChunks-1)
}

func (s *cachedReceiptStore) LatestVersion() int64 {
	return s.backend.LatestVersion()
}

func (s *cachedReceiptStore) SetLatestVersion(version int64) error {
	return s.backend.SetLatestVersion(version)
}

func (s *cachedReceiptStore) SetEarliestVersion(version int64) error {
	return s.backend.SetEarliestVersion(version)
}

func (s *cachedReceiptStore) GetReceipt(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error) {
	start := time.Now()
	receipt, ok := s.cache.GetReceipt(txHash)
	s.reportCacheGetDuration(time.Since(start).Seconds())
	if ok {
		s.reportCacheHit()
		return receipt, nil
	}
	s.reportCacheMiss()
	return s.backend.GetReceipt(ctx, txHash)
}

func (s *cachedReceiptStore) GetReceiptFromStore(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error) {
	if receipt, ok := s.cache.GetReceipt(txHash); ok {
		s.reportCacheHit()
		return receipt, nil
	}
	s.reportCacheMiss()
	return s.backend.GetReceiptFromStore(ctx, txHash)
}

func (s *cachedReceiptStore) SetReceipts(ctx sdk.Context, receipts []ReceiptRecord) error {
	if err := s.backend.SetReceipts(ctx, receipts); err != nil {
		return err
	}
	s.cacheReceipts(receipts)
	return nil
}

// FilterLogs queries logs across a range of blocks.
// When the cache fully covers the requested range the backend is skipped
// entirely, avoiding an unnecessary DuckDB/parquet query for recent blocks.
func (s *cachedReceiptStore) FilterLogs(ctx sdk.Context, fromBlock, toBlock uint64, crit filters.FilterCriteria) ([]*ethtypes.Log, error) {
	scanStart := time.Now()
	// Take a single cache snapshot so rotation cannot advance the cache minimum
	// past the logs we already copied out of the cache.
	cacheLogs, cacheMin, hasCacheLogs := s.cache.FilterLogsWithMinBlock(fromBlock, toBlock, crit)
	s.reportCacheFilterScanDuration(time.Since(scanStart).Seconds())
	if hasCacheLogs && fromBlock >= cacheMin {
		// Cache logs come from map-backed chunks, so direct cache hits need sorting.
		s.reportLogFilterCacheHit()
		sortLogs(cacheLogs)
		return cacheLogs, nil
	}

	// Narrow the backend query to only the block range not covered by cache.
	backendTo := toBlock
	if hasCacheLogs && cacheMin <= toBlock && cacheMin > fromBlock {
		backendTo = cacheMin - 1
	}

	backendLogs, err := s.backend.FilterLogs(ctx, fromBlock, backendTo, crit)
	if err != nil {
		return nil, err
	}

	if len(cacheLogs) == 0 {
		s.reportLogFilterCacheMiss()
		// ReceiptStore backends are not required to return ordered logs.
		sortLogs(backendLogs)
		return backendLogs, nil
	}
	s.reportLogFilterCacheHit()
	if len(backendLogs) == 0 {
		sortLogs(cacheLogs)
		return cacheLogs, nil
	}

	type logKey struct {
		blockNum uint64
		txIndex  uint
		logIndex uint
	}
	seen := make(map[logKey]struct{}, len(backendLogs))
	for _, lg := range backendLogs {
		seen[logKey{lg.BlockNumber, lg.TxIndex, lg.Index}] = struct{}{}
	}

	result := backendLogs
	for _, lg := range cacheLogs {
		key := logKey{lg.BlockNumber, lg.TxIndex, lg.Index}
		if _, exists := seen[key]; !exists {
			result = append(result, lg)
		}
	}

	sortLogs(result)
	return result, nil
}

func sortLogs(logs []*ethtypes.Log) {
	sort.Slice(logs, func(i, j int) bool {
		if logs[i].BlockNumber != logs[j].BlockNumber {
			return logs[i].BlockNumber < logs[j].BlockNumber
		}
		if logs[i].TxIndex != logs[j].TxIndex {
			return logs[i].TxIndex < logs[j].TxIndex
		}
		return logs[i].Index < logs[j].Index
	})
}

func (s *cachedReceiptStore) Close() error {
	return s.backend.Close()
}

func (s *cachedReceiptStore) cacheReceipts(receipts []ReceiptRecord) {
	if len(receipts) == 0 {
		return
	}

	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	receiptsByBlock := make(map[uint64][]ReceiptRecord)
	blockNumbers := make([]uint64, 0, len(receipts))
	for _, record := range receipts {
		if record.Receipt == nil {
			continue
		}
		blockNumber := record.Receipt.BlockNumber
		if _, exists := receiptsByBlock[blockNumber]; !exists {
			blockNumbers = append(blockNumbers, blockNumber)
		}
		receiptsByBlock[blockNumber] = append(receiptsByBlock[blockNumber], record)
	}

	sort.Slice(blockNumbers, func(i, j int) bool {
		return blockNumbers[i] < blockNumbers[j]
	})

	for _, blockNumber := range blockNumbers {
		blockReceipts := receiptsByBlock[blockNumber]
		sort.Slice(blockReceipts, func(i, j int) bool {
			left := blockReceipts[i].Receipt
			right := blockReceipts[j].Receipt
			if left.TransactionIndex != right.TransactionIndex {
				return left.TransactionIndex < right.TransactionIndex
			}
			return bytes.Compare(blockReceipts[i].TxHash[:], blockReceipts[j].TxHash[:]) < 0
		})

		logStartIndex := uint(0)
		cacheEntries := make([]receiptCacheEntry, 0, len(blockReceipts))
		cacheLogs := make([]*ethtypes.Log, 0)
		for _, record := range blockReceipts {
			receipt := record.Receipt
			txLogs := getLogsForTx(receipt, logStartIndex)
			logStartIndex += uint(len(txLogs))

			cacheEntries = append(cacheEntries, receiptCacheEntry{
				TxHash:  record.TxHash,
				Receipt: receipt,
			})
			cacheLogs = append(cacheLogs, txLogs...)
		}

		s.maybeRotateCacheLocked(blockNumber)
		s.cache.AddReceiptsBatch(blockNumber, cacheEntries)
		if len(cacheLogs) > 0 {
			s.cache.AddLogsForBlock(blockNumber, cacheLogs)
		}
	}
}

func (s *cachedReceiptStore) maybeRotateCacheLocked(blockNumber uint64) {
	if s.cacheRotateInterval == 0 {
		return
	}
	if s.cacheNextRotate == 0 {
		s.cacheNextRotate = blockNumber + s.cacheRotateInterval
		return
	}
	for blockNumber >= s.cacheNextRotate {
		s.cache.Rotate()
		s.cacheNextRotate += s.cacheRotateInterval
	}
}

func (s *cachedReceiptStore) reportCacheHit() {
	if s.readMetrics != nil {
		s.readMetrics.ReportReceiptCacheHit()
	}
}

func (s *cachedReceiptStore) reportCacheMiss() {
	if s.readMetrics != nil {
		s.readMetrics.ReportReceiptCacheMiss()
	}
}

func (s *cachedReceiptStore) reportLogFilterCacheHit() {
	if s.readMetrics != nil {
		s.readMetrics.ReportLogFilterCacheHit()
	}
}

func (s *cachedReceiptStore) reportLogFilterCacheMiss() {
	if s.readMetrics != nil {
		s.readMetrics.ReportLogFilterCacheMiss()
	}
}

func (s *cachedReceiptStore) reportCacheFilterScanDuration(seconds float64) {
	if s.readMetrics != nil {
		s.readMetrics.RecordCacheFilterScanDuration(seconds)
	}
}

func (s *cachedReceiptStore) reportCacheGetDuration(seconds float64) {
	if s.readMetrics != nil {
		s.readMetrics.RecordCacheGetDuration(seconds)
	}
}
