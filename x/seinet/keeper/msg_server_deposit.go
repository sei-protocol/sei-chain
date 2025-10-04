package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/x/seinet/types"
)

func (k msgServer) DepositToVault(
	goCtx context.Context,
	msg *types.MsgDepositToVault,
) (*types.MsgDepositToVaultResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	depositor, err := sdk.AccAddressFromBech32(msg.Depositor)
	if err != nil {
		return nil, err
	}

	amount, err := sdk.ParseCoinsNormalized(msg.Amount)
	if err != nil {
		return nil, err
	}

	if err := k.bankKeeper.SendCoinsFromAccountToModule(
		ctx,
		depositor,
		types.SeinetVaultAccount,
		amount,
	); err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent("vault_deposit",
			sdk.NewAttribute("depositor", depositor.String()),
			sdk.NewAttribute("amount", amount.String()),
		),
	)

	return &types.MsgDepositToVaultResponse{}, nil
}
