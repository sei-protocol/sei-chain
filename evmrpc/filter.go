package evmrpc

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	abci "github.com/tendermint/tendermint/abci/types"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/coretypes"
)

type filter struct {
	fc      filters.FilterCriteria
	cursors map[common.Address]string
}

type FilterAPI struct {
	tmClient     rpcclient.Client
	nextFilterID uint64
	filters      map[uint64]filter
}

func NewFilterAPI(tmClient rpcclient.Client) *FilterAPI {
	filters := make(map[uint64]filter)
	return &FilterAPI{tmClient: tmClient, nextFilterID: 1, filters: filters}
}

func (a *FilterAPI) NewFilter(
	ctx context.Context,
	crit filters.FilterCriteria,
) (*uint64, error) {
	curFilterID := a.nextFilterID
	a.nextFilterID++
	a.filters[curFilterID] = filter{crit, make(map[common.Address]string)}
	return &curFilterID, nil
}

func (a *FilterAPI) GetFilterChanges(
	ctx context.Context,
	filterID uint64,
) ([]*ethtypes.Log, error) {
	filter, ok := a.filters[filterID]
	if !ok {
		return nil, errors.New("filter does not exist")
	}
	res, cursors, err := a.getLogsOverAddresses(ctx, filter.fc, filter.cursors)
	if err != nil {
		return nil, err
	}
	updatedFilter := a.filters[filterID]
	updatedFilter.cursors = cursors
	a.filters[filterID] = updatedFilter
	return res, nil
}

func (a *FilterAPI) GetFilterLogs(
	ctx context.Context,
	filterID uint64,
) ([]*ethtypes.Log, error) {
	filter, ok := a.filters[filterID]
	if !ok {
		return nil, errors.New("filter does not exist")
	}
	noCursors := make(map[common.Address]string)
	res, cursors, err := a.getLogsOverAddresses(ctx, filter.fc, noCursors)
	if err != nil {
		return nil, err
	}
	updatedFilter := a.filters[filterID]
	updatedFilter.cursors = cursors
	a.filters[filterID] = updatedFilter
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
	q := NewQueryBuilder()
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
	hasMore := true
	logs := []*ethtypes.Log{}
	for hasMore {
		res, err := a.tmClient.Events(ctx, &coretypes.RequestEvents{
			Filter: &coretypes.EventFilter{Query: q.Build()},
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
	ctx context.Context,
	filterID uint64,
) (bool, error) {
	_, found := a.filters[filterID]
	if !found {
		return false, nil
	}
	delete(a.filters, filterID)
	return true, nil
}
