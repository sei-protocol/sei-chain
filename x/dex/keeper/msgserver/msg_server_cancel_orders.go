package msgserver

import (
	"context"
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	typesutils "github.com/sei-protocol/sei-chain/x/dex/types/utils"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
)

func (k msgServer) CancelOrders(goCtx context.Context, msg *types.MsgCancelOrders) (*types.MsgCancelOrdersResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	for _, cancellation := range msg.GetCancellations() {
		var allocation *types.Allocation
		var found bool
		if cancellation.PositionDirection == types.PositionDirection_LONG {
			allocation, found = k.GetLongAllocationForOrderID(ctx, msg.ContractAddr, cancellation.PriceDenom, cancellation.AssetDenom, cancellation.Price, cancellation.Id)
		} else {
			allocation, found = k.GetShortAllocationForOrderID(ctx, msg.ContractAddr, cancellation.PriceDenom, cancellation.AssetDenom, cancellation.Price, cancellation.Id)
		}
		if !found {
			continue
		}
		if allocation.Account != msg.Creator {
			return nil, errors.New("cannot cancel orders created by others")
		}
		pair := types.Pair{PriceDenom: cancellation.PriceDenom, AssetDenom: cancellation.AssetDenom}
		pairStr := typesutils.GetPairString(&pair)
		pairBlockCancellations := dexutils.GetMemState(ctx.Context()).GetBlockCancels(ctx, typesutils.ContractAddress(msg.GetContractAddr()), pairStr)
		cancelledInCurrentBlock := false
		for _, cancelInCurrentBlock := range pairBlockCancellations.Get() {
			if cancelInCurrentBlock.Id == cancellation.Id {
				cancelledInCurrentBlock = true
				break
			}
		}
		if !cancelledInCurrentBlock {
			// only cancel if it's not cancelled in a previous tx in the same block
			cancel := types.Cancellation{
				Id:        cancellation.Id,
				Initiator: types.CancellationInitiator_USER,
				Creator:   msg.Creator,
			}
			pairBlockCancellations.Add(&cancel)
		}
	}

	return &types.MsgCancelOrdersResponse{}, nil
}
