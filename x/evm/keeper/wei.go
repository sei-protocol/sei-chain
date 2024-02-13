package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/sei-protocol/sei-chain/x/evm/state"
)

func (k *Keeper) SettleWeiEscrowAccounts(ctx sdk.Context, evmTxDeferredInfoList []EvmTxDeferredInfo) {
	denom := k.GetBaseDenom(ctx)
	// settle surplus escrow first
	for _, info := range evmTxDeferredInfoList {
		escrow := state.GetTempWeiEscrowAddress(info.TxIndx)
		seiBalance := k.BankKeeper().GetBalance(ctx, escrow, denom)
		if !seiBalance.Amount.IsPositive() {
			continue
		}
		if err := k.BankKeeper().SendCoinsFromAccountToModule(ctx, escrow, banktypes.WeiEscrowName, sdk.NewCoins(seiBalance)); err != nil {
			ctx.Logger().Error(fmt.Sprintf("failed to send %s from escrow %d to global escrow", seiBalance.String(), info.TxIndx))
			// This should not happen in any case. We want to halt the chain if it does
			panic(err)
		}
	}
	// settle deficit escrows
	for _, info := range evmTxDeferredInfoList {
		escrow := state.GetTempWeiEscrowAddress(info.TxIndx)
		seiBalance := k.BankKeeper().GetBalance(ctx, escrow, denom)
		if !seiBalance.Amount.IsNegative() {
			continue
		}
		settleAmt := sdk.NewCoin(seiBalance.Denom, seiBalance.Amount.Neg())
		if err := k.BankKeeper().SendCoinsFromModuleToAccount(ctx, banktypes.WeiEscrowName, escrow, sdk.NewCoins(settleAmt)); err != nil {
			ctx.Logger().Error(fmt.Sprintf("failed to send %s from global escrow to escrow %d", settleAmt.String(), info.TxIndx))
			// This should not happen in any case. We want to halt the chain if it does
			panic(err)
		}
	}
	// sanity check
	for _, info := range evmTxDeferredInfoList {
		escrow := state.GetTempWeiEscrowAddress(info.TxIndx)
		seiBalance := k.BankKeeper().GetBalance(ctx, escrow, denom)
		if !seiBalance.Amount.IsZero() {
			panic(fmt.Sprintf("failed to settle escrow account %d which still has a balance of %s", info.TxIndx, seiBalance.String()))
		}
	}
}
