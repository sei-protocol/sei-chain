//go:build mock_balances

package keeper

/*
==============================================================================
============================= !!! WARNING !!! ================================
==============================================================================
== This file is ONLY for TESTING/BENCHMARKING.                              ==
== It enables automatic top-off of bank accounts with insufficient funds.   ==
== DO NOT USE IN PRODUCTION OR MAINNET BUILDS.                              ==
== This is enabled only when the 'mock_balances' build tag is set.          ==
==============================================================================
*/

import (
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/giga/deps/xbank/types"
)

// MockBalanceUsei is the amount to set when an account needs funds (1 trillion usei)
const MockBalanceUsei = 1_000_000_000_000

// ensureMinimumBalance checks if the account needs topping off.
// Returns (coin, true) if it handled the request, (zero, false) to use normal path.
func (k BaseViewKeeper) ensureMinimumBalance(ctx sdk.Context, addr sdk.AccAddress, denom string) (sdk.Coin, bool) {
	if ctx.ChainID() == "pacific-1" {
		panic("FATAL: mock_balances build tag enabled on pacific-1 mainnet - this is a critical misconfiguration")
	}

	// Read the actual balance from the store
	accountStore := k.getAccountStore(ctx, addr)
	bz := accountStore.Get([]byte(denom))

	var balance sdk.Coin
	if bz != nil {
		k.cdc.MustUnmarshal(bz, &balance)
	} else {
		balance = sdk.NewCoin(denom, sdk.ZeroInt())
	}

	// If balance is sufficient or this isn't the base denom, return as-is
	if denom != sdk.MustGetBaseDenom() || balance.Amount.GTE(sdk.NewInt(1_000_000)) {
		return balance, true
	}

	// Mint mock balance
	k.mintMockBalance(ctx, addr, denom)

	return sdk.NewCoin(denom, sdk.NewInt(MockBalanceUsei)), true
}

// mintMockBalance performs the actual minting - updating both the account balance
// and the total supply.
func (k BaseViewKeeper) mintMockBalance(ctx sdk.Context, addr sdk.AccAddress, denom string) {
	mintAmount := sdk.NewInt(MockBalanceUsei)

	// Create account if needed
	if !k.ak.HasAccount(ctx, addr) {
		k.ak.SetAccount(ctx, k.ak.NewAccountWithAddress(ctx, addr))
	}

	// 1. Update account balance
	balance := sdk.NewCoin(denom, mintAmount)
	bz, err := k.cdc.Marshal(&balance)
	if err != nil {
		return
	}
	accountStore := k.getAccountStore(ctx, addr)
	accountStore.Set([]byte(denom), bz)

	// 2. Update total supply
	store := k.GetKVStore(ctx)
	supplyStore := prefix.NewStore(store, types.SupplyKey)

	var currentSupply sdk.Int
	supplyBz := supplyStore.Get([]byte(denom))
	if supplyBz != nil {
		if err := currentSupply.Unmarshal(supplyBz); err != nil {
			return
		}
	} else {
		currentSupply = sdk.ZeroInt()
	}

	newSupply := currentSupply.Add(mintAmount)
	intBytes, err := newSupply.Marshal()
	if err != nil {
		return
	}
	supplyStore.Set([]byte(denom), intBytes)
}
