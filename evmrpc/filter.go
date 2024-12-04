package evmrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/coretypes"
	tmtypes "github.com/tendermint/tendermint/types"
)

const TxSearchPerPage = 10

type FilterType byte

const (
	UnknownSubscription FilterType = iota
	LogsSubscription
	BlocksSubscription
)

type filter struct {
	typ      FilterType
	fc       filters.FilterCriteria
	deadline *time.Timer

	// BlocksSubscription
	blockCursor string

	// LogsSubscription
	lastToHeight int64
}

type FilterAPI struct {
	tmClient       rpcclient.Client
	filtersMu      sync.Mutex
	filters        map[ethrpc.ID]filter
	filterConfig   *FilterConfig
	logFetcher     *LogFetcher
	connectionType ConnectionType
	namespace      string
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

func NewFilterAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, filterConfig *FilterConfig, connectionType ConnectionType, namespace string) *FilterAPI {
	logFetcher := &LogFetcher{tmClient: tmClient, k: k, ctxProvider: ctxProvider, filterConfig: filterConfig, includeSyntheticReceipts: shouldIncludeSynthetic(namespace)}
	filters := make(map[ethrpc.ID]filter)
	api := &FilterAPI{
		namespace:      namespace,
		tmClient:       tmClient,
		filtersMu:      sync.Mutex{},
		filters:        filters,
		filterConfig:   filterConfig,
		logFetcher:     logFetcher,
		connectionType: connectionType,
	}

	go api.timeoutLoop(filterConfig.timeout)

	return api
}

func (a *FilterAPI) timeoutLoop(timeout time.Duration) {
	ticker := time.NewTicker(timeout)
	defer ticker.Stop()
	for {
		<-ticker.C
		a.filtersMu.Lock()
		for id, filter := range a.filters {
			select {
			case <-filter.deadline.C:
				delete(a.filters, id)
			default:
				continue
			}
		}
		a.filtersMu.Unlock()
	}
}

func (a *FilterAPI) NewFilter(
	_ context.Context,
	crit filters.FilterCriteria,
) (id ethrpc.ID, err error) {
	defer recordMetrics(fmt.Sprintf("%s_newFilter", a.namespace), a.connectionType, time.Now(), err == nil)
	a.filtersMu.Lock()
	defer a.filtersMu.Unlock()
	curFilterID := ethrpc.NewID()
	a.filters[curFilterID] = filter{
		typ:          LogsSubscription,
		fc:           crit,
		deadline:     time.NewTimer(a.filterConfig.timeout),
		lastToHeight: 0,
	}
	return curFilterID, nil
}

func (a *FilterAPI) NewBlockFilter(
	_ context.Context,
) (id ethrpc.ID, err error) {
	defer recordMetrics(fmt.Sprintf("%s_newBlockFilter", a.namespace), a.connectionType, time.Now(), err == nil)
	a.filtersMu.Lock()
	defer a.filtersMu.Unlock()
	curFilterID := ethrpc.NewID()
	a.filters[curFilterID] = filter{
		typ:         BlocksSubscription,
		deadline:    time.NewTimer(a.filterConfig.timeout),
		blockCursor: "",
	}
	return curFilterID, nil
}

