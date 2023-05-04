package simulation

// DONTCOVER

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/sei-protocol/sei-chain/x/mint/types"
)

// RandomizedGenState generates a random GenesisState for mint.
func RandomizedGenState(simState *module.SimulationState) {
	mintDenom := sdk.DefaultBondDenom
	randomProvision := uint64(rand.Int63n(1000000))
	currentDate := time.Now()
	// Epochs are every minute, set reduction period to be 1 year
	tokenReleaseSchedule := []types.ScheduledTokenRelease{}

	for i := 1; i <= 10; i++ {
		scheduledTokenRelease := types.ScheduledTokenRelease{
			StartDate:          currentDate.AddDate(1, 0, 0).Format(types.TokenReleaseDateFormat),
			EndDate:            currentDate.AddDate(3, 0, 0).Format(types.TokenReleaseDateFormat),
			TokenReleaseAmount: randomProvision / uint64(i),
		}
		tokenReleaseSchedule = append(tokenReleaseSchedule, scheduledTokenRelease)
	}

	params := types.NewParams(mintDenom, tokenReleaseSchedule)

	mintGenesis := types.NewGenesisState(types.InitialMinter(), params)

	bz, err := json.MarshalIndent(&mintGenesis, "", " ")
	if err != nil {
		panic(err)
	}
	// TODO: Do some randomization later
	fmt.Printf("Selected deterministically generated minting parameters:\n%s\n", bz)
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(mintGenesis)
}
