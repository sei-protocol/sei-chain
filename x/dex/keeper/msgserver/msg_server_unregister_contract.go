package msgserver

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
)

func (k msgServer) UnregisterContract(goCtx context.Context, msg *types.MsgUnregisterContract) (*types.MsgUnregisterContractResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if err := msg.ValidateBasic(); err != nil {
		ctx.Logger().Error(fmt.Sprintf("request invalid: %s", err))
		return nil, err
	}

	contract, err := k.GetContract(ctx, msg.ContractAddr)
	if err != nil {
		return nil, err
	}
	if contract.Creator != msg.Creator {
		return nil, sdkerrors.ErrUnauthorized
	}
	if err := k.DoUnregisterContractWithRefund(ctx, contract); err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeUnregisterContract,
		sdk.NewAttribute(types.AttributeKeyContractAddress, msg.ContractAddr),
	))

	dexutils.GetMemState(ctx.Context()).ClearContractToDependencies(ctx)
	return &types.MsgUnregisterContractResponse{}, nil
}
