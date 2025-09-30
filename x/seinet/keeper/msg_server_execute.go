package keeper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/sei-protocol/sei-chain/x/seinet/types"
)

func (k msgServer) ExecutePaywordSettlement(
	goCtx context.Context,
	msg *types.MsgExecutePaywordSettlement,
) (*types.MsgExecutePaywordSettlementResponse, error) {
	if msg == nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "message cannot be nil")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	executor, err := sdk.AccAddressFromBech32(msg.Executor)
	if err != nil {
		return nil, err
	}

	recipient, err := sdk.AccAddressFromBech32(msg.Recipient)
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

	normalizedHash, err := types.NormalizeHexHash(msg.CovenantHash)
	if err != nil {
		return nil, sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid covenant hash: %s", err)
	}

	hashed := sha256.Sum256([]byte(msg.Payword))
	if hex.EncodeToString(hashed[:]) != normalizedHash {
		return nil, sdkerrors.Wrap(sdkerrors.ErrUnauthorized, "payword does not match covenant hash")
	}

	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.SeinetVaultAccount, recipient, amount); err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"payword_settlement",
			sdk.NewAttribute("executor", executor.String()),
			sdk.NewAttribute("recipient", recipient.String()),
			sdk.NewAttribute("amount", amount.String()),
			sdk.NewAttribute("covenant_hash", normalizedHash),
		),
	)

	return &types.MsgExecutePaywordSettlementResponse{}, nil
}
