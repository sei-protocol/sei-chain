package auth

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/keeper"
)

// MaxAccountsScannedPerBlock is the number of accounts EndBlocker scans for the legacy vesting
// account rewrite.
const MaxAccountsScannedPerBlock = 1000

// EndBlocker rewrites the next batch of accounts stored under a type of the removed vesting module.
func EndBlocker(ctx sdk.Context, k keeper.AccountKeeper) {
	k.RewriteLegacyVestingAccounts(ctx, MaxAccountsScannedPerBlock)
}
