package evmrpc

import (
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
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/coretypes"
	tmtypes "github.com/tendermint/tendermint/types"
)

const TxSearchPerPage = 10

const MaxNumOfWorkers = 500

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

func NewFilterAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, txConfig client.TxConfig, filterConfig *FilterConfig, connectionType ConnectionType, namespace string) *FilterAPI {
	logFetcher := &LogFetcher{tmClient: tmClient, k: k, ctxProvider: ctxProvider, txConfig: txConfig, filterConfig: filterConfig, includeSyntheticReceipts: shouldIncludeSynthetic(namespace)}
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
	txConfig                 client.TxConfig
	ctxProvider              func(int64) sdk.Context
	filterConfig             *FilterConfig
	includeSyntheticReceipts bool
}

func (f *LogFetcher) GetLogsByFilters(ctx context.Context, crit filters.FilterCriteria, lastToHeight int64) (res []*ethtypes.Log, end int64, err error) {
	bloomIndexes := EncodeFilters(crit.Addresses, crit.Topics)
	blocks, end, applyOpenEndedLogLimit, err := f.fetchBlocksByCrit(ctx, crit, lastToHeight, bloomIndexes)
	if err != nil {
		return nil, 0, err
	}
	runner := NewParallelRunner(MaxNumOfWorkers, 1000)
	resultsChan := make(chan *ethtypes.Log, 1000)
	res = []*ethtypes.Log{}
	for block := range blocks {
		b := block
		runner.Queue <- func() {
			matchedLogs := f.GetLogsForBlock(b, crit, bloomIndexes)
			for _, log := range matchedLogs {
				resultsChan <- log
			}
		}
	}
	close(runner.Queue)
	go func() {
		runner.Done.Wait()
		close(resultsChan)
	}()

	// Aggregate results into the final slice
	for result := range resultsChan {
		res = append(res, result)
	}

	// Sorting res in ascending order
	sort.Slice(res, func(i, j int) bool {
		return res[i].BlockNumber < res[j].BlockNumber
	})

	// Apply rate limit
	if applyOpenEndedLogLimit && f.filterConfig.maxLog > 0 && int64(len(res)) > f.filterConfig.maxLog {
		return nil, 0, fmt.Errorf("requested range has %d logs which is more than the maximum of %d", len(res), f.filterConfig.maxLog)
	}

	return res, end, err
}

func (f *LogFetcher) GetLogsForBlock(block *coretypes.ResultBlock, crit filters.FilterCriteria, filters [][]bloomIndexes) []*ethtypes.Log {
	possibleLogs := f.FindLogsByBloom(block, filters)
	matchedLogs := utils.Filter(possibleLogs, func(l *ethtypes.Log) bool { return f.IsLogExactMatch(l, crit) })
	for _, l := range matchedLogs {
		l.BlockHash = common.BytesToHash(block.BlockID.Hash)
	}
	return matchedLogs
}

func (f *LogFetcher) FindLogsByBloom(block *coretypes.ResultBlock, filters [][]bloomIndexes) (res []*ethtypes.Log) {
	ctx := f.ctxProvider(LatestCtxHeight)
	totalLogs := uint(0)
	for _, hash := range getTxHashesFromBlock(block, f.txConfig, f.includeSyntheticReceipts) {
		receipt, err := f.k.GetReceipt(ctx, hash)
		if err != nil {
			// ignore the error if receipt is not found when includeSyntheticReceipts is true
			if !f.includeSyntheticReceipts {
				ctx.Logger().Error(fmt.Sprintf("FindLogsByBloom: unable to find receipt for hash %s", hash.Hex()))
			}
			continue
		}
		if !f.includeSyntheticReceipts && (receipt.TxType == ShellEVMTxType || receipt.EffectiveGasPrice == 0) {
			continue
		}
		if len(receipt.LogsBloom) > 0 && MatchFilters(ethtypes.Bloom(receipt.LogsBloom), filters) {
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

func (f *LogFetcher) fetchBlocksByCrit(ctx context.Context, crit filters.FilterCriteria, lastToHeight int64, bloomIndexes [][]bloomIndexes) (chan *coretypes.ResultBlock, int64, bool, error) {
	if crit.BlockHash != nil {
		block, err := blockByHashWithRetry(ctx, f.tmClient, crit.BlockHash[:], 1)
		if err != nil {
			return nil, 0, false, err
		}
		res := make(chan *coretypes.ResultBlock, 1)
		defer close(res)
		res <- block
		return res, 0, false, err
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
		return nil, 0, false, fmt.Errorf("a maximum of %d blocks worth of logs may be requested at a time", f.filterConfig.maxBlock)
	}
	// begin should always be <= end block at this point
	if begin > end {
		return nil, 0, false, fmt.Errorf("fromBlock %d is after toBlock %d", begin, end)
	}
	res := make(chan *coretypes.ResultBlock, end-begin+1)
	defer close(res)
	runner := NewParallelRunner(MaxNumOfWorkers, int(end-begin+1))
	defer runner.Done.Wait()
	defer close(runner.Queue)
	for height := begin; height <= end; height++ {
		h := height
		runner.Queue <- func() {
			if h == 0 {
				return
			}
			if len(crit.Addresses) != 0 || len(crit.Topics) != 0 {
				providerCtx := f.ctxProvider(h)
				blockBloom := f.k.GetBlockBloom(providerCtx)
				if !MatchFilters(blockBloom, bloomIndexes) {
					return
				}
			}
			block, err := blockByNumberWithRetry(ctx, f.tmClient, &h, 1)
			if err != nil {
				panic(err)
			}
			res <- block
		}
	}
	return res, end, applyOpenEndedLogLimit, nil
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
