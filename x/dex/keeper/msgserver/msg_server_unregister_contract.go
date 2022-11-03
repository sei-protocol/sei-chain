package msgserver

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	appparams "github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k msgServer) UnregisterContract(goCtx context.Context, msg *types.MsgUnregisterContract) (*types.MsgUnregisterContractResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	contract, err := k.GetContract(ctx, msg.ContractAddr)
	if err != nil {
		return nil, err
	}
	if contract.Creator != msg.Creator {
		return nil, sdkerrors.ErrUnauthorized
	}
	// refund remaining rent to the creator
	creatorAddr, err := sdk.AccAddressFromBech32(contract.Creator)
	if err != nil {
		return nil, err
	}
	if err := k.BankKeeper.SendCoins(ctx, k.AccountKeeper.GetModuleAddress(types.ModuleName), creatorAddr, sdk.NewCoins(sdk.NewCoin(appparams.BaseCoinUnit, sdk.NewInt(int64(contract.RentBalance))))); err != nil {
		return nil, err
	}
	k.DeleteContract(ctx, msg.ContractAddr)
	return &types.MsgUnregisterContractResponse{}, nil
}
