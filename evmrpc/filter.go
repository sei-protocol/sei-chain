package evmrpc

import (
	"container/heap"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"golang.org/x/time/rate"

	evmrpcconfig "github.com/sei-protocol/sei-chain/evmrpc/config"
	"github.com/sei-protocol/sei-chain/evmrpc/ethbloom"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/utils/metrics"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/coretypes"
	tmtypes "github.com/tendermint/tendermint/types"
)

const TxSearchPerPage = 10

const (
	// DB Concurrency Read Limit
	MaxDBReadConcurrency = 16

	// Default request limits (used as fallback values)
	DefaultMaxBlockRange = 2000
	DefaultMaxLogLimit   = 10000

	// global request rate limit, only applies to queries > RPSLimitThreshold
	GlobalRPSLimit    = 30
	RPSLimitThreshold = 100 // block range queries below this threshold bypass rate limiting
)

// BlockCacheEntry for sotring block, bloom, and receipts cache
type BlockCacheEntry struct {
	sync.RWMutex
	Block    *coretypes.ResultBlock
	Bloom    ethtypes.Bloom
	Receipts map[common.Hash]*evmtypes.Receipt
}

type BlockCache = *expirable.LRU[int64, *BlockCacheEntry]

// Factory function for creating block cache with 5-minute TTL
func NewBlockCache(maxSize int) BlockCache {
	return expirable.NewLRU[int64, *BlockCacheEntry](maxSize, nil, 5*time.Minute)
}

// Helper functions for cache access (fine-grained locking)
func getCachedReceipt(globalBlockCache BlockCache, blockHeight int64, txHash common.Hash) (*evmtypes.Receipt, bool) {
	if entry, found := globalBlockCache.Get(blockHeight); found {
		entry.RLock()
		defer entry.RUnlock()
		if receipt, hasReceipt := entry.Receipts[txHash]; hasReceipt {
			return receipt, true
		}
	}
	return nil, false
}

func getOrSetCachedReceipt(cacheCreationMutex *sync.Mutex, globalBlockCache BlockCache, ctx sdk.Context, k *keeper.Keeper, block *coretypes.ResultBlock, txHash common.Hash) (*evmtypes.Receipt, bool) {
	blockHeight := block.Block.Height
	receipt, found := getCachedReceipt(globalBlockCache, blockHeight, txHash)
	if found {
		return receipt, true
	}
	receipt, err := k.GetReceipt(ctx, txHash)
	if err != nil {
		return nil, false
	}
	setCachedReceipt(cacheCreationMutex, globalBlockCache, blockHeight, block, txHash, receipt)
	return receipt, true
}

// LoadOrStore ensures atomic cache entry creation (like sync.Map.LoadOrStore)
func loadOrStoreCacheEntry(cacheCreationMutex *sync.Mutex, globalBlockCache BlockCache, blockHeight int64, block *coretypes.ResultBlock) *BlockCacheEntry {
	// Fast path: try to get existing entry
	if entry, found := globalBlockCache.Get(blockHeight); found {
		// If we have a block and the entry's block is nil, fill it
		if block != nil {
			fillMissingFields(entry, block, ethtypes.Bloom{})
		}
		return entry
	}

	// Slow path: create new entry with mutex protection
	cacheCreationMutex.Lock()
	defer cacheCreationMutex.Unlock()

	// Double-check after acquiring lock
	if entry, found := globalBlockCache.Get(blockHeight); found {
		// If we have a block and the entry's block is nil, fill it
		if block != nil {
			fillMissingFields(entry, block, ethtypes.Bloom{})
		}
		return entry
	}

	// Create and store new entry
	entry := &BlockCacheEntry{
		Block:    block,
		Receipts: make(map[common.Hash]*evmtypes.Receipt),
	}
	globalBlockCache.Add(blockHeight, entry)
	return entry
}

// fillMissingFields safely fills missing Block and Bloom fields
func fillMissingFields(entry *BlockCacheEntry, block *coretypes.ResultBlock, bloom ethtypes.Bloom) {
	entry.Lock()
	defer entry.Unlock()

	// Fill Block if missing
	if entry.Block == nil && block != nil {
		entry.Block = block
	}

	// Fill Bloom if missing and provided
	if entry.Bloom == (ethtypes.Bloom{}) && bloom != (ethtypes.Bloom{}) {
		entry.Bloom = bloom
	}
}

func setCachedReceipt(cacheCreationMutex *sync.Mutex, globalBlockCache BlockCache, blockHeight int64, block *coretypes.ResultBlock, txHash common.Hash, receipt *evmtypes.Receipt) {
	// Use LoadOrStore to get the entry atomically
	entry := loadOrStoreCacheEntry(cacheCreationMutex, globalBlockCache, blockHeight, block)

	// Now safely update the entry with fine-grained locking
	entry.Lock()
	entry.Receipts[txHash] = receipt
	entry.Unlock()
}

// logCollector interface for different collection strategies
type logCollector interface {
	Append(*ethtypes.Log)
}

