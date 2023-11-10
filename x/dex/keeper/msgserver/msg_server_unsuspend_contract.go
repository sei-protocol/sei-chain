package msgserver

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
)

func (k msgServer) UnsuspendContract(goCtx context.Context, msg *types.MsgUnsuspendContract) (*types.MsgUnsuspendContractResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if err := msg.ValidateBasic(); err != nil {
		ctx.Logger().Error(fmt.Sprintf("request invalid: %s", err))
		return nil, err
	}

	contract, err := k.GetContract(ctx, msg.ContractAddr)
	if err != nil {
		return &types.MsgUnsuspendContractResponse{}, err
	}

	if !contract.Suspended {
		return &types.MsgUnsuspendContractResponse{}, types.ErrContractNotSuspended
	}

	cost := k.GetContractUnsuspendCost(ctx)
	if contract.RentBalance < cost {
		return &types.MsgUnsuspendContractResponse{}, types.ErrInsufficientRent
	}

	contract.Suspended = false
	contract.SuspensionReason = ""
	contract.RentBalance -= cost
	if err := k.SetContract(ctx, &contract); err != nil {
		return &types.MsgUnsuspendContractResponse{}, err
	}

	// suspension changes will also affect dependency traversal since suspended contracts are skipped
	dexutils.GetMemState(ctx.Context()).ClearContractToDependencies(ctx)

	return &types.MsgUnsuspendContractResponse{}, nil
}
