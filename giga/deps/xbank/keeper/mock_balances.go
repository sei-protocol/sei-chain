//go:build mock_balances

package keeper

/*
==============================================================================
============================= !!! WARNING !!! ================================
==============================================================================
== This file is ONLY for TESTING PURPOSES.                                  ==
== It enables MOCK BALANCES for bank accounts.                              ==
== DO NOT USE IN PRODUCTION OR MAINNET BUILDS.                              ==
== This file is included only when the 'mock_balances' build tag is set,    ==
== and overrides the default GetBalance behavior.                           ==
== It is used to mock balances for accounts that don't have any balances    ==
== yet, enabling benchmark/load testing without pre-funding accounts.       ==
==============================================================================
*/

import (
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/giga/deps/xbank/types"
)

// Default mock balance: 1 trillion usei (should be plenty for gas)
const MockBalanceUsei = 1_000_000_000_000

// GetBalance returns the balance of a specific denomination for a given account.
// With mock_balances enabled, it will mint coins if the account has insufficient funds.
// This produces normal side effects (balance + supply updates) that work correctly
// with OCC (Optimistic Concurrency Control) parallel execution.
func (k BaseViewKeeper) GetBalance(ctx sdk.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	// SAFETY: Never allow mock balances on mainnet
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
		return balance
	}

	// Mint mock balance - this updates both balance AND supply as normal side effects.
	// OCC will handle any conflicts by re-executing the transaction.
	k.mintMockBalance(ctx, addr, denom)

	return sdk.NewCoin(denom, sdk.NewInt(MockBalanceUsei))
}

// mintMockBalance performs the actual minting - updating both the account balance
// and the total supply. This uses the same store path (GetKVStore) as regular
// bank operations, so the side effects work correctly with OCC conflict detection.
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

// FlushMockedSupply is a no-op - supply is updated immediately during minting.
func FlushMockedSupply(ctx sdk.Context, storeKey sdk.StoreKey) {
	// No-op
}
