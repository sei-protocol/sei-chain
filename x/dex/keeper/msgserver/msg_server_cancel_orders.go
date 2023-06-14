package msgserver

import (
	"context"
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/sei-protocol/sei-chain/x/dex/utils"
)

func (k msgServer) CancelOrders(goCtx context.Context, msg *types.MsgCancelOrders) (*types.MsgCancelOrdersResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if err := msg.ValidateBasic(); err != nil {
		ctx.Logger().Error(fmt.Sprintf("request invalid: %s", err))
		return nil, err
	}

	events := []sdk.Event{}
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
		pairBlockCancellations := utils.GetMemState(ctx.Context()).GetBlockCancels(ctx, types.ContractAddress(msg.GetContractAddr()), pair)
		if !pairBlockCancellations.Has(cancellation) {
			// only cancel if it's not cancelled in a previous tx in the same block
			cancel := types.Cancellation{
				Id:                cancellation.Id,
				Initiator:         types.CancellationInitiator_USER,
				Creator:           msg.Creator,
				ContractAddr:      msg.ContractAddr,
				Price:             cancellation.Price,
				AssetDenom:        cancellation.AssetDenom,
				PriceDenom:        cancellation.PriceDenom,
				PositionDirection: cancellation.PositionDirection,
			}
			pairBlockCancellations.Add(&cancel)
			events = append(events, sdk.NewEvent(
				types.EventTypeCancelOrder,
				sdk.NewAttribute(types.AttributeKeyCancellationID, fmt.Sprint(cancellation.Id)),
			))
		}
	}
	ctx.EventManager().EmitEvents(events)
	utils.GetMemState(ctx.Context()).SetDownstreamsToProcess(ctx, msg.ContractAddr, k.GetContractWithoutGasCharge)
	return &types.MsgCancelOrdersResponse{}, nil
}
