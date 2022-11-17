package msgserver

import (
	"context"
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	typesutils "github.com/sei-protocol/sei-chain/x/dex/types/utils"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
)

func (k msgServer) CancelOrders(goCtx context.Context, msg *types.MsgCancelOrders) (*types.MsgCancelOrdersResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// validate cancellation requests
	if err := k.validateCancels(msg); err != nil {
		return nil, err
	}

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
		}
	}

	return &types.MsgCancelOrdersResponse{}, nil
}

func (k msgServer) validateCancels(cancels *types.MsgCancelOrders) error {
	if len(cancels.Creator) == 0 {
		return fmt.Errorf("invalid cancellation, creator cannot be empty")
	}
	if len(cancels.ContractAddr) == 0 {
		return fmt.Errorf("invalid cancellation, contract address cannot be empty")
	}

	for _, cancellation := range cancels.GetCancellations() {
		if cancellation.Price.IsNil() {
			return fmt.Errorf("invalid cancellation price: %s", cancellation.Price)
		}
		if len(cancellation.AssetDenom) == 0 {
			return fmt.Errorf("invalid cancellation, asset denom is empty")
		}
		if len(cancellation.PriceDenom) == 0 {
			return fmt.Errorf("invalid cancellation, price denom is empty")
		}
	}

	return nil
}
