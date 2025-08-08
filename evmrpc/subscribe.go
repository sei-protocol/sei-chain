package evmrpc

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/coretypes"
	tmtypes "github.com/tendermint/tendermint/types"
)

const SleepInterval = 5 * time.Second
const NewHeadsListenerBuffer = 10

type SubscriptionAPI struct {
	tmClient            rpcclient.Client
	subscriptionManager *SubscriptionManager
	subscriptonConfig   *SubscriptionConfig

	logFetcher          *LogFetcher
	newHeadListenersMtx *sync.RWMutex
	newHeadListeners    map[rpc.ID]chan map[string]interface{}
	connectionType      ConnectionType
}

type SubscriptionConfig struct {
	subscriptionCapacity int
	newHeadLimit         uint64
}

func NewSubscriptionAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, logFetcher *LogFetcher, subscriptionConfig *SubscriptionConfig, filterConfig *FilterConfig, connectionType ConnectionType) *SubscriptionAPI {
	logFetcher.filterConfig = filterConfig
	api := &SubscriptionAPI{
		tmClient:            tmClient,
		subscriptionManager: NewSubscriptionManager(tmClient),
		subscriptonConfig:   subscriptionConfig,
		logFetcher:          logFetcher,
		newHeadListenersMtx: &sync.RWMutex{},
		newHeadListeners:    make(map[rpc.ID]chan map[string]interface{}),
		connectionType:      connectionType,
	}
	id, subCh, err := api.subscriptionManager.Subscribe(context.Background(), NewHeadQueryBuilder(), api.subscriptonConfig.subscriptionCapacity)
	if err != nil {
		panic(err)
	}
	go func() {
		defer recoverAndLog()
		defer func() {
			_ = api.subscriptionManager.Unsubscribe(context.Background(), id)
		}()
		for {
			res := <-subCh
			eventHeader := res.Data.(tmtypes.EventDataNewBlockHeader)
			ctx := ctxProvider(eventHeader.Header.Height)
			baseFeePerGas := k.GetNextBaseFeePerGas(ctx).TruncateInt().BigInt()
			ethHeader, err := encodeTmHeader(eventHeader, baseFeePerGas)
			if err != nil {
				fmt.Printf("error encoding new head event %#v due to %s\n", res.Data, err)
				continue
			}
			api.newHeadListenersMtx.Lock()
			toDelete := []rpc.ID{}
			for id, c := range api.newHeadListeners {
				if !handleListener(c, ethHeader) {
					toDelete = append(toDelete, id)
				}
			}
			for _, id := range toDelete {
				delete(api.newHeadListeners, id)
			}
			api.newHeadListenersMtx.Unlock()
		}
	}()
	return api
}

func handleListener(c chan map[string]interface{}, ethHeader map[string]interface{}) bool {
	// if the channel is already closed, sending to it/closing it will panic
	defer func() { _ = recover() }()
	select {
	case c <- ethHeader:
		return true
	default:
		// this path is hit when the buffer is full, meaning that the subscriber is not consuming
		// fast enough
		close(c)
		return false
	}
}

func (a *SubscriptionAPI) NewHeads(ctx context.Context) (s *rpc.Subscription, err error) {
	defer recordMetrics("eth_newHeads", a.connectionType, time.Now(), err == nil)
	notifier, supported := rpc.NotifierFromContext(ctx)
	if !supported {
		return &rpc.Subscription{}, rpc.ErrNotificationsUnsupported
	}

	rpcSub := notifier.CreateSubscription()
	listener := make(chan map[string]interface{}, NewHeadsListenerBuffer)
	a.newHeadListenersMtx.Lock()
	defer a.newHeadListenersMtx.Unlock()
	if uint64(len(a.newHeadListeners)) >= a.subscriptonConfig.newHeadLimit {
		return nil, errors.New("no new subscription can be created")
	}
	a.newHeadListeners[rpcSub.ID] = listener

	go func() {
		defer recoverAndLog()
	OUTER:
		for {
			select {
			case res, ok := <-listener:
				if err := notifier.Notify(rpcSub.ID, res); err != nil {
					break OUTER
				}
				if !ok {
					break OUTER
				}
			case <-rpcSub.Err():
				break OUTER
			}
		}
		a.newHeadListenersMtx.Lock()
		defer a.newHeadListenersMtx.Unlock()
		delete(a.newHeadListeners, rpcSub.ID)
		defer func() { _ = recover() }() // might have already been closed
		close(listener)
	}()

	return rpcSub, nil
}

