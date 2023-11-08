package evmrpc

import (
	"context"
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/coretypes"
)

type SubscriptionAPI struct {
	tmClient    rpcclient.Client
	keeper      *keeper.Keeper
	ctxProvider func(int64) sdk.Context
	txDecoder   sdk.TxDecoder
}

func NewSubscriptionAPI(tmClient rpcclient.Client, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, txDecoder sdk.TxDecoder) *SubscriptionAPI {
	return &SubscriptionAPI{tmClient: tmClient, keeper: k, ctxProvider: ctxProvider, txDecoder: txDecoder}
}

// TODO: need to fix this name, but when I change it to Subscribe, the ws client doesn't work
func (a *SubscriptionAPI) Subscribe(ctx context.Context, eventName string) (*rpc.Subscription, error) {
	notifier, supported := rpc.NotifierFromContext(ctx)
	if !supported {
		return &rpc.Subscription{}, rpc.ErrNotificationsUnsupported
	}

	rpcSub := notifier.CreateSubscription()

	switch eventName {
	case "newHeads":
		notifier.Notify(rpcSub.ID, 1)
		fmt.Println("newHeads")
	case "logs":
		fmt.Println("logs")
	case "newPendingTransactions":
		return nil, errors.New("newPendingTransactions not supported")
	default:
		return nil, fmt.Errorf("unsupported subscription type: %s", eventName)
	}
	return rpcSub, nil
}

const SubscriberPrefix = "evm.rpc."

type SubscriberID uint64

type SubscriptionManager struct {
	NextID        SubscriberID
	Subscriptions map[SubscriberID]<-chan coretypes.ResultEvent

	tmClient rpcclient.Client
}

func NewSubscriptionManager(tmClient rpcclient.Client) *SubscriptionManager {
	return &SubscriptionManager{
		NextID:        1,
		Subscriptions: map[SubscriberID]<-chan coretypes.ResultEvent{},
		tmClient:      tmClient,
	}
}

func (s *SubscriptionManager) Subscribe(ctx context.Context, q *QueryBuilder, limit int) (SubscriberID, error) {
	id := s.NextID
	// ignore deprecation here since the new endpoint does not support polling
	//nolint:staticcheck
	res, err := s.tmClient.Subscribe(ctx, fmt.Sprintf("%s%d", SubscriberPrefix, id), q.Build(), limit)
	if err != nil {
		return 0, err
	}
	s.Subscriptions[id] = res
	s.NextID++
	return id, nil
}
