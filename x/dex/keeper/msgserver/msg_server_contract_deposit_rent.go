package msgserver

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	appparams "github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k msgServer) ContractDepositRent(goCtx context.Context, msg *types.MsgContractDepositRent) (*types.MsgContractDepositRentResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if err := msg.ValidateBasic(); err != nil {
		ctx.Logger().Error(fmt.Sprintf("request invalid: %s", err))
		return nil, err
	}

	// first check if the deposit itself exceeds the limit
	if err := k.ValidateRentBalance(msg.GetAmount()); err != nil {
		return nil, err
	}

	contract, err := k.GetContract(ctx, msg.ContractAddr)
	if err != nil {
		return nil, err
	}
	// check if the balance post deposit exceeds the limit.
	// not checking the sum because it might overflow.
	if k.maxAllowedRentBalance()-msg.GetAmount() < contract.RentBalance {
		return nil, fmt.Errorf("rent balance %d will exceed the limit of %d after depositing %d", contract.RentBalance, k.maxAllowedRentBalance(), msg.GetAmount())
	}
	if contract.RentBalance > 0 && contract.Creator != msg.Sender {
		// a sender can only "claim" the contract if the rent balance is 0
		return nil, sdkerrors.ErrUnauthorized
	}
	// deposit
	creatorAddr, err := sdk.AccAddressFromBech32(contract.Creator)
	if err != nil {
		return nil, err
	}
	if err := k.BankKeeper.SendCoins(ctx, creatorAddr, k.AccountKeeper.GetModuleAddress(types.ModuleName), sdk.NewCoins(sdk.NewCoin(appparams.BaseCoinUnit, sdk.NewInt(int64(msg.Amount))))); err != nil {
		return nil, err
	}
	contract.Creator = msg.Sender
	contract.RentBalance += msg.Amount
	if err := k.SetContract(ctx, &contract); err != nil {
		return nil, err
	}
	return &types.MsgContractDepositRentResponse{}, nil
}