// sliceCollector for direct slice append
type sliceCollector struct {
	logs []*ethtypes.Log
}

func (c *sliceCollector) Append(log *ethtypes.Log) {
	c.logs = append(c.logs, log)
}

// pooledCollector for reused slice
type pooledCollector struct {
	logs *[]*ethtypes.Log
}

func (c *pooledCollector) Append(log *ethtypes.Log) {
	*c.logs = append(*c.logs, log)
}

type FilterType byte

const (
	UnknownSubscription FilterType = iota
	LogsSubscription
	BlocksSubscription
)

type filter struct {
	typ        FilterType
	fc         filters.FilterCriteria
	cancelFunc context.CancelFunc
	lastAccess time.Time

	// BlocksSubscription
	blockCursor string

	// LogsSubscription
	lastToHeight int64
}

// Log slice pool to reduce allocations in batch processing
type LogSlicePool struct {
	pool sync.Pool
}

func NewLogSlicePool() *LogSlicePool {
	return &LogSlicePool{
		pool: sync.Pool{
			New: func() interface{} {
				slice := make([]*ethtypes.Log, 0, 100) // Pre-allocate capacity of 100
				return &slice
			},
		},
	}
}

func (p *LogSlicePool) Get() []*ethtypes.Log {
	slicePtr := p.pool.Get().(*[]*ethtypes.Log)
	*slicePtr = (*slicePtr)[:0] // Reset length but keep capacity
	return *slicePtr
}

func (p *LogSlicePool) Put(slice []*ethtypes.Log) {
	if cap(slice) < 1000 { // Avoid storing overly large slices
		p.pool.Put(&slice)
	}
}

// kWayMergeItem is used in the heap for the k-way merge.
type kWayMergeItem struct {
	log      *ethtypes.Log
	batchIdx int // Which batch this log came from
	itemIdx  int // The index within that batch
}

// logMergeHeap is a min-heap of kWayMergeItem
type logMergeHeap []*kWayMergeItem

