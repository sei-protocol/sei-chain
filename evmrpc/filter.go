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
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/coretypes"
	tmtypes "github.com/tendermint/tendermint/types"
	"golang.org/x/time/rate"
)

const TxSearchPerPage = 10

const (
	// Worker pool settings
	MaxNumOfWorkers = 24 // each worker will handle a batch of WorkerBatchSize blocks
	WorkerBatchSize = 100
	WorkerQueueSize = 200

	// DB Concurrency Read Limit
	MaxDBReadConcurrency = 16

	// Request limits
	MaxBlockRange = 2000
	MaxLogLimit   = 10000

	// global request rate limit
	GlobalRPSLimit = 50
)

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

// Global worker pool
type WorkerPool struct {
	workers   int
	taskQueue chan func()
	once      sync.Once
	done      chan struct{}
	wg        sync.WaitGroup
}

var (
	globalWorkerPool *WorkerPool
	poolOnce         sync.Once
	// Semaphore to limit concurrent I/O operations against the database
	dbReadSemaphore  = make(chan struct{}, MaxDBReadConcurrency)
	globalBlockCache = NewBlockCache(1000)

	// every request consumes 1 token, regardless of block range
	globalRPSLimiter = rate.NewLimiter(rate.Limit(GlobalRPSLimit), GlobalRPSLimit)
	// Global bloom cache - smaller than block cache since blooms are tiny
	globalBloomCache = NewBloomCache(50000)

	// Global receipt cache - smaller than block cache since receipts are tiny
	globalReceiptCache = NewReceiptCache(50000)
)

func getGlobalWorkerPool() *WorkerPool {
	poolOnce.Do(func() {
		globalWorkerPool = &WorkerPool{
			workers:   MaxNumOfWorkers,
			taskQueue: make(chan func(), WorkerQueueSize),
			done:      make(chan struct{}),
		}
		globalWorkerPool.start()
	})
	return globalWorkerPool
}

func (wp *WorkerPool) start() {
	wp.once.Do(func() {
		for i := 0; i < wp.workers; i++ {
			wp.wg.Add(1)
			go func() {
				defer wp.wg.Done()
				// The worker will exit gracefully when the taskQueue is closed and drained.
				for task := range wp.taskQueue {
					task()
				}
			}()
		}
	})
}

// Fail fast submit - reject if queue is full
func (wp *WorkerPool) submit(task func()) error {
	select {
	case wp.taskQueue <- task:
		return nil
	case <-wp.done:
		return fmt.Errorf("worker pool is closing")
	default:
		// Queue is full - fail fast
		return fmt.Errorf("worker pool queue is full")
	}
}

func (wp *WorkerPool) Close() {
	close(wp.done)      // Signal that no new tasks should be submitted.
	close(wp.taskQueue) // Close the queue to signal workers to drain and exit.
	wp.wg.Wait()        // Wait for all workers to finish their remaining tasks.
}

// Generic LRU Cache implementation using generics
type LRUNode[K comparable, V any] struct {
	key        K
	value      V
	timestamp  time.Time
	prev, next *LRUNode[K, V]
}

type LRUCache[K comparable, V any] struct {
	nodes      map[K]*LRUNode[K, V]
	head, tail *LRUNode[K, V]
	maxSize    int
	size       int
	mutex      sync.RWMutex
}

func NewLRUCache[K comparable, V any](maxSize int) *LRUCache[K, V] {
	return &LRUCache[K, V]{
		nodes:   make(map[K]*LRUNode[K, V]),
		maxSize: maxSize,
	}
}

// Get retrieves a value from the cache
func (lru *LRUCache[K, V]) Get(key K) (V, bool) {
	lru.mutex.Lock()
	defer lru.mutex.Unlock()

	var zero V
	node, exists := lru.nodes[key]
	if !exists {
		return zero, false
	}

	// Check TTL (5 minutes for all caches)
	if time.Since(node.timestamp) > 300*time.Second {
		lru.removeNode(node)
		return zero, false
	}

	lru.moveToHead(node)
	return node.value, true
}

