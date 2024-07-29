package types

import (
	"encoding/json"
	"fmt"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Validate performs basic validation of supply genesis data returning an
// error for any failed validation criteria.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}

	seenMetadatas := make(map[string]bool)
	totalSupply, err := getTotalSupply(&gs)
	if err != nil {
		return err
	}

	for _, metadata := range gs.DenomMetadata {
		if seenMetadatas[metadata.Base] {
			return fmt.Errorf("duplicate client metadata for denom %s", metadata.Base)
		}

		if err := metadata.Validate(); err != nil {
			return err
		}

		seenMetadatas[metadata.Base] = true
	}

	if !gs.Supply.Empty() {
		// NOTE: this errors if supply for any given coin is zero
		err := gs.Supply.Validate()
		if err != nil {
			return err
		}

		if !gs.Supply.IsEqual(totalSupply) {
			return fmt.Errorf("genesis supply is incorrect, expected %v, got %v", gs.Supply, totalSupply)
		}
	}

	return nil
}

func getTotalSupply(genState *GenesisState) (sdk.Coins, error) {
	totalSupply := sdk.Coins{}
	totalWeiBalance := sdk.ZeroInt()

	genState.Balances = SanitizeGenesisBalances(genState.Balances)
	seenBalances := make(map[string]bool)
	for _, balance := range genState.Balances {
		if seenBalances[balance.Address] {
			return nil, fmt.Errorf("duplicate balance for address %s", balance.Address)
		}
		seenBalances[balance.Address] = true
		coins := balance.Coins
		err := balance.Validate()
		if err != nil {
			return nil, err
		}
		totalSupply = totalSupply.Add(coins...)
	}
	for _, weiBalance := range genState.WeiBalances {
		totalWeiBalance = totalWeiBalance.Add(weiBalance.Amount)
	}
	weiInUsei, weiRemainder := SplitUseiWeiAmount(totalWeiBalance)
	if !weiRemainder.IsZero() {
		return nil, fmt.Errorf("non-zero wei remainder %s", weiRemainder)
	}
	baseDenom, err := sdk.GetBaseDenom()
	if err != nil {
		if !weiInUsei.IsZero() {
			return nil, fmt.Errorf("base denom is not registered %s yet there exists wei balance %s", err, weiInUsei)
		}
	} else {
		totalSupply = totalSupply.Add(sdk.NewCoin(baseDenom, weiInUsei))
	}
	return totalSupply, nil
}

var OneUseiInWei sdk.Int = sdk.NewInt(1_000_000_000_000)

func SplitUseiWeiAmount(amt sdk.Int) (sdk.Int, sdk.Int) {
	return amt.Quo(OneUseiInWei), amt.Mod(OneUseiInWei)
}

// NewGenesisState creates a new genesis state.
func NewGenesisState(params Params, balances []Balance, supply sdk.Coins, denomMetaData []Metadata, weiBalances []WeiBalance) *GenesisState {
	return &GenesisState{
		Params:        params,
		Balances:      balances,
		Supply:        supply,
		DenomMetadata: denomMetaData,
		WeiBalances:   weiBalances,
	}
}

// DefaultGenesisState returns a default bank module genesis state.
func DefaultGenesisState() *GenesisState {
	return NewGenesisState(DefaultParams(), []Balance{}, sdk.Coins{}, []Metadata{}, []WeiBalance{})
}

// GetGenesisStateFromAppState returns x/bank GenesisState given raw application
// genesis state.
func GetGenesisStateFromAppState(cdc codec.JSONCodec, appState map[string]json.RawMessage) *GenesisState {
	var genesisState GenesisState

	if appState[ModuleName] != nil {
		cdc.MustUnmarshalJSON(appState[ModuleName], &genesisState)
	}

	return &genesisState
}
