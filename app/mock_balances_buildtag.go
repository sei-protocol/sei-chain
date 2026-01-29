//go:build mock_balances

package app

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	gigabankkeeper "github.com/sei-protocol/sei-chain/giga/deps/xbank/keeper"
)

// MockBalancesEnabled indicates whether mock_balances build tag is set.
const MockBalancesEnabled = true

// FlushMockedSupplyIfNeeded flushes the mocked supply to the store.
// This is called after OCC completes but before the invariance check.
// With mock_balances, we defer supply updates to avoid OCC conflicts.
func (app *App) FlushMockedSupplyIfNeeded(ctx sdk.Context) {
	if app.GigaExecutorEnabled {
		gigabankkeeper.FlushMockedSupply(ctx, app.BankKeeper.GetStoreKey())
	}
}
