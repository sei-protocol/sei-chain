package migrations

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	dextypes "github.com/sei-protocol/sei-chain/x/dex/types"
)

func V17ToV18(ctx sdk.Context, dexkeeper keeper.Keeper) error {
	// iterate over all contracts and unregister them
	for _, c := range dexkeeper.GetAllContractInfo(ctx) {
		if err := dexkeeper.DoUnregisterContractWithRefund(ctx, c); err != nil {
			return err
		}
	}
	// get module address
	dexAddr := dexkeeper.AccountKeeper.GetModuleAddress(dextypes.ModuleName)
	// send usei to the feecollector
	useiCoins := dexkeeper.BankKeeper.GetBalance(ctx, dexAddr, sdk.MustGetBaseDenom())
	if err := dexkeeper.BankKeeper.SendCoinsFromModuleToModule(ctx, dextypes.ModuleName, authtypes.FeeCollectorName, sdk.NewCoins(useiCoins)); err != nil {
		return err
	}
	// get bank balances remaining for module
	balances := dexkeeper.BankKeeper.GetAllBalances(ctx, dexAddr)
	// update accountkeeper to give dex burner perms
	dexkeeper.CreateModuleAccount(ctx)
	// burn all remaining module balances - need burner perms
	if err := dexkeeper.BankKeeper.BurnCoins(ctx, dextypes.ModuleName, balances); err != nil {
		return err
	}
	return nil
}
