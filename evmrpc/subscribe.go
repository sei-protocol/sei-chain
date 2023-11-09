package evmrpc

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

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
		fmt.Println("not supported")
		return &rpc.Subscription{}, rpc.ErrNotificationsUnsupported
	}

	rpcSub := notifier.CreateSubscription()

	switch eventName {
	case "newHeads":
		fmt.Println("newHeads")
		go func() {
			counter := 0
			for {
				notifier.Notify(rpcSub.ID, fmt.Sprint("in eventName switch: new heads", counter))
				counter += 1
				time.Sleep(20 * time.Millisecond)
			}
		}()
		// err := a.handleNewHeadsSubscription(ctx, notifier, rpcSub)
		// if err != nil {
		// 	return nil, err
		// }
		return rpcSub, nil
	case "logs":
		fmt.Println("logs")
	case "newPendingTransactions":
		return nil, errors.New("newPendingTransactions not supported")
	default:
		return nil, fmt.Errorf("unsupported subscription type: %s", eventName)
	}
	return rpcSub, nil
}

func (a *SubscriptionAPI) handleNewHeadsSubscription(ctx context.Context, notifier *rpc.Notifier, rpcSub *rpc.Subscription) error {
	notifier.Notify(rpcSub.ID, "new heads")
	subscriberId, err := a.subscriptionManager.Subscribe(ctx, NewHeadQueryBuilder(), 10)
	if err != nil {
		return err
	}
	// need to take stuff from SubscriptionCh below and push it to nofifier.Notify
	// launch a goroutine to do this?
	// need to make sure goroutine exits when subscription is cancelled
	// ISSUE: how to cancel goroutine when subscription is cancelled? do we have a map from
	// subscription id to cancel channel?
	// What is the separation of responsibilities between subManager and subAPI?
	//  - maybe subscriptionManager should absorb all the complexity of managing subscriptions??
	go func() {
		for {
			select {
			case res := <-a.subscriptionManager.SubscriptionInfo[subscriberId].SubscriptionCh:
				err := notifier.Notify(rpcSub.ID, res)
				if err != nil {
					fmt.Println("error notifying")
					return
				}
			case <-ctx.Done():
				return
				// TODO: do a case for quitting
			}
		}
	}()
	return nil
}

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