// Put stores a value in the cache
func (lru *LRUCache[K, V]) Put(key K, value V) {
	lru.mutex.Lock()
	defer lru.mutex.Unlock()

	if node, exists := lru.nodes[key]; exists {
		node.value = value
		node.timestamp = time.Now()
		lru.moveToHead(node)
		return
	}

	newNode := &LRUNode[K, V]{key: key, value: value, timestamp: time.Now()}

	if lru.size >= lru.maxSize {
		lru.removeTail()
	}

	lru.addToHead(newNode)
	lru.nodes[key] = newNode
	lru.size++
}

func (lru *LRUCache[K, V]) moveToHead(node *LRUNode[K, V]) {
	if lru.head == node {
		return
	}

	if node.prev != nil {
		node.prev.next = node.next
	}
	if node.next != nil {
		node.next.prev = node.prev
	}
	if lru.tail == node {
		lru.tail = node.prev
	}

	node.prev = nil
	node.next = lru.head
	if lru.head != nil {
		lru.head.prev = node
	}
	lru.head = node
	if lru.tail == nil {
		lru.tail = node
	}
}

func (lru *LRUCache[K, V]) removeTail() {
	if lru.tail == nil {
		return
	}

	delete(lru.nodes, lru.tail.key)

	if lru.tail.prev != nil {
		lru.tail.prev.next = nil
		lru.tail = lru.tail.prev
	} else {
		lru.head = nil
		lru.tail = nil
	}

	lru.size--
}

func (lru *LRUCache[K, V]) addToHead(node *LRUNode[K, V]) {
	node.prev = nil
	node.next = lru.head
	if lru.head != nil {
		lru.head.prev = node
	}
	lru.head = node
	if lru.tail == nil {
		lru.tail = node
	}
}

func (lru *LRUCache[K, V]) removeNode(node *LRUNode[K, V]) {
	if node.prev != nil {
		node.prev.next = node.next
	} else {
		lru.head = node.next
	}

	if node.next != nil {
		node.next.prev = node.prev
	} else {
		lru.tail = node.prev
	}

	delete(lru.nodes, node.key)
	lru.size--
}

// Type aliases for specific cache types
type BlockCache = LRUCache[int64, *coretypes.ResultBlock]
type BloomCache = LRUCache[int64, ethtypes.Bloom]
type ReceiptCache = LRUCache[common.Hash, *evmtypes.Receipt]

// Factory functions for creating specific cache types
func NewBlockCache(maxSize int) *BlockCache {
	return NewLRUCache[int64, *coretypes.ResultBlock](maxSize)
}

func NewBloomCache(maxSize int) *BloomCache {
	return NewLRUCache[int64, ethtypes.Bloom](maxSize)
}

func NewReceiptCache(maxSize int) *ReceiptCache {
	return NewLRUCache[common.Hash, *evmtypes.Receipt](maxSize)
}

// Log slice pool to reduce allocations in batch processing
type LogSlicePool struct {
	pool sync.Pool
}

func NewLogSlicePool() *LogSlicePool {
	return &LogSlicePool{
		pool: sync.Pool{
			New: func() interface{} {
				return make([]*ethtypes.Log, 0, 100) // Pre-allocate capacity of 100
			},
		},
	}
}

func (p *LogSlicePool) Get() []*ethtypes.Log {
	return p.pool.Get().([]*ethtypes.Log)[:0] // Reset length but keep capacity
}

func (p *LogSlicePool) Put(slice []*ethtypes.Log) {
	if cap(slice) < 1000 { // Avoid storing overly large slices
		p.pool.Put(slice)
	}
}

// Global log slice pool
var globalLogSlicePool = NewLogSlicePool()

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
	tmClient       rpcclient.Client
	filtersMu      sync.RWMutex
	filters        map[ethrpc.ID]filter
	toDelete       chan ethrpc.ID
	filterConfig   *FilterConfig
	logFetcher     *LogFetcher
	connectionType ConnectionType
	namespace      string
	shutdownCtx    context.Context
	shutdownCancel context.CancelFunc
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

func NewFilterAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, txConfig client.TxConfig, filterConfig *FilterConfig, connectionType ConnectionType, namespace string) *FilterAPI {
	if filterConfig.maxBlock <= 0 {
		filterConfig.maxBlock = MaxBlockRange
	}
	if filterConfig.maxLog <= 0 {
		filterConfig.maxLog = MaxLogLimit
	}

	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	logFetcher := &LogFetcher{tmClient: tmClient, k: k, ctxProvider: ctxProvider, txConfig: txConfig, filterConfig: filterConfig, includeSyntheticReceipts: shouldIncludeSynthetic(namespace)}
	filters := make(map[ethrpc.ID]filter)
	api := &FilterAPI{
		namespace:      namespace,
		tmClient:       tmClient,
		filtersMu:      sync.RWMutex{},
		filters:        filters,
		toDelete:       make(chan ethrpc.ID, 1000),
		filterConfig:   filterConfig,
		logFetcher:     logFetcher,
		connectionType: connectionType,
		shutdownCtx:    shutdownCtx,
		shutdownCancel: shutdownCancel,
	}

	go api.cleanupLoop(filterConfig.timeout)
	return api
}

// Unified cleanup loop that handles both timeout and manual deletion
func (a *FilterAPI) cleanupLoop(timeout time.Duration) {
	ticker := time.NewTicker(timeout / 2) // Check more frequently than timeout
	defer ticker.Stop()

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
	defer recordMetrics(fmt.Sprintf("%s_newFilter", a.namespace), a.connectionType, time.Now(), err == nil)

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
	defer recordMetrics(fmt.Sprintf("%s_newBlockFilter", a.namespace), a.connectionType, time.Now(), err == nil)

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
	defer recordMetrics(fmt.Sprintf("%s_getFilterChanges", a.namespace), a.connectionType, time.Now(), err == nil)

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
	defer recordMetrics(fmt.Sprintf("%s_getFilterLogs", a.namespace), a.connectionType, time.Now(), err == nil)

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
	if !globalRPSLimiter.Allow() {
		return nil, fmt.Errorf("log query rate limit exceeded, please try again later")
	}
	defer recordMetrics(fmt.Sprintf("%s_getLogs", a.namespace), a.connectionType, time.Now(), err == nil)

	// Calculate block range
	latest := a.logFetcher.ctxProvider(LatestCtxHeight).BlockHeight()
	begin, end := latest, latest
	if crit.FromBlock != nil {
		begin = getHeightFromBigIntBlockNumber(latest, crit.FromBlock)
	}
	if crit.ToBlock != nil {
		end = getHeightFromBigIntBlockNumber(latest, crit.ToBlock)
		if crit.FromBlock == nil && begin > end {
			begin = end
		}
	}

	blockRange := end - begin + 1

	if blockRange > MaxBlockRange {
		return nil, fmt.Errorf("block range too large (%d), maximum allowed is %d blocks", blockRange, MaxBlockRange)
	}

	logs, _, err := a.logFetcher.GetLogsByFilters(ctx, crit, 0)
	return logs, err
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
	defer recordMetrics(fmt.Sprintf("%s_uninstallFilter", a.namespace), a.connectionType, time.Now(), res)

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
	txConfig                 client.TxConfig
	ctxProvider              func(int64) sdk.Context
	filterConfig             *FilterConfig
	includeSyntheticReceipts bool
}

