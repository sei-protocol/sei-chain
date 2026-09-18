package core

import (
	"context"
	"errors"

	evmonlyrpc "github.com/sei-protocol/sei-chain/giga/evmonly/rpc"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// ErrNewHeadsUnavailable is returned by SubscribeNewHeads on nodes that do not
// execute blocks through Autobahn.
var ErrNewHeadsUnavailable = errors.New("newHeads subscriptions require Autobahn execution")

// headSubscription yields every block number executed after the subscription
// was created, in order, without skipping.
type headSubscription struct {
	executed utils.AtomicRecv[atypes.GlobalBlockNumber]
	next     atypes.GlobalBlockNumber
}

// Next blocks until block s.next has been executed and returns its height.
func (s *headSubscription) Next(ctx context.Context) (int64, error) {
	n := s.next
	if _, err := s.executed.Wait(ctx, func(executed atypes.GlobalBlockNumber) bool {
		return executed >= n
	}); err != nil {
		return 0, err
	}
	s.next = n + 1
	return utils.Clamp[int64](n), nil
}

// SubscribeNewHeads returns a subscription that starts at the first block
// executed after this call.
func (env *Environment) SubscribeNewHeads(context.Context) (evmonlyrpc.HeadSubscription, error) {
	giga, ok := env.gigaRouter().Get()
	if !ok {
		return nil, ErrNewHeadsUnavailable
	}
	executed := giga.ExecutedHeights()
	return &headSubscription{executed: executed, next: executed.Load() + 1}, nil
}
