package evmrpc

import (
	"context"
	"errors"
	"fmt"
	"sync"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/coretypes"
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

func (a *SubscriptionAPI) Subscribe(ctx context.Context, eventName string) (*rpc.Subscription, error) {
	notifier, supported := rpc.NotifierFromContext(ctx)
	if !supported {
		return &rpc.Subscription{}, rpc.ErrNotificationsUnsupported
	}

	rpcSub := notifier.CreateSubscription()

	switch eventName {
	case "newHeads":
		fmt.Println("newHeads")
		a.handleNewHeadsSubscription(notifier, rpcSub)
	case "logs":
		fmt.Println("logs")
	case "newPendingTransactions":
		return nil, errors.New("newPendingTransactions not supported")
	default:
		return nil, fmt.Errorf("unsupported subscription type: %s", eventName)
	}
	return rpcSub, nil
}

func (a *SubscriptionAPI) handleNewHeadsSubscription(notifier *rpc.Notifier, rpcSub *rpc.Subscription) {
	notifier.Notify(rpcSub.ID, "new heads")
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

func (s *SubscriptionManager) Subscribe(ctx context.Context, q *QueryBuilder, limit int) (SubscriberID, error) {
	query := q.Build()
	s.subMu.Lock()
	defer s.subMu.Unlock()
	id := s.NextID
	// ignore deprecation here since the new endpoint does not support polling
	//nolint:staticcheck
	res, err := s.tmClient.Subscribe(ctx, fmt.Sprintf("%s%d", SubscriberPrefix, id), query, limit)
	if err != nil {
		return 0, err
	}
	s.SubscriptionInfo[id] = SubInfo{Query: query, SubscriptionCh: res}
	s.NextID++
	return id, nil
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
