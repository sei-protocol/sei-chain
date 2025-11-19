package processblock

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (a *App) NewAccount() seitypes.AccAddress {
	ctx := a.Ctx()
	address := seitypes.AccAddress(GenerateRandomPubKey().Address())
	a.AccountKeeper.SetAccount(ctx, a.AccountKeeper.NewAccountWithAddress(ctx, address))
	return address
}

func (a *App) NewSignableAccount(name string) seitypes.AccAddress {
	ctx := a.Ctx()
	address := a.GenerateSignableKey(name)
	a.AccountKeeper.SetAccount(ctx, a.AccountKeeper.NewAccountWithAddress(ctx, address))
	return address
}
