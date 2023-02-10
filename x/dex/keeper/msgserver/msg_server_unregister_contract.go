package msgserver

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	appparams "github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/x/dex/types"
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
	// refund remaining rent to the creator
	creatorAddr, _ := sdk.AccAddressFromBech32(contract.Creator)
	if err := k.BankKeeper.SendCoins(ctx, k.AccountKeeper.GetModuleAddress(types.ModuleName), creatorAddr, sdk.NewCoins(sdk.NewCoin(appparams.BaseCoinUnit, sdk.NewInt(int64(contract.RentBalance))))); err != nil {
		return nil, err
	}
	k.DeleteContract(ctx, msg.ContractAddr)
	k.RemoveAllLongBooksForContract(ctx, msg.ContractAddr)
	k.RemoveAllShortBooksForContract(ctx, msg.ContractAddr)
	k.RemoveAllPricesForContract(ctx, msg.ContractAddr)
	k.DeleteMatchResultState(ctx, msg.ContractAddr)
	k.DeleteNextOrderID(ctx, msg.ContractAddr)
	k.DeleteAllRegisteredPairsForContract(ctx, msg.ContractAddr)
	k.RemoveAllTriggeredOrders(ctx, msg.ContractAddr)
	return &types.MsgUnregisterContractResponse{}, nil
}
