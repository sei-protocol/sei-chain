package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/cosmos/cosmos-sdk/x/bank/types"
)

// this denom is only used within the context of import/export
const GenesisWeiDenom = "genesis-wei"

// InitGenesis initializes the bank module's state from a given genesis state.
func (k BaseKeeper) InitGenesis(ctx sdk.Context, genState *types.GenesisState) {
	k.SetParams(ctx, genState.Params)

	totalSupply := sdk.Coins{}
	totalWeiBalance := sdk.ZeroInt()

	genState.Balances = types.SanitizeGenesisBalances(genState.Balances)
	for _, balance := range genState.Balances {
		addr := balance.GetAddress()
		coins := balance.Coins
		if amt := coins.AmountOf(GenesisWeiDenom); !amt.IsZero() {
			if err := k.AddWei(ctx, addr, amt); err != nil {
				panic(fmt.Errorf("error on setting wei %w", err))
			}
			coins = coins.Sub(sdk.NewCoins(sdk.NewCoin(GenesisWeiDenom, amt)))
			totalWeiBalance = totalWeiBalance.Add(amt)
		}

		if err := k.initBalances(ctx, addr, coins); err != nil {
			panic(fmt.Errorf("error on setting balances %w", err))
		}

		totalSupply = totalSupply.Add(coins...)
	}
	weiInUsei, weiRemainder := SplitUseiWeiAmount(totalWeiBalance)
	if !weiRemainder.IsZero() {
		panic(fmt.Errorf("non-zero wei remainder %s", weiRemainder))
	}
	baseDenom, err := sdk.GetBaseDenom()
	if err != nil {
		if !weiInUsei.IsZero() {
			panic(fmt.Errorf("base denom is not registered %s yet there exists wei balance %s", err, weiInUsei))
		}
	} else {
		totalSupply = totalSupply.Add(sdk.NewCoin(baseDenom, weiInUsei))
	}

	if !genState.Supply.Empty() && !genState.Supply.IsEqual(totalSupply) {
		panic(fmt.Errorf("genesis supply is incorrect, expected %v, got %v", genState.Supply, totalSupply))
	}

	for _, supply := range totalSupply {
		k.SetSupply(ctx, supply)
	}

	for _, meta := range genState.DenomMetadata {
		k.SetDenomMetaData(ctx, meta)
	}
}

// ExportGenesis returns the bank module's genesis state.
func (k BaseKeeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	totalSupply, _, err := k.GetPaginatedTotalSupply(ctx, &query.PageRequest{Limit: query.MaxLimit})
	if err != nil {
		panic(fmt.Errorf("unable to fetch total supply %v", err))
	}
	balances := k.GetAccountsBalances(ctx)
	balancesWithWei := make([]types.Balance, len(balances))
	for i, balance := range balances {
		if amt := k.GetWeiBalance(ctx, balance.GetAddress()); !amt.IsZero() {
			balance.Coins = balance.Coins.Add(sdk.NewCoin(GenesisWeiDenom, amt))
		}
		balancesWithWei[i] = balance
	}

	return types.NewGenesisState(
		k.GetParams(ctx),
		balancesWithWei,
		totalSupply,
		k.GetAllDenomMetaData(ctx),
	)
}
