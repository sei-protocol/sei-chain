package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/sei-protocol/sei-chain/x/oracle/types"
)

// GetOracleAccount returns oracle ModuleAccount
func (k Keeper) GetOracleAccount(ctx sdk.Context) authtypes.ModuleAccountI {
	return k.accountKeeper.GetModuleAccount(ctx, types.ModuleName)
}

// GetRewardPool retrieves the balance of the oracle module account
func (k Keeper) GetRewardPool(ctx sdk.Context, denom string) sdk.Coin {
	acc := k.accountKeeper.GetModuleAccount(ctx, types.ModuleName)
	return k.bankKeeper.GetBalance(ctx, acc.GetAddress(), denom)
}

// GetRewardPool retrieves the balance of the oracle module account
func (k Keeper) GetRewardPoolLegacy(ctx sdk.Context) sdk.Coins {
	acc := k.accountKeeper.GetModuleAccount(ctx, types.ModuleName)
	return k.bankKeeper.GetAllBalances(ctx, acc.GetAddress())
}
