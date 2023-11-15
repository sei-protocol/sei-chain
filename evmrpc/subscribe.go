package evmrpc

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/coretypes"
	tmtypes "github.com/tendermint/tendermint/types"
)

type SubscriptionAPI struct {
	// TODO: delete any of these if unusued
	tmClient            rpcclient.Client
	keeper              *keeper.Keeper
	ctxProvider         func(int64) sdk.Context
	txDecoder           sdk.TxDecoder
	subscriptionManager *SubscriptionManager
}

func NewSubscriptionAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, txDecoder sdk.TxDecoder) *SubscriptionAPI {
	return &SubscriptionAPI{
		tmClient:            tmClient,
		keeper:              k,
		ctxProvider:         ctxProvider,
		txDecoder:           txDecoder,
		subscriptionManager: NewSubscriptionManager(tmClient),
	}
}

type FilterQuery struct {
	BlockHash *common.Hash     `json:"blockHash"`
	FromBlock *big.Int         `json:"fromBlock"`
	ToBlock   *big.Int         `json:"toBlock"`
	Addresses []common.Address `json:"address"`
	Topics    [][]common.Hash  `json:"topics"`
}

func (a *SubscriptionAPI) Subscribe(ctx context.Context, eventName string, filter *FilterQuery) (*rpc.Subscription, error) {
	notifier, supported := rpc.NotifierFromContext(ctx)
	if !supported {
		return &rpc.Subscription{}, rpc.ErrNotificationsUnsupported
	}

	rpcSub := notifier.CreateSubscription()

	switch eventName {
	case "newHeads":
		subscriberId, subCh, err := a.subscriptionManager.Subscribe(ctx, NewHeadQueryBuilder(), 100)
		if err != nil {
			return nil, err
		}

		// TODO: try to not launch a newHead subscription for every new subscriber maybe (or maybe do)
		// TODO: timeouts!
		go func() {
			for {
				select {
				case res := <-subCh:
					ethHeader, err := encodeTmHeader(a.ctxProvider(LatestCtxHeight), res.Data.(*tmtypes.EventDataNewBlockHeader))
					if err != nil {
						a.subscriptionManager.Unsubscribe(ctx, subscriberId)
						return
					}
					err = notifier.Notify(rpcSub.ID, ethHeader)
					if err != nil {
						a.subscriptionManager.Unsubscribe(ctx, subscriberId)
						return
					}
				case err := <-rpcSub.Err():
					notifier.Notify(rpcSub.ID, err)
					// TODO: try to test these cases
					a.subscriptionManager.Unsubscribe(ctx, subscriberId)
					return
				case <-notifier.Closed():
					a.subscriptionManager.Unsubscribe(ctx, subscriberId)
					return
				}
			}
		}()
		return rpcSub, nil
	case "logs":
		resultEventAllAddrs := make(chan coretypes.ResultEvent)
		for _, address := range filter.Addresses {
			q := getBuiltQuery(filter.BlockHash, filter.FromBlock, filter.ToBlock, address, filter.Topics)
			subscriberID, subCh, err := a.subscriptionManager.Subscribe(ctx, q, 100)
			if err != nil {
				return nil, err
			}
			go func() {
				for {
					select {
					case res := <-subCh:
						resultEventAllAddrs <- res
					case <-rpcSub.Err():
						a.subscriptionManager.Unsubscribe(ctx, subscriberID)
						return
					case <-notifier.Closed():
						a.subscriptionManager.Unsubscribe(ctx, subscriberID)
						return
					}
				}
			}()
		}

		go func() {
			for {
				select {
				case res := <-resultEventAllAddrs:
					for _, abciEvent := range res.Events {
						ethLog, err := encodeEventToLog(abciEvent)
						if err != nil {
							if err == InvalidEventAttributeError {
								continue
							}
							notifier.Notify(rpcSub.ID, err)
						}
						if err != nil {
							notifier.Notify(rpcSub.ID, err)
							return
						}
						err = notifier.Notify(rpcSub.ID, ethLog)
						if err != nil {
							return
						}
					}
				}
			}
		}()
		return rpcSub, nil
	case "newPendingTransactions":
		return nil, errors.New("newPendingTransactions not supported")
	default:
		return nil, fmt.Errorf("unsupported subscription type: %s", eventName)
	}
}

// TODO: figure this out
// func (a *SubscriptionAPI) Unsubscribe(ctx context.Context, id rpc.ID) error {
// 	return a.subscriptionManager.Unsubscribe(ctx, id)
// }

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
	ctx sdk.Context,
	header *tmtypes.EventDataNewBlockHeader,
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
