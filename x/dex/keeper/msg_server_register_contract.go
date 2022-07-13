package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k msgServer) RegisterContract(goCtx context.Context, msg *types.MsgRegisterContract) (*types.MsgRegisterContractResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	for _, contract := range k.GetAllContractInfo(ctx) {
		if msg.Contract.ContractAddr == contract.ContractAddr {
			return &types.MsgRegisterContractResponse{}, nil
		}
	}
	if err := k.SetContract(ctx, msg.Contract); err != nil {
		return &types.MsgRegisterContractResponse{}, err
	}

	return &types.MsgRegisterContractResponse{}, nil
}
