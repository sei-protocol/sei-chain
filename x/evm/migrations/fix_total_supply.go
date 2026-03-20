package migrations

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	bankkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("x", "evm", "migrations")

// This migration is to fix total supply mismatch caused by mishandled
// ante surplus
func FixTotalSupply(ctx sdk.Context, k *keeper.Keeper) error {
	balances := k.BankKeeper().GetAccountsBalances(ctx)
	correctSupply := sdk.ZeroInt()
	for _, balance := range balances {
		correctSupply = correctSupply.Add(balance.Coins.AmountOf(sdk.MustGetBaseDenom()))
	}
	totalWeiBalance := sdk.ZeroInt()
	k.BankKeeper().IterateAllWeiBalances(ctx, func(aa sdk.AccAddress, i sdk.Int) bool {
		totalWeiBalance = totalWeiBalance.Add(i)
		return false
	})
	weiInUsei, weiRemainder := bankkeeper.SplitUseiWeiAmount(totalWeiBalance)
	if !weiRemainder.IsZero() {
		logger.Error("wei total supply has been compromised as well; rounding up and adding to reserve")
		if err := k.BankKeeper().AddWei(ctx, k.AccountKeeper().GetModuleAddress(types.ModuleName), bankkeeper.OneUseiInWei.Sub(weiRemainder)); err != nil {
			return err
		}
		weiInUsei = weiInUsei.Add(sdk.OneInt())
	}
	correctSupply = correctSupply.Add(weiInUsei)
	currentSupply := k.BankKeeper().GetSupply(ctx, sdk.MustGetBaseDenom()).Amount
	if !currentSupply.Equal(correctSupply) {
		k.BankKeeper().SetSupply(ctx, sdk.NewCoin(sdk.MustGetBaseDenom(), correctSupply))
	}
	return nil
}