func (f *LogFetcher) GetLogsByFilters(ctx context.Context, crit filters.FilterCriteria, lastToHeight int64) (res []*ethtypes.Log, end int64, err error) {
	latest := f.ctxProvider(LatestCtxHeight).BlockHeight()
	begin, end := latest, latest
	if crit.FromBlock != nil {
		begin = getHeightFromBigIntBlockNumber(latest, crit.FromBlock)
	}
	if crit.ToBlock != nil {
		end = getHeightFromBigIntBlockNumber(latest, crit.ToBlock)
		if crit.FromBlock == nil && begin > end {
			begin = end
		}
	}
	if lastToHeight > begin {
		begin = lastToHeight
	}

	blockRange := end - begin + 1

	if blockRange > MaxBlockRange {
		return nil, 0, fmt.Errorf("block range too large (%d), maximum allowed is %d blocks", blockRange, MaxBlockRange)
	}

	bloomIndexes := EncodeFilters(crit.Addresses, crit.Topics)
	blocks, end, applyOpenEndedLogLimit, err := f.fetchBlocksByCrit(ctx, crit, lastToHeight, bloomIndexes)
	if err != nil {
		return nil, 0, err
	}

	runner := getGlobalWorkerPool()
	var resultsMutex sync.Mutex
	sortedBatches := make([][]*ethtypes.Log, 0)
	var wg sync.WaitGroup
	var submitError error

	processBatch := func(batch []*coretypes.ResultBlock) {
		defer wg.Done()
		// Each worker gets a clean slice from the pool
		localLogs := globalLogSlicePool.Get()

		for _, block := range batch {
			f.GetLogsForBlockPooled(block, crit, bloomIndexes, &localLogs)
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
	blockBatch := make([]*coretypes.ResultBlock, 0, WorkerBatchSize)
	for block := range blocks {
		blockBatch = append(blockBatch, block)

		if len(blockBatch) >= WorkerBatchSize {
			batch := blockBatch
			wg.Add(1)

			if err := runner.submit(func() { processBatch(batch) }); err != nil {
				wg.Done()
				submitError = fmt.Errorf("system overloaded, please reduce request frequency: %w", err)
				break
			}
			blockBatch = make([]*coretypes.ResultBlock, 0, WorkerBatchSize)
		}
	}

	if submitError != nil {
		return nil, 0, submitError
	}

	// Process remaining blocks
	if len(blockBatch) > 0 {
		wg.Add(1)
		if err := runner.submit(func() { processBatch(blockBatch) }); err != nil {
			wg.Done()
			return nil, 0, fmt.Errorf("system overloaded, please reduce request frequency: %w", err)
		}
	}

	wg.Wait()

	// Now that all workers are done, we put the slices back into the pool.
	// This must be done after the merge is complete.
	defer func() {
		for _, batch := range sortedBatches {
			globalLogSlicePool.Put(batch)
		}
	}()

	res = f.mergeSortedLogs(sortedBatches)

	// Apply rate limit
	if applyOpenEndedLogLimit && int64(len(res)) >= f.filterConfig.maxLog {
		res = res[:int(f.filterConfig.maxLog)]
	}

	return res, end, err
}

func (f *LogFetcher) mergeSortedLogs(batches [][]*ethtypes.Log) []*ethtypes.Log {
	totalSize := 0
	for _, b := range batches {
		totalSize += len(b)
	}
	if totalSize == 0 {
		return nil
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

func (f *LogFetcher) GetLogsForBlock(block *coretypes.ResultBlock, crit filters.FilterCriteria, filters [][]bloomIndexes) []*ethtypes.Log {
	possibleLogs := f.FindLogsByBloom(block, crit, filters)
	matchedLogs := utils.Filter(possibleLogs, func(l *ethtypes.Log) bool { return f.IsLogExactMatch(l, crit) })
	for _, l := range matchedLogs {
		l.BlockHash = common.BytesToHash(block.BlockID.Hash)
	}
	return matchedLogs
}

// Pooled version that reuses slice allocation
func (f *LogFetcher) GetLogsForBlockPooled(block *coretypes.ResultBlock, crit filters.FilterCriteria, filters [][]bloomIndexes, result *[]*ethtypes.Log) {
	ctx := f.ctxProvider(LatestCtxHeight)
	totalLogs := uint(0)

	for _, hash := range getTxHashesFromBlock(block, f.txConfig, f.includeSyntheticReceipts) {
		// Try to get receipt from cache first
		receipt, found := globalReceiptCache.Get(hash)
		if !found {
			// Cache miss - fetch from database and cache it
			var err error
			receipt, err = f.k.GetReceipt(ctx, hash)
			if err != nil {
				if !f.includeSyntheticReceipts {
					ctx.Logger().Error(fmt.Sprintf("FindLogsByBloom: unable to find receipt for hash %s", hash.Hex()))
				}
				continue
			}
			// Store in cache for future use
			globalReceiptCache.Put(hash, receipt)
		}

		if !f.includeSyntheticReceipts && (receipt.TxType == ShellEVMTxType || receipt.EffectiveGasPrice == 0) {
			continue
		}

		// check bloom filter if filter is provided
		if len(crit.Addresses) != 0 || len(crit.Topics) != 0 {
			if len(receipt.LogsBloom) > 0 && MatchFilters(ethtypes.Bloom(receipt.LogsBloom), filters) {
				*result = append(*result, keeper.GetLogsForTx(receipt, totalLogs)...)
			}
		} else {
			// no filter, return all logs
			*result = append(*result, keeper.GetLogsForTx(receipt, totalLogs)...)
		}
		totalLogs += uint(len(receipt.Logs))
	}
}

func (f *LogFetcher) FindLogsByBloom(block *coretypes.ResultBlock, crit filters.FilterCriteria, filters [][]bloomIndexes) (res []*ethtypes.Log) {
	ctx := f.ctxProvider(LatestCtxHeight)
	totalLogs := uint(0)

	for _, hash := range getTxHashesFromBlock(block, f.txConfig, f.includeSyntheticReceipts) {
		// Try to get receipt from cache first
		receipt, found := globalReceiptCache.Get(hash)
		if !found {
			// Cache miss - fetch from database and cache it
			var err error
			receipt, err = f.k.GetReceipt(ctx, hash)
			if err != nil {
				if !f.includeSyntheticReceipts {
					ctx.Logger().Error(fmt.Sprintf("FindLogsByBloom: unable to find receipt for hash %s", hash.Hex()))
				}
				continue
			}
			// Store in cache for future use
			globalReceiptCache.Put(hash, receipt)
		}

		if !f.includeSyntheticReceipts && (receipt.TxType == ShellEVMTxType || receipt.EffectiveGasPrice == 0) {
			continue
		}

		// check bloom filter if filter is provided
		if len(crit.Addresses) != 0 || len(crit.Topics) != 0 {
			if len(receipt.LogsBloom) > 0 && MatchFilters(ethtypes.Bloom(receipt.LogsBloom), filters) {
				res = append(res, keeper.GetLogsForTx(receipt, totalLogs)...)
			}
		} else {
			// no filter, return all logs
			res = append(res, keeper.GetLogsForTx(receipt, totalLogs)...)
		}
		totalLogs += uint(len(receipt.Logs))
	}
	return
}

// Pooled version that reuses slice allocation
func (f *LogFetcher) FindLogsByBloomPooled(block *coretypes.ResultBlock, crit filters.FilterCriteria, filters [][]bloomIndexes, result *[]*ethtypes.Log) {
	ctx := f.ctxProvider(LatestCtxHeight)
	totalLogs := uint(0)

	for _, hash := range getTxHashesFromBlock(block, f.txConfig, f.includeSyntheticReceipts) {
		// Try to get receipt from cache first
		receipt, found := globalReceiptCache.Get(hash)
		if !found {
			// Cache miss - fetch from database and cache it
			var err error
			receipt, err = f.k.GetReceipt(ctx, hash)
			if err != nil {
				if !f.includeSyntheticReceipts {
					ctx.Logger().Error(fmt.Sprintf("FindLogsByBloomPooled: unable to find receipt for hash %s", hash.Hex()))
				}
				continue
			}
			// Store in cache for future use
			globalReceiptCache.Put(hash, receipt)
		}

		if !f.includeSyntheticReceipts && (receipt.TxType == ShellEVMTxType || receipt.EffectiveGasPrice == 0) {
			continue
		}

		// check bloom filter if filter is provided
		if len(crit.Addresses) != 0 || len(crit.Topics) != 0 {
			if len(receipt.LogsBloom) > 0 && MatchFilters(ethtypes.Bloom(receipt.LogsBloom), filters) {
				*result = append(*result, keeper.GetLogsForTx(receipt, totalLogs)...)
			}
		} else {
			// no filter, return all logs
			*result = append(*result, keeper.GetLogsForTx(receipt, totalLogs)...)
		}
		totalLogs += uint(len(receipt.Logs))
	}
}

func (f *LogFetcher) IsLogExactMatch(log *ethtypes.Log, crit filters.FilterCriteria) bool {
	addrMatch := len(crit.Addresses) == 0
	for _, addrFilter := range crit.Addresses {
		if log.Address == addrFilter {
			addrMatch = true
			break
		}
	}
	return addrMatch && matchTopics(crit.Topics, log.Topics)
}

// Optimized fetchBlocksByCrit with batch processing
func (f *LogFetcher) fetchBlocksByCrit(ctx context.Context, crit filters.FilterCriteria, lastToHeight int64, bloomIndexes [][]bloomIndexes) (chan *coretypes.ResultBlock, int64, bool, error) {
	if crit.BlockHash != nil {
		block, err := blockByHashWithRetry(ctx, f.tmClient, crit.BlockHash[:], 1)
		if err != nil {
			return nil, 0, false, err
		}
		res := make(chan *coretypes.ResultBlock, 1)
		res <- block
		close(res)
		return res, 0, false, nil
	}

	applyOpenEndedLogLimit := f.filterConfig.maxLog > 0 && (crit.FromBlock == nil || crit.ToBlock == nil)
	latest := f.ctxProvider(LatestCtxHeight).BlockHeight()
	begin, end := latest, latest
	if crit.FromBlock != nil {
		begin = getHeightFromBigIntBlockNumber(latest, crit.FromBlock)
	}
	if crit.ToBlock != nil {
		end = getHeightFromBigIntBlockNumber(latest, crit.ToBlock)
		if crit.FromBlock == nil && begin > end {
			begin = end
		}
	}
	if lastToHeight > begin {
		begin = lastToHeight
	}

	blockRange := end - begin + 1
	if applyOpenEndedLogLimit && blockRange > f.filterConfig.maxBlock {
		begin = end - f.filterConfig.maxBlock + 1
		if begin < 1 {
			begin = 1
		}
	} else if !applyOpenEndedLogLimit && f.filterConfig.maxBlock > 0 && blockRange > f.filterConfig.maxBlock {
		return nil, 0, false, fmt.Errorf("a maximum of %d blocks worth of logs may be requested at a time", f.filterConfig.maxBlock)
	}

	if begin > end {
		return nil, 0, false, fmt.Errorf("fromBlock %d is after toBlock %d", begin, end)
	}

	res := make(chan *coretypes.ResultBlock, end-begin+1)
	errChan := make(chan error, 1)
	runner := getGlobalWorkerPool()
	var wg sync.WaitGroup

	// Batch processing with fail-fast
	for batchStart := begin; batchStart <= end; batchStart += int64(WorkerBatchSize) {
		batchEnd := batchStart + int64(WorkerBatchSize) - 1
		if batchEnd > end {
			batchEnd = end
		}

		wg.Add(1)
		if err := runner.submit(func(start, endHeight int64) func() {
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
func (f *LogFetcher) processBatch(ctx context.Context, start, end int64, crit filters.FilterCriteria, bloomIndexes [][]bloomIndexes, res chan *coretypes.ResultBlock, errChan chan error) {
	for height := start; height <= end; height++ {
		if height == 0 {
			continue
		}

		// check cache first, without holding the semaphore
		if cachedBlock, found := globalBlockCache.Get(height); found {
			res <- cachedBlock
			continue
		}

		// Block cache miss, acquire semaphore for I/O operations
		dbReadSemaphore <- struct{}{}

		// Re-check cache after acquiring semaphore, in case another worker fetched it.
		if cachedBlock, found := globalBlockCache.Get(height); found {
			<-dbReadSemaphore
			res <- cachedBlock
			continue
		}

		// check bloom filter if cache miss AND we have filters
		if len(crit.Addresses) != 0 || len(crit.Topics) != 0 {
			// Try bloom cache first
			if cachedBloom, found := globalBloomCache.Get(height); found {
				if !MatchFilters(cachedBloom, bloomIndexes) {
					<-dbReadSemaphore
					continue // skip the block if bloom filter does not match
				}
			} else {
				// Bloom cache miss - read from database
				providerCtx := f.ctxProvider(height)
				blockBloom := f.k.GetBlockBloom(providerCtx)

				// Cache the bloom for future use
				globalBloomCache.Put(height, blockBloom)

				if !MatchFilters(blockBloom, bloomIndexes) {
					<-dbReadSemaphore
					continue // skip the block if bloom filter does not match
				}
			}
		}

		// fetch block from network
		block, err := blockByNumberWithRetry(ctx, f.tmClient, &height, 1)
		if err != nil {
			select {
			case errChan <- fmt.Errorf("failed to fetch block at height %d: %w", height, err):
			default:
			}
			<-dbReadSemaphore
			continue
		}

		globalBlockCache.Put(height, block)
		<-dbReadSemaphore
		res <- block
	}
}

func matchTopics(topics [][]common.Hash, eventTopics []common.Hash) bool {
	for i, topicList := range topics {
		if len(topicList) == 0 {
			// anything matches for this position
			continue
		}
		if i >= len(eventTopics) {
			return false
		}
		matched := false
		for _, topic := range topicList {
			if topic == eventTopics[i] {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}
