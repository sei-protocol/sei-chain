//go:build !mock_balances

package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetBalance returns the balance of a specific denomination for a given account
// by address.
func (k BaseViewKeeper) GetBalance(ctx sdk.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	accountStore := k.getAccountStore(ctx, addr)

	bz := accountStore.Get([]byte(denom))
	if bz == nil {
		return sdk.NewCoin(denom, sdk.ZeroInt())
	}

	var balance sdk.Coin
	k.cdc.MustUnmarshal(bz, &balance)

	return balance
}

// FlushMockedSupply is a no-op when mock_balances is not enabled.
func FlushMockedSupply(ctx sdk.Context, storeKey sdk.StoreKey) {
	// No-op: mock_balances not enabled
}
