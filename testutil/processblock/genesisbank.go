package processblock

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
)

func (a *App) FundAccount(acc sdk.AccAddress, amount int64) {
	a.FundAccountWithDenom(acc, amount, "usei")
}

func (a *App) FundModule(moduleName string, amount int64) {
	a.FundModuleWithDenom(moduleName, amount, "usei")
}

func (a *App) FundAccountWithDenom(acc sdk.AccAddress, amount int64, denom string) {
	ctx := a.Ctx()
	amounts := sdk.NewCoins(sdk.NewCoin(denom, sdk.NewInt(amount)))
	if err := a.BankKeeper.MintCoins(ctx, minttypes.ModuleName, amounts); err != nil {
		panic(err)
	}
	if err := a.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, acc, amounts); err != nil {
		panic(err)
	}
	m, b := bankkeeper.TotalSupply(a.BankKeeper)(ctx)
	if b {
		panic(m)
	}
}

func (a *App) FundModuleWithDenom(moduleName string, amount int64, denom string) {
	ctx := a.Ctx()
	amounts := sdk.NewCoins(sdk.NewCoin(denom, sdk.NewInt(amount)))
	if err := a.BankKeeper.MintCoins(ctx, minttypes.ModuleName, amounts); err != nil {
		panic(err)
	}
	if err := a.BankKeeper.SendCoinsFromModuleToModule(ctx, minttypes.ModuleName, moduleName, amounts); err != nil {
		panic(err)
	}
	m, b := bankkeeper.TotalSupply(a.BankKeeper)(ctx)
	if b {
		panic(m)
	}
}
