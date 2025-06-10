package evmrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/utils/metrics"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/coretypes"
	tmtypes "github.com/tendermint/tendermint/types"
)

const TxSearchPerPage = 10

// Worker pool configuration
const MaxNumOfWorkers = 200
const WorkerQueueSize = 10000

// Default range limits
const DefaultMaxBlockRange = 2000
const DefaultMaxLogLimit = 10000

// Batch processing constants
const BatchSize = 25

const (
	// Block range thresholds
	SmallRangeThreshold  = 100
	MediumRangeThreshold = 200
	MaxBlockRange        = 2000

	// Corresponding RPS limits
	SmallRangeRPS  = 50 // 50 RPS for ≤100 blocks
	MediumRangeRPS = 25 // 25 RPS for ≤200 blocks
	LargeRangeRPS  = 10 // 10 RPS for ≤2000 blocks

	// Global fallback limit
	GlobalMaxRPS = 50
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
				for {
					select {
					case task, ok := <-wp.taskQueue:
						if !ok {
							return
						}
						task()
					case <-wp.done:
						return
					}
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
		task()
		return nil
	default:
		// Queue is full - fail fast
		return fmt.Errorf("worker pool queue is full, rejecting request")
	}
}

func (wp *WorkerPool) Close() {
	close(wp.done)
	close(wp.taskQueue)
	wp.wg.Wait()
}

// O(1) LRU Cache implementation
type LRUNode struct {
	height     int64
	block      *coretypes.ResultBlock
	timestamp  time.Time
	prev, next *LRUNode
}

type BlockCache struct {
	nodes      map[int64]*LRUNode
	head, tail *LRUNode
	maxSize    int
	size       int
	mutex      sync.RWMutex
}

func NewBlockCache(maxSize int) *BlockCache {
	return &BlockCache{
		nodes:   make(map[int64]*LRUNode),
		maxSize: maxSize,
	}
}

func (bc *BlockCache) Get(height int64) (*coretypes.ResultBlock, bool) {
	bc.mutex.RLock()
	defer bc.mutex.RUnlock()

	node, exists := bc.nodes[height]
	if !exists {
		return nil, false
	}

	if time.Since(node.timestamp) > 300*time.Second {
		return nil, false
	}

	return node.block, true
}

func (bc *BlockCache) Put(height int64, block *coretypes.ResultBlock) {
	bc.mutex.Lock()
	defer bc.mutex.Unlock()

	if node, exists := bc.nodes[height]; exists {
		node.block = block
		node.timestamp = time.Now()
		bc.moveToHead(node)
		return
	}

	newNode := &LRUNode{height: height, block: block, timestamp: time.Now()}

	if bc.size >= bc.maxSize {
		bc.removeTail()
	}

	bc.addToHead(newNode)
	bc.nodes[height] = newNode
	bc.size++
}

func (bc *BlockCache) moveToHead(node *LRUNode) {
	if bc.head == node {
		return
	}

	if node.prev != nil {
		node.prev.next = node.next
	}
	if node.next != nil {
		node.next.prev = node.prev
	}
	if bc.tail == node {
		bc.tail = node.prev
	}

	node.prev = nil
	node.next = bc.head
	if bc.head != nil {
		bc.head.prev = node
	}
	bc.head = node
	if bc.tail == nil {
		bc.tail = node
	}
}

func (bc *BlockCache) removeTail() {
	if bc.tail == nil {
		return
	}

	delete(bc.nodes, bc.tail.height)

	if bc.tail.prev != nil {
		bc.tail.prev.next = nil
		bc.tail = bc.tail.prev
	} else {
		bc.head = nil
		bc.tail = nil
	}

	bc.size--
}

func (bc *BlockCache) addToHead(node *LRUNode) {
	node.prev = nil
	node.next = bc.head
	if bc.head != nil {
		bc.head.prev = node
	}
	bc.head = node
	if bc.tail == nil {
		bc.tail = node
	}
}

var globalBlockCache = NewBlockCache(3000)

// **重构后的Dynamic Rate Limiting System**
type RateLimitConfig struct {
	Threshold int64
	RPS       float64
	Limiter   *rate.Limiter
}

var rateLimitConfigs = []RateLimitConfig{
	{SmallRangeThreshold, SmallRangeRPS, rate.NewLimiter(SmallRangeRPS, int(SmallRangeRPS))},
	{MediumRangeThreshold, MediumRangeRPS, rate.NewLimiter(MediumRangeRPS, int(MediumRangeRPS))},
	{MaxBlockRange, LargeRangeRPS, rate.NewLimiter(LargeRangeRPS, int(LargeRangeRPS))},
}

// Global rate limiter for GetLogs requests
var globalGetLogsLimiter = rate.NewLimiter(rate.Limit(GlobalMaxRPS), GlobalMaxRPS)