func (a *SubscriptionAPI) Logs(ctx context.Context, filter *filters.FilterCriteria) (s *rpc.Subscription, err error) {
	defer recordMetrics("eth_logs", a.connectionType, time.Now(), err == nil)
	notifier, supported := rpc.NotifierFromContext(ctx)
	if !supported {
		return &rpc.Subscription{}, rpc.ErrNotificationsUnsupported
	}
	// create empty filter if filter does not exist
	if filter == nil {
		filter = &filters.FilterCriteria{}
	}
	// when fromBlock is 0 and toBlock is latest, adjust the filter
	// to unbounded filter
	if filter.FromBlock != nil && filter.FromBlock.Int64() == 0 &&
		filter.ToBlock != nil && filter.ToBlock.Int64() < 0 {
		latest := big.NewInt(a.logFetcher.ctxProvider(LatestCtxHeight).BlockHeight())
		unboundedFilter := &filters.FilterCriteria{
			FromBlock: latest, // set to latest block height
			ToBlock:   nil,    // set to nil to continue listening
			Addresses: filter.Addresses,
			Topics:    filter.Topics,
		}
		filter = unboundedFilter
	}

	rpcSub := notifier.CreateSubscription()

	if filter.BlockHash != nil {
		go func() {
			defer recoverAndLog()
			logs, _, err := a.logFetcher.GetLogsByFilters(ctx, *filter, 0)
			if err != nil {
				_ = notifier.Notify(rpcSub.ID, err)
				return
			}
			for _, log := range logs {
				if err := notifier.Notify(rpcSub.ID, log); err != nil {
					return
				}
			}
		}()
		return rpcSub, nil
	}

	go func() {
		defer recoverAndLog()
		begin := int64(0)
		for {
			logs, lastToHeight, err := a.logFetcher.GetLogsByFilters(ctx, *filter, begin)
			if err != nil {
				_ = notifier.Notify(rpcSub.ID, err)
				return
			}
			for _, log := range logs {
				if err := notifier.Notify(rpcSub.ID, log); err != nil {
					return
				}
			}
			if filter.ToBlock != nil && lastToHeight >= filter.ToBlock.Int64() {
				return
			}
			begin = lastToHeight
			filter.FromBlock = big.NewInt(lastToHeight + 1)
			time.Sleep(SleepInterval)
		}
	}()

	return rpcSub, nil
}

const SubscriberPrefix = "evm.rpc."

type SubscriberID uint64

type SubInfo struct {
	Query          string
	SubscriptionCh <-chan coretypes.ResultEvent
}

type SubscriptionManager struct {
	subMu            sync.Mutex
	NextID           SubscriberID
	SubscriptionInfo map[SubscriberID]SubInfo
	tmClient         rpcclient.Client
}

func NewSubscriptionManager(tmClient rpcclient.Client) *SubscriptionManager {
	return &SubscriptionManager{
		subMu:            sync.Mutex{},
		NextID:           1,
		SubscriptionInfo: make(map[SubscriberID]SubInfo),
		tmClient:         tmClient,
	}
}

func (s *SubscriptionManager) Subscribe(ctx context.Context, q *QueryBuilder, limit int) (SubscriberID, <-chan coretypes.ResultEvent, error) {
	query := q.Build()
	s.subMu.Lock()
	defer s.subMu.Unlock()
	id := s.NextID
	// ignore deprecation here since the new endpoint does not support polling
	//nolint:staticcheck
	res, err := s.tmClient.Subscribe(ctx, fmt.Sprintf("%s%d", SubscriberPrefix, id), query, limit)
	if err != nil {
		return 0, nil, err
	}
	s.SubscriptionInfo[id] = SubInfo{Query: query, SubscriptionCh: res}
	s.NextID++
	return id, res, nil
}

func (s *SubscriptionManager) Unsubscribe(ctx context.Context, id SubscriberID) error {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	// ignore deprecation here since the new endpoint does not support polling
	//nolint:staticcheck
	err := s.tmClient.Unsubscribe(ctx, SubscriberPrefix, s.SubscriptionInfo[id].Query)
	if err != nil {
		return err
	}
	delete(s.SubscriptionInfo, id)
	return nil
}

func encodeTmHeader(
	header tmtypes.EventDataNewBlockHeader,
	baseFee *big.Int,
) (map[string]interface{}, error) {
	blockHash := common.HexToHash(header.Header.Hash().String())
	number := big.NewInt(header.Header.Height)
	miner := common.HexToAddress(header.Header.ProposerAddress.String())
	gasWanted := int64(0)
	lastHash := common.HexToHash(header.Header.LastBlockID.Hash.String())
	resultHash := common.HexToHash(header.Header.LastResultsHash.String())
	appHash := common.HexToHash(header.Header.AppHash.String())
	txHash := common.HexToHash(header.Header.DataHash.String())
	for _, txRes := range header.ResultFinalizeBlock.TxResults {
		gasWanted += txRes.GasUsed
	}
	gasLimit := uint64(header.ResultFinalizeBlock.ConsensusParamUpdates.Block.MaxGas)
	result := map[string]interface{}{
		"difficulty":            (*hexutil.Big)(utils.Big0), // inapplicable to Sei
		"extraData":             hexutil.Bytes{},            // inapplicable to Sei
		"gasLimit":              hexutil.Uint64(gasLimit),
		"gasUsed":               hexutil.Uint64(gasWanted),
		"logsBloom":             ethtypes.Bloom{}, // inapplicable to Sei
		"miner":                 miner,
		"nonce":                 ethtypes.BlockNonce{}, // inapplicable to Sei
		"number":                (*hexutil.Big)(number),
		"parentHash":            lastHash,
		"receiptsRoot":          resultHash,
		"sha3Uncles":            common.Hash{}, // inapplicable to Sei
		"stateRoot":             appHash,
		"timestamp":             hexutil.Uint64(header.Header.Time.Unix()),
		"transactionsRoot":      txHash,
		"mixHash":               common.Hash{},     // inapplicable to Sei
		"excessBlobGas":         hexutil.Uint64(0), // inapplicable to Sei
		"parentBeaconBlockRoot": common.Hash{},     // inapplicable to Sei
		"hash":                  blockHash,
		"withdrawlsRoot":        common.Hash{}, // inapplicable to Sei
		"baseFeePerGas":         (*hexutil.Big)(baseFee),
		"withdrawalsRoot":       common.Hash{},     // inapplicable to Sei
		"blobGasUsed":           hexutil.Uint64(0), // inapplicable to Sei
	}
	return result, nil
}
