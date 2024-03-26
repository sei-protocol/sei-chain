package evmrpc

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/utils"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/coretypes"
	tmtypes "github.com/tendermint/tendermint/types"
)

const SleepInterval = 5 * time.Second

type SubscriptionAPI struct {
	tmClient            rpcclient.Client
	subscriptionManager *SubscriptionManager
	subscriptonConfig   *SubscriptionConfig

	logFetcher *LogFetcher
}

type SubscriptionConfig struct {
	subscriptionCapacity int
}

func NewSubscriptionAPI(tmClient rpcclient.Client, logFetcher *LogFetcher, subscriptionConfig *SubscriptionConfig) *SubscriptionAPI {
	return &SubscriptionAPI{
		tmClient:            tmClient,
		subscriptionManager: NewSubscriptionManager(tmClient),
		subscriptonConfig:   subscriptionConfig,
		logFetcher:          logFetcher,
	}
}

func (a *SubscriptionAPI) NewHeads(ctx context.Context) (*rpc.Subscription, error) {
	notifier, supported := rpc.NotifierFromContext(ctx)
	if !supported {
		return &rpc.Subscription{}, rpc.ErrNotificationsUnsupported
	}

	rpcSub := notifier.CreateSubscription()
	subscriberID, subCh, err := a.subscriptionManager.Subscribe(context.Background(), NewHeadQueryBuilder(), a.subscriptonConfig.subscriptionCapacity)
	if err != nil {
		return nil, err
	}

	go func() {
		defer func() {
			_ = a.subscriptionManager.Unsubscribe(context.Background(), subscriberID)
		}()
		for {
			select {
			case res := <-subCh:
				ethHeader, err := encodeTmHeader(res.Data.(tmtypes.EventDataNewBlockHeader))
				if err != nil {
					return
				}
				err = notifier.Notify(rpcSub.ID, ethHeader)
				if err != nil {
					return
				}
			case <-rpcSub.Err():
				return
			case <-notifier.Closed():
				return
			}
		}
	}()
	return rpcSub, nil
}

func (a *SubscriptionAPI) Logs(ctx context.Context, filter *filters.FilterCriteria) (*rpc.Subscription, error) {
	notifier, supported := rpc.NotifierFromContext(ctx)
	if !supported {
		return &rpc.Subscription{}, rpc.ErrNotificationsUnsupported
	}

	rpcSub := notifier.CreateSubscription()

	if filter.BlockHash != nil {
		go func() {
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
) (map[string]interface{}, error) {
	blockHash := common.HexToHash(header.Header.Hash().String())
	number := big.NewInt(header.Header.Height)
	miner := common.HexToAddress(header.Header.ProposerAddress.String())
	gasLimit, gasWanted := int64(0), int64(0)
	lastHash := common.HexToHash(header.Header.LastBlockID.Hash.String())
	resultHash := common.HexToHash(header.Header.LastResultsHash.String())
	appHash := common.HexToHash(header.Header.AppHash.String())
	txHash := common.HexToHash(header.Header.DataHash.String())
	for _, txRes := range header.ResultFinalizeBlock.TxResults {
		gasLimit += txRes.GasWanted
		gasWanted += txRes.GasUsed
	}
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
		"withdrawlsRoot":        common.Hash{},     // inapplicable to Sei
		"baseFeePerGas":         hexutil.Uint64(0), // inapplicable to Sei
		"withdrawalsRoot":       common.Hash{},     // inapplicable to Sei
		"blobGasUsed":           hexutil.Uint64(0), // inapplicable to Sei
	}
	return result, nil
}