func (h logMergeHeap) Len() int { return len(h) }
func (h logMergeHeap) Less(i, j int) bool {
	if h[i].log.BlockNumber != h[j].log.BlockNumber {
		return h[i].log.BlockNumber < h[j].log.BlockNumber
	}
	return h[i].log.Index < h[j].log.Index
}
func (h logMergeHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *logMergeHeap) Push(x interface{}) { *h = append(*h, x.(*kWayMergeItem)) }
func (h *logMergeHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

type FilterAPI struct {
	tmClient         rpcclient.Client
	filtersMu        sync.RWMutex
	filters          map[ethrpc.ID]filter
	toDelete         chan ethrpc.ID
	filterConfig     *FilterConfig
	logFetcher       *LogFetcher
	connectionType   ConnectionType
	namespace        string
	shutdownCtx      context.Context
	shutdownCancel   context.CancelFunc
	globalRPSLimiter *rate.Limiter
}

type FilterConfig struct {
	timeout  time.Duration
	maxLog   int64
	maxBlock int64
}

type EventItemDataWrapper struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

func NewFilterAPI(
	tmClient rpcclient.Client,
	k *keeper.Keeper,
	ctxProvider func(int64) sdk.Context,
	txConfigProvider func(int64) client.TxConfig,
	filterConfig *FilterConfig,
	connectionType ConnectionType,
	namespace string,
	dbReadSemaphore chan struct{},
	globalBlockCache BlockCache,
	cacheCreationMutex *sync.Mutex,
	globalLogSlicePool *LogSlicePool,
	watermarks *WatermarkManager,
) *FilterAPI {
	if filterConfig.maxBlock <= 0 {
		filterConfig.maxBlock = DefaultMaxBlockRange
	}
	if filterConfig.maxLog <= 0 {
		filterConfig.maxLog = DefaultMaxLogLimit
	}

	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	logFetcher := &LogFetcher{
		tmClient:                 tmClient,
		k:                        k,
		ctxProvider:              ctxProvider,
		txConfigProvider:         txConfigProvider,
		filterConfig:             filterConfig,
		includeSyntheticReceipts: shouldIncludeSynthetic(namespace),
		dbReadSemaphore:          dbReadSemaphore,
		globalBlockCache:         globalBlockCache,
		cacheCreationMutex:       cacheCreationMutex,
		globalLogSlicePool:       globalLogSlicePool,
		watermarks:               watermarks,
	}
	filters := make(map[ethrpc.ID]filter)
	api := &FilterAPI{
		namespace:        namespace,
		tmClient:         tmClient,
		filtersMu:        sync.RWMutex{},
		filters:          filters,
		toDelete:         make(chan ethrpc.ID, 1000),
		filterConfig:     filterConfig,
		logFetcher:       logFetcher,
		connectionType:   connectionType,
		shutdownCtx:      shutdownCtx,
		shutdownCancel:   shutdownCancel,
		globalRPSLimiter: rate.NewLimiter(rate.Limit(GlobalRPSLimit), GlobalRPSLimit),
	}

	go api.cleanupLoop(filterConfig.timeout)
	return api
}

// Unified cleanup loop that handles both timeout and manual deletion
func (a *FilterAPI) cleanupLoop(timeout time.Duration) {
	ticker := time.NewTicker(timeout / 2) // Check more frequently than timeout
	defer func() {
		ticker.Stop()
		recoverAndLog()
	}()

	for {
		select {
		case <-a.shutdownCtx.Done():
			return
		case <-ticker.C:
			// Clean up expired filters
			a.cleanupExpiredFilters(timeout)
		case filterID := <-a.toDelete:
			// Handle manual filter deletion
			a.removeFilter(filterID)
		}
	}
}

func (a *FilterAPI) cleanupExpiredFilters(timeout time.Duration) {
	now := time.Now()
	toRemove := make([]ethrpc.ID, 0)

	// First pass: identify expired filters (read lock)
	a.filtersMu.RLock()
	for id, filter := range a.filters {
		if now.Sub(filter.lastAccess) > timeout {
			toRemove = append(toRemove, id)
		}
	}
	a.filtersMu.RUnlock()

	// Second pass: remove expired filters (write lock)
	if len(toRemove) > 0 {
		a.filtersMu.Lock()
		for _, id := range toRemove {
			if filter, exists := a.filters[id]; exists {
				delete(a.filters, id)
				if filter.cancelFunc != nil {
					filter.cancelFunc()
				}
			}
		}
		a.filtersMu.Unlock()
	}
}

func (a *FilterAPI) removeFilter(filterID ethrpc.ID) {
	a.filtersMu.Lock()
	defer a.filtersMu.Unlock()

	if filter, exists := a.filters[filterID]; exists {
		delete(a.filters, filterID)
		if filter.cancelFunc != nil {
			filter.cancelFunc()
		}
	}
}

func (a *FilterAPI) updateFilterAccess(filterID ethrpc.ID) {
	a.filtersMu.Lock()
	defer a.filtersMu.Unlock()

	if filter, exists := a.filters[filterID]; exists {
		filter.lastAccess = time.Now()
		a.filters[filterID] = filter
	}
}

func (a *FilterAPI) NewFilter(
	ctx context.Context,
	crit filters.FilterCriteria,
) (id ethrpc.ID, err error) {
	defer recordMetricsWithError(fmt.Sprintf("%s_newFilter", a.namespace), a.connectionType, time.Now(), err)

	_, cancel := context.WithCancel(a.shutdownCtx)

	a.filtersMu.Lock()
	defer a.filtersMu.Unlock()

	curFilterID := ethrpc.NewID()
	a.filters[curFilterID] = filter{
		typ:          LogsSubscription,
		fc:           crit,
		cancelFunc:   cancel,
		lastAccess:   time.Now(),
		lastToHeight: 0,
	}
	return curFilterID, nil
}

func (a *FilterAPI) NewBlockFilter(
	ctx context.Context,
) (id ethrpc.ID, err error) {
	defer recordMetricsWithError(fmt.Sprintf("%s_newBlockFilter", a.namespace), a.connectionType, time.Now(), err)

	_, cancel := context.WithCancel(a.shutdownCtx)

	a.filtersMu.Lock()
	defer a.filtersMu.Unlock()

	curFilterID := ethrpc.NewID()
	a.filters[curFilterID] = filter{
		typ:         BlocksSubscription,
		cancelFunc:  cancel,
		lastAccess:  time.Now(),
		blockCursor: "",
	}
	return curFilterID, nil
}

func (a *FilterAPI) GetFilterChanges(
	ctx context.Context,
	filterID ethrpc.ID,
) (res interface{}, err error) {
	defer recordMetricsWithError(fmt.Sprintf("%s_getFilterChanges", a.namespace), a.connectionType, time.Now(), err)

	// Read filter with read lock
	a.filtersMu.RLock()
	filter, ok := a.filters[filterID]
	a.filtersMu.RUnlock()

	if !ok {
		return nil, errors.New("filter does not exist")
	}

	// Update access time
	a.updateFilterAccess(filterID)

	switch filter.typ {
	case BlocksSubscription:
		hashes, cursor, err := a.getBlockHeadersAfter(ctx, filter.blockCursor)
		if err != nil {
			return nil, err
		}

		// Update filter with write lock
		a.filtersMu.Lock()
		if updatedFilter, exists := a.filters[filterID]; exists {
			updatedFilter.blockCursor = cursor
			a.filters[filterID] = updatedFilter
		}
		a.filtersMu.Unlock()

		return hashes, nil
	case LogsSubscription:
		// filter by hash would have no updates if it has previously queried for this crit
		if filter.fc.BlockHash != nil && filter.lastToHeight > 0 {
			return nil, nil
		}
		// filter with a ToBlock would have no updates if it has previously queried for this crit
		if filter.fc.ToBlock != nil && filter.lastToHeight >= filter.fc.ToBlock.Int64() {
			return nil, nil
		}
		logs, lastToHeight, err := a.logFetcher.GetLogsByFilters(ctx, filter.fc, filter.lastToHeight)
		if err != nil {
			return nil, err
		}

		// Update filter with write lock
		a.filtersMu.Lock()
		if updatedFilter, exists := a.filters[filterID]; exists {
			updatedFilter.lastToHeight = lastToHeight + 1
			a.filters[filterID] = updatedFilter
		}
		a.filtersMu.Unlock()

		return logs, nil
	default:
		return nil, errors.New("unknown filter type")
	}
}

func (a *FilterAPI) GetFilterLogs(
	ctx context.Context,
	filterID ethrpc.ID,
) (res []*ethtypes.Log, err error) {
	defer recordMetricsWithError(fmt.Sprintf("%s_getFilterLogs", a.namespace), a.connectionType, time.Now(), err)

	// Read filter with read lock
	a.filtersMu.RLock()
	filter, ok := a.filters[filterID]
	a.filtersMu.RUnlock()

	if !ok {
		return nil, errors.New("filter does not exist")
	}

	// Update access time
	a.updateFilterAccess(filterID)

	logs, lastToHeight, err := a.logFetcher.GetLogsByFilters(ctx, filter.fc, 0)
	if err != nil {
		return nil, err
	}

	// Update filter with write lock
	a.filtersMu.Lock()
	if updatedFilter, exists := a.filters[filterID]; exists {
		updatedFilter.lastToHeight = lastToHeight
		a.filters[filterID] = updatedFilter
	}
	a.filtersMu.Unlock()

	return logs, nil
}

func (a *FilterAPI) GetLogs(ctx context.Context, crit filters.FilterCriteria) (res []*ethtypes.Log, err error) {
	startTime := time.Now()
	defer recordMetricsWithError(fmt.Sprintf("%s_getLogs", a.namespace), a.connectionType, startTime, err)

	latest, err := a.logFetcher.latestHeight(ctx)
	if err != nil {
		return nil, err
	}
	earliest, err := a.logFetcher.earliestHeight(ctx)
	if err != nil {
		earliest = 0
	}

	begin, end, err := ComputeBlockBounds(latest, earliest, 0, crit)
	if err != nil {
		return nil, err
	}

	blockRange := end - begin + 1

	// Record metrics for eth_getLogs
	defer func() {
		GetGlobalMetrics().RecordGetLogsRequest(blockRange, time.Since(startTime), startTime, err)
	}()

	// Use config value instead of hardcoded constant
	if blockRange > a.filterConfig.maxBlock {
		return nil, fmt.Errorf("block range too large (%d), maximum allowed is %d blocks", blockRange, a.filterConfig.maxBlock)
	}

	// Early rejection for pruned blocks - avoid wasting resources on blocks that don't exist
	if earliest > 0 && begin < earliest {
		return nil, fmt.Errorf("requested block range [%d, %d] includes pruned blocks, earliest available block is %d", begin, end, earliest)
	}

	// Only apply rate limiting for large queries (> RPSLimitThreshold blocks)
	if blockRange > RPSLimitThreshold && !a.globalRPSLimiter.Allow() {
		return nil, fmt.Errorf("log query rate limit exceeded for large queries, please try again later")
	}

	// Backpressure: early rejection based on system load
	m := GetGlobalMetrics()

	// Check 1: Too many pending tasks (queue backlog)
	pending := m.TasksSubmitted.Load() - m.TasksCompleted.Load()
	maxPending := int64(float64(m.QueueCapacity.Load()) * 0.8) // 80% threshold
	if pending > maxPending {
		return nil, fmt.Errorf("server too busy, rejecting new request (pending: %d, threshold: %d)", pending, maxPending)
	}

	// Check 2: I/O saturated (semaphore exhausted)
	semInUse := m.DBSemaphoreAcquired.Load()
	semCapacity := m.DBSemaphoreCapacity.Load()
	if semCapacity > 0 && float64(semInUse)/float64(semCapacity) >= 0.8 {
		return nil, fmt.Errorf("server I/O saturated, rejecting new request (semaphore: %d/%d in use)", semInUse, semCapacity)
	}

	logs, _, err := a.logFetcher.GetLogsByFilters(ctx, crit, 0)
	if err != nil {
		return nil, err
	}

	// Ensure we never return nil, always return an array (even if empty)
	if logs == nil {
		logs = []*ethtypes.Log{}
	}

	return logs, nil
}

// get block headers after a certain cursor. Can use an empty string cursor
// to get the latest block header.
func (a *FilterAPI) getBlockHeadersAfter(
	ctx context.Context,
	cursor string,
) ([]common.Hash, string, error) {
	q := NewBlockQueryBuilder()
	builtQuery := q.Build()
	hasMore := true
	headers := []common.Hash{}
	for hasMore {
		res, err := a.tmClient.Events(ctx, &coretypes.RequestEvents{
			Filter: &coretypes.EventFilter{Query: builtQuery},
			After:  cursor,
		})
		if err != nil {
			return nil, "", err
		}
		hasMore = res.More
		cursor = res.Newest

		for _, item := range res.Items {
			wrapper := EventItemDataWrapper{}
			err := json.Unmarshal(item.Data, &wrapper)
			if err != nil {
				return nil, "", err
			}
			block := tmtypes.EventDataNewBlock{}
			err = json.Unmarshal(wrapper.Value, &block)
			if err != nil {
				return nil, "", err
			}
			headers = append(headers, common.BytesToHash(block.Block.Hash()))
		}
	}
	return headers, cursor, nil
}

func (a *FilterAPI) UninstallFilter(
	_ context.Context,
	filterID ethrpc.ID,
) (res bool) {
	defer recordMetrics(fmt.Sprintf("%s_uninstallFilter", a.namespace), a.connectionType, time.Now())

	// Check if filter exists
	a.filtersMu.RLock()
	_, found := a.filters[filterID]
	a.filtersMu.RUnlock()

	if !found {
		return false
	}

	// Queue for deletion in cleanup loop to avoid race conditions
	select {
	case a.toDelete <- filterID:
		return true
	default:
		// Channel is full, fall back to direct deletion
		a.removeFilter(filterID)
		return true
	}
}

// Cleanup method for graceful shutdown
func (a *FilterAPI) Cleanup() {
	a.shutdownCancel()

	// Cancel all remaining filters
	a.filtersMu.Lock()
	for _, filter := range a.filters {
		if filter.cancelFunc != nil {
			filter.cancelFunc()
		}
	}
	a.filters = make(map[ethrpc.ID]filter)
	a.filtersMu.Unlock()

	close(a.toDelete)
}

type LogFetcher struct {
	tmClient                 rpcclient.Client
	k                        *keeper.Keeper
	txConfigProvider         func(int64) client.TxConfig
	ctxProvider              func(int64) sdk.Context
	filterConfig             *FilterConfig
	includeSyntheticReceipts bool
	dbReadSemaphore          chan struct{}
	globalBlockCache         BlockCache
	cacheCreationMutex       *sync.Mutex
	globalLogSlicePool       *LogSlicePool
	watermarks               *WatermarkManager
}

// ComputeBlockBounds validates that the requested block range lies within the
// available bounds and returns the effective range, taking incremental
// pagination into account. The function never widens the range – any request
// that extends beyond the available history results in an error so we avoid
// returning truncated data.
func ComputeBlockBounds(latest, earliest, lastToHeight int64, crit filters.FilterCriteria) (int64, int64, error) {
	begin := latest
	end := latest

	if crit.FromBlock != nil {
		begin = getHeightFromBigIntBlockNumber(latest, crit.FromBlock)
	}
	if crit.ToBlock != nil {
		end = getHeightFromBigIntBlockNumber(latest, crit.ToBlock)
		if crit.FromBlock == nil && begin > end {
			begin = end
		}
	}

	if begin > end {
		return 0, 0, fmt.Errorf("requested fromBlock %d is greater than toBlock %d", begin, end)
	}
	if begin < earliest {
		return 0, 0, fmt.Errorf("requested fromBlock %d is before earliest available block %d", begin, earliest)
	}
	if end > latest {
		return 0, 0, fmt.Errorf("requested toBlock %d is after latest available block %d", end, latest)
	}
	if begin > latest {
		return 0, 0, fmt.Errorf("requested fromBlock %d is after latest available block %d", begin, latest)
	}
	if end < earliest {
		return 0, 0, fmt.Errorf("requested toBlock %d is before earliest available block %d", end, earliest)
	}

	if lastToHeight > begin {
		begin = lastToHeight
	}

	return begin, end, nil
}

func (f *LogFetcher) GetLogsByFilters(ctx context.Context, crit filters.FilterCriteria, lastToHeight int64) (res []*ethtypes.Log, end int64, err error) {
	latest, err := f.latestHeight(ctx)
	if err != nil {
		return nil, 0, err
	}
	earliest, err := f.earliestHeight(ctx)
	if err != nil {
		earliest = 0
	}
	begin, end, err := ComputeBlockBounds(latest, earliest, lastToHeight, crit)
	if err != nil {
		return nil, 0, err
	}
	if begin > end {
		return []*ethtypes.Log{}, end, nil
	}

	blockRange := end - begin + 1

	// Use config value instead of hardcoded constant
	if blockRange > f.filterConfig.maxBlock {
		return nil, 0, fmt.Errorf("block range too large (%d), maximum allowed is %d blocks", blockRange, f.filterConfig.maxBlock)
	}

	// Try efficient range query first (supported by parquet/DuckDB backend)
	// #nosec G115 -- begin and end are validated to be positive block heights above
	if logs, rangeErr := f.tryFilterLogsRange(ctx, uint64(begin), uint64(end), crit); rangeErr == nil {
		return logs, end, nil
	} else if !errors.Is(rangeErr, receipt.ErrRangeQueryNotSupported) {
		// If it's a real error (not just unsupported), return it
		return nil, 0, rangeErr
	}
	// Fall back to block-by-block querying for backends that don't support range queries

	bloomIndexes := EncodeFilters(crit.Addresses, crit.Topics)
	blocks, end, applyOpenEndedLogLimit, err := f.fetchBlocksByCrit(ctx, crit, lastToHeight, bloomIndexes)
	if err != nil {
		return nil, 0, err
	}

	runner := GetGlobalWorkerPool()
	var resultsMutex sync.Mutex
	sortedBatches := make([][]*ethtypes.Log, 0)
	var wg sync.WaitGroup
	var submitError error

	processBatch := func(batch []*coretypes.ResultBlock) {
		defer func() {
			// Add metrics for log processing
			metrics.IncrementRpcRequestCounter("num_blocks_fetched", "logs", true)
			wg.Done()
		}()
		// Each worker gets a clean slice from the pool
		localLogs := f.globalLogSlicePool.Get()

		for _, block := range batch {
			f.GetLogsForBlockPooled(block, crit, &localLogs)
		}

		// Sort the local batch
		sort.Slice(localLogs, func(i, j int) bool {
			if localLogs[i].BlockNumber != localLogs[j].BlockNumber {
				return localLogs[i].BlockNumber < localLogs[j].BlockNumber
			}
			return localLogs[i].Index < localLogs[j].Index
		})

		// Append the sorted (and now owned) slice to the shared list
		resultsMutex.Lock()
		sortedBatches = append(sortedBatches, localLogs)
		resultsMutex.Unlock()
	}

	// Batch process with fail-fast
	blockBatch := make([]*coretypes.ResultBlock, 0, evmrpcconfig.WorkerBatchSize)
	for block := range blocks {
		blockBatch = append(blockBatch, block)

		if len(blockBatch) >= evmrpcconfig.WorkerBatchSize {
			batch := blockBatch
			wg.Add(1)

			if err := runner.SubmitWithMetrics(func() { processBatch(batch) }); err != nil {
				wg.Done()
				submitError = fmt.Errorf("system overloaded, please reduce request frequency: %w", err)
				break
			}
			blockBatch = make([]*coretypes.ResultBlock, 0, evmrpcconfig.WorkerBatchSize)
		}
	}

	if submitError != nil {
		return nil, 0, submitError
	}

	// Process remaining blocks
	if len(blockBatch) > 0 {
		wg.Add(1)
		if err := runner.SubmitWithMetrics(func() { processBatch(blockBatch) }); err != nil {
			wg.Done()
			return nil, 0, fmt.Errorf("system overloaded, please reduce request frequency: %w", err)
		}
	}

	wg.Wait()

	// Now that all workers are done, we put the slices back into the pool.
	// This must be done after the merge is complete.
	defer func() {
		for _, batch := range sortedBatches {
			f.globalLogSlicePool.Put(batch)
		}
	}()

	res = f.mergeSortedLogs(sortedBatches)

	// Apply rate limit
	if applyOpenEndedLogLimit && int64(len(res)) >= f.filterConfig.maxLog {
		res = res[:int(f.filterConfig.maxLog)]
	}

	// Ensure we never return nil, always return an array (even if empty)
	if res == nil {
		res = []*ethtypes.Log{}
	}

	return res, end, err
}

func (f *LogFetcher) mergeSortedLogs(batches [][]*ethtypes.Log) []*ethtypes.Log {
	totalSize := 0
	for _, b := range batches {
		totalSize += len(b)
	}
	if totalSize == 0 {
		return []*ethtypes.Log{}
	}

	res := make([]*ethtypes.Log, 0, totalSize)
	h := &logMergeHeap{}

	// Initialize the heap with the first element from each non-empty batch
	for i, batch := range batches {
		if len(batch) > 0 {
			heap.Push(h, &kWayMergeItem{
				log:      batch[0],
				batchIdx: i,
				itemIdx:  0,
			})
		}
	}

	// Process the heap until it's empty
	for h.Len() > 0 {
		item := heap.Pop(h).(*kWayMergeItem)
		res = append(res, item.log)

		// If there are more items in the batch the popped item came from, add the next one to the heap
		nextItemIdx := item.itemIdx + 1
		if nextItemIdx < len(batches[item.batchIdx]) {
			heap.Push(h, &kWayMergeItem{
				log:      batches[item.batchIdx][nextItemIdx],
				batchIdx: item.batchIdx,
				itemIdx:  nextItemIdx,
			})
		}
	}

	return res
}

func (f *LogFetcher) latestHeight(ctx context.Context) (int64, error) {
	return f.watermarks.LatestHeight(ctx)
}

func (f *LogFetcher) earliestHeight(ctx context.Context) (int64, error) {
	return f.watermarks.EarliestHeight(ctx)
}

// tryFilterLogsRange attempts to use the efficient range query if supported by the backend.
// Returns ErrRangeQueryNotSupported if the backend doesn't support range queries.
func (f *LogFetcher) tryFilterLogsRange(_ context.Context, fromBlock, toBlock uint64, crit filters.FilterCriteria) ([]*ethtypes.Log, error) {
	store := f.k.ReceiptStore()
	if store == nil {
		return nil, receipt.ErrRangeQueryNotSupported
	}

	// Use a context at the toBlock height for the query
	// #nosec G115 -- toBlock is a block height which fits in int64
	sdkCtx := f.ctxProvider(int64(toBlock))

	logs, err := store.FilterLogs(sdkCtx, fromBlock, toBlock, crit)
	if err != nil {
		return nil, err
	}

	return logs, nil
}

// Pooled version that reuses slice allocation
func (f *LogFetcher) GetLogsForBlockPooled(block *coretypes.ResultBlock, crit filters.FilterCriteria, result *[]*ethtypes.Log) {
	collector := &pooledCollector{logs: result}
	f.collectLogs(block, crit, collector)
}

// Unified log collection logic - fallback path that fetches receipts individually
func (f *LogFetcher) collectLogs(block *coretypes.ResultBlock, crit filters.FilterCriteria, collector logCollector) {
	ctx := f.ctxProvider(block.Block.Height)

	txHashes := getTxHashesFromBlock(f.ctxProvider, f.txConfigProvider, f.k, block, f.includeSyntheticReceipts, f.cacheCreationMutex, f.globalBlockCache)
	if len(txHashes) == 0 {
		return
	}

	blockHeight := block.Block.Height
	blockHash := common.BytesToHash(block.BlockID.Hash)

	// Pre-encode bloom filter indexes for fast per-receipt filtering
	hasFilters := len(crit.Addresses) != 0 || len(crit.Topics) != 0
	var filterIndexes [][]BloomIndexes
	if hasFilters {
		filterIndexes = EncodeFilters(crit.Addresses, crit.Topics)
	}

	// Fetch receipts individually and filter logs locally
	var logIndex uint
	for txIdx, txHashEntry := range txHashes {
		rcpt, err := f.k.GetReceipt(ctx, txHashEntry.hash)
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("collectLogs: unable to find receipt for hash %s: %v", txHashEntry.hash.Hex(), err))
			continue
		}

		// Skip receipt if its bloom filter doesn't match the criteria
		if hasFilters && len(rcpt.LogsBloom) > 0 && !MatchFilters(ethtypes.Bloom(rcpt.LogsBloom), filterIndexes) {
			logIndex += uint(len(rcpt.Logs))
			continue
		}

		// Extract logs from receipt
		for _, log := range rcpt.Logs {
			// #nosec G115 -- blockHeight and txIdx are validated non-negative
			ethLog := &ethtypes.Log{
				Address:     common.HexToAddress(log.Address),
				Data:        log.Data,
				BlockNumber: uint64(blockHeight),
				TxHash:      txHashEntry.hash,
				TxIndex:     uint(txIdx),
				BlockHash:   blockHash,
				Index:       logIndex,
				Removed:     false,
			}
			ethLog.Topics = make([]common.Hash, len(log.Topics))
			for i, topic := range log.Topics {
				ethLog.Topics[i] = common.HexToHash(topic)
			}
			logIndex++

			if !MatchesCriteria(ethLog, crit) {
				continue
			}
			collector.Append(ethLog)
		}
	}
}

