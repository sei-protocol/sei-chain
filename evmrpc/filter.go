package evmrpc

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/types"
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

type offset struct {
	nextPage    int
	itemsToRead int // if a previous query ends on page 3 with only 8 items, then itemsToRead is set to (10-8)=2 so that the next query will not skip the two remaining
}

type filter struct {
	typ      FilterType
	fc       filters.FilterCriteria
	deadline *time.Timer

	// BlocksSubscription
	blockCursor string

	// LogsSubscription
	logsCursors map[common.Address]offset
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

type EventItemDataWrapper struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
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
		logsCursors: make(map[common.Address]offset),
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

	noCursors := make(map[common.Address]offset)
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
		make(map[common.Address]offset),
	)
	return logs, err
}

// pulls logs from tendermint client over multiple addresses.
func (a *FilterAPI) getLogsOverAddresses(
	ctx context.Context,
	crit filters.FilterCriteria,
	cursors map[common.Address]offset,
) ([]*ethtypes.Log, map[common.Address]offset, error) {
	res := make([]*ethtypes.Log, 0)
	if len(crit.Addresses) == 0 {
		crit.Addresses = append(crit.Addresses, common.Address{})
	}
	updatedAddrToCursor := make(map[common.Address]offset)
	for _, address := range crit.Addresses {
		var cursor offset
		if _, ok := cursors[address]; !ok {
			cursor = offset{nextPage: 1, itemsToRead: TxSearchPerPage}
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

// pulls logs from tendermint client for a single address.
func (a *FilterAPI) getLogs(
	ctx context.Context,
	blockHash *common.Hash,
	fromBlock *big.Int,
	toBlock *big.Int,
	address common.Address,
	topics [][]common.Hash,
	cursor offset,
) ([]*ethtypes.Log, offset, error) {
	builtQuery := getBuiltQuery(
		NewTxSearchQueryBuilder(),
		blockHash,
		fromBlock,
		toBlock,
		address,
		topics,
	).Build()
	page := cursor.nextPage
	perPage := TxSearchPerPage
	itemsToRead := cursor.itemsToRead
	logs := []*ethtypes.Log{}
	for {
		res, err := a.tmClient.TxSearch(ctx, builtQuery, false, &page, &perPage, "asc")
		if err != nil {
			return nil, offset{}, err
		}

		txs := res.Txs
		if perPage-itemsToRead < len(txs) {
			txs = txs[perPage-itemsToRead:]
		} else {
			txs = []*coretypes.ResultTx{}
		}
		for _, tx := range txs {
			for _, event := range tx.TxResult.Events {
				// needs to do filtering again because the response contains all events
				// of a transaction that contains any matching event.
				// Once we rebase tendermint to a newer version that supports `match_events`
				// keyword we can skip this step.
				if event.Type != types.EventTypeEVMLog {
					continue
				}
				contractMatched := address == common.Address{}
				topicsMatched := len(topics) == 0
				for _, attr := range event.Attributes {
					if string(attr.Key) == types.AttributeTypeContractAddress {
						if common.HexToAddress(string(attr.Value)) == address {
							contractMatched = true
						}
					} else if string(attr.Key) == types.AttributeTypeTopics {
						if matchTopics(topics, utils.Map(strings.Split(string(attr.Value), ","), common.HexToHash)) {
							topicsMatched = true
						}
					}
				}
				if !contractMatched || !topicsMatched {
					continue
				}
				ethLog, err := encodeEventToLog(event)
				if err != nil {
					return nil, offset{}, err
				}
				logs = append(logs, ethLog)
			}
		}

		if res.TotalCount < page*perPage {
			return logs, offset{itemsToRead: page*perPage - res.TotalCount, nextPage: page}, nil
		} else if res.TotalCount == page*perPage {
			return logs, offset{itemsToRead: perPage, nextPage: page + 1}, nil
		}
		page++
		itemsToRead = perPage
	}
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

func getBuiltQuery(
	q *QueryBuilder,
	blockHash *common.Hash,
	fromBlock *big.Int,
	toBlock *big.Int,
	address common.Address,
	topics [][]common.Hash,
) *QueryBuilder {
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
