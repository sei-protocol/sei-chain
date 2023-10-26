package evmrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	abci "github.com/tendermint/tendermint/abci/types"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/coretypes"
)

type filter struct {
	fromBlock rpc.BlockNumber
	toBlock   rpc.BlockNumber
	addresses []common.Address
	topics    []common.Hash

	cursors map[common.Address]string
	// TODO: expiration
}

type FilterAPI struct {
	tmClient     rpcclient.Client
	keeper       *keeper.Keeper
	ctxProvider  func(int64) sdk.Context
	nextFilterId uint64
	filters      map[uint64]filter
}

func NewFilterAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context) *FilterAPI {
	filters := make(map[uint64]filter)
	return &FilterAPI{tmClient: tmClient, keeper: k, ctxProvider: ctxProvider, nextFilterId: 1, filters: filters}
}

func (a *FilterAPI) NewFilter(
	ctx context.Context,
	fromBlock rpc.BlockNumber,
	toBlock rpc.BlockNumber,
	addresses []common.Address,
	topics []string,
) (*uint64, error) {
	err := a.checkFromAndToBlock(ctx, fromBlock, toBlock)
	if err != nil {
		return nil, err
	}
	var topicsRes []common.Hash
	if topics == nil {
		topicsRes = make([]common.Hash, 0)
	} else {
		for _, topic := range topics {
			topicsRes = append(topicsRes, common.HexToHash(topic))
		}
	}
	curFilterId := a.nextFilterId
	a.nextFilterId++
	f := filter{
		fromBlock: fromBlock,
		toBlock:   toBlock,
		addresses: addresses,
		topics:    topicsRes,
	}
	a.filters[curFilterId] = f
	return &curFilterId, nil
}

// TODO: check if this is the same impl as: https://github.com/ethereum/go-ethereum/blob/58ae1df6840e512b263a4fc2e021e1ec5637ca21/ethclient/ethclient.go#L454
func (a *FilterAPI) checkFromAndToBlock(ctx context.Context, fromBlock, toBlock rpc.BlockNumber) error {
	fromBlockPtr, err := getBlockNumber(ctx, a.tmClient, fromBlock)
	if err != nil {
		return err
	}
	toBlockPtr, err := getBlockNumber(ctx, a.tmClient, toBlock)
	if err != nil {
		return err
	}
	if fromBlockPtr == nil && toBlockPtr != nil {
		return errors.New("from block is after to block")
	}
	if toBlockPtr != nil {
		if *fromBlockPtr > *toBlockPtr {
			return errors.New("from block is after to block")
		}
	}
	return nil
}

func (a *FilterAPI) GetFilterChanges(
	ctx context.Context,
	filterId uint64,
) ([]*ethtypes.Log, error) {
	filter, ok := a.filters[filterId]
	if !ok {
		return nil, errors.New("filter does not exist")
	}
	res, cursors, err := a.getLogsOverAddresses(ctx, common.Hash{}, filter.addresses, filter.fromBlock, filter.toBlock, filter.topics, filter.cursors)
	if err != nil {
		return nil, err
	}
	updatedFilter := a.filters[filterId]
	updatedFilter.cursors = cursors
	fmt.Println("cursors set to: ", cursors)
	a.filters[filterId] = updatedFilter
	return res, nil
}

func (a *FilterAPI) GetFilterLogs(
	ctx context.Context,
	filterId uint64,
) ([]*ethtypes.Log, error) {
	filter, ok := a.filters[filterId]
	if !ok {
		return nil, errors.New("filter does not exist")
	}
	noCursors := make(map[common.Address]string)
	res, cursors, err := a.getLogsOverAddresses(ctx, common.Hash{}, filter.addresses, filter.fromBlock, filter.toBlock, filter.topics, noCursors)
	if err != nil {
		return nil, err
	}
	updatedFilter := a.filters[filterId]
	updatedFilter.cursors = cursors
	a.filters[filterId] = updatedFilter
	return res, nil
}

func (a *FilterAPI) GetLogs(
	ctx context.Context,
	blockHash common.Hash,
	addresses []common.Address,
	fromBlock rpc.BlockNumber,
	toBlock rpc.BlockNumber,
	topics []common.Hash,
) ([]*ethtypes.Log, error) {
	noCursors := make(map[common.Address]string)
	logs, _, err := a.getLogsOverAddresses(ctx, blockHash, addresses, fromBlock, toBlock, topics, noCursors)
	return logs, err
}

func (a *FilterAPI) getLogsOverAddresses(
	ctx context.Context,
	blockHash common.Hash,
	addresses []common.Address,
	fromBlock rpc.BlockNumber,
	toBlock rpc.BlockNumber,
	topics []common.Hash,
	cursors map[common.Address]string,
) ([]*ethtypes.Log, map[common.Address]string, error) {
	res := make([]*ethtypes.Log, 0)
	if len(addresses) == 0 {
		addresses = append(addresses, common.Address{})
	}
	updatedAddrToCursor := make(map[common.Address]string)
	for _, address := range addresses {
		var cursor string
		if _, ok := cursors[address]; !ok {
			cursor = ""
		} else {
			cursor = cursors[address]
		}
		resAddr, cursor, err := a.getLogs(ctx, blockHash, address, fromBlock, toBlock, topics, cursor)
		if err != nil {
			return nil, nil, err
		}
		res = append(res, resAddr...)
		updatedAddrToCursor[address] = cursor
	}
	return res, updatedAddrToCursor, nil
}

// TODO: need to handle OR case (union together for multiple addresses and multiple topics)
func (a *FilterAPI) getLogs(
	ctx context.Context,
	blockHash common.Hash,
	address common.Address,
	fromBlock rpc.BlockNumber,
	toBlock rpc.BlockNumber,
	topics []common.Hash,
	cursor string,
) ([]*ethtypes.Log, string, error) {
	fmt.Println("getLogs", blockHash, address, fromBlock, toBlock, topics, cursor)
	// only block hash or block number is supported, not both
	if (blockHash != common.Hash{}) && (fromBlock > 0 || toBlock > 0) {
		return nil, "", errors.New("block hash and block number cannot both be specified")
	}
	err := a.checkFromAndToBlock(ctx, fromBlock, toBlock)
	if err != nil {
		return nil, "", err
	}

	q := NewQueryBuilder()
	if (blockHash != common.Hash{}) {
		q = q.FilterBlockHash(blockHash.Hex())
	}
	if fromBlock > 0 {
		q = q.FilterBlockNumberStart(fromBlock.Int64())
	}
	if toBlock > 0 {
		q = q.FilterBlockNumberEnd(toBlock.Int64())
	}
	if (address != common.Address{}) {
		q = q.FilterContractAddress(address.Hex())
	}
	if len(topics) > 0 {
		topicsStr := make([]string, len(topics))
		for i, topic := range topics {
			topicsStr[i] = topic.Hex()
		}
		q = q.FilterTopics(topicsStr)
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
	filterId uint64,
) (bool, error) {
	_, found := a.filters[filterId]
	if !found {
		return false, nil
	}
	delete(a.filters, filterId)
	return true, nil
}
