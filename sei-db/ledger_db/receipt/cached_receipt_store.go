package receipt

import (
	"sort"
	"sync"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
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
}

func newCachedReceiptStore(backend ReceiptStore) ReceiptStore {
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
	}
	if provider, ok := backend.(cacheWarmupProvider); ok {
		store.cacheReceipts(provider.warmupReceipts())
	}
	return store
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
	if receipt, ok := s.cache.GetReceipt(txHash); ok {
		return receipt, nil
	}
	return s.backend.GetReceipt(ctx, txHash)
}

func (s *cachedReceiptStore) GetReceiptFromStore(ctx sdk.Context, txHash common.Hash) (*types.Receipt, error) {
	if receipt, ok := s.cache.GetReceipt(txHash); ok {
		return receipt, nil
	}
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
// Checks the cache first for recent blocks, then delegates to the backend.
func (s *cachedReceiptStore) FilterLogs(ctx sdk.Context, fromBlock, toBlock uint64, crit filters.FilterCriteria) ([]*ethtypes.Log, error) {
	// First get logs from backend (parquet closed files)
	backendLogs, err := s.backend.FilterLogs(ctx, fromBlock, toBlock, crit)
	if err != nil {
		return nil, err
	}

	// Then check cache for blocks that might not be in closed files yet
	cacheLogs := s.cache.FilterLogs(fromBlock, toBlock, crit)

	// Merge results, avoiding duplicates by tracking seen (blockNum, txIndex, logIndex)
	if len(cacheLogs) == 0 {
		return backendLogs, nil
	}
	if len(backendLogs) == 0 {
		sortLogs(cacheLogs)
		return cacheLogs, nil
	}

	// Build set of backend log keys to deduplicate
	type logKey struct {
		blockNum uint64
		txIndex  uint
		logIndex uint
	}
	seen := make(map[logKey]struct{}, len(backendLogs))
	for _, lg := range backendLogs {
		seen[logKey{lg.BlockNumber, lg.TxIndex, lg.Index}] = struct{}{}
	}

	// Add cache logs that aren't already in backend results
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

	var (
		currentBlock  uint64
		hasBlock      bool
		logStartIndex uint
		cacheLogs     []*ethtypes.Log
	)
	cacheEntries := make([]receiptCacheEntry, 0, len(receipts))

	fillCache := func(blockNumber uint64) {
		if len(cacheEntries) == 0 && len(cacheLogs) == 0 {
			return
		}
		s.maybeRotateCacheLocked(blockNumber)
		s.cache.AddReceiptsBatch(blockNumber, cacheEntries)
		if len(cacheLogs) > 0 {
			s.cache.AddLogsForBlock(blockNumber, cacheLogs)
		}
		cacheEntries = cacheEntries[:0]
		cacheLogs = cacheLogs[:0]
	}

	for _, record := range receipts {
		if record.Receipt == nil {
			continue
		}

		receipt := record.Receipt
		blockNumber := receipt.BlockNumber
		if !hasBlock {
			currentBlock = blockNumber
			hasBlock = true
		}
		if blockNumber != currentBlock {
			fillCache(currentBlock)
			currentBlock = blockNumber
			logStartIndex = 0
		}

		txLogs := getLogsForTx(receipt, logStartIndex)
		logStartIndex += uint(len(txLogs))

		cacheEntries = append(cacheEntries, receiptCacheEntry{
			TxHash:  record.TxHash,
			Receipt: receipt,
		})
		cacheLogs = append(cacheLogs, txLogs...)
	}

	if hasBlock {
		fillCache(currentBlock)
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
