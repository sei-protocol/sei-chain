//go:build !mock_balances

package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ensureMinimumBalance is a no-op in production builds.
// Returns (zero, false) to indicate normal path should be used.
func (k BaseViewKeeper) ensureMinimumBalance(ctx sdk.Context, addr sdk.AccAddress, denom string) (sdk.Coin, bool) {
	return sdk.Coin{}, false
}
