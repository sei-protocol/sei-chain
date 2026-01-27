//go:build !mock_balances

package app

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// FlushMockedSupplyIfNeeded is a no-op when mock_balances is not enabled.
func (app *App) FlushMockedSupplyIfNeeded(ctx sdk.Context) {
	// No-op: mock_balances not enabled
}
