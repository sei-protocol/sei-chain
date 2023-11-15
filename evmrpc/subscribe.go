package evmrpc

import (
	"context"
	"fmt"
	"math/big"
	"sync"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/coretypes"
	tmtypes "github.com/tendermint/tendermint/types"
)

type SubscriptionAPI struct {
	tmClient            rpcclient.Client
	ctxProvider         func(int64) sdk.Context
	subscriptionManager *SubscriptionManager
	subscriptonConfig   *SubscriptionConfig
}

type SubscriptionConfig struct {
	subscriptionCapacity int
}

func NewSubscriptionAPI(tmClient rpcclient.Client, ctxProvider func(int64) sdk.Context, subscriptionConfig *SubscriptionConfig) *SubscriptionAPI {
	return &SubscriptionAPI{
		tmClient:            tmClient,
		ctxProvider:         ctxProvider,
		subscriptionManager: NewSubscriptionManager(tmClient),
		subscriptonConfig:   subscriptionConfig,
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

	resultEventAllAddrs := make(chan coretypes.ResultEvent, len(filter.Addresses)*a.subscriptonConfig.subscriptionCapacity)
	for _, address := range filter.Addresses {
		q := getBuiltQuery(filter.BlockHash, filter.FromBlock, filter.ToBlock, address, filter.Topics)
		subscriberID, subCh, err := a.subscriptionManager.Subscribe(context.Background(), q, a.subscriptonConfig.subscriptionCapacity)
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
					resultEventAllAddrs <- res
				case <-rpcSub.Err():
					return
				case <-notifier.Closed():
					return
				}
			}
		}()
	}

	go func() {
		for {
			res := <-resultEventAllAddrs
			for _, abciEvent := range res.Events {
				ethLog, err := encodeEventToLog(abciEvent)
				if err != nil {
					if err == ErrInvalidEventAttribute {
						continue
					}
					err = notifier.Notify(rpcSub.ID, err)
					if err != nil {
						return
					}
				}
				err = notifier.Notify(rpcSub.ID, ethLog)
				if err != nil {
					return
				}
			}
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
	number := big.NewInt(header.Header.Height)
	miner := common.HexToAddress(string(header.Header.ProposerAddress))
	gasLimit, gasWanted := int64(0), int64(0)
	lastHash := common.HexToHash(string(header.Header.LastBlockID.Hash))
	resultHash := common.HexToHash(string(header.Header.LastResultsHash))
	appHash := common.HexToHash(string(header.Header.AppHash))
	txHash := common.HexToHash(string(header.Header.DataHash))
	for _, txRes := range header.ResultFinalizeBlock.TxResults {
		gasLimit += txRes.GasWanted
		gasWanted += txRes.GasUsed
	}
	result := map[string]interface{}{
		"difficulty":       (*hexutil.Big)(big.NewInt(0)), // inapplicable to Sei
		"extraData":        hexutil.Bytes{},               // inapplicable to Sei
		"gasLimit":         hexutil.Uint64(gasLimit),
		"gasUsed":          hexutil.Uint64(gasWanted),
		"logsBloom":        ethtypes.Bloom{}, // inapplicable to Sei
		"miner":            miner,
		"nonce":            ethtypes.BlockNonce{}, // inapplicable to Sei
		"number":           (*hexutil.Big)(number),
		"parentHash":       lastHash,
		"receiptsRoot":     resultHash,
		"sha3Uncles":       common.Hash{}, // inapplicable to Sei
		"stateRoot":        appHash,
		"timestamp":        hexutil.Uint64(header.Header.Time.Unix()),
		"transactionsRoot": txHash,
	}
	return result, nil
}
