package evmrpc

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
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

func (a *SubscriptionAPI) Subscribe2(ctx context.Context, eventName string) (uint64, error) {
	// Depending on subType, set up the appropriate query
	switch eventName {
	case "newHeads":
		fmt.Println("newHeads")
	case "logs":
		fmt.Println("logs")
	case "newPendingTransactions":
		fmt.Println("newPendingTransactions")
	default:
		return 0, fmt.Errorf("unsupported subscription type: %s", eventName)
	}
	return 0, nil
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
