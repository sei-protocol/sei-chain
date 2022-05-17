package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k msgServer) RegisterPair(goCtx context.Context, msg *types.MsgRegisterPair) (*types.MsgRegisterPairResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	for _, pair := range k.GetAllRegisteredPairs(ctx, msg.ContractAddr) {
		if pair == *msg.Pair {
			return &types.MsgRegisterPairResponse{}, nil
		}
	}
	k.AddRegisteredPair(ctx, msg.ContractAddr, *msg.Pair)
	k.Orders[msg.ContractAddr][(*msg.Pair).String()] = dexcache.NewOrders()
	k.OrderPlacements[msg.ContractAddr][(*msg.Pair).String()] = dexcache.NewOrderPlacements()
	k.OrderCancellations[msg.ContractAddr][(*msg.Pair).String()] = dexcache.NewOrderCancellations()

	return &types.MsgRegisterPairResponse{}, nil
}
