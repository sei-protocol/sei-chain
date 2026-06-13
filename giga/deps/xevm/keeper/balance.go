package keeper

import (
	"math/big"

	"github.com/sei-protocol/sei-chain/giga/deps/xevm/state"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

// GetBalance returns addr's EVM-denominated balance in wei: spendable usei
// (scaled to wei) plus the wei remainder.
//
// Spendable usei is computed as (total − locked) rather than via
// BankKeeper.SpendableCoins, which iterates; LockedCoins does not.
func (k *Keeper) GetBalance(ctx sdk.Context, addr sdk.AccAddress) *big.Int {
	bk := k.BankKeeper()
	denom := k.GetBaseDenom(ctx)

	total := bk.GetBalance(ctx, addr, denom).Amount
	locked := bk.LockedCoins(ctx, addr).AmountOf(denom)
	spendable := sdk.MaxInt(total.Sub(locked), sdk.ZeroInt())

	wei := bk.GetWeiBalance(ctx, addr)
	return spendable.Mul(state.SdkUseiToSweiMultiplier).Add(wei).BigInt()
}
