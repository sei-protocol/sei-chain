package core

import (
	"context"
	"fmt"

	evmonlyrpc "github.com/sei-protocol/sei-chain/giga/evmonly/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/eventbus"
	tmpubsub "github.com/sei-protocol/sei-chain/sei-tendermint/internal/pubsub"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// headSubscription yields the heights of committed blocks as the event bus
// publishes their NewBlockHeader events.
type headSubscription struct {
	bus      *eventbus.EventBus
	clientID string
	sub      eventbus.Subscription
}

// Next returns the next committed block height, or an error once the
// subscription has been canceled or fell too far behind the event bus.
func (s *headSubscription) Next(ctx context.Context) (int64, error) {
	msg, err := s.sub.Next(ctx)
	if err != nil {
		return 0, err
	}
	header, ok := msg.Data().(types.EventDataNewBlockHeader)
	if !ok {
		return 0, fmt.Errorf("unexpected new-head event payload %T", msg.Data())
	}
	return header.Header.Height, nil
}

// Cancel removes the subscription from the event bus.
func (s *headSubscription) Cancel() {
	_ = s.bus.UnsubscribeAll(context.Background(), s.clientID)
}

// SubscribeNewHeads subscribes clientID to committed block heights. clientID
// must be unique per subscription.
func (env *Environment) SubscribeNewHeads(ctx context.Context, clientID string) (evmonlyrpc.HeadSubscription, error) {
	subCtx, cancel := context.WithTimeout(ctx, SubscribeTimeout)
	defer cancel()
	sub, err := env.EventBus.SubscribeWithArgs(subCtx, tmpubsub.SubscribeArgs{
		ClientID: clientID,
		Query:    types.EventQueryNewBlockHeader,
		Limit:    subBufferSize,
	})
	if err != nil {
		return nil, err
	}
	return &headSubscription{bus: env.EventBus, clientID: clientID, sub: sub}, nil
}
