package keeper

import (
	"math/big"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/x/evm/state"
)

func (k *Keeper) GetBalance(ctx sdk.Context, addr sdk.AccAddress) *big.Int {
	if balance, ok := ctx.GetCachedEVMBalance(addr); ok {
		return balance
	}
	denom := k.GetBaseDenom(ctx)
	allUsei := k.BankKeeper().GetBalance(ctx, addr, denom).Amount
	lockedUsei := k.BankKeeper().LockedCoins(ctx, addr).AmountOf(denom) // LockedCoins doesn't use iterators
	usei := allUsei.Sub(lockedUsei)
	wei := k.BankKeeper().GetWeiBalance(ctx, addr)
	balance := usei.Mul(state.SdkUseiToSweiMultiplier).Add(wei).BigInt()
	ctx.SetCachedEVMBalance(addr, balance)
	return balance
}
