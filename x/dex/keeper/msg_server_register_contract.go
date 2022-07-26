package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/contract"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k msgServer) RegisterContract(goCtx context.Context, msg *types.MsgRegisterContract) (*types.MsgRegisterContractResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	// TODO: add validation such that only the user who stored the code can register contract

	// always override contract info so that it can be updated
	if err := k.SetContract(ctx, msg.Contract); err != nil {
		return &types.MsgRegisterContractResponse{}, err
	}

	if _, err := contract.TopologicalSortContractInfo(k.GetAllContractInfo(ctx)); err != nil {
		return &types.MsgRegisterContractResponse{}, err
	}

	return &types.MsgRegisterContractResponse{}, nil
}
