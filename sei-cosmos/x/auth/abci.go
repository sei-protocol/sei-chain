package auth

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/keeper"
)

// MaxAccountsScannedPerBlock is the number of accounts the legacy vesting account rewrite scans per
// block.
const MaxAccountsScannedPerBlock = 1000

// EndBlocker continues the rewrite of accounts stored under a type of the removed vesting module.
func EndBlocker(ctx sdk.Context, k keeper.AccountKeeper) {
	k.RewriteLegacyVestingAccounts(ctx, MaxAccountsScannedPerBlock)
}
