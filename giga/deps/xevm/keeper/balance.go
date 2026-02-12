package keeper

import (
	"math/big"

	"github.com/sei-protocol/sei-chain/giga/deps/xevm/state"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

func (k *Keeper) GetBalance(ctx sdk.Context, addr sdk.AccAddress) *big.Int {
	denom := k.GetBaseDenom(ctx)
	allUsei := k.BankKeeper().GetBalance(ctx, addr, denom).Amount
	lockedUsei := k.BankKeeper().LockedCoins(ctx, addr).AmountOf(denom) // LockedCoins doesn't use iterators
	usei := allUsei.Sub(lockedUsei)
	wei := k.BankKeeper().GetWeiBalance(ctx, addr)
	return usei.Mul(state.SdkUseiToSweiMultiplier).Add(wei).BigInt()
}