func getDynamicRateLimitConfig(blockRange int64) RateLimitConfig {
	for _, config := range rateLimitConfigs {
		if blockRange <= config.Threshold {
			return config
		}
	}
	// Fallback to largest range config
	return rateLimitConfigs[len(rateLimitConfigs)-1]
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
		filterConfig.maxBlock = DefaultMaxBlockRange
	}
	if filterConfig.maxLog <= 0 {
		filterConfig.maxLog = DefaultMaxLogLimit
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

func (a *FilterAPI) GetLogs(
	ctx context.Context,
	crit filters.FilterCriteria,
) (res []*ethtypes.Log, err error) {
	defer recordMetrics(fmt.Sprintf("%s_getLogs", a.namespace), a.connectionType, time.Now(), err == nil)

	// Global TPS limiting
	if !globalGetLogsLimiter.Allow() {
		return nil, fmt.Errorf("getLogs rate limit exceeded (%d RPS), please try again later", GlobalMaxRPS)
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

	rateLimitConfig := getDynamicRateLimitConfig(blockRange)
	if !rateLimitConfig.Limiter.Allow() {
		return nil, 0, fmt.Errorf("rate limit exceeded for %d block range (%.0f RPS limit), please try again later", blockRange, rateLimitConfig.RPS)
	}

	bloomIndexes := EncodeFilters(crit.Addresses, crit.Topics)
	blocks, end, applyOpenEndedLogLimit, err := f.fetchBlocksByCrit(ctx, crit, lastToHeight, bloomIndexes)
	if err != nil {
		return nil, 0, err
	}

	runner := getGlobalWorkerPool()
	resultsChan := make(chan *ethtypes.Log, 5000)
	res = []*ethtypes.Log{}
	var wg sync.WaitGroup
	var submitError error

	// Batch process with fail-fast
	blockBatch := make([]*coretypes.ResultBlock, 0, BatchSize)
	for block := range blocks {
		blockBatch = append(blockBatch, block)

		if len(blockBatch) >= BatchSize {
			batch := blockBatch
			wg.Add(1)

			// Fail fast if worker pool is full
			if err := runner.submit(func() {
				defer func() {
					metrics.IncrementRpcRequestCounter("num_blocks_fetched", "logs", true)
					wg.Done()
				}()
				f.processBatchLogs(batch, crit, bloomIndexes, resultsChan)
			}); err != nil {
				wg.Done()
				submitError = fmt.Errorf("system overloaded, please reduce request frequency: %w", err)
				break
			}
			blockBatch = make([]*coretypes.ResultBlock, 0, BatchSize)
		}
	}

	if submitError != nil {
		return nil, 0, submitError
	}

	// Process remaining blocks
	if len(blockBatch) > 0 {
		wg.Add(1)
		if err := runner.submit(func() {
			defer wg.Done()
			f.processBatchLogs(blockBatch, crit, bloomIndexes, resultsChan)
		}); err != nil {
			wg.Done()
			return nil, 0, fmt.Errorf("system overloaded, please reduce request frequency: %w", err)
		}
	}

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Aggregate results
	for result := range resultsChan {
		res = append(res, result)
	}

	// Sort results
	sort.Slice(res, func(i, j int) bool {
		return res[i].BlockNumber < res[j].BlockNumber
	})

	// Apply rate limit
	if applyOpenEndedLogLimit && int64(len(res)) >= f.filterConfig.maxLog {
		res = res[:int(f.filterConfig.maxLog)]
	}

	return res, end, err
}

// New batch log processing function
func (f *LogFetcher) processBatchLogs(blocks []*coretypes.ResultBlock, crit filters.FilterCriteria, bloomIndexes [][]bloomIndexes, resultsChan chan *ethtypes.Log) {
	for _, block := range blocks {
		matchedLogs := f.GetLogsForBlock(block, crit, bloomIndexes)
		for _, log := range matchedLogs {
			resultsChan <- log
		}
	}
}

func (f *LogFetcher) GetLogsForBlock(block *coretypes.ResultBlock, crit filters.FilterCriteria, filters [][]bloomIndexes) []*ethtypes.Log {
	possibleLogs := f.FindLogsByBloom(block, crit, filters)
	matchedLogs := utils.Filter(possibleLogs, func(l *ethtypes.Log) bool { return f.IsLogExactMatch(l, crit) })
	for _, l := range matchedLogs {
		l.BlockHash = common.BytesToHash(block.BlockID.Hash)
	}
	return matchedLogs
}

func (f *LogFetcher) FindLogsByBloom(block *coretypes.ResultBlock, crit filters.FilterCriteria, filters [][]bloomIndexes) (res []*ethtypes.Log) {
	ctx := f.ctxProvider(LatestCtxHeight)
	totalLogs := uint(0)

	for _, hash := range getTxHashesFromBlock(block, f.txConfig, f.includeSyntheticReceipts) {
		receipt, err := f.k.GetReceipt(ctx, hash)
		if err != nil {
			if !f.includeSyntheticReceipts {
				ctx.Logger().Error(fmt.Sprintf("FindLogsByBloom: unable to find receipt for hash %s", hash.Hex()))
			}
			continue
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
	for batchStart := begin; batchStart <= end; batchStart += int64(BatchSize) {
		batchEnd := batchStart + int64(BatchSize) - 1
		if batchEnd > end {
			batchEnd = end
		}

		wg.Add(1)
		if err := runner.submit(func(start, endHeight int64) func() {
			return func() {
				defer func() {
					metrics.IncrementRpcRequestCounter("num_blocks_fetched", "blocks", true)
					wg.Done()
				}()
				f.processBatch(ctx, start, endHeight, crit, bloomIndexes, res, errChan)
			}
		}(batchStart, batchEnd)); err != nil {
			wg.Done()
			return nil, 0, false, fmt.Errorf("system overloaded: %w", err)
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

		// check cache first
		if cachedBlock, found := globalBlockCache.Get(height); found {
			res <- cachedBlock
			continue
		}

		// check bloom filter if cache miss
		if len(crit.Addresses) != 0 || len(crit.Topics) != 0 {
			providerCtx := f.ctxProvider(height)
			blockBloom := f.k.GetBlockBloom(providerCtx)
			if !MatchFilters(blockBloom, bloomIndexes) {
				continue // skip the block if bloom filter does not match
			}
		}

		// fetch block from network
		block, err := blockByNumberWithRetry(ctx, f.tmClient, &height, 1)
		if err != nil {
			select {
			case errChan <- fmt.Errorf("failed to fetch block at height %d: %w", height, err):
			default:
			}
			continue
		}

		globalBlockCache.Put(height, block)
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
