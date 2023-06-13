package processblock

import (
	"fmt"

	"github.com/CosmWasm/wasmd/x/wasm"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/sei-protocol/sei-chain/testutil/processblock/msgs"
	"github.com/sei-protocol/sei-chain/utils"
	dextypes "github.com/sei-protocol/sei-chain/x/dex/types"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
)

type Preset struct {
	Admin            sdk.AccAddress // a signable account that's not supposed to run out of tokens
	SignableAccounts []sdk.AccAddress
	AllAccounts      []sdk.AccAddress
	AllValidators    []sdk.ValAddress
	AllContracts     []sdk.AccAddress
	AllDexMarkets    []*msgs.Market
}

// 3 unsignable accounts
// 3 bonded validators
func CommonPreset(app *App) *Preset {
	fmt.Printf("Fee collector: %s\n", app.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName).String())
	fmt.Printf("Mint module: %s\n", app.AccountKeeper.GetModuleAddress(minttypes.ModuleName).String())
	fmt.Printf("Distribution module: %s\n", app.AccountKeeper.GetModuleAddress(distrtypes.ModuleName).String())
	fmt.Printf("Staking bonded pool: %s\n", app.AccountKeeper.GetModuleAddress(stakingtypes.BondedPoolName).String())
	fmt.Printf("Staking unbonded pool: %s\n", app.AccountKeeper.GetModuleAddress(stakingtypes.NotBondedPoolName).String())
	fmt.Printf("Dex module: %s\n", app.AccountKeeper.GetModuleAddress(dextypes.ModuleName).String())
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
		app.FundAccount(sdk.AccAddress(val), 10000000)
		app.NewDelegation(sdk.AccAddress(val), val, 7000000)
		p.AllAccounts = append(p.AllAccounts, sdk.AccAddress(val))
		p.AllValidators = append(p.AllValidators, val)
		fmt.Printf("CommonPreset val: %s\n", sdk.AccAddress(val).String())
	}
	return p
}

func DexPreset(app *App, numAccounts int, numMarkets int) *Preset {
	p := CommonPreset(app)
	for i := 0; i < numAccounts; i++ {
		acc := app.NewSignableAccount(fmt.Sprintf("DexPreset%d", i))
		app.FundAccount(acc, 10000000)
		p.AllAccounts = append(p.AllAccounts, acc)
		p.SignableAccounts = append(p.SignableAccounts, acc)
		fmt.Printf("DexPreset account: %s\n", acc.String())
	}
	for i := 0; i < numMarkets; i++ {
		contract := app.NewContract(p.Admin, "./mars.wasm")
		market := msgs.NewMarket(contract.String(), "SEI", fmt.Sprintf("ATOM%d", i))
		p.AllContracts = append(p.AllContracts, contract)
		p.AllDexMarkets = append(p.AllDexMarkets, market)
		fmt.Printf("DexPreset contract: %s\n", contract.String())
	}
	return p
}

// always with enough fee
func (p *Preset) AdminSign(app *App, msgs ...sdk.Msg) signing.Tx {
	return app.Sign(p.Admin, 10000000, msgs...)
}

func (p *Preset) DoRegisterMarkets(app *App) {
	block := utils.Map(p.AllDexMarkets, func(m *msgs.Market) signing.Tx {
		return p.AdminSign(app, m.Register(p.Admin, []string{}, 20000000)...)
	})
	for i, code := range app.RunBlock(block) {
		if code != 0 {
			panic(fmt.Sprintf("error code %d when registering the %d-th market", code, i))
		}
	}
}
