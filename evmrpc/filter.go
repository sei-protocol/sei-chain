package evmrpc

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	abci "github.com/tendermint/tendermint/abci/types"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/coretypes"
	tmtypes "github.com/tendermint/tendermint/types"
)

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
	logsCursors map[common.Address]string
}

type FilterAPI struct {
	tmClient     rpcclient.Client
	nextFilterID uint64
	filtersMu    sync.Mutex
	filters      map[uint64]filter
	filterConfig *FilterConfig
}

type FilterConfig struct {
	timeout time.Duration
}

func NewFilterAPI(tmClient rpcclient.Client, filterConfig *FilterConfig) *FilterAPI {
	filters := make(map[uint64]filter)
	api := &FilterAPI{
		tmClient:     tmClient,
		nextFilterID: 1,
		filtersMu:    sync.Mutex{},
		filters:      filters,
		filterConfig: filterConfig,
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
) (*uint64, error) {
	a.filtersMu.Lock()
	defer a.filtersMu.Unlock()
	curFilterID := a.nextFilterID
	a.nextFilterID++
	a.filters[curFilterID] = filter{
		typ:         LogsSubscription,
		fc:          crit,
		deadline:    time.NewTimer(a.filterConfig.timeout),
		logsCursors: make(map[common.Address]string),
	}
	return &curFilterID, nil
}

func (a *FilterAPI) NewBlockFilter(
	_ context.Context,
) (*uint64, error) {
	a.filtersMu.Lock()
	defer a.filtersMu.Unlock()
	curFilterID := a.nextFilterID
	a.nextFilterID++
	a.filters[curFilterID] = filter{
		typ:         BlocksSubscription,
		deadline:    time.NewTimer(a.filterConfig.timeout),
		blockCursor: "",
	}
	return &curFilterID, nil
}

func (a *FilterAPI) GetFilterChanges(
	ctx context.Context,
	filterID uint64,
) (interface{}, error) {
	a.filtersMu.Lock()
	filter, ok := a.filters[filterID]
	a.filtersMu.Unlock()
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
		a.filtersMu.Lock()
		updatedFilter := a.filters[filterID]
		updatedFilter.blockCursor = cursor
		a.filters[filterID] = updatedFilter
		a.filtersMu.Unlock()
		return hashes, nil
	case LogsSubscription:
		res, cursors, err := a.getLogsOverAddresses(ctx, filter.fc, filter.logsCursors)
		if err != nil {
			return nil, err
		}
		a.filtersMu.Lock()
		updatedFilter := a.filters[filterID]
		updatedFilter.logsCursors = cursors
		a.filters[filterID] = updatedFilter
		a.filtersMu.Unlock()
		return res, nil
	default:
		return nil, errors.New("unknown filter type")
	}
}

func (a *FilterAPI) GetFilterLogs(
	ctx context.Context,
	filterID uint64,
) ([]*ethtypes.Log, error) {
	a.filtersMu.Lock()
	filter, ok := a.filters[filterID]
	a.filtersMu.Unlock()
	if !ok {
		return nil, errors.New("filter does not exist")
	}

	if !filter.deadline.Stop() {
		// timer expired but filter is not yet removed in timeout loop
		// receive timer value and reset timer
		<-filter.deadline.C
	}
	filter.deadline.Reset(a.filterConfig.timeout)

	noCursors := make(map[common.Address]string)
	res, cursors, err := a.getLogsOverAddresses(ctx, filter.fc, noCursors)
	if err != nil {
		return nil, err
	}
	a.filtersMu.Lock()
	updatedFilter := a.filters[filterID]
	updatedFilter.logsCursors = cursors
	a.filters[filterID] = updatedFilter
	a.filtersMu.Unlock()
	return res, nil
}

func (a *FilterAPI) GetLogs(
	ctx context.Context,
	crit filters.FilterCriteria,
) ([]*ethtypes.Log, error) {
	logs, _, err := a.getLogsOverAddresses(
		ctx,
		crit,
		make(map[common.Address]string),
	)
	return logs, err
}

// pulls logs from tendermint client over multiple addresses.
func (a *FilterAPI) getLogsOverAddresses(
	ctx context.Context,
	crit filters.FilterCriteria,
	cursors map[common.Address]string,
) ([]*ethtypes.Log, map[common.Address]string, error) {
	res := make([]*ethtypes.Log, 0)
	if len(crit.Addresses) == 0 {
		crit.Addresses = append(crit.Addresses, common.Address{})
	}
	updatedAddrToCursor := make(map[common.Address]string)
	for _, address := range crit.Addresses {
		var cursor string
		if _, ok := cursors[address]; !ok {
			cursor = ""
		} else {
			cursor = cursors[address]
		}
		resAddr, cursor, err := a.getLogs(
			ctx,
			crit.BlockHash,
			crit.FromBlock,
			crit.ToBlock,
			address,
			crit.Topics,
			cursor,
		)
		if err != nil {
			return nil, nil, err
		}
		res = append(res, resAddr...)
		updatedAddrToCursor[address] = cursor
	}
	return res, updatedAddrToCursor, nil
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
			block := tmtypes.EventDataNewBlock{}
			err := json.Unmarshal(item.Data, &block)
			if err != nil {
				return nil, "", err
			}
			headers = append(headers, common.BytesToHash(block.Block.Hash()))
		}
	}
	return headers, cursor, nil
}

// pulls logs from tendermint client for a single address.
func (a *FilterAPI) getLogs(
	ctx context.Context,
	blockHash *common.Hash,
	fromBlock *big.Int,
	toBlock *big.Int,
	address common.Address,
	topics [][]common.Hash,
	cursor string,
) ([]*ethtypes.Log, string, error) {
	builtQuery := getBuiltQuery(blockHash, fromBlock, toBlock, address, topics).Build()
	hasMore := true
	logs := []*ethtypes.Log{}
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
		for _, log := range res.Items {
			abciEvent := abci.Event{}
			err := json.Unmarshal(log.Data, &abciEvent)
			if err != nil {
				return nil, "", err
			}
			ethLog, err := encodeEventToLog(abciEvent)
			if err != nil {
				return nil, "", err
			}
			logs = append(logs, ethLog)
		}
	}
	return logs, cursor, nil
}

func (a *FilterAPI) UninstallFilter(
	_ context.Context,
	filterID uint64,
) bool {
	a.filtersMu.Lock()
	defer a.filtersMu.Unlock()
	_, found := a.filters[filterID]
	if !found {
		return false
	}
	delete(a.filters, filterID)
	return true
}

func getBuiltQuery(
	blockHash *common.Hash,
	fromBlock *big.Int,
	toBlock *big.Int,
	address common.Address,
	topics [][]common.Hash,
) *QueryBuilder {
	q := NewTxQueryBuilder()
	if blockHash != nil {
		q = q.FilterBlockHash(blockHash.Hex())
	}
	if fromBlock != nil {
		q = q.FilterBlockNumberStart(fromBlock.Int64())
	}
	if toBlock != nil {
		q = q.FilterBlockNumberEnd(toBlock.Int64())
	}
	if (address != common.Address{}) {
		q = q.FilterContractAddress(address.Hex())
	}
	if len(topics) > 0 {
		topicsStrs := make([][]string, len(topics))
		for i, topic := range topics {
			topicsStrs[i] = make([]string, len(topic))
			for j, t := range topic {
				topicsStrs[i][j] = t.Hex()
			}
		}
		q = q.FilterTopics(topicsStrs)
	}
	return q
}
