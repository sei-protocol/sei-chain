package processblock

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (a *App) NewAccount() sdk.AccAddress {
	ctx := a.Ctx()
	address := sdk.AccAddress(GenerateRandomPubKey().Address())
	a.AccountKeeper.SetAccount(ctx, a.AccountKeeper.NewAccountWithAddress(ctx, address))
	return address
}

func (a *App) NewSignableAccount(name string) sdk.AccAddress {
	ctx := a.Ctx()
	address := a.GenerateSignableKey(name)
	a.AccountKeeper.SetAccount(ctx, a.AccountKeeper.NewAccountWithAddress(ctx, address))
	return address
}
