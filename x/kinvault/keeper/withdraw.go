package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/sei-protocol/sei-chain/x/kinvault/types"
)

func (k Keeper) WithdrawWithSigil(ctx sdk.Context, msg *types.MsgWithdrawWithSigil) (*sdk.Result, error) {
	if !k.soulsyncKeeper.VerifyKinProof(ctx, msg.Sender, msg.KinProof) {
		return nil, sdkerrors.Wrap(sdkerrors.ErrUnauthorized, "invalid kin proof")
	}

	if !k.holoKeeper.CheckPresence(ctx, msg.Sender, msg.HoloPresence) {
		return nil, sdkerrors.Wrap(sdkerrors.ErrUnauthorized, "holo presence invalid")
	}

	k.vaultKeeper.Withdraw(ctx, msg.VaultId, msg.Sender)

	return &sdk.Result{Events: ctx.EventManager().ABCIEvents()}, nil
}