// MatchesCriteria checks if a log matches the filter criteria.
func MatchesCriteria(log *ethtypes.Log, crit filters.FilterCriteria) bool {
	return ethbloom.MatchesCriteria(log, crit)
}

// Optimized fetchBlocksByCrit with batch processing
func (f *LogFetcher) fetchBlocksByCrit(ctx context.Context, crit filters.FilterCriteria, lastToHeight int64, bloomIndexes [][]BloomIndexes) (chan *coretypes.ResultBlock, int64, bool, error) {
	if crit.BlockHash != nil {
		// Check for invalid zero hash
		zeroHash := common.Hash{}
		if *crit.BlockHash == zeroHash {
			// For invalid hash, return empty channel instead of error
			res := make(chan *coretypes.ResultBlock)
			close(res)
			return res, 0, false, nil
		}

		block, err := blockByHashRespectingWatermarks(ctx, f.tmClient, f.watermarks, crit.BlockHash[:], 1)
		if err != nil {
			// For non-existent blocks, return empty channel instead of error
			res := make(chan *coretypes.ResultBlock)
			close(res)
			return res, 0, false, nil
		}
		res := make(chan *coretypes.ResultBlock, 1)
		res <- block
		close(res)
		return res, 0, false, nil
	}

	applyOpenEndedLogLimit := f.filterConfig.maxLog > 0 && (crit.FromBlock == nil || crit.ToBlock == nil)
	latest, err := f.watermarks.LatestHeight(ctx)
	if err != nil {
		return nil, 0, false, err
	}
	earliest, err := f.watermarks.EarliestHeight(ctx)
	if err != nil {
		earliest = 0
	}
	begin, end, err := ComputeBlockBounds(latest, earliest, lastToHeight, crit)
	if err != nil {
		return nil, 0, false, err
	}

	blockRange := end - begin + 1
	if applyOpenEndedLogLimit && blockRange > f.filterConfig.maxBlock {
		begin = end - f.filterConfig.maxBlock + 1
		if begin < earliest {
			begin = earliest
		}
	} else if !applyOpenEndedLogLimit && f.filterConfig.maxBlock > 0 && blockRange > f.filterConfig.maxBlock {
		// Use consistent error message format
		return nil, 0, false, fmt.Errorf("block range too large (%d), maximum allowed is %d blocks", blockRange, f.filterConfig.maxBlock)
	}

	if begin > end {
		return nil, 0, false, fmt.Errorf("fromBlock %d is after toBlock %d", begin, end)
	}

	res := make(chan *coretypes.ResultBlock, end-begin+1)
	errChan := make(chan error, 1)
	runner := GetGlobalWorkerPool()
	var wg sync.WaitGroup

	// Batch processing with fail-fast
	for batchStart := begin; batchStart <= end; batchStart += int64(evmrpcconfig.WorkerBatchSize) {
		batchEnd := batchStart + int64(evmrpcconfig.WorkerBatchSize) - 1
		if batchEnd > end {
			batchEnd = end
		}

		wg.Add(1)
		if err := runner.SubmitWithMetrics(func(start, endHeight int64) func() {
			return func() {
				defer wg.Done()
				f.processBatch(ctx, start, endHeight, crit, bloomIndexes, res, errChan)
			}
		}(batchStart, batchEnd)); err != nil {
			wg.Done()
			return nil, 0, false, fmt.Errorf("system overloaded, please reduce request frequency: %w", err)
		}
	}

	go func() {
		defer recoverAndLog()
		wg.Wait()
		close(res)
		close(errChan)
	}()

	var firstErr error
	for err := range errChan {
		if firstErr == nil {
			firstErr = err
		}
	}

	if firstErr != nil {
		return nil, 0, false, firstErr
	}

	return res, end, applyOpenEndedLogLimit, nil
}

