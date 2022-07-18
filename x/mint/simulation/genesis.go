package simulation

// DONTCOVER

import (
	"encoding/json"
	"fmt"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/sei-protocol/sei-chain/x/mint/types"
)

// RandomizedGenState generates a random GenesisState for mint.
func RandomizedGenState(simState *module.SimulationState) {
	// minter

	mintDenom := sdk.DefaultBondDenom
	epochProvisions := sdk.NewDec(500000) // TODO: Randomize this
	params := types.NewParams(mintDenom, epochProvisions, sdk.NewDecWithPrec(5, 1), 60*24*3*52)

	mintGenesis := types.NewGenesisState(types.InitialMinter(), params, 0)

	bz, err := json.MarshalIndent(&mintGenesis, "", " ")
	if err != nil {
		panic(err)
	}
	// TODO: Do some randomization later
	fmt.Printf("Selected deterministically generated minting parameters:\n%s\n", bz)
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(mintGenesis)
}
