package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/sei-protocol/sei-chain/x/seinet/types"
)

func (k msgServer) ExecutePaywordSettlement(
	goCtx context.Context,
	msg *types.MsgExecutePaywordSettlement,
) (*types.MsgExecutePaywordSettlementResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	executor, err := sdk.AccAddressFromBech32(msg.Executor)
	if err != nil {
		return nil, err
	}

	payee, err := sdk.AccAddressFromBech32(msg.Payee)
	if err != nil {
		return nil, err
	}

	amount, err := sdk.ParseCoinsNormalized(msg.Amount)
	if err != nil {
		return nil, err
	}

	if !amount.IsAllPositive() {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, "amount must be positive")
	}

	covenant, found := k.GetCovenant(ctx, msg.CovenantId)
	if !found {
		return nil, types.ErrCovenantNotFound
	}

	if covenant.Creator != msg.Executor {
		return nil, types.ErrCovenantUnauthorized
	}

	if covenant.Payee != msg.Payee {
		return nil, types.ErrCovenantPayeeMismatch
	}

	if amount.IsAnyGT(covenant.AmountDue) {
		return nil, types.ErrCovenantInsufficientFunds
	}

	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.SeinetVaultAccount, payee, amount); err != nil {
		return nil, err
	}

	remaining := covenant.AmountDue.Sub(amount)
	if remaining.Empty() || remaining.IsZero() {
		k.RemoveCovenant(ctx, msg.CovenantId)
	} else {
		covenant.AmountDue = remaining
		k.SetCovenant(ctx, covenant)
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent("payword_settlement",
			sdk.NewAttribute("executor", executor.String()),
			sdk.NewAttribute("covenant_id", msg.CovenantId),
			sdk.NewAttribute("payee", payee.String()),
			sdk.NewAttribute("amount", amount.String()),
			sdk.NewAttribute("remaining", remaining.String()),
		),
	)

	return &types.MsgExecutePaywordSettlementResponse{}, nil
}