func (a *FilterAPI) GetFilterChanges(
	ctx context.Context,
	filterID ethrpc.ID,
) (res interface{}, err error) {
	defer recordMetrics(fmt.Sprintf("%s_getFilterChanges", a.namespace), a.connectionType, time.Now(), err == nil)
	a.filtersMu.Lock()
	defer a.filtersMu.Unlock()
	filter, ok := a.filters[filterID]
	if !ok {
		return nil, errors.New("filter does not exist")
	}

	if !filter.deadline.Stop() {
		// timer expired but filter is not yet removed in timeout loop
		// receive timer value and reset timer
		<-filter.deadline.C
	}
	filter.deadline.Reset(a.filterConfig.timeout)

	switch filter.typ {
	case BlocksSubscription:
		hashes, cursor, err := a.getBlockHeadersAfter(ctx, filter.blockCursor)
		if err != nil {
			return nil, err
		}
		updatedFilter := a.filters[filterID]
		updatedFilter.blockCursor = cursor
		a.filters[filterID] = updatedFilter
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
		updatedFilter := a.filters[filterID]
		updatedFilter.lastToHeight = lastToHeight + 1
		a.filters[filterID] = updatedFilter
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
	a.filtersMu.Lock()
	defer a.filtersMu.Unlock()
	filter, ok := a.filters[filterID]
	if !ok {
		return nil, errors.New("filter does not exist")
	}

	if !filter.deadline.Stop() {
		// timer expired but filter is not yet removed in timeout loop
		// receive timer value and reset timer
		<-filter.deadline.C
	}
	filter.deadline.Reset(a.filterConfig.timeout)

	logs, lastToHeight, err := a.logFetcher.GetLogsByFilters(ctx, filter.fc, 0)
	if err != nil {
		return nil, err
	}
	updatedFilter := a.filters[filterID]
	updatedFilter.lastToHeight = lastToHeight
	a.filters[filterID] = updatedFilter
	return logs, nil
}

func (a *FilterAPI) GetLogs(
	ctx context.Context,
	crit filters.FilterCriteria,
) (res []*ethtypes.Log, err error) {
	defer recordMetrics(fmt.Sprintf("%s_getLogs", a.namespace), a.connectionType, time.Now(), err == nil)
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
	a.filtersMu.Lock()
	defer a.filtersMu.Unlock()
	_, found := a.filters[filterID]
	if !found {
		return false
	}
	delete(a.filters, filterID)
	return true
}

type LogFetcher struct {
	tmClient                 rpcclient.Client
	k                        *keeper.Keeper
	ctxProvider              func(int64) sdk.Context
	filterConfig             *FilterConfig
	includeSyntheticReceipts bool
}

func (f *LogFetcher) GetLogsByFilters(ctx context.Context, crit filters.FilterCriteria, lastToHeight int64) (res []*ethtypes.Log, end int64, err error) {
	bloomIndexes := EncodeFilters(crit.Addresses, crit.Topics)
	if crit.BlockHash != nil {
		block, err := blockByHashWithRetry(ctx, f.tmClient, crit.BlockHash[:], 1)
		if err != nil {
			return nil, 0, err
		}
		return f.GetLogsForBlock(ctx, block, crit, bloomIndexes), block.Block.Height, nil
	}
	applyOpenEndedLogLimit := f.filterConfig.maxLog > 0 && (crit.FromBlock == nil || crit.ToBlock == nil)
	latest := f.ctxProvider(LatestCtxHeight).BlockHeight()
	begin, end := latest, latest
	if crit.FromBlock != nil {
		begin = getHeightFromBigIntBlockNumber(latest, crit.FromBlock)
	}
	if crit.ToBlock != nil {
		end = getHeightFromBigIntBlockNumber(latest, crit.ToBlock)
		// only if fromBlock is not specified, default it to end block
		if crit.FromBlock == nil && begin > end {
			begin = end
		}
	}
	if lastToHeight > begin {
		begin = lastToHeight
	}
	if !applyOpenEndedLogLimit && f.filterConfig.maxBlock > 0 && end >= (begin+f.filterConfig.maxBlock) {
		end = begin + f.filterConfig.maxBlock - 1
	}
	// begin should always be <= end block at this point
	if begin > end {
		return nil, 0, fmt.Errorf("fromBlock %d is after toBlock %d", begin, end)
	}

	var blockHeights []int64
	if len(crit.Addresses) != 0 || len(crit.Topics) != 0 {
		blockHeights = f.FindBlocksByBloom(begin, end, bloomIndexes)
	} else {
		blockHeights = make([]int64, end-begin+1)
		for i := range blockHeights {
			blockHeights[i] = begin + int64(i)
		}
	}

	wg := sync.WaitGroup{}
	res = []*ethtypes.Log{}
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("%s", e)
		}
	}()
	slots := make([][]*ethtypes.Log, len(blockHeights))
	for i, height := range blockHeights {
		wg.Add(1)
		h := height
		i := i
		go func() {
			defer wg.Done()
			block, err := blockByNumberWithRetry(ctx, f.tmClient, &h, 1)
			if err != nil {
				panic(err)
			}
			slots[i] = f.GetLogsForBlock(ctx, block, crit, bloomIndexes)
		}()
	}
	wg.Wait()
	for _, logs := range slots {
		res = append(res, logs...)
	}
	if applyOpenEndedLogLimit && int64(len(res)) >= f.filterConfig.maxLog {
		res = res[:int(f.filterConfig.maxLog)]
	}

	return res, end, nil
}

func (f *LogFetcher) GetLogsForBlock(ctx context.Context, block *coretypes.ResultBlock, crit filters.FilterCriteria, filters [][]bloomIndexes) []*ethtypes.Log {
	possibleLogs := f.FindLogsByBloom(block, filters)
	matchedLogs := utils.Filter(possibleLogs, func(l *ethtypes.Log) bool { return f.IsLogExactMatch(l, crit) })
	for _, l := range matchedLogs {
		l.BlockHash = common.Hash(block.BlockID.Hash)
	}
	return matchedLogs
}

func (f *LogFetcher) FindBlocksByBloom(begin, end int64, filters [][]bloomIndexes) (res []int64) {
	numWorkers := int(math.Min(100, float64(end-begin)))
	var wg sync.WaitGroup
	tasks := make(chan int64, end-begin+1)
	results := make(chan int64, end-begin+1)

	// Worker function
	worker := func() {
		defer wg.Done()
		for height := range tasks {
			if height == 0 {
				continue // Skip genesis height
			}
			ctx := f.ctxProvider(height)
			blockBloom := f.k.GetBlockBloom(ctx)
			if MatchFilters(blockBloom, filters) {
				results <- height
			}
		}
	}

	// Start workers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go worker()
	}

	// Send tasks
	go func() {
		for height := begin; height <= end; height++ {
			tasks <- height
		}
		close(tasks) // Close the tasks channel to signal workers
	}()

	// Collect results
	go func() {
		wg.Wait()
		close(results) // Close the results channel after workers finish
	}()

	// Aggregate results into the final slice
	for result := range results {
		res = append(res, result)
	}

	// Sorting in ascending order
	sort.Slice(res, func(i, j int) bool {
		return res[i] < res[j]
	})

	return
}

func (f *LogFetcher) FindLogsByBloom(block *coretypes.ResultBlock, filters [][]bloomIndexes) (res []*ethtypes.Log) {
	ctx := f.ctxProvider(LatestCtxHeight)
	txHashes := f.k.GetTxHashesOnHeight(ctx, block.Block.Height)
	for _, hash := range txHashes {
		receipt, err := f.k.GetReceipt(ctx, hash)
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("FindLogsByBloom: unable to find receipt for hash %s", hash.Hex()))
			continue
		}
		if !f.includeSyntheticReceipts && (receipt.TxType == ShellEVMTxType || receipt.EffectiveGasPrice == 0) {
			continue
		}
		if len(receipt.LogsBloom) > 0 && MatchFilters(ethtypes.Bloom(receipt.LogsBloom), filters) {
			res = append(res, keeper.GetLogsForTx(receipt)...)
		}
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