// Batch processing function for blocks
func (f *LogFetcher) processBatch(ctx context.Context, start, end int64, crit filters.FilterCriteria, bloomIndexes [][]BloomIndexes, res chan *coretypes.ResultBlock, errChan chan error) {
	defer func() {
		metrics.IncrementRpcRequestCounter("num_blocks_fetched", "blocks", true)
	}()

	wpMetrics := GetGlobalMetrics()

	for height := start; height <= end; height++ {
		if height == 0 {
			continue
		}

		// check cache first, without holding the semaphore
		if cachedEntry, found := f.globalBlockCache.Get(height); found {
			if cachedEntry.Block != nil {
				if err := f.watermarks.EnsureBlockHeightAvailable(ctx, cachedEntry.Block.Block.Height); err != nil {
					continue
				}
			}
			res <- cachedEntry.Block
			continue
		}

		// Block cache miss, acquire semaphore for I/O operations
		semWaitStart := time.Now()
		f.dbReadSemaphore <- struct{}{}
		wpMetrics.RecordDBSemaphoreWait(time.Since(semWaitStart))
		wpMetrics.RecordDBSemaphoreAcquire()

		// Re-check cache after acquiring semaphore, in case another worker cached it.
		if cachedEntry, found := f.globalBlockCache.Get(height); found {
			<-f.dbReadSemaphore
			wpMetrics.RecordDBSemaphoreRelease()
			if cachedEntry.Block != nil {
				if err := f.watermarks.EnsureBlockHeightAvailable(ctx, cachedEntry.Block.Block.Height); err != nil {
					continue
				}
			}
			res <- cachedEntry.Block
			continue
		}

		// check bloom filter if cache miss AND we have filters
		var blockBloom ethtypes.Bloom
		if len(crit.Addresses) != 0 || len(crit.Topics) != 0 {
			// Bloom cache miss - read from database
			providerCtx := f.ctxProvider(height)
			if f.includeSyntheticReceipts {
				blockBloom = f.k.GetBlockBloom(providerCtx)
			} else {
				blockBloom = f.k.GetEvmOnlyBlockBloom(providerCtx)
			}

			// When we cannot retrieve a bloom for the EVM-only view (all zeroes),
			// skip the bloom pre-filter instead of short-circuiting the block.
			if blockBloom != (ethtypes.Bloom{}) && !MatchFilters(blockBloom, bloomIndexes) {
				<-f.dbReadSemaphore
				wpMetrics.RecordDBSemaphoreRelease()
				continue // skip the block if bloom filter does not match
			}
		}

		// fetch block from network
		block, err := blockByNumberRespectingWatermarks(ctx, f.tmClient, f.watermarks, &height, 1)
		if err != nil {
			select {
			case errChan <- fmt.Errorf("failed to fetch block at height %d: %w", height, err):
			default:
			}
			<-f.dbReadSemaphore
			wpMetrics.RecordDBSemaphoreRelease()
			continue
		}

		// Use LoadOrStore to create/get cache entry atomically
		entry := loadOrStoreCacheEntry(f.cacheCreationMutex, f.globalBlockCache, height, block)
		// Fill bloom if we have it and it's missing
		if blockBloom != (ethtypes.Bloom{}) {
			fillMissingFields(entry, block, blockBloom)
		}
		<-f.dbReadSemaphore
		wpMetrics.RecordDBSemaphoreRelease()
		res <- block
	}
}
