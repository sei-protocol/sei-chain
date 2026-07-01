package keeper

import (
	"math/big"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/x/evm/state"
)

// GetBalance returns the spendable EVM balance (in wei) of addr, excluding any
// locked usei (e.g. from vesting accounts).
func (k *Keeper) GetBalance(ctx sdk.Context, addr sdk.AccAddress) *big.Int {
	bk := k.BankKeeper()
	denom := k.GetBaseDenom(ctx)

	// LockedCoins doesn't use iterators, so this stays cheap.
	totalUsei := bk.GetBalance(ctx, addr, denom).Amount
	lockedUsei := bk.LockedCoins(ctx, addr).AmountOf(denom)

	spendableUsei := totalUsei.Sub(lockedUsei)
	if spendableUsei.IsNegative() {
		spendableUsei = sdk.ZeroInt()
	}

	wei := bk.GetWeiBalance(ctx, addr)
	return spendableUsei.Mul(state.SdkUseiToSweiMultiplier).Add(wei).BigInt()
}
