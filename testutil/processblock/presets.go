package processblock

import (
	"fmt"

	"github.com/CosmWasm/wasmd/x/wasm"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
)

type Preset struct {
	Admin            seitypes.AccAddress // a signable account that's not supposed to run out of tokens
	SignableAccounts []seitypes.AccAddress
	AllAccounts      []seitypes.AccAddress
	AllValidators    []seitypes.ValAddress
}

// 3 unsignable accounts
// 3 bonded validators
func CommonPreset(app *App) *Preset {
	fmt.Printf("Fee collector: %s\n", app.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName).String())
	fmt.Printf("Mint module: %s\n", app.AccountKeeper.GetModuleAddress(minttypes.ModuleName).String())
	fmt.Printf("Distribution module: %s\n", app.AccountKeeper.GetModuleAddress(distrtypes.ModuleName).String())
	fmt.Printf("Staking bonded pool: %s\n", app.AccountKeeper.GetModuleAddress(stakingtypes.BondedPoolName).String())
	fmt.Printf("Staking unbonded pool: %s\n", app.AccountKeeper.GetModuleAddress(stakingtypes.NotBondedPoolName).String())
	fmt.Printf("Wasm module: %s\n", app.AccountKeeper.GetModuleAddress(wasm.ModuleName).String())
	p := &Preset{
		Admin: app.NewSignableAccount("admin"),
	}
	fmt.Printf("Admin: %s\n", p.Admin.String())
	app.FundAccount(p.Admin, 100000000000)
	for i := 0; i < 3; i++ {
		acc := app.NewAccount()
		p.AllAccounts = append(p.AllAccounts, acc)
		fmt.Printf("CommonPreset account: %s\n", acc.String())
	}
	for i := 0; i < 3; i++ {
		val := app.NewValidator()
		app.FundAccount(seitypes.AccAddress(val), 10000000)
		app.NewDelegation(seitypes.AccAddress(val), val, 7000000)
		p.AllAccounts = append(p.AllAccounts, seitypes.AccAddress(val))
		p.AllValidators = append(p.AllValidators, val)
		fmt.Printf("CommonPreset val: %s\n", seitypes.AccAddress(val).String())
	}
	return p
}

// always with enough fee
func (p *Preset) AdminSign(app *App, msgs ...seitypes.Msg) signing.Tx {
	return app.Sign(p.Admin, 10000000, msgs...)
}
