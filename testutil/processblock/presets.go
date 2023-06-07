package processblock

import sdk "github.com/cosmos/cosmos-sdk/types"

// 4 funded accounts, 2 unfunded accounts
// 3 bonded validators
func CommonPreset(app *App) {
	for i := 0; i < 4; i++ {
		acc := app.NewAccount()
		app.FundAccount(acc, 10000000)
	}
	for i := 0; i < 2; i++ {
		app.NewAccount()
	}
	for i := 0; i < 3; i++ {
		val := app.NewValidator()
		app.FundAccount(sdk.AccAddress(val), 10000000)
		app.NewDelegation(sdk.AccAddress(val), val, 7000000)
	}
}
